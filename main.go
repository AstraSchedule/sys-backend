package main

import (
	"fmt"
	"sys-backend/config"
	"sys-backend/router"
	"sys-backend/startup"

	"github.com/sirupsen/logrus"
)

func main() {
	startup.StartInit()

	logrus.Infof("程序初始化流程结束，即将启动 HTTP 服务：%+v", config.Configs)

	r := router.Setup()

	err := r.Run(fmt.Sprintf("%s:%d", config.Configs.Server.Host, config.Configs.Server.Port))
	if err != nil {
		logrus.Fatal(err.Error())
		return
	}
}
