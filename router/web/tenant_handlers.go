package web

import (
	"net/http"
	"sys-backend/db"
	"sys-backend/service"

	"github.com/gin-gonic/gin"
)

type TenantView struct {
	Subdomain string `json:"subdomain"`
	Namespace string `json:"namespace"`
	Status    string `json:"status"` // normal, orphan, abnormal
}

func ListTenants(c *gin.Context) {
	// 1. Fetch from Cloudflare
	cfTenants, err := service.FetchSaaSSubdomains()
	if err != nil {
		// If Cloudflare not configured, just scan DB
		cfTenants = nil
	}

	// 2. Scan all namespaces from Astra DB
	var rows []struct {
		Namespace string `gorm:"column:namespace"`
	}
	db.DB.Table("users").Select("DISTINCT namespace").Scan(&rows)

	dbNamespaces := make(map[string]bool)
	for _, r := range rows {
		dbNamespaces[r.Namespace] = true
	}

	cfMap := make(map[string]bool)
	for _, t := range cfTenants {
		cfMap[t.Namespace] = true
	}

	// 3. Build result
	var result []TenantView

	// Normal: in both CF and DB
	for _, t := range cfTenants {
		if dbNamespaces[t.Namespace] {
			result = append(result, TenantView{
				Subdomain: t.Subdomain,
				Namespace: t.Namespace,
				Status:    "normal",
			})
		} else {
			result = append(result, TenantView{
				Subdomain: t.Subdomain,
				Namespace: t.Namespace,
				Status:    "orphan",
			})
		}
	}

	// Abnormal: in DB but not in CF
	for ns := range dbNamespaces {
		if !cfMap[ns] {
			subdomain := namespaceToSubdomain(ns)
			result = append(result, TenantView{
				Subdomain: subdomain,
				Namespace: ns,
				Status:    "abnormal",
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

func namespaceToSubdomain(ns string) string {
	parts := []string{}
	for _, p := range splitNamespace(ns) {
		parts = append(parts, p)
	}
	if len(parts) >= 3 {
		return parts[2]
	}
	return ns
}

func splitNamespace(ns string) []string {
	result := []string{}
	current := ""
	for _, c := range ns {
		if c == '/' {
			if current != "" {
				result = append(result, current)
			}
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}
