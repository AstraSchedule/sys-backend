package web

import (
	"sys-backend/config"
	"sys-backend/db"
	"sys-backend/model/dbTable"
	"sys-backend/service"

	"github.com/gin-gonic/gin"
	gormsqlite "github.com/libtnb/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDBInitialized = false

func ensureTestDB() {
	if testDBInitialized {
		return
	}

	// Set up config
	config.Configs = config.Config{
		Server: config.ServerConfig{
			Host:   "127.0.0.1",
			Port:   9000,
			Domain: []string{"http://localhost"},
		},
		Astra: config.AstraConfig{
			Token: "test-token",
			URL:   "http://localhost:9000",
		},
	}

	// Initialize sys database
	sysDB, err := gorm.Open(gormsqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		panic(err)
	}
	sysDB.AutoMigrate(&dbTable.SystemUser{})
	db.SysDB = sysDB

	// Initialize astra database (empty for now)
	astraDB, err := gorm.Open(gormsqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		panic(err)
	}
	db.DB = astraDB

	testDBInitialized = true
}

func setupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	return router
}

func setupTestUser() *dbTable.SystemUser {
	// Delete existing test user first
	db.SysDB.Where("username = ?", "testuser").Delete(&dbTable.SystemUser{})

	hash, _ := service.HashPassword("test123")
	user := &dbTable.SystemUser{
		Username:     "testuser",
		PasswordHash: hash,
		Role:         "readwrite",
	}
	db.SysDB.Create(user)
	return user
}
