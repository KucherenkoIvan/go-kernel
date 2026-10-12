package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KucherenkoIvan/go-kernel/config"
)

type testConfig struct {
	Addr    string        `env:"TCFG_ADDR" default:":8080"`
	DBURL   string        `env:"TCFG_DB_URL" required:"true"`
	Debug   bool          `env:"TCFG_DEBUG" default:"false"`
	Timeout time.Duration `env:"TCFG_TIMEOUT" default:"5s"`
	Tags    []string      `env:"TCFG_TAGS" default:"a,b"`

	Nested struct {
		Level string `env:"TCFG_LOG_LEVEL" default:"info"`
	}
}

func TestLoad_EnvAndDefaults(t *testing.T) {
	t.Setenv("TCFG_DB_URL", "postgres://x")
	t.Setenv("TCFG_TIMEOUT", "250ms")

	cfg, err := config.Load[testConfig]("/nonexistent/.env")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":8080" || cfg.DBURL != "postgres://x" || cfg.Debug ||
		cfg.Timeout != 250*time.Millisecond || cfg.Nested.Level != "info" {
		t.Fatalf("got %+v", cfg)
	}
	if len(cfg.Tags) != 2 || cfg.Tags[0] != "a" {
		t.Fatalf("slice default: %v", cfg.Tags)
	}
}

func TestLoad_RequiredMissing(t *testing.T) {
	_, err := config.Load[testConfig]("/nonexistent/.env")
	if err == nil || !contains(err, "TCFG_DB_URL is required") {
		t.Fatalf("got %v", err)
	}
}

func TestLoad_EnvFilePrecedence(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	localFile := filepath.Join(dir, ".env.local")

	os.WriteFile(envFile, []byte("TCFG_DB_URL=from-env-file\nTCFG_ADDR=:9999\n"), 0o600)             //nolint:errcheck
	os.WriteFile(localFile, []byte("# local overrides\nexport TCFG_DB_URL=\"from-local\"\n"), 0o600) //nolint:errcheck

	// files only: .env.local wins over .env
	cfg, err := config.Load[testConfig](localFile, envFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DBURL != "from-local" || cfg.Addr != ":9999" {
		t.Fatalf("file precedence: %+v", cfg)
	}

	// OS env beats both files
	t.Setenv("TCFG_DB_URL", "from-os")
	cfg, err = config.Load[testConfig](localFile, envFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DBURL != "from-os" {
		t.Fatalf("OS env must win: %+v", cfg)
	}
}

type validatedConfig struct {
	Min int `env:"TCFG_MIN" default:"10"`
	Max int `env:"TCFG_MAX" default:"5"`
}

func (c *validatedConfig) Validate() error {
	if c.Min > c.Max {
		return errors.New("MIN must not exceed MAX")
	}
	return nil
}

func TestLoad_ValidateHook(t *testing.T) {
	_, err := config.Load[validatedConfig]("/nonexistent/.env")
	if err == nil || !contains(err, "MIN must not exceed MAX") {
		t.Fatalf("got %v", err)
	}

	t.Setenv("TCFG_MAX", "20")
	if _, err := config.Load[validatedConfig]("/nonexistent/.env"); err != nil {
		t.Fatal(err)
	}
}

func TestLoad_BadValue(t *testing.T) {
	t.Setenv("TCFG_DB_URL", "x")
	t.Setenv("TCFG_TIMEOUT", "not-a-duration")
	_, err := config.Load[testConfig]("/nonexistent/.env")
	if err == nil || !contains(err, "TCFG_TIMEOUT") {
		t.Fatalf("got %v", err)
	}
}

func contains(err error, substr string) bool {
	return err != nil && strings.Contains(err.Error(), substr)
}

type namedList []string

func TestLoad_NamedSliceType(t *testing.T) {
	t.Setenv("TCFG_NAMED", "a, b")
	cfg, err := config.Load[struct {
		List namedList `env:"TCFG_NAMED"`
	}]("/nonexistent/.env")
	if err != nil || len(cfg.List) != 2 || cfg.List[1] != "b" {
		t.Fatalf("got %+v, %v", cfg, err)
	}
}
