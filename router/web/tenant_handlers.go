package web

import (
	"net/http"
	"sys-backend/db"
	"sys-backend/model/dbTable"

	"github.com/gin-gonic/gin"
)

func ListTenants(c *gin.Context) {
	var tenants []dbTable.Tenant
	if err := db.SysDB.Find(&tenants).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tenants})
}

func CreateTenant(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"detail": "功能尚未实现"})
}

func UpdateTenant(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"detail": "功能尚未实现"})
}

func DeleteTenant(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"detail": "功能尚未实现"})
}
