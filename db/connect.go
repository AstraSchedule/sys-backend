package db

import (
	"fmt"
	"sys-backend/config"

	gormsqlite "github.com/libtnb/sqlite"
	"github.com/sirupsen/logrus"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var DB *gorm.DB

func Connect() {
	cfg := config.Configs.DB

	var dsn string
	switch cfg.Type {
	case "sqlite":
		if cfg.Path == "" {
			cfg.Path = "./data/sys_backend.db"
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
		DB, err = gorm.Open(gormsqlite.Open(dsn), &gorm.Config{})
	case "mysql":
		DB, err = gorm.Open(gormmysql.Open(dsn), &gorm.Config{})
	}

	if err != nil {
		logrus.Fatalf("数据库连接失败: %v", err)
	}

	logrus.Infof("数据库连接成功 (类型: %s)", cfg.Type)
}

func GetDB() *gorm.DB {
	return DB
}
