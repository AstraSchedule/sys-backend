package db

import (
	"fmt"
	"os"
	"path/filepath"
	"sys-backend/config"

	gormsqlite "github.com/libtnb/sqlite"
	"github.com/sirupsen/logrus"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var SysDB *gorm.DB

func ConnectSysDB() {
	cfg := config.Configs.SysDB

	var dsn string
	switch cfg.Type {
	case "sqlite":
		if cfg.Path == "" {
			cfg.Path = "./data/sys_backend.db"
		}
		if err := os.MkdirAll(filepath.Dir(cfg.Path), 0755); err != nil {
			logrus.Fatalf("创建数据库目录失败: %v", err)
		}
		dsn = cfg.Path
	case "mysql":
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
			cfg.User, cfg.Pass, cfg.Host, cfg.Port, cfg.Name)
	default:
		logrus.Fatalf("不支持的数据库类型: %s", cfg.Type)
	}

	var err error
	switch cfg.Type {
	case "sqlite":
		SysDB, err = gorm.Open(gormsqlite.Open(dsn), &gorm.Config{})
	case "mysql":
		SysDB, err = gorm.Open(gormmysql.Open(dsn), &gorm.Config{})
	}

	if err != nil {
		logrus.Fatalf("系统数据库连接失败: %v", err)
	}

	logrus.Infof("系统数据库连接成功 (类型: %s)", cfg.Type)
}

func GetSysDB() *gorm.DB {
	return SysDB
}
