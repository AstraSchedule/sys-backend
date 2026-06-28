package web

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func ListTenants(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"detail": "功能尚未实现"})
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
