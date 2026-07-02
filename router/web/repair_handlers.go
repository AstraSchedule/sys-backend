package web

import (
	"net/http"
	"sys-backend/config"
	"sys-backend/db"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// RepairDatabase 对 SQLite 数据库执行 VACUUM 修复
func RepairDatabase(c *gin.Context) {
	results := make(map[string]string)

	// 修复 Astra 数据库
	if config.Configs.DB.Type == "sqlite" {
		if err := db.DB.Exec("VACUUM").Error; err != nil {
			logrus.Errorf("VACUUM astra db failed: %v", err)
			results["astra_db"] = "修复失败: " + err.Error()
		} else {
			logrus.Info("VACUUM astra db success")
			results["astra_db"] = "修复成功"
		}
	} else {
		results["astra_db"] = "非 SQLite，跳过"
	}

	// 修复 Sys 数据库
	if config.Configs.SysDB.Type == "sqlite" {
		if err := db.SysDB.Exec("VACUUM").Error; err != nil {
			logrus.Errorf("VACUUM sys db failed: %v", err)
			results["sys_db"] = "修复失败: " + err.Error()
		} else {
			logrus.Info("VACUUM sys db success")
			results["sys_db"] = "修复成功"
		}
	} else {
		results["sys_db"] = "非 SQLite，跳过"
	}

	c.JSON(http.StatusOK, gin.H{"status": "success", "results": results})
}
