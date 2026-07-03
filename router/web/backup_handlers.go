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

type ImportRequest struct {
	BackupPayload
	Source    string `json:"source"`
	Namespace string `json:"namespace"`
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

// injectNamespace 为记录列表中的每条记录注入 namespace 字段
func injectNamespace(records []map[string]interface{}, namespace string) {
	for _, record := range records {
		record["namespace"] = namespace
	}
}

func ImportBackup(c *gin.Context) {
	var req ImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效参数"})
		return
	}

	// 自部署备份：为 Astra 表记录注入 namespace
	if req.Source == "self-hosted" && req.Namespace != "" {
		injectNamespace(req.Schedules, req.Namespace)
		injectNamespace(req.ClientConfigs, req.Namespace)
		injectNamespace(req.Timetables, req.Namespace)
		injectNamespace(req.Subjects, req.Namespace)
		injectNamespace(req.DataVersions, req.Namespace)
		injectNamespace(req.AutorunRecords, req.Namespace)
		injectNamespace(req.CountdownRecords, req.Namespace)
	}

	mode := "full"
	if m, ok := req.Meta["mode"].(string); ok {
		mode = m
	}

	if mode == "full" || mode == "dashboard" {
		importTableRows(db.SysDB, "system_users", req.SystemUsers)
		importTableRows(db.SysDB, "tenants", req.Tenants)
	}

	if mode == "full" || mode == "saas" {
		importTableRows(db.DB, "schedules", req.Schedules)
		importTableRows(db.DB, "client_configs", req.ClientConfigs)
		importTableRows(db.DB, "timetables", req.Timetables)
		importTableRows(db.DB, "subjects", req.Subjects)
		importTableRows(db.DB, "data_versions", req.DataVersions)
		importTableRows(db.DB, "autorun_records", req.AutorunRecords)
		importTableRows(db.DB, "countdown_records", req.CountdownRecords)
	}

	c.JSON(http.StatusOK, gin.H{"status": 200, "message": "导入成功"})
}
