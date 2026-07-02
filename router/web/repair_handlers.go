package web

import (
	"fmt"
	"net/http"
	"sys-backend/config"
	"sys-backend/db"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func checkAndRepair(name string, gdb *gorm.DB) string {
	// 1. 完整性检查
	var result string
	if err := gdb.Raw("PRAGMA integrity_check").Scan(&result).Error; err != nil {
		return fmt.Sprintf("完整性检查失败: %v", err)
	}
	if result == "ok" {
		return "数据库完好，无需修复"
	}

	logrus.Infof("[%s] integrity_check: %s", name, result)

	// 2. 尝试 VACUUM 重建
	if err := gdb.Exec("VACUUM").Error; err != nil {
		logrus.Errorf("[%s] VACUUM failed: %v", name, err)
		return fmt.Sprintf("完整性异常，VACUUM 修复失败: %v。建议手动恢复。", err)
	}

	// 3. 再次检查
	if err := gdb.Raw("PRAGMA integrity_check").Scan(&result).Error; err != nil {
		return fmt.Sprintf("VACUUM 完成但复查失败: %v", err)
	}
	if result == "ok" {
		return "数据库已修复"
	}
	return fmt.Sprintf("VACUUM 完成但仍有问题: %s", result)
}

// RepairDatabase 检查并尝试修复 SQLite 数据库
func RepairDatabase(c *gin.Context) {
	results := make(map[string]string)

	if config.Configs.DB.Type == "sqlite" {
		results["astra_db"] = checkAndRepair("astra", db.DB)
	} else {
		results["astra_db"] = "非 SQLite，跳过"
	}

	if config.Configs.SysDB.Type == "sqlite" {
		results["sys_db"] = checkAndRepair("sys", db.SysDB)
	} else {
		results["sys_db"] = "非 SQLite，跳过"
	}

	c.JSON(http.StatusOK, gin.H{"status": "success", "results": results})
}
