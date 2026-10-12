package config_test

import (
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KucherenkoIvan/go-kernel/config"
)

type mode string

type flagConfig struct {
	Addr    string        `env:"FCFG_ADDR" default:":8080" flag:"addr" help:"listen address"`
	Mode    mode          `env:"FCFG_MODE" default:"auto" flag:"mode"`
	Debug   bool          `env:"FCFG_DEBUG" default:"false" flag:"debug"`
	Workers int           `env:"FCFG_WORKERS" default:"4" flag:"workers"`
	Ratio   float64       `env:"FCFG_RATIO" default:"0.5" flag:"ratio"`
	Timeout time.Duration `env:"FCFG_TIMEOUT" default:"5s" flag:"timeout"`
	Peers   []string      `env:"FCFG_PEERS" default:"a,b" flag:"peers"`
	EnvOnly string        `env:"FCFG_ENV_ONLY" default:"x"`

	Section struct {
		Level string `env:"FCFG_LEVEL" default:"info" flag:"level" help:"log level"`
	}
}

func loadAndBind(t *testing.T, args ...string) (*flagConfig, *flag.FlagSet) {
	t.Helper()
	cfg, err := config.LoadWith[flagConfig](config.WithoutEnvFiles())
	if err != nil {
		t.Fatal(err)
	}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := config.BindFlags(fs, cfg); err != nil {
		t.Fatal(err)
	}
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	return cfg, fs
}

func TestBindFlags_Precedence(t *testing.T) {
	t.Setenv("FCFG_ADDR", ":7000")
	t.Setenv("FCFG_WORKERS", "8")
	t.Setenv("FCFG_LEVEL", "warn")

	cfg, _ := loadAndBind(t, "-addr", ":9000", "-level=debug")

	if cfg.Addr != ":9000" {
		t.Errorf("flag must override env: Addr = %q", cfg.Addr)
	}
	if cfg.Section.Level != "debug" {
		t.Errorf("flag must override env in a nested section: Level = %q", cfg.Section.Level)
	}
	if cfg.Workers != 8 {
		t.Errorf("unset flag must leave the env value: Workers = %d", cfg.Workers)
	}
	if cfg.Mode != "auto" {
		t.Errorf("env unset, flag unset: default must stay: Mode = %q", cfg.Mode)
	}
}

func TestBindFlags_EnvOverridesDefault(t *testing.T) {
	t.Setenv("FCFG_TIMEOUT", "250ms")
	_, fs := loadAndBind(t)
	if got := fs.Lookup("timeout").DefValue; got != "250ms" {
		t.Fatalf("flag default must be the env value, got %q", got)
	}
	if got := fs.Lookup("workers").DefValue; got != "4" {
		t.Fatalf("flag default must be the default tag without env, got %q", got)
	}
}

func TestBindFlags_AllTypes(t *testing.T) {
	cfg, _ := loadAndBind(t,
		"-mode", "server", "-debug", "-workers", "16", "-ratio", "0.25",
		"-timeout", "1m", "-peers", "x, y,z")

	if cfg.Mode != "server" || !cfg.Debug || cfg.Workers != 16 || cfg.Ratio != 0.25 ||
		cfg.Timeout != time.Minute || strings.Join(cfg.Peers, "|") != "x|y|z" {
		t.Fatalf("got %+v", cfg)
	}
}

func TestBindFlags_BadValueIsParseError(t *testing.T) {
	cfg, err := config.LoadWith[flagConfig](config.WithoutEnvFiles())
	if err != nil {
		t.Fatal(err)
	}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := config.BindFlags(fs, cfg); err != nil {
		t.Fatal(err)
	}
	if err := fs.Parse([]string{"-timeout", "soon"}); err == nil || !strings.Contains(err.Error(), "duration") {
		t.Fatalf("got %v", err)
	}
}

func TestBindFlags_HelpNamesEnvVar(t *testing.T) {
	_, fs := loadAndBind(t)
	cases := map[string]string{
		"addr":  "listen address (env FCFG_ADDR)",
		"level": "log level (env FCFG_LEVEL)",
		"mode":  "env FCFG_MODE",
	}
	for name, want := range cases {
		if got := fs.Lookup(name).Usage; got != want {
			t.Errorf("-%s usage = %q, want %q", name, got, want)
		}
	}
	if fs.Lookup("FCFG_ENV_ONLY") != nil || fs.Lookup("envonly") != nil {
		t.Error("a field without a flag tag must not get a flag")
	}

	var buf strings.Builder
	fs.SetOutput(&buf)
	fs.PrintDefaults()
	if !strings.Contains(buf.String(), "(env FCFG_ADDR)") || !strings.Contains(buf.String(), "(default :8080)") {
		t.Fatalf("PrintDefaults:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "(default false)") {
		t.Fatalf("a false bool default must not be printed, like flag.Bool:\n%s", buf.String())
	}
}

type deferredConfig struct {
	Min int `env:"DCFG_MIN" default:"10" flag:"min"`
	Max int `env:"DCFG_MAX" default:"5" flag:"max"`
}

func (c *deferredConfig) Validate() error {
	if c.Min > c.Max {
		return errors.New("min must not exceed max")
	}
	return nil
}

func TestDeferValidate_SeesFlagValues(t *testing.T) {
	// the env/default values alone are invalid; Load without the deferral fails
	if _, err := config.LoadWith[deferredConfig](config.WithoutEnvFiles()); err == nil {
		t.Fatal("undeferred load must validate")
	}

	cfg, err := config.LoadWith[deferredConfig](config.WithoutEnvFiles(), config.DeferValidate())
	if err != nil {
		t.Fatalf("deferred load must not validate: %v", err)
	}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	if err := config.BindFlags(fs, cfg); err != nil {
		t.Fatal(err)
	}
	if err := fs.Parse([]string{"-max", "20"}); err != nil {
		t.Fatal(err)
	}
	if err := config.Validate(cfg); err != nil {
		t.Fatalf("validate must see the flag value: %v", err)
	}

	cfg.Max = 1
	if err := config.Validate(cfg); err == nil || !strings.Contains(err.Error(), "config: validation: min must not exceed max") {
		t.Fatalf("got %v", err)
	}
}

func TestValidate_WithoutHookIsNil(t *testing.T) {
	if err := config.Validate(&flagConfig{}); err != nil {
		t.Fatal(err)
	}
}

func TestBindFlags_Errors(t *testing.T) {
	type duplicate struct {
		A string `flag:"name"`
		B struct {
			C string `flag:"name"`
		}
	}
	type unsupported struct {
		Port uint16            `flag:"port"`
		Map  map[string]string `flag:"map"`
	}
	type badName struct {
		A string `flag:"-a"`
		B string `flag:""`
	}
	type ok struct {
		Taken string `flag:"taken"`
		Fresh string `flag:"fresh"`
	}

	cases := []struct {
		name string
		bind func(fs *flag.FlagSet) error
		want []string
	}{
		{"duplicate", func(fs *flag.FlagSet) error { return config.BindFlags(fs, &duplicate{}) },
			[]string{"B.C: flag -name already used by A"}},
		{"unsupported", func(fs *flag.FlagSet) error { return config.BindFlags(fs, &unsupported{}) },
			[]string{"Port: unsupported field type uint16", "Map: unsupported field type map[string]string"}},
		{"bad name", func(fs *flag.FlagSet) error { return config.BindFlags(fs, &badName{}) },
			[]string{`A: flag name "-a"`, "B: empty flag name"}},
		{"already on the flag set", func(fs *flag.FlagSet) error {
			fs.String("taken", "", "")
			return config.BindFlags(fs, &ok{})
		}, []string{"Taken: flag -taken already defined on the flag set"}},
		{"not a struct", func(fs *flag.FlagSet) error { s := "x"; return config.BindFlags(fs, &s) },
			[]string{"pointer to a struct"}},
		{"nil config", func(fs *flag.FlagSet) error { return config.BindFlags[ok](fs, nil) },
			[]string{"non-nil config"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			err := tc.bind(fs)
			for _, want := range tc.want {
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Errorf("error %v does not mention %q", err, want)
				}
			}
			if fs.Lookup("fresh") != nil {
				t.Error("no flag may be registered when binding fails")
			}
		})
	}
}

func TestLoadWith_WithoutEnvFilesIgnoresWorkingDirEnv(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("TCFG_DB_URL=from-file\n"), 0o600) //nolint:errcheck
	t.Chdir(dir)

	// the default lookup reads ./.env — Load's behavior is unchanged
	cfg, err := config.Load[testConfig]()
	if err != nil || cfg.DBURL != "from-file" {
		t.Fatalf("Load() must read ./.env: %+v, %v", cfg, err)
	}
	cfg, err = config.LoadWith[testConfig]()
	if err != nil || cfg.DBURL != "from-file" {
		t.Fatalf("LoadWith() must read ./.env: %+v, %v", cfg, err)
	}

	// without env files the variable is simply missing
	_, err = config.LoadWith[testConfig](config.WithoutEnvFiles())
	if err == nil || !contains(err, "TCFG_DB_URL is required") {
		t.Fatalf("WithoutEnvFiles must not read ./.env: %v", err)
	}
	_, err = config.LoadWith[testConfig](config.WithEnvFiles())
	if err == nil || !contains(err, "TCFG_DB_URL is required") {
		t.Fatalf("WithEnvFiles() with no paths must read no file: %v", err)
	}
}

func TestLoadWith_WithEnvFiles(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "custom.env")
	os.WriteFile(file, []byte("TCFG_DB_URL=from-custom\n"), 0o600) //nolint:errcheck

	cfg, err := config.LoadWith[testConfig](config.WithEnvFiles(file))
	if err != nil || cfg.DBURL != "from-custom" {
		t.Fatalf("got %+v, %v", cfg, err)
	}
}
