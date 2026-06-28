package web

import (
	"encoding/json"
	"net/http"
	"sys-backend/db"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func serializeMapValues(data map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range data {
		switch val := v.(type) {
		case map[string]interface{}, []interface{}:
			jsonBytes, err := json.Marshal(val)
			if err != nil {
				result[k] = val
			} else {
				result[k] = string(jsonBytes)
			}
		default:
			result[k] = val
		}
	}
	return result
}

func ListTables(c *gin.Context) {
	var astraTables []string
	db.DB.Raw("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&astraTables)

	var sysTables []string
	db.SysDB.Raw("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&sysTables)

	tables := append(astraTables, sysTables...)
	c.JSON(http.StatusOK, gin.H{"data": tables})
}

func ListTableData(c *gin.Context) {
	table := c.Param("table")
	var result []map[string]interface{}

	var gdb *gorm.DB
	switch table {
	case "system_users", "tenants":
		gdb = db.SysDB
	default:
		gdb = db.DB
	}

	if err := gdb.Table(table).Find(&result).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

func GetRecord(c *gin.Context) {
	table := c.Param("table")
	id := c.Param("id")

	var result map[string]interface{}
	var gdb *gorm.DB

	switch table {
	case "system_users", "tenants":
		gdb = db.SysDB
	default:
		gdb = db.DB
	}

	if err := gdb.Table(table).Where("id = ?", id).First(&result).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "记录不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

func CreateRecord(c *gin.Context) {
	table := c.Param("table")

	var data map[string]interface{}
	if err := c.ShouldBindJSON(&data); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效参数"})
		return
	}

	var gdb *gorm.DB
	switch table {
	case "system_users", "tenants":
		gdb = db.SysDB
	default:
		gdb = db.DB
	}

	serialized := serializeMapValues(data)
	if err := gdb.Table(table).Create(serialized).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": 200, "message": "创建成功"})
}

func UpdateRecord(c *gin.Context) {
	table := c.Param("table")
	id := c.Param("id")

	var data map[string]interface{}
	if err := c.ShouldBindJSON(&data); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效参数"})
		return
	}

	var gdb *gorm.DB
	switch table {
	case "system_users", "tenants":
		gdb = db.SysDB
	default:
		gdb = db.DB
	}

	serialized := serializeMapValues(data)
	if err := gdb.Table(table).Where("id = ?", id).Updates(serialized).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": 200, "message": "更新成功"})
}

func DeleteRecord(c *gin.Context) {
	table := c.Param("table")
	id := c.Param("id")

	var gdb *gorm.DB
	switch table {
	case "system_users", "tenants":
		gdb = db.SysDB
	default:
		gdb = db.DB
	}

	result := gdb.Table(table).Where("id = ?", id).Delete(nil)
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"detail": "记录不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": 200, "deleted": result.RowsAffected})
}
