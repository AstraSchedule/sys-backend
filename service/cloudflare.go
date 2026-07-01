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

	client := cloudflare.NewClient(option.WithAPIToken(cfg.APIToken))

	var allRecords []dns.RecordResponse
	pager := client.DNS.Records.ListAutoPaging(context.Background(), dns.RecordListParams{
		ZoneID:  cloudflare.F(cfg.ZoneID),
		PerPage: cloudflare.F(100.0),
	})
	for pager.Next() {
		allRecords = append(allRecords, pager.Current())
	}
	if err := pager.Err(); err != nil {
		return nil, fmt.Errorf("查询 Cloudflare DNS 记录失败: %v", err)
	}

	var tenants []TenantInfo
	for _, r := range allRecords {
		recordType := string(r.Type)
		if recordType != "CNAME" && recordType != "A" {
			continue
		}
		if !strings.Contains(strings.ToLower(r.Comment), "saas") {
			continue
		}
		parts := strings.Split(r.Name, ".")
		if len(parts) < 2 {
			continue
		}
		subdomain := parts[0]
		tenants = append(tenants, TenantInfo{
			Subdomain: subdomain,
			Namespace: subdomainToNamespace(subdomain),
			Status:    "normal",
		})
	}

	logrus.Infof("从 Cloudflare 获取到 %d 个 SaaS 子域名", len(tenants))
	return tenants, nil
}

func subdomainToNamespace(subdomain string) string {
	return "cn/getastra/" + subdomain
}
