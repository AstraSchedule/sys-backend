package web

import (
	"net/http"
	"sys-backend/db"
	"sys-backend/service"

	"github.com/gin-gonic/gin"
)

type TenantView struct {
	RecordID  string `json:"record_id"`
	Subdomain string `json:"subdomain"`
	Namespace string `json:"namespace"`
	Target    string `json:"target,omitempty"`
	Type      string `json:"type,omitempty"`
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
		status := "orphan"
		if dbNamespaces[t.Namespace] {
			status = "normal"
		}
		result = append(result, TenantView{
			RecordID:  t.RecordID,
			Subdomain: t.Subdomain,
			Namespace: t.Namespace,
			Target:    t.Target,
			Type:      t.Type,
			Status:    status,
		})
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

func CreateTenant(c *gin.Context) {
	var req struct {
		Subdomain string `json:"subdomain"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Subdomain == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "subdomain 不能为空"})
		return
	}

	tenant, err := service.CreateTenant(req.Subdomain)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": 200,
		"data": TenantView{
			RecordID:  tenant.RecordID,
			Subdomain: tenant.Subdomain,
			Namespace: tenant.Namespace,
			Target:    tenant.Target,
			Type:      tenant.Type,
			Status:    tenant.Status,
		},
	})
}

func DeleteTenant(c *gin.Context) {
	recordID := c.Param("id")
	if recordID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "记录 ID 不能为空"})
		return
	}

	var req struct {
		Namespace string `json:"namespace"`
	}
	c.ShouldBindJSON(&req)

	if err := service.DeleteTenant(recordID, req.Namespace); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": 200, "message": "租户已删除（DNS + 数据库数据）"})
}

func BanTenant(c *gin.Context) {
	recordID := c.Param("id")
	if recordID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "记录 ID 不能为空"})
		return
	}

	if err := service.BanTenant(recordID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": 200, "message": "租户已封禁（仅删除 DNS 记录）"})
}

func CompleteTenant(c *gin.Context) {
	var req struct {
		Namespace string `json:"namespace"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Namespace == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "namespace 不能为空"})
		return
	}

	// 检查是否已有用户
	var count int64
	if err := db.DB.Table("users").Where("namespace = ?", req.Namespace).Count(&count).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if count > 0 {
		c.JSON(http.StatusOK, gin.H{"status": 200, "message": "该命名空间已存在用户，无需补全"})
		return
	}

	// 创建默认管理员
	hash, err := service.HashPassword("admin")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成密码哈希失败"})
		return
	}
	admin := map[string]interface{}{
		"namespace":      req.Namespace,
		"username":       "admin",
		"password_hash":  hash,
		"role":           "admin",
		"must_change_pwd": true,
	}
	if err := db.DB.Table("users").Create(admin).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": 200, "message": "已为该命名空间创建默认管理员账户"})
}

func CompleteTenantDNS(c *gin.Context) {
	var req struct {
		Namespace string `json:"namespace"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Namespace == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "namespace 不能为空"})
		return
	}

	subdomain := namespaceToSubdomain(req.Namespace)
	tenant, err := service.CreateTenant(subdomain)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": 200, "data": tenant, "message": "DNS 记录已创建"})
}

func CleanupTenant(c *gin.Context) {
	var req struct {
		Namespace string `json:"namespace"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Namespace == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "namespace 不能为空"})
		return
	}

	if err := service.CleanupTenant(req.Namespace); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": 200, "message": "残留数据已清理"})
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
