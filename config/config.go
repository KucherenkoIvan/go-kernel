// Package config loads typed configuration from environment variables, with
// .env-file support for development. Zero dependencies.
//
//	type AppConfig struct {
//		HTTPAddr    string        `env:"HTTP_ADDR" default:":8080"`
//		DatabaseURL string        `env:"DATABASE_URL" required:"true"`
//		Debug       bool          `env:"DEBUG" default:"false"`
//		Timeout     time.Duration `env:"TIMEOUT" default:"5s"`
//	}
//
//	cfg := config.MustLoad[AppConfig]()
//
// Precedence: OS environment > .env.local > .env — files never override real
// environment variables, so production stays file-proof. Config is loaded
// once in main() and passed down as a value; it is never a global.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Load populates T from the environment. envFiles defaults to
// [".env.local", ".env"]; earlier files win. Missing files are fine.
// After population, a `func (*T) Validate() error` method, if present, is
// called for cross-field rules.
func Load[T any](envFiles ...string) (*T, error) {
	if len(envFiles) == 0 {
		envFiles = []string{".env.local", ".env"}
	}

	fileVars := map[string]string{}
	for _, path := range envFiles {
		vars, err := parseEnvFile(path)
		if err != nil {
			return nil, fmt.Errorf("config: %w", err)
		}
		for k, v := range vars {
			if _, exists := fileVars[k]; !exists { // earlier files win
				fileVars[k] = v
			}
		}
	}

	lookup := func(key string) (string, bool) {
		if v, ok := os.LookupEnv(key); ok {
			return v, true
		}
		v, ok := fileVars[key]
		return v, ok
	}

	var cfg T
	var errs []error
	populate(reflect.ValueOf(&cfg).Elem(), lookup, &errs)
	if len(errs) > 0 {
		return nil, fmt.Errorf("config: %w", errors.Join(errs...))
	}

	if v, ok := any(&cfg).(interface{ Validate() error }); ok {
		if err := v.Validate(); err != nil {
			return nil, fmt.Errorf("config: validation: %w", err)
		}
	}
	return &cfg, nil
}

// MustLoad is Load that panics on error — for use in main().
func MustLoad[T any](envFiles ...string) *T {
	cfg, err := Load[T](envFiles...)
	if err != nil {
		panic(err)
	}
	return cfg
}

func populate(v reflect.Value, lookup func(string) (string, bool), errs *[]error) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field, value := t.Field(i), v.Field(i)
		if !field.IsExported() {
			continue
		}

		key, hasEnv := field.Tag.Lookup("env")
		if !hasEnv {
			// nested config sections recurse; everything else is ignored
			if field.Type.Kind() == reflect.Struct && field.Type != reflect.TypeOf(time.Time{}) {
				populate(value, lookup, errs)
			}
			continue
		}

		raw, found := lookup(key)
		if !found {
			if def, ok := field.Tag.Lookup("default"); ok {
				raw = def
			} else if field.Tag.Get("required") == "true" {
				*errs = append(*errs, fmt.Errorf("%s is required", key))
				continue
			} else {
				continue // leave zero value
			}
		}

		if err := setValue(value, raw); err != nil {
			*errs = append(*errs, fmt.Errorf("%s: %w", key, err))
		}
	}
}

func setValue(v reflect.Value, raw string) error {
	switch v.Kind() {
	case reflect.String:
		v.SetString(raw)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return fmt.Errorf("parsing bool %q: %w", raw, err)
		}
		v.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v.Type() == reflect.TypeOf(time.Duration(0)) {
			d, err := time.ParseDuration(raw)
			if err != nil {
				return fmt.Errorf("parsing duration %q: %w", raw, err)
			}
			v.SetInt(int64(d))
			return nil
		}
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return fmt.Errorf("parsing int %q: %w", raw, err)
		}
		v.SetInt(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return fmt.Errorf("parsing float %q: %w", raw, err)
		}
		v.SetFloat(f)
	case reflect.Slice:
		if v.Type().Elem().Kind() != reflect.String {
			return fmt.Errorf("unsupported slice type %s", v.Type())
		}
		parts := strings.Split(raw, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		v.Set(reflect.ValueOf(out))
	default:
		return fmt.Errorf("unsupported field type %s", v.Type())
	}
	return nil
}

// parseEnvFile reads a dotenv-style file: KEY=VALUE lines, # comments,
// optional `export ` prefixes and surrounding quotes. A missing file is not
// an error.
func parseEnvFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	vars := map[string]string{}
	for n, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%s:%d: not a KEY=VALUE line", path, n+1)
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		vars[key] = value
	}
	return vars, nil
}
