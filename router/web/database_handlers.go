package web

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sys-backend/config"
	"sys-backend/db"
	"sys-backend/model/dbTable"
	"sys-backend/service"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

var astraTableNames = []string{
	"schedules", "client_configs", "timetables",
	"subjects", "data_versions", "autorun_records", "countdown_records", "users",
}

var sysTableNames = []string{
	"system_users", "tenants",
}

func isAstraTable(name string) bool {
	for _, t := range astraTableNames {
		if t == name {
			return true
		}
	}
	return false
}

func DropTable(c *gin.Context) {
	tableName := c.Param("table")

	// Check if table exists in either database
	var count int64
	db.SysDB.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", tableName).Scan(&count)
	isSys := count > 0

	db.DB.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", tableName).Scan(&count)
	isAstra := count > 0

	if !isSys && !isAstra {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "表不存在"})
		return
	}

	if isAstra {
		if err := callAstraDropTable(tableName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "调用 Astra 后端失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": 200, "message": fmt.Sprintf("表 %s 已删除并重建", tableName)})
		return
	}

	// Sys tables: direct GORM with model-based migration
	gdb := getDBForTable(tableName)
	gdb.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName))
	switch tableName {
	case "system_users":
		db.SysDB.AutoMigrate(&dbTable.SystemUser{})
	}

	c.JSON(http.StatusOK, gin.H{"status": 200, "message": fmt.Sprintf("表 %s 已删除并重建", tableName)})
}

func RebuildDatabase(c *gin.Context) {
	var req struct {
		Scope  string `json:"scope"`
		Import bool   `json:"import"`
	}
	c.ShouldBindJSON(&req)
	if req.Scope == "" {
		req.Scope = "full"
	}

	var sysTbls, astraTbls []string
	switch req.Scope {
	case "astra":
		astraTbls = astraTableNames
	case "sys":
		sysTbls = sysTableNames
	default:
		sysTbls = sysTableNames
		astraTbls = astraTableNames
	}

	// Backup sys
	type backupEntry struct {
		Name string
		Data []map[string]interface{}
	}
	var sysBackups []backupEntry
	for _, t := range sysTbls {
		var rows []map[string]interface{}
		db.SysDB.Table(t).Find(&rows)
		sysBackups = append(sysBackups, backupEntry{Name: t, Data: rows})
	}

	// Drop sys
	for _, t := range sysTbls {
		db.SysDB.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", t))
	}
	db.SysDB.AutoMigrate(&dbTable.SystemUser{})

	// Restore sys
	if req.Import {
		for _, b := range sysBackups {
			for _, row := range b.Data {
				delete(row, "id")
				delete(row, "created_at")
				delete(row, "updated_at")
				db.SysDB.Table(b.Name).Create(row)
			}
			logrus.Infof("已恢复表 %s: %d 条记录", b.Name, len(b.Data))
		}
	}

	// Astra: delegate to AstraScheduleServerGo
	for _, t := range astraTbls {
		if err := callAstraDropTable(t); err != nil {
			logrus.Warnf("调用 Astra 后端删除表 %s 失败: %v", t, err)
		}
	}

	// Default admin
	if req.Scope == "full" || req.Scope == "sys" {
		var count int64
		db.SysDB.Model(&dbTable.SystemUser{}).Count(&count)
		if count == 0 {
			hash, _ := service.HashPassword("admin")
			db.SysDB.Create(&dbTable.SystemUser{
				Username:      "admin",
				PasswordHash:  hash,
				Role:          "readwrite",
				MustChangePwd: true,
			})
			logrus.Info("已创建默认管理员: admin/admin")
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": 200, "message": "数据库重建成功", "scope": req.Scope})
}

func getDBForTable(name string) *gorm.DB {
	for _, t := range sysTableNames {
		if t == name {
			return db.SysDB
		}
	}
	return db.DB
}

func loadTLSContent(val string) ([]byte, error) {
	if strings.HasPrefix(val, "-----") {
		return []byte(val), nil
	}
	return os.ReadFile(val)
}

func callAstraDropTable(tableName string) error {
	astraURL := config.Configs.Astra.URL
	if astraURL == "" {
		return fmt.Errorf("Astra 后端地址未配置")
	}

	url := fmt.Sprintf("%s/web/admin/drop-table/%s", astraURL, tableName)
	req, err := http.NewRequest("DELETE", url, bytes.NewBuffer(nil))
	if err != nil {
		return err
	}

	// Build TLS client with mTLS certificate
	transport := &http.Transport{}
	cfCfg := config.Configs.Cloudflare
	if cfCfg.TLSCert != "" && cfCfg.TLSKey != "" {
		certPEM, err := loadTLSContent(cfCfg.TLSCert)
		if err != nil {
			return fmt.Errorf("加载客户端证书失败: %v", err)
		}
		keyPEM, err := loadTLSContent(cfCfg.TLSKey)
		if err != nil {
			return fmt.Errorf("加载客户端私钥失败: %v", err)
		}

		cert, err := tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			return fmt.Errorf("解析客户端证书失败: %v", err)
		}

		tlsCfg := &tls.Config{
			Certificates: []tls.Certificate{cert},
		}

		if cfCfg.TLSCACert != "" {
			caPEM, err := loadTLSContent(cfCfg.TLSCACert)
			if err != nil {
				return fmt.Errorf("加载 CA 证书失败: %v", err)
			}
			caCertPool := x509.NewCertPool()
			caCertPool.AppendCertsFromPEM(caPEM)
			tlsCfg.RootCAs = caCertPool
		}

		transport.TLSClientConfig = tlsCfg
	}

	client := &http.Client{Timeout: 30 * time.Second, Transport: transport}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("Astra 后端返回 %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
