package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type Config struct {
	Server ServerConfig `mapstructure:"server"`
	DB     DBConfig     `mapstructure:"db"`
	SysDB  DBConfig     `mapstructure:"sys_db"`
	Astra  AstraConfig  `mapstructure:"astra"`
	ESA    ESAConfig    `mapstructure:"esa"`
	MTLS   MTLSConfig   `mapstructure:"mtls"`
	Log    LogConfig    `mapstructure:"log"`
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
	URL            string `mapstructure:"url"`
	Token          string `mapstructure:"token"`
	InternalSecret string `mapstructure:"internal_secret"`
}

// ESAConfig 是阿里云边缘安全加速（ESA）站点内租户 DNS 记录的配置。
//
// 站点已由 Cloudflare 迁到 ESA（NS 接入），租户记录改由 ESA OpenAPI 维护。
type ESAConfig struct {
	AccessKeyID     string `mapstructure:"access_key_id"`
	AccessKeySecret string `mapstructure:"access_key_secret"`
	SiteID          int64  `mapstructure:"site_id"`
	// SiteName 是站点根域名，用于拼出完整记录名。
	SiteName string `mapstructure:"site_name"`
	// Target 是回源目标。ESA 开启代理加速时会把回源目标解析成实际 IP 再回源，
	// 因此这里必须填「源地址池」（esa.source_type=OP），不能填站点内的域名，否则自环。
	Target string `mapstructure:"target"`
	// Proxied 控制是否开启 ESA 代理加速。纯 DNS 解析时置 false。
	Proxied bool `mapstructure:"proxied"`
	// BizName 是加速业务场景，开启代理时必填：image_video / api / web。
	BizName string `mapstructure:"biz_name"`
	// SourceType 是回源类型，CNAME 记录可选 OP / Domain / OSS / S3 / LB。
	SourceType string `mapstructure:"source_type"`
	TTL        int32  `mapstructure:"ttl"`
	Endpoint   string `mapstructure:"endpoint"`
	// TenantComment 是租户记录的备注标记。
	//
	// 站点里既有租户记录，也有 class/to/sys 这类基础设施记录，靠备注把两者分开；
	// 历史记录没有备注，再按「域名在 Astra 数据库里有对应 namespace」兜底识别。
	TenantComment string `mapstructure:"tenant_comment"`
}

// MTLSConfig 是调用 Astra 后端时使用的出站 mTLS 客户端证书。
type MTLSConfig struct {
	TLSCert   string `mapstructure:"tls_cert"`
	TLSKey    string `mapstructure:"tls_key"`
	TLSCACert string `mapstructure:"tls_ca_cert"`
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
		"astra.url", "astra.token", "astra.internal_secret",
		"esa.access_key_id", "esa.access_key_secret", "esa.site_id", "esa.site_name",
		"esa.target", "esa.proxied", "esa.biz_name", "esa.source_type", "esa.ttl",
		"esa.endpoint", "esa.tenant_comment",
		"mtls.tls_cert", "mtls.tls_key", "mtls.tls_ca_cert",
		"log.debug",
	}
	for _, key := range envKeys {
		if v.GetString(key) != "" {
			v.Set(key, v.GetString(key))
		}
	}

	// 默认值集中在这里，避免调用侧到处写「空值就用某某」。
	v.SetDefault("esa.proxied", true)
	v.SetDefault("esa.biz_name", "api")
	v.SetDefault("esa.source_type", "OP")
	v.SetDefault("esa.ttl", 1)
	v.SetDefault("esa.endpoint", "esa.cn-hangzhou.aliyuncs.com")
	v.SetDefault("esa.tenant_comment", "SaaS")

	if err := v.Unmarshal(&Configs); err != nil {
		return fmt.Errorf("解析配置失败: %w", err)
	}

	return nil
}
