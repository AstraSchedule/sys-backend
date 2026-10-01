package web

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"mime"
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

func resolveAllowedTable(name string) (string, bool) {
	switch name {
	case "schedules":
		return "schedules", true
	case "client_configs":
		return "client_configs", true
	case "timetables":
		return "timetables", true
	case "subjects":
		return "subjects", true
	case "data_versions":
		return "data_versions", true
	case "autorun_records":
		return "autorun_records", true
	case "countdown_records":
		return "countdown_records", true
	case "users":
		return "users", true
	case "system_users":
		return "system_users", true
	case "tenants":
		return "tenants", true
	default:
		return "", false
	}
}

func mustAllowedTable(name string) string {
	table, ok := resolveAllowedTable(name)
	if !ok {
		panic("unexpected table name")
	}
	return table
}

func isAstraTable(name string) bool {
	for _, t := range astraTableNames {
		if t == name {
			return true
		}
	}
	return false
}

func isAllowedTable(name string) bool {
	return isAstraTable(name) || isSysTable(name)
}

func isSysTable(name string) bool {
	for _, t := range sysTableNames {
		if t == name {
			return true
		}
	}
	return false
}

func DropTable(c *gin.Context) {
	tableName, ok := resolveAllowedTable(c.Param("table"))

	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "不允许的操作表"})
		return
	}

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
	gdb.Migrator().DropTable(tableName)
	switch tableName {
	case "system_users":
		db.SysDB.AutoMigrate(&dbTable.SystemUser{})
	}

	c.JSON(http.StatusOK, gin.H{"status": 200, "message": fmt.Sprintf("表 %s 已删除并重建", tableName)})
}

type backupEntry struct {
	Name string
	Data []map[string]interface{}
}

// backupSysTables 备份 sys 数据库中的指定表
func backupSysTables(tables []string) []backupEntry {
	var backups []backupEntry
	for _, t := range tables {
		table := mustAllowedTable(t)
		var rows []map[string]interface{}
		db.SysDB.Table(table).Find(&rows)
		backups = append(backups, backupEntry{Name: table, Data: rows})
	}
	return backups
}

// dropAndRestoreSys 删除并恢复 sys 数据库中的表
func dropAndRestoreSys(tables []string, backups []backupEntry, shouldImport bool) {
	for _, t := range tables {
		db.SysDB.Migrator().DropTable(mustAllowedTable(t))
	}
	db.SysDB.AutoMigrate(&dbTable.SystemUser{})
	if shouldImport {
		for _, b := range backups {
			for _, row := range b.Data {
				delete(row, "id")
				delete(row, "created_at")
				delete(row, "updated_at")
				db.SysDB.Table(b.Name).Create(row)
			}
			logrus.Infof("已恢复表 %s: %d 条记录", b.Name, len(b.Data))
		}
	}
}

// ensureDefaultAdmin 确保存在默认管理员账户
func ensureDefaultAdmin() {
	var count int64
	db.SysDB.Model(&dbTable.SystemUser{}).Count(&count)
	if count > 0 {
		return
	}
	hash, _ := service.HashPassword("admin")
	db.SysDB.Create(&dbTable.SystemUser{
		Username:      "admin",
		PasswordHash:  hash,
		Role:          "readwrite",
		MustChangePwd: true,
	})
	logrus.Info("已创建默认管理员: admin/admin")
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

	backups := backupSysTables(sysTbls)
	dropAndRestoreSys(sysTbls, backups, req.Import)

	for _, t := range astraTbls {
		if err := callAstraDropTable(t); err != nil {
			logrus.Warnf("调用 Astra 后端删除表 %s 失败: %v", t, err)
		}
	}

	if req.Scope == "full" || req.Scope == "sys" {
		ensureDefaultAdmin()
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

// buildMTLSTransport 构建带 mTLS 客户端证书的 HTTP Transport
func buildMTLSTransport() (*http.Transport, error) {
	transport := &http.Transport{}
	mtlsCfg := config.Configs.MTLS
	hasCert, hasKey := mtlsCfg.TLSCert != "", mtlsCfg.TLSKey != ""
	if !hasCert && !hasKey {
		return transport, nil
	}
	// 只配一半等于静默降级成明文出站：宁可让调用直接失败，也不要在不知情的情况下失去 mTLS。
	if hasCert != hasKey {
		return nil, fmt.Errorf("mTLS 的客户端证书与私钥必须成对配置（cert 已配置=%v，key 已配置=%v）", hasCert, hasKey)
	}
	certPEM, err := loadTLSContent(mtlsCfg.TLSCert)
	if err != nil {
		return nil, fmt.Errorf("加载客户端证书失败: %v", err)
	}
	keyPEM, err := loadTLSContent(mtlsCfg.TLSKey)
	if err != nil {
		return nil, fmt.Errorf("加载客户端私钥失败: %v", err)
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("解析客户端证书失败: %v", err)
	}
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}}
	if mtlsCfg.TLSCACert != "" {
		caPEM, err := loadTLSContent(mtlsCfg.TLSCACert)
		if err != nil {
			return nil, fmt.Errorf("加载 CA 证书失败: %v", err)
		}
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caPEM)
		tlsCfg.RootCAs = caCertPool
	}
	transport.TLSClientConfig = tlsCfg
	return transport, nil
}

// astraBackendUserAgent 是系统管理端访问 Astra 后端（usr-backend）时携带的 User-Agent。
// 这是服务对服务的调用，语义上属于客户端同族，因此用 AstraSchedule/ 前缀标识自己，
// 而不是面向网页端的 AstraWeb/。线上 WAF 会对不含 AstraSchedule 的 UA 发起 JS 质询，
// 而质询响应是 200 + HTML，不带标识的请求会被拦成质询页、根本到不了后端。
const astraBackendUserAgent = "AstraSchedule/System"

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
	req.Header.Set("User-Agent", astraBackendUserAgent)

	if secret := config.Configs.Astra.InternalSecret; secret != "" {
		req.Header.Set("X-Internal-Secret", secret)
	}

	transport, err := buildMTLSTransport()
	if err != nil {
		return err
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
	// WAF 的 JS 质询会返回 200 + HTML，只判断状态码会把"被拦下"当成"删表成功"。
	if contentType := resp.Header.Get("Content-Type"); !isJSONMediaType(contentType) {
		return fmt.Errorf("Astra 后端返回了非 JSON 响应（Content-Type: %s），可能被 WAF 拦截: %.200s", contentType, string(body))
	}
	return nil
}

// isJSONMediaType 判断响应媒体类型是否为 application/json。
//
// 媒体类型大小写不敏感（Application/JSON 同样是合法 JSON），参数也不能用子串匹配
// 糊弄（text/html; note="application/json" 会骗过 strings.Contains），
// 因此按 RFC 9110 解析出媒体类型后精确比较；解析失败按不匹配处理。
func isJSONMediaType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && mediaType == "application/json"
}
