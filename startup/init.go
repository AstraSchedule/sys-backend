package startup

import (
	"sys-backend/config"
	"sys-backend/db"
	"sys-backend/model/dbTable"
	"sys-backend/service"

	"github.com/sirupsen/logrus"
)

func StartInit() {
	if err := config.LoadConfig(); err != nil {
		logrus.Fatal(err)
	}

	if config.Configs.Log.Debug {
		logrus.SetLevel(logrus.TraceLevel)
	}

	db.Connect()
	db.ConnectSysDB()

	// Auto migrate
	db.SysDB.AutoMigrate(&dbTable.SystemUser{})

	// Create default admin user
	createDefaultAdmin()
}

func createDefaultAdmin() {
	var count int64
	db.SysDB.Model(&dbTable.SystemUser{}).Where("role = ?", "readwrite").Count(&count)
	if count == 0 {
		hash, _ := service.HashPassword("admin")
		admin := dbTable.SystemUser{
			Username:      "admin",
			PasswordHash:  hash,
			Role:          "readwrite",
			MustChangePwd: true,
		}
		db.SysDB.Create(&admin)
		logrus.Info("已创建默认管理员账户: admin/admin")
	}
}
