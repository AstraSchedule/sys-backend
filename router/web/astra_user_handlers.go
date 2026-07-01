package web

import (
	"net/http"
	"sys-backend/db"
	"sys-backend/service"

	"github.com/gin-gonic/gin"
)

type astraUserRecord struct {
	ID                 uint   `json:"id"`
	Namespace          string `json:"namespace"`
	Username           string `json:"username"`
	Role               string `json:"role"`
	Scope              string `json:"scope"`
	MustChangePwd      bool   `json:"must_change_pwd"`
	MustChangeUsername bool   `json:"must_change_username"`
}

func ListAstraUsers(c *gin.Context) {
	namespace := c.Query("namespace")

	var rows []struct {
		ID                 uint   `gorm:"column:id"`
		Namespace          string `gorm:"column:namespace"`
		Username           string `gorm:"column:username"`
		Role               string `gorm:"column:role"`
		Scope              string `gorm:"column:scope"`
		MustChangePwd      bool   `gorm:"column:must_change_pwd"`
		MustChangeUsername bool   `gorm:"column:must_change_username"`
	}

	query := db.DB.Table("users")
	if namespace != "" {
		query = query.Where("namespace = ?", namespace)
	}
	query.Find(&rows)

	out := make([]astraUserRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, astraUserRecord{
			ID: r.ID, Namespace: r.Namespace, Username: r.Username,
			Role: r.Role, Scope: r.Scope, MustChangePwd: r.MustChangePwd,
			MustChangeUsername: r.MustChangeUsername,
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func CreateAstraUser(c *gin.Context) {
	var req struct {
		Namespace string `json:"namespace" binding:"required"`
		Username  string `json:"username" binding:"required"`
		Password  string `json:"password" binding:"required"`
		Role      string `json:"role" binding:"required"`
		Scope     string `json:"scope"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效参数"})
		return
	}

	hash, err := service.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "密码哈希失败"})
		return
	}

	row := map[string]interface{}{
		"namespace":       req.Namespace,
		"username":        req.Username,
		"password_hash":   hash,
		"role":            req.Role,
		"scope":           req.Scope,
		"must_change_pwd": true,
	}

	if err := db.DB.Table("users").Create(row).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"detail": "用户名已存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": 200, "message": "创建成功"})
}

func UpdateAstraUser(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Username *string `json:"username"`
		Password *string `json:"password"`
		Role     *string `json:"role"`
		Scope    *string `json:"scope"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效参数"})
		return
	}

	updates := map[string]interface{}{}
	if req.Username != nil {
		updates["username"] = *req.Username
	}
	if req.Password != nil {
		hash, err := service.HashPassword(*req.Password)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": "密码哈希失败"})
			return
		}
		updates["password_hash"] = hash
		updates["must_change_pwd"] = false
	}
	if req.Role != nil {
		updates["role"] = *req.Role
	}
	if req.Scope != nil {
		updates["scope"] = *req.Scope
	}

	if err := db.DB.Table("users").Where("id = ?", id).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": 200, "message": "更新成功"})
}

func DeleteAstraUser(c *gin.Context) {
	id := c.Param("id")
	result := db.DB.Table("users").Where("id = ?", id).Delete(nil)
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"detail": "用户不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": 200, "deleted": result.RowsAffected})
}
