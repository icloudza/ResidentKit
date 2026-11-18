package config

import (
	"github.com/go-sql-driver/mysql"
	"os"
	"path/filepath"
	"testing"
)

func load(t *testing.T, yaml string) (*Config, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.yaml")
	yaml = "database:\n  user: rk\n  password: \"p@ss:w/d\"\n" + yaml
	if err := os.WriteFile(p, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STATE_DIRECTORY", "/state")
	return Load(p)
}

func TestDefaultsBehindProxy(t *testing.T) {
	c, err := load(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Gateway.Listen != "127.0.0.1:8765" || c.GatewayTLS() || !c.GatewayIsLoopback() || len(c.Gateway.TrustedProxies) != 2 {
		t.Fatalf("gateway %+v", c.Gateway)
	}
}

func TestDatabaseDSN(t *testing.T) {
	c, err := load(t, "")
	if err != nil {
		t.Fatal(err)
	}
	// 密码里的特殊字符原样保留；默认本机 3306、库名 residentkit。
	want := "rk:p@ss:w/d@tcp(127.0.0.1:3306)/residentkit?parseTime=true&charset=utf8mb4"
	if got := c.Database.DSN(); got != want {
		t.Fatalf("dsn %q, want %q", got, want)
	}
	if m, err := mysql.ParseDSN(c.Database.DSN()); err != nil || m.Passwd != "p@ss:w/d" || m.DBName != "residentkit" {
		t.Fatalf("round trip: %+v %v", m, err)
	}
}

func TestEnvOverridesSecrets(t *testing.T) {
	t.Setenv("RK_DATABASE_PASSWORD", "from-env")
	t.Setenv("RK_AUTH_JWT_KEY", "jwt-from-env")
	c, err := load(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Database.Password != "from-env" || c.Auth.JWTKey != "jwt-from-env" {
		t.Fatalf("got %q %q", c.Database.Password, c.Auth.JWTKey)
	}
}

func TestValidate(t *testing.T) {
	for _, y := range []string{
		"gateway:\n  tls_cert: /a.pem\n",         // 只填一半
		"gateway:\n  trusted_proxies: [nginx]\n", // 不是 IP
	} {
		if _, err := load(t, y); err == nil {
			t.Errorf("accepted: %q", y)
		}
	}
}
