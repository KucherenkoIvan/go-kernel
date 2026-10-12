package config

import (
	"errors"
	"flag"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// BindFlags registers one flag on fs per field of cfg carrying a
// `flag:"name"` tag, nested sections included. Each flag writes straight into
// its field, and its default is the field's current value — so bind after
// LoadWith and a flag left unset keeps the environment's value, while a flag
// that is set wins over it. The usage line is the optional `help:"..."` tag
// followed by the field's env var, e.g. "listen address (env HTTP_ADDR)".
// A field tagged `secret:"true"` keeps its loaded value out of the usage
// output (no "(default ...)"); better still, keep secrets off flags entirely,
// since argv is visible to other users through ps.
//
// Supported field types are the env types: string, bool, ints, floats,
// time.Duration and []string (comma-separated; a flag replaces the whole
// list). Bool flags take the -name / -name=false form. Duplicate names,
// names already defined on fs, malformed names and unsupported types are
// all reported in the returned error, and then no flag is registered.
func BindFlags[T any](fs *flag.FlagSet, cfg *T) error {
	if fs == nil || cfg == nil {
		return errors.New("config: BindFlags needs a FlagSet and a non-nil config")
	}
	v := reflect.ValueOf(cfg).Elem()
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("config: BindFlags needs a pointer to a struct, got %s", v.Type())
	}

	var binds []flagBinding
	var errs []error
	collectFlags(v, "", &binds, &errs)

	seen := map[string]string{}
	for _, b := range binds {
		switch {
		case b.name == "":
			errs = append(errs, fmt.Errorf("%s: empty flag name", b.path))
		case strings.HasPrefix(b.name, "-") || strings.Contains(b.name, "="):
			errs = append(errs, fmt.Errorf("%s: flag name %q must not start with - or contain =", b.path, b.name))
		case seen[b.name] != "":
			errs = append(errs, fmt.Errorf("%s: flag -%s already used by %s", b.path, b.name, seen[b.name]))
		case fs.Lookup(b.name) != nil:
			errs = append(errs, fmt.Errorf("%s: flag -%s already defined on the flag set", b.path, b.name))
		}
		if seen[b.name] == "" {
			seen[b.name] = b.path
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("config: %w", errors.Join(errs...))
	}

	for _, b := range binds {
		var fv flag.Value = &fieldValue{v: b.value}
		zero := "" // what the flag package treats as "no default to print"
		if b.value.Kind() == reflect.Bool {
			fv, zero = &boolFieldValue{fieldValue{v: b.value}}, "false"
		}
		fs.Var(fv, b.name, b.usage)
		if b.secret { // the loaded value must not reach -h output
			fs.Lookup(b.name).DefValue = zero
		}
	}
	return nil
}

type flagBinding struct {
	name, usage, path string
	secret            bool
	value             reflect.Value
}

func collectFlags(v reflect.Value, prefix string, binds *[]flagBinding, errs *[]error) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field, value := t.Field(i), v.Field(i)
		if !field.IsExported() {
			continue
		}
		path := prefix + field.Name

		name, hasFlag := field.Tag.Lookup("flag")
		if !hasFlag {
			_, hasEnv := field.Tag.Lookup("env")
			if !hasEnv && field.Type.Kind() == reflect.Struct && field.Type != reflect.TypeOf(time.Time{}) {
				collectFlags(value, path+".", binds, errs)
			}
			continue
		}

		if !supportedKind(field.Type) {
			*errs = append(*errs, fmt.Errorf("%s: unsupported field type %s for flag -%s", path, field.Type, name))
			continue
		}

		usage := field.Tag.Get("help")
		if env, ok := field.Tag.Lookup("env"); ok {
			if usage == "" {
				usage = "env " + env
			} else {
				usage += " (env " + env + ")"
			}
		}
		*binds = append(*binds, flagBinding{
			name: name, usage: usage, path: path, value: value,
			secret: field.Tag.Get("secret") == "true",
		})
	}
}

// supportedKind mirrors setValue's cases.
func supportedKind(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Float32, reflect.Float64:
		return true
	case reflect.Slice:
		return t.Elem().Kind() == reflect.String
	default:
		return false
	}
}

// fieldValue is a flag.Value writing into one struct field, parsed by the
// same rules as the field's env var.
type fieldValue struct{ v reflect.Value }

func (f *fieldValue) Set(raw string) error { return setValue(f.v, raw) }

func (f *fieldValue) String() string {
	if f == nil || !f.v.IsValid() { // flag's zero-value probe
		return ""
	}
	switch f.v.Kind() {
	case reflect.String:
		return f.v.String()
	case reflect.Bool:
		return strconv.FormatBool(f.v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if f.v.Type() == reflect.TypeOf(time.Duration(0)) {
			return time.Duration(f.v.Int()).String()
		}
		return strconv.FormatInt(f.v.Int(), 10)
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(f.v.Float(), 'g', -1, f.v.Type().Bits())
	case reflect.Slice:
		parts := make([]string, f.v.Len())
		for i := range parts {
			parts[i] = f.v.Index(i).String()
		}
		return strings.Join(parts, ",")
	}
	return ""
}

// boolFieldValue takes the -name form, and its zero value prints "false" so
// usage output omits "(default false)", as for flag.Bool.
type boolFieldValue struct{ fieldValue }

func (*boolFieldValue) IsBoolFlag() bool { return true }

func (f *boolFieldValue) String() string {
	if f == nil || !f.v.IsValid() {
		return "false"
	}
	return f.fieldValue.String()
}
