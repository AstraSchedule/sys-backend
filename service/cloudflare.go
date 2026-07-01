package service

import (
	"context"
	"fmt"
	"strings"

	"sys-backend/config"

	"github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/dns"
	"github.com/cloudflare/cloudflare-go/v7/option"
	"github.com/sirupsen/logrus"
)

type TenantInfo struct {
	RecordID  string
	Subdomain string
	Namespace string
	Target    string
	Type      string
	Status    string // "normal", "orphan", "abnormal"
}

func newClient() *cloudflare.Client {
	cfg := config.Configs.Cloudflare
	return cloudflare.NewClient(option.WithAPIToken(cfg.APIToken))
}

func zoneID() string {
	return config.Configs.Cloudflare.ZoneID
}

func fullDomain(subdomain string) string {
	return subdomain + "." + config.Configs.Cloudflare.Domain
}

func FetchSaaSSubdomains() ([]TenantInfo, error) {
	cfg := config.Configs.Cloudflare
	if cfg.APIToken == "" || cfg.ZoneID == "" {
		return nil, fmt.Errorf("Cloudflare API 凭据未配置")
	}

	logrus.Infof("[CF] 开始获取 DNS 记录, ZoneID=%s, Token=%s***", cfg.ZoneID, cfg.APIToken[:min(6, len(cfg.APIToken))])

	client := newClient()

	var allRecords []dns.RecordResponse
	pager, err := client.DNS.Records.List(context.TODO(), dns.RecordListParams{
		ZoneID: cloudflare.F(zoneID()),
	})
	if err != nil {
		logrus.Errorf("[CF] 获取 DNS 记录失败: %v", err)
		return nil, fmt.Errorf("查询 Cloudflare DNS 记录失败: %v", err)
	}

	allRecords = append(allRecords, pager.Result...)
	logrus.Infof("[CF] 获取到 %d 条 DNS 记录", len(allRecords))

	var tenants []TenantInfo
	for _, r := range allRecords {
		recordType := string(r.Type)
		logrus.Debugf("[CF] 记录: id=%s name=%s type=%s content=%s comment=%q", r.ID, r.Name, recordType, r.Content, r.Comment)

		if recordType != "CNAME" && recordType != "A" {
			continue
		}
		if !strings.Contains(strings.ToLower(r.Comment), "saas") { // 匹配 "SaaS" 或 "saas"
			continue
		}
		parts := strings.Split(r.Name, ".")
		if len(parts) < 2 {
			continue
		}
		subdomain := parts[0]
		logrus.Infof("[CF] 匹配 SaaS 子域名: %s -> namespace=%s", subdomain, subdomainToNamespace(subdomain))
		tenants = append(tenants, TenantInfo{
			RecordID:  r.ID,
			Subdomain: subdomain,
			Namespace: subdomainToNamespace(subdomain),
			Target:    r.Content,
			Type:      recordType,
			Status:    "normal",
		})
	}

	logrus.Infof("[CF] 最终匹配 %d 个 SaaS 子域名", len(tenants))
	return tenants, nil
}

// CreateTenant 在 Cloudflare 创建 CNAME 记录，指向 class.getastra.cn
func CreateTenant(subdomain string) (*TenantInfo, error) {
	cfg := config.Configs.Cloudflare
	if cfg.APIToken == "" || cfg.ZoneID == "" {
		return nil, fmt.Errorf("Cloudflare API 凭据未配置")
	}

	client := newClient()
	name := fullDomain(subdomain)

	record, err := client.DNS.Records.New(context.TODO(), dns.RecordNewParams{
		ZoneID: cloudflare.F(zoneID()),
		Body: dns.CNAMERecordParam{
			Name:    cloudflare.F(name),
			Type:    cloudflare.F(dns.CNAMERecordTypeCNAME),
			Content: cloudflare.F("class.getastra.cn"),
			Proxied: cloudflare.F(true),
			Comment: cloudflare.F("SaaS"),
			TTL:     cloudflare.F(dns.TTL1),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("创建 DNS 记录失败: %v", err)
	}

	logrus.Infof("[CF] 已创建 SaaS 记录: %s -> class.getastra.cn (id=%s)", name, record.ID)
	return &TenantInfo{
		RecordID:  record.ID,
		Subdomain: subdomain,
		Namespace: subdomainToNamespace(subdomain),
		Target:    "class.getastra.cn",
		Type:      "CNAME",
		Status:    "normal",
	}, nil
}

// DeleteTenant 删除 Cloudflare DNS 记录
func DeleteTenant(recordID string) error {
	client := newClient()

	_, err := client.DNS.Records.Delete(context.TODO(), recordID, dns.RecordDeleteParams{
		ZoneID: cloudflare.F(zoneID()),
	})
	if err != nil {
		return fmt.Errorf("删除 DNS 记录失败: %v", err)
	}

	logrus.Infof("[CF] 已删除 DNS 记录: id=%s", recordID)
	return nil
}

func subdomainToNamespace(subdomain string) string {
	return "cn/getastra/" + subdomain
}
