package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type Config struct {
	Server     ServerConfig     `mapstructure:"server"`
	DB         DBConfig         `mapstructure:"db"`
	SysDB      DBConfig         `mapstructure:"sys_db"`
	Astra      AstraConfig      `mapstructure:"astra"`
	Cloudflare CloudflareConfig `mapstructure:"cloudflare"`
	Log        LogConfig        `mapstructure:"log"`
}

type ServerConfig struct {
	Host   string   `mapstructure:"host"`
	Port   int      `mapstructure:"port"`
	Domain []string `mapstructure:"domain"`
}

type DBConfig struct {
	Type string `mapstructure:"type"`
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
	User string `mapstructure:"user"`
	Pass string `mapstructure:"pass"`
	Name string `mapstructure:"name"`
	Path string `mapstructure:"path"`
}

type AstraConfig struct {
	URL   string `mapstructure:"url"`
	Token string `mapstructure:"token"`
}

type CloudflareConfig struct {
	APIToken string `mapstructure:"api_token"`
	ZoneID   string `mapstructure:"zone_id"`
	Domain   string `mapstructure:"domain"`
}

type SecretConfig struct {
	Token string `mapstructure:"token"`
}

type LogConfig struct {
	Debug bool `mapstructure:"debug"`
}

var Configs Config

var configCandidates = []struct {
	path string
	typ  string
}{
	{"config.toml", "toml"},
	{"config.yaml", "yaml"},
	{"config.yml", "yaml"},
	{"config.json", "json"},
	{".env", "env"},
}

func LoadConfig() error {
	v := viper.New()

	v.SetEnvPrefix("ASTRA")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	loaded := false
	for _, c := range configCandidates {
		if _, err := os.Stat(c.path); err == nil {
			v.SetConfigFile(c.path)
			v.SetConfigType(c.typ)
			if err := v.ReadInConfig(); err != nil {
				logrus.Warnf("读取 %s 失败: %v", c.path, err)
				continue
			}
			logrus.Infof("已加载配置文件: %s", v.ConfigFileUsed())
			loaded = true
			break
		}
	}

	if !loaded {
		logrus.Info("未找到配置文件，使用环境变量配置")
	}

	envKeys := []string{
		"server.host", "server.port",
		"db.type", "db.host", "db.port", "db.user", "db.pass", "db.name", "db.path",
		"sys_db.type", "sys_db.host", "sys_db.port", "sys_db.user", "sys_db.pass", "sys_db.name", "sys_db.path",
		"astra.url", "astra.token",
		"cloudflare.api_token", "cloudflare.zone_id", "cloudflare.domain",
		"log.debug",
	}
	for _, key := range envKeys {
		if v.GetString(key) != "" {
			v.Set(key, v.GetString(key))
		}
	}

	if err := v.Unmarshal(&Configs); err != nil {
		return fmt.Errorf("解析配置失败: %w", err)
	}

	return nil
}
