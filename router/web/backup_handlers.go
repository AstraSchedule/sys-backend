package web

import (
	"net/http"
	"sys-backend/db"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type BackupPayload struct {
	Meta             map[string]interface{}   `json:"meta"`
	SystemUsers      []map[string]interface{} `json:"system_users"`
	Tenants          []map[string]interface{} `json:"tenants"`
	Schedules        []map[string]interface{} `json:"schedules"`
	ClientConfigs    []map[string]interface{} `json:"client_configs"`
	Timetables       []map[string]interface{} `json:"timetables"`
	Subjects         []map[string]interface{} `json:"subjects"`
	DataVersions     []map[string]interface{} `json:"data_versions"`
	AutorunRecords   []map[string]interface{} `json:"autorun_records"`
	CountdownRecords []map[string]interface{} `json:"countdown_records"`
}

func ExportBackup(c *gin.Context) {
	mode := c.DefaultQuery("mode", "full")

	payload := BackupPayload{
		Meta: map[string]interface{}{
			"mode": mode,
		},
	}

	// Export based on mode
	if mode == "full" || mode == "dashboard" {
		db.SysDB.Table("system_users").Find(&payload.SystemUsers)
		db.SysDB.Table("tenants").Find(&payload.Tenants)
	}

	if mode == "full" || mode == "saas" {
		db.DB.Table("schedules").Find(&payload.Schedules)
		db.DB.Table("client_configs").Find(&payload.ClientConfigs)
		db.DB.Table("timetables").Find(&payload.Timetables)
		db.DB.Table("subjects").Find(&payload.Subjects)
		db.DB.Table("data_versions").Find(&payload.DataVersions)
		db.DB.Table("autorun_records").Find(&payload.AutorunRecords)
		db.DB.Table("countdown_records").Find(&payload.CountdownRecords)
	}

	c.Header("Content-Disposition", "attachment; filename=backup.json")
	c.JSON(http.StatusOK, payload)
}

// importTableRows 将记录列表导入到指定表
func importTableRows(gdb *gorm.DB, table string, records []map[string]interface{}) {
	for _, record := range records {
		gdb.Table(table).Create(record)
	}
}

func ImportBackup(c *gin.Context) {
	var payload BackupPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效参数"})
		return
	}

	mode := "full"
	if m, ok := payload.Meta["mode"].(string); ok {
		mode = m
	}

	if mode == "full" || mode == "dashboard" {
		importTableRows(db.SysDB, "system_users", payload.SystemUsers)
		importTableRows(db.SysDB, "tenants", payload.Tenants)
	}

	if mode == "full" || mode == "saas" {
		importTableRows(db.DB, "schedules", payload.Schedules)
		importTableRows(db.DB, "client_configs", payload.ClientConfigs)
		importTableRows(db.DB, "timetables", payload.Timetables)
		importTableRows(db.DB, "subjects", payload.Subjects)
		importTableRows(db.DB, "data_versions", payload.DataVersions)
		importTableRows(db.DB, "autorun_records", payload.AutorunRecords)
		importTableRows(db.DB, "countdown_records", payload.CountdownRecords)
	}

	c.JSON(http.StatusOK, gin.H{"status": 200, "message": "导入成功"})
}
