package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"sys-backend/config"

	"github.com/sirupsen/logrus"
)

type CFRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	Comment string `json:"comment"`
}

type CFResponse struct {
	Success bool       `json:"success"`
	Result  []CFRecord `json:"result"`
}

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

	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records?per_page=100", cfg.ZoneID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 Cloudflare API 失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var cfResp CFResponse
	if err := json.Unmarshal(body, &cfResp); err != nil {
		return nil, fmt.Errorf("解析 Cloudflare 响应失败: %v", err)
	}

	if !cfResp.Success {
		return nil, fmt.Errorf("Cloudflare API 返回失败")
	}

	var tenants []TenantInfo
	for _, r := range cfResp.Result {
		if r.Type != "CNAME" && r.Type != "A" {
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
		namespace := subdomainToNamespace(subdomain)
		tenants = append(tenants, TenantInfo{
			Subdomain: subdomain,
			Namespace: namespace,
			Status:    "normal",
		})
	}

	logrus.Infof("从 Cloudflare 获取到 %d 个 SaaS 子域名", len(tenants))
	return tenants, nil
}

func subdomainToNamespace(subdomain string) string {
	return "cn/getastra/" + subdomain
}
