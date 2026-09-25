package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"sys-backend/config"
	"sys-backend/db"

	openapiutil "github.com/alibabacloud-go/darabonba-openapi/v2/utils"
	esa "github.com/alibabacloud-go/esa-20240910/v3/client"
	"github.com/sirupsen/logrus"
)

// TenantInfo 是系统端展示的一条租户 DNS 记录。
type TenantInfo struct {
	RecordID  string
	Subdomain string
	Namespace string
	Target    string
	Type      string
	Status    string // "normal", "orphan", "abnormal"
}

// esaClient 构造 ESA OpenAPI 客户端。
//
// 凭据走最小权限的 RAM 身份（只授予 esa 记录读写），不复用主账号 AK。
func esaClient() (*esa.Client, error) {
	cfg := config.Configs.ESA
	if cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" {
		return nil, fmt.Errorf("ESA API 凭据未配置")
	}
	if cfg.SiteID == 0 {
		return nil, fmt.Errorf("ESA 站点 ID（esa.site_id）未配置")
	}

	client, err := esa.NewClient(&openapiutil.Config{
		AccessKeyId:     &cfg.AccessKeyID,
		AccessKeySecret: &cfg.AccessKeySecret,
		Endpoint:        &cfg.Endpoint,
	})
	if err != nil {
		return nil, fmt.Errorf("初始化 ESA 客户端失败: %w", err)
	}
	return client, nil
}

// listCNAMERecords 分页拉取站点内的全部 CNAME 记录。
//
// 有 TotalCount 时按总数判断是否还有下一页，缺失时退回「本页不足一页即结束」。
func listCNAMERecords(ctx context.Context, client *esa.Client) ([]*esa.ListRecordsResponseBodyRecords, error) {
	const pageSize = 100

	all := make([]*esa.ListRecordsResponseBodyRecords, 0, pageSize)
	for page := 1; ; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		resp, err := client.ListRecords((&esa.ListRecordsRequest{}).
			SetSiteId(config.Configs.ESA.SiteID).
			SetType("CNAME").
			SetPageNumber(int32(page)).
			SetPageSize(int32(pageSize)))
		if err != nil {
			return nil, fmt.Errorf("查询 ESA 记录失败: %w", err)
		}
		if resp == nil || resp.Body == nil {
			// 空响应不是「没有下一页」：把它当成功会让租户列表凭空清空。
			return nil, fmt.Errorf("ESA 返回空响应（第 %d 页）", page)
		}

		records := resp.Body.Records
		all = append(all, records...)

		if len(records) == 0 || len(records) < pageSize {
			return all, nil
		}
		if resp.Body.TotalCount != nil && len(all) >= int(*resp.Body.TotalCount) {
			return all, nil
		}
	}
}

// FetchSaaSSubdomains 返回站点内的租户 DNS 记录。
//
// 租户记录有两条判据，满足任意一条即算：
//   - 备注里带配置的租户标记（本服务与注册服务新建记录时都会写入）；
//   - 子域名在 Astra 数据库里有对应 namespace（迁移前的历史记录没有备注，靠这条兜底）。
//
// 站点内同时存在 class/to/sys 这类基础设施记录，两条都不满足时会被忽略，
// 不会在租户列表里冒出来。
func FetchSaaSSubdomains() ([]TenantInfo, error) {
	cfg := config.Configs.ESA
	if cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" || cfg.SiteID == 0 {
		return nil, fmt.Errorf("ESA API 凭据未配置")
	}

	client, err := esaClient()
	if err != nil {
		return nil, err
	}

	records, err := listCNAMERecords(context.Background(), client)
	if err != nil {
		logrus.Errorf("[ESA] 获取 DNS 记录失败: %v", err)
		return nil, err
	}
	logrus.Infof("[ESA] 获取到 %d 条 CNAME 记录", len(records))

	namespaces := scanNamespaces()

	tenants := make([]TenantInfo, 0, len(records))
	for _, r := range records {
		if r == nil {
			continue
		}

		subdomain, ok := subdomainOf(deref(r.RecordName))
		if !ok {
			continue
		}
		if !isTenantRecord(deref(r.Comment), subdomain, namespaces, cfg.TenantComment) {
			continue
		}

		logrus.Infof("[ESA] 匹配租户子域名: %s -> namespace=%s", subdomain, subdomainToNamespace(subdomain))
		tenants = append(tenants, TenantInfo{
			RecordID:  strconv.FormatInt(derefInt64(r.RecordId), 10),
			Subdomain: subdomain,
			Namespace: subdomainToNamespace(subdomain),
			Target:    recordValue(r),
			Type:      "CNAME",
			Status:    "normal",
		})
	}

	logrus.Infof("[ESA] 最终匹配 %d 个租户子域名", len(tenants))
	return tenants, nil
}

// scanNamespaces 返回 Astra 数据库里出现过的全部 namespace。
func scanNamespaces() map[string]bool {
	var rows []struct {
		Namespace string `gorm:"column:namespace"`
	}
	db.GetDB().Table("users").Select("DISTINCT namespace").Scan(&rows)

	namespaces := make(map[string]bool, len(rows))
	for _, r := range rows {
		if r.Namespace != "" {
			namespaces[r.Namespace] = true
		}
	}
	return namespaces
}

// isTenantRecord 判断一条记录是否属于租户记录。
//
// marker 为空表示没配置标记，此时只靠 namespace 兜底判断。
func isTenantRecord(comment, subdomain string, namespaces map[string]bool, marker string) bool {
	if hasTenantMarker(comment, marker) {
		return true
	}
	return namespaces[subdomainToNamespace(subdomain)]
}

// hasTenantMarker 判断备注是否以租户标记开头。
//
// 只认「标记出现在开头」：备注等于标记，或标记之后紧跟非字母数字字符（如「SaaS 租户」）。
// 不用子串匹配，是因为 "non-SaaS" 这类反向说明会被误判成租户标记；
// 注册服务（reg-to）用同一规则写入，两边必须保持一致。
func hasTenantMarker(comment, marker string) bool {
	if marker == "" {
		return false
	}
	trimmed := strings.TrimSpace(comment)
	if len(trimmed) < len(marker) {
		return false
	}
	if !strings.EqualFold(trimmed[:len(marker)], marker) {
		return false
	}
	if len(trimmed) == len(marker) {
		return true
	}
	next, _ := utf8.DecodeRuneInString(trimmed[len(marker):])
	return !unicode.IsLetter(next) && !unicode.IsDigit(next)
}

// subdomainOf 从完整记录名反推子域名；不在站点域名空间内时返回 false。
func subdomainOf(recordName string) (string, bool) {
	site := config.Configs.ESA.SiteName
	if site == "" {
		return "", false
	}
	suffix := "." + site
	if !strings.HasSuffix(strings.ToLower(recordName), strings.ToLower(suffix)) {
		return "", false
	}
	subdomain := recordName[:len(recordName)-len(suffix)]
	if subdomain == "" {
		return "", false
	}
	return subdomain, true
}

func recordValue(record *esa.ListRecordsResponseBodyRecords) string {
	if record == nil || record.Data == nil {
		return ""
	}
	return deref(record.Data.Value)
}

// validateSubdomain 校验 DNS 子域标签合法性（RFC 1123 标签规则），
// 防止 DNS 记录注入或通配符劫持（如创建 *.getastra.cn 的 CNAME）
func validateSubdomain(s string) error {
	if len(s) < 1 || len(s) > 63 {
		return fmt.Errorf("subdomain 长度必须在 1~63 字符之间")
	}
	if s[0] == '-' || s[len(s)-1] == '-' {
		return fmt.Errorf("subdomain 不能以连字符开头或结尾")
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return fmt.Errorf("subdomain 仅允许小写字母、数字和连字符")
		}
	}
	return nil
}

// CreateTenant 在 ESA 站点内创建租户的 CNAME 记录。
func CreateTenant(subdomain string) (*TenantInfo, error) {
	if err := validateSubdomain(subdomain); err != nil {
		return nil, fmt.Errorf("非法 subdomain: %w", err)
	}

	cfg := config.Configs.ESA
	if cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" || cfg.SiteID == 0 {
		return nil, fmt.Errorf("ESA API 凭据未配置")
	}
	if cfg.Target == "" {
		return nil, fmt.Errorf("ESA 回源目标（esa.target）未配置")
	}

	client, err := esaClient()
	if err != nil {
		return nil, err
	}

	name := fullDomain(subdomain)
	resp, err := client.CreateRecord((&esa.CreateRecordRequest{}).
		SetSiteId(cfg.SiteID).
		SetRecordName(name).
		SetType("CNAME").
		SetData((&esa.CreateRecordRequestData{}).SetValue(cfg.Target)).
		SetTtl(cfg.TTL).
		SetProxied(cfg.Proxied).
		SetBizName(cfg.BizName).
		SetSourceType(cfg.SourceType).
		SetComment(cfg.TenantComment))
	if err != nil {
		return nil, fmt.Errorf("创建 DNS 记录失败: %v", err)
	}

	recordID := ""
	if resp != nil && resp.Body != nil && resp.Body.RecordId != nil {
		recordID = strconv.FormatInt(*resp.Body.RecordId, 10)
	}
	logrus.Infof("[ESA] 已创建租户记录: %s -> %s (id=%s)", name, cfg.Target, recordID)

	return &TenantInfo{
		RecordID:  recordID,
		Subdomain: subdomain,
		Namespace: subdomainToNamespace(subdomain),
		Target:    cfg.Target,
		Type:      "CNAME",
		Status:    "normal",
	}, nil
}

// DeleteTenant 删除 DNS 记录并清除数据库中该 namespace 的所有数据
func DeleteTenant(recordID, namespace string) error {
	if err := deleteDNSRecord(recordID); err != nil {
		return err
	}
	if err := DeleteNamespaceData(namespace); err != nil {
		logrus.Warnf("DNS 已删除但数据库清理失败: %v", err)
	}
	return nil
}

// BanTenant 封禁租户：仅删除 DNS 记录，保留数据库数据
func BanTenant(recordID string) error {
	return deleteDNSRecord(recordID)
}

// CleanupTenant 清理残留：仅删除数据库中该 namespace 的数据（用于异常租户）
func CleanupTenant(namespace string) error {
	return DeleteNamespaceData(namespace)
}

func deleteDNSRecord(recordID string) error {
	id, err := strconv.ParseInt(strings.TrimSpace(recordID), 10, 64)
	if err != nil {
		return fmt.Errorf("DNS 记录 ID 非法: %q", recordID)
	}

	client, err := esaClient()
	if err != nil {
		return err
	}

	if _, err := client.DeleteRecord((&esa.DeleteRecordRequest{}).SetRecordId(id)); err != nil {
		return fmt.Errorf("删除 DNS 记录失败: %v", err)
	}

	logrus.Infof("[ESA] 已删除 DNS 记录: id=%d", id)
	return nil
}

// DeleteNamespaceData 删除 Astra 数据库中指定 namespace 的所有数据
func DeleteNamespaceData(namespace string) error {
	if namespace == "" {
		return fmt.Errorf("namespace 不能为空，跳过数据库清理")
	}
	tables := []string{"users", "autorun_records", "countdown_records", "schedules", "client_configs", "timetables", "subjects", "data_versions"}
	for _, table := range tables {
		result := db.GetDB().Exec(fmt.Sprintf("DELETE FROM %s WHERE namespace = ?", table), namespace)
		if result.Error != nil {
			logrus.Warnf("删除 %s 表中 namespace=%s 数据失败: %v", table, namespace, result.Error)
		} else {
			logrus.Infof("已删除 %s 表中 %d 条 namespace=%s 记录", table, result.RowsAffected, namespace)
		}
	}
	return nil
}

// fullDomain 返回子域名对应的完整记录名。
func fullDomain(subdomain string) string {
	return subdomain + "." + config.Configs.ESA.SiteName
}

func subdomainToNamespace(subdomain string) string {
	return "cn/getastra/" + subdomain
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func derefInt64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}
