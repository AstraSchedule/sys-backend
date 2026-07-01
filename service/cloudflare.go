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
	Subdomain string
	Namespace string
	Status    string // "normal", "orphan", "abnormal"
}

func FetchSaaSSubdomains() ([]TenantInfo, error) {
	cfg := config.Configs.Cloudflare
	if cfg.APIToken == "" || cfg.ZoneID == "" {
		return nil, fmt.Errorf("Cloudflare API 凭据未配置")
	}

	logrus.Infof("[CF] 开始获取 DNS 记录, ZoneID=%s, Token=%s***", cfg.ZoneID, cfg.APIToken[:min(6, len(cfg.APIToken))])

	client := cloudflare.NewClient(option.WithAPIToken(cfg.APIToken))

	var allRecords []dns.RecordResponse
	pager, err := client.DNS.Records.List(context.TODO(), dns.RecordListParams{
		ZoneID: cloudflare.F(cfg.ZoneID),
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
		logrus.Debugf("[CF] 记录: name=%s type=%s content=%s comment=%q", r.Name, recordType, r.Content, r.Comment)

		if recordType != "CNAME" && recordType != "A" {
			logrus.Debugf("[CF] 跳过: 非 CNAME/A 类型 (%s)", recordType)
			continue
		}
		if !strings.Contains(strings.ToLower(r.Comment), "saas") {
			logrus.Debugf("[CF] 跳过: comment 不含 'saas' (comment=%q)", r.Comment)
			continue
		}
		parts := strings.Split(r.Name, ".")
		if len(parts) < 2 {
			logrus.Debugf("[CF] 跳过: 域名格式无效 (%s)", r.Name)
			continue
		}
		subdomain := parts[0]
		logrus.Infof("[CF] 匹配 SaaS 子域名: %s -> namespace=%s", subdomain, subdomainToNamespace(subdomain))
		tenants = append(tenants, TenantInfo{
			Subdomain: subdomain,
			Namespace: subdomainToNamespace(subdomain),
			Status:    "normal",
		})
	}

	logrus.Infof("[CF] 最终匹配 %d 个 SaaS 子域名", len(tenants))
	return tenants, nil
}

func subdomainToNamespace(subdomain string) string {
	return "cn/getastra/" + subdomain
}
