// Package config 加载服务配置。
//
// 全部配置（含数据库连接、JWT 密钥）来自一个 YAML 文件，可用 RK_* 环境变量覆盖，
// 如 RK_DATABASE_PASSWORD、RK_AUTH_JWT_KEY。配置文件含密钥，权限设为仅服务用户可读。
package config

import (
	"errors"
	"io/fs"
	"regexp"
	"strconv"

	"fmt"
	"github.com/go-sql-driver/mysql"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Log      LogConfig      `mapstructure:"log"`
	Gateway  GatewayConfig  `mapstructure:"gateway"`
	Console  ConsoleConfig  `mapstructure:"console"`
	Database DatabaseConfig `mapstructure:"database"`
	Auth     AuthConfig     `mapstructure:"auth"`
	Invoke   InvokeConfig   `mapstructure:"invoke"`
	Retain   RetainConfig   `mapstructure:"retain"`
	Release  ReleaseConfig  `mapstructure:"release"`
	Repo     RepoConfig     `mapstructure:"repo"`
	Enroll   EnrollConfig   `mapstructure:"enroll"`
}

// EnrollConfig 设备接入。AppScheme 是 App 注册的 URL scheme，控制台「添加设备」的二维码用它打开 App。
type EnrollConfig struct {
	AppScheme string `mapstructure:"app_scheme"`
}

// RepoConfig 软件源：/repo/<令牌>/ 供 Sileo 等添加为源，首次安装用（已装设备走 OTA）。
// 关闭时整个 /repo/ 路径返回 404。安装链接在控制台「发布」页生成。
type RepoConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Label   string `mapstructure:"label"` // Sileo 里显示的源名称
}

// ReleaseConfig OTA 发布。公钥为空时不能上传版本；私钥只在发布者本机。
type ReleaseConfig struct {
	PublicKey string `mapstructure:"public_key"` // rk-release pubkey 的输出
	Dir       string `mapstructure:"dir"`        // 安装包存放目录，默认 $STATE_DIRECTORY/releases
}

type LogConfig struct {
	Level  string `mapstructure:"level"`  // debug | info | warn | error
	Format string `mapstructure:"format"` // json | console
}

// GatewayConfig 服务端口与设备接入。设备连接、开放 API、控制台都走 Listen 这一个端口。
type GatewayConfig struct {
	Listen           string        `mapstructure:"listen"`
	Path             string        `mapstructure:"path"`
	IdleTimeout      time.Duration `mapstructure:"idle_timeout"`
	HandshakeTimeout time.Duration `mapstructure:"handshake_timeout"`
	MaxMessageBytes  int64         `mapstructure:"max_message_bytes"`
	MaxConnections   int           `mapstructure:"max_connections"`
	MaxPerIP         int           `mapstructure:"max_per_ip"`
	BanAfter         int           `mapstructure:"ban_after"`
	BanWindow        time.Duration `mapstructure:"ban_window"`
	BanFor           time.Duration `mapstructure:"ban_for"`
	// PublicURL 外部访问网关的地址（如 https://rk.example.com），控制台据此显示开放 API 的地址。
	PublicURL string `mapstructure:"public_url"`
	// TLSCert / TLSKey 证书文件路径。前面有 Nginx 等反代负责 HTTPS 时留空（网关走明文，只监听本机）；
	// 没有反代、设备直连网关时填上。
	TLSCert string `mapstructure:"tls_cert"`
	TLSKey  string `mapstructure:"tls_key"`
	// TrustedProxies 反代的地址：来自这些地址的请求按 X-Real-IP / X-Forwarded-For 取设备真实 IP
	// （连接数限制、封禁都按真实 IP 算）。
	TrustedProxies []string `mapstructure:"trusted_proxies"`
}

// ConsoleConfig 控制台。控制台和设备接入共用 gateway.listen 一个端口。
type ConsoleConfig struct {
	TrustedOrigin []string `mapstructure:"trusted_origins"` // 额外允许的 Origin
}

type DatabaseConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	User            string        `mapstructure:"user"`
	Password        string        `mapstructure:"password"`
	Name            string        `mapstructure:"name"` // 数据库名
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
	SlowThreshold   time.Duration `mapstructure:"slow_threshold"`
}

type AuthConfig struct {
	JWTKey          string        `mapstructure:"jwt_key"`  // 会话签名密钥，至少 32 个字符
	TOTPKey         string        `mapstructure:"totp_key"` // 两步验证密钥的加密密钥，至少 32 个字符；设好后不要再改
	AccessTTL       time.Duration `mapstructure:"access_ttl"`
	RefreshTTL      time.Duration `mapstructure:"refresh_ttl"`
	MaxFailures     int           `mapstructure:"max_failures"`
	LockFor         time.Duration `mapstructure:"lock_for"`
	LoginRatePerMin int64         `mapstructure:"login_rate_per_min"`
	APIRatePerMin   int64         `mapstructure:"api_rate_per_min"`
}

type InvokeConfig struct {
	DefaultTTL time.Duration `mapstructure:"default_ttl"` // 离线待发指令的有效期
	WaitMax    time.Duration `mapstructure:"wait_max"`    // 同步等待结果的最长时间
}

type RetainConfig struct {
	Events      time.Duration `mapstructure:"events"`
	Invocations time.Duration `mapstructure:"invocations"`
	Audit       time.Duration `mapstructure:"audit"`
}

func defaults(v *viper.Viper) {
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")

	v.SetDefault("gateway.listen", "127.0.0.1:8765")
	v.SetDefault("gateway.trusted_proxies", []string{"127.0.0.1", "::1"})
	v.SetDefault("gateway.tls_cert", "")
	v.SetDefault("gateway.tls_key", "")
	v.SetDefault("gateway.public_url", "")
	v.SetDefault("gateway.path", "/rk")
	v.SetDefault("gateway.idle_timeout", "120s")
	v.SetDefault("gateway.handshake_timeout", "10s")
	v.SetDefault("gateway.max_message_bytes", 64*1024)
	v.SetDefault("gateway.max_connections", 1024)
	v.SetDefault("gateway.max_per_ip", 8)
	v.SetDefault("gateway.ban_after", 5)
	v.SetDefault("gateway.ban_window", "10m")
	v.SetDefault("gateway.ban_for", "30m")

	// 声明了默认值，RK_DATABASE_PASSWORD 等环境变量才能覆盖
	v.SetDefault("database.host", "127.0.0.1")
	v.SetDefault("database.port", 3306)
	v.SetDefault("database.user", "")
	v.SetDefault("database.password", "")
	v.SetDefault("database.name", "residentkit")
	v.SetDefault("auth.jwt_key", "")
	v.SetDefault("auth.totp_key", "")
	v.SetDefault("database.max_open_conns", 20)
	v.SetDefault("database.max_idle_conns", 5)
	v.SetDefault("database.conn_max_lifetime", "30m")
	v.SetDefault("database.slow_threshold", "300ms")

	v.SetDefault("auth.access_ttl", "15m")
	v.SetDefault("auth.refresh_ttl", "168h")
	v.SetDefault("auth.max_failures", 5)
	v.SetDefault("auth.lock_for", "15m")
	v.SetDefault("auth.login_rate_per_min", 10)
	v.SetDefault("auth.api_rate_per_min", 600)

	v.SetDefault("invoke.default_ttl", "10m")
	v.SetDefault("invoke.wait_max", "30s")

	v.SetDefault("enroll.app_scheme", "residentkit")
	v.SetDefault("repo.enabled", false)
	v.SetDefault("repo.label", "ResidentKit")

	v.SetDefault("retain.events", "720h")
	v.SetDefault("retain.invocations", "2160h")
	v.SetDefault("retain.audit", "0s") // 默认永久保留：运行账号对审计表只有插入与查询权限
}

// Load 读取 YAML（可选）并应用 RK_* 环境变量。
func Load(path string) (*Config, error) {
	v := viper.New()
	defaults(v)
	v.SetEnvPrefix("RK")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("找不到配置文件 %s：把 config.yaml 放在程序同目录", path)
			}
			return nil, fmt.Errorf("读取配置文件 %s 失败: %w", path, err)
		}
	}
	var c Config
	if err := v.Unmarshal(&c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if c.Release.Dir == "" {
		// systemd StateDirectory= 时放在那里；否则放在配置文件旁边的 data/。
		if base := os.Getenv("STATE_DIRECTORY"); base != "" {
			c.Release.Dir = filepath.Join(base, "releases")
		} else if path != "" {
			c.Release.Dir = filepath.Join(filepath.Dir(path), "data", "releases")
		} else {
			c.Release.Dir = filepath.Join("data", "releases")
		}
	}
	return &c, c.validate()
}

func (c *Config) validate() error {
	if c.Database.User == "" || c.Database.Name == "" {
		return fmt.Errorf("database.user / database.name 未配置")
	}
	if !appSchemeRe.MatchString(c.Enroll.AppScheme) {
		return fmt.Errorf("enroll.app_scheme %q 不是合法的 URL scheme", c.Enroll.AppScheme)
	}
	if (c.Gateway.TLSCert == "") != (c.Gateway.TLSKey == "") {
		return fmt.Errorf("gateway.tls_cert 和 gateway.tls_key 要同时填写")
	}
	for _, p := range c.Gateway.TrustedProxies {
		if net.ParseIP(p) == nil {
			if _, _, err := net.ParseCIDR(p); err != nil {
				return fmt.Errorf("gateway.trusted_proxies: %q 不是 IP 或网段", p)
			}
		}
	}
	if c.Gateway.MaxMessageBytes < 1024 {
		return fmt.Errorf("gateway.max_message_bytes too small")
	}
	return nil
}

var appSchemeRe = regexp.MustCompile(`^[a-z][a-z0-9+.-]{1,31}$`)

func isLoopback(addr string) bool {
	return strings.HasPrefix(addr, "127.") || strings.HasPrefix(addr, "localhost:") || strings.HasPrefix(addr, "[::1]:")
}

// GatewayTLS 网关是否自己做 TLS（否则由反代负责）。
func (c *Config) GatewayTLS() bool { return c.Gateway.TLSCert != "" }

// HTTPS 浏览器是否经 HTTPS 访问（服务自己做 TLS，或对外地址是 https）：决定 Cookie 的 Secure 与 HSTS。
func (c *Config) HTTPS() bool {
	return c.GatewayTLS() || strings.HasPrefix(c.Gateway.PublicURL, "https://")
}

// GatewayIsLoopback 网关是否只监听本机（前面有反代）。
func (c *Config) GatewayIsLoopback() bool { return isLoopback(c.Gateway.Listen) }

// DSN 运行账号的连接串。时间按 UTC 读写，字符集 utf8mb4。
func (d DatabaseConfig) DSN() string {

	m := mysql.NewConfig()
	m.User, m.Passwd, m.DBName = d.User, d.Password, d.Name
	m.Net, m.Addr = "tcp", net.JoinHostPort(d.Host, strconv.Itoa(d.Port))
	m.ParseTime, m.Loc = true, time.UTC
	m.Params = map[string]string{"charset": "utf8mb4"}
	return m.FormatDSN()
}
