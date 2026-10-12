# config: Environment Configuration

Typed config from env vars with `.env`-file support, and optional flags bound from the same struct. Package readme: [`config/README.md`](../../config/README.md).

## Declaring config

```go
type AppConfig struct {
	HTTPAddr    string        `env:"HTTP_ADDR" default:":8080"`
	GRPCAddr    string        `env:"GRPC_ADDR" default:":9090"`
	DatabaseURL string        `env:"DATABASE_URL" required:"true"`
	Debug       bool          `env:"DEBUG" default:"false"`
	Timeout     time.Duration `env:"TIMEOUT" default:"5s"`
	CORSOrigins []string      `env:"CORS_ORIGINS" default:"http://localhost:3000"` // comma-separated

	Kafka struct { // nested structs group sections; no prefixing magic
		Brokers []string `env:"KAFKA_BROKERS" required:"true"`
		GroupID string   `env:"KAFKA_GROUP_ID" required:"true"`
	}
}
```

Supported types: `string` (and named string types), `bool`, all int sizes, `float32`/`float64`, `time.Duration`, `[]string` (and slices of named string types). A field without an `env` tag is ignored (nested structs recurse).

| Tag | Meaning |
|---|---|
| `env:"NAME"` | the environment variable (and `.env` key) the field reads |
| `default:"..."` | value when the variable is unset |
| `required:"true"` | unset is an error; every missing one is reported at once |
| `flag:"name"` | `BindFlags` registers `-name` for the field |
| `help:"..."` | the flag's description; the env var is appended: `listen address (env HTTP_ADDR)` |
| `secret:"true"` | the flag's usage shows no `(default ...)`, so `-h` never prints the loaded value |

## Loading

```go
cfg := config.MustLoad[AppConfig]() // panic on error — this is main()'s first act
```

- **All** missing required variables are reported at once — one failed start, not one per restart.
- Precedence: **flags > OS environment > `.env.local` > `.env` > `default:`** (flags only when bound, see below). Files never override real env, so production is file-proof. Convention: `.env` holds shared dev defaults (committed), `.env.local` personal overrides (gitignored).
- File format: `KEY=VALUE`, `#` comments, optional `export ` prefix and surrounding quotes.

### Options: choosing the env files

`Load[T](envFiles...)` keeps its shape; `LoadWith[T](opts...)` is the same loader with options (`MustLoadWith` panics instead of returning the error):

```go
config.LoadWith[AppConfig]()                                // = Load(): .env.local, .env, then Validate
config.LoadWith[AppConfig](config.WithEnvFiles("dev.env"))  // = Load("dev.env"): these files instead
config.LoadWith[AppConfig](config.WithoutEnvFiles())        // OS environment only
```

`WithoutEnvFiles` is for daemons and system services: their working directory is wherever the service manager started them, and a stray `.env` there must not configure them. `WithEnvFiles()` with no paths also reads no file.

## Flags

A command-line tool (or a daemon whose operator passes flags) binds the same struct to a `flag.FlagSet` — the env names, defaults and parsing rules are written once, in the tags:

```go
type ServeConfig struct {
	Addr     string        `env:"HTTP_ADDR" default:":8080" flag:"addr" help:"listen address"`
	Workers  int           `env:"WORKERS" default:"4" flag:"workers" help:"worker count"`
	Debug    bool          `env:"DEBUG" flag:"debug"`
	Timeout  time.Duration `env:"TIMEOUT" default:"5s" flag:"timeout"`
	Database struct {
		URL string `env:"DATABASE_URL" flag:"db" help:"database URL"`
	}
}

func (c *ServeConfig) Validate() error {
	if c.Database.URL == "" {
		return errors.New("a database URL is required (-db or DATABASE_URL)")
	}
	return nil
}

func serve(args []string) error {
	cfg, err := config.LoadWith[ServeConfig](config.WithoutEnvFiles(), config.DeferValidate())
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	if err := config.BindFlags(fs, cfg); err != nil {
		return err
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := config.Validate(cfg); err != nil { // sees env and flags together
		return err
	}
	// ...
}
```

```
$ serve -h
  -addr value
    	listen address (env HTTP_ADDR) (default :8080)
  -db value
    	database URL (env DATABASE_URL)
  ...
```

- **Bind after loading.** A flag's default is the field's value at bind time — the env value, or the `default:` tag — so an unset flag keeps it and a set flag wins.
- **`DeferValidate` + `Validate(cfg)`.** `LoadWith` normally runs `Validate()` right away, before any flag is parsed; deferring it lets the hook see the final values. `Validate` is a no-op for types without the hook, and for a nil config.
- **`required:` is an environment check.** It runs at load time, before flags exist. A value that may come from either side (like `-db` above) leaves `required` off and is checked in `Validate()`.
- **Keep secrets off flags.** A flag's default is the loaded value, so `-h` would print a token read from the environment, and anything passed on the command line is visible to every local user through `ps`. Secrets stay `env:`-only. If a secret field must have a flag anyway, tag it `secret:"true"`: `-h` then shows no default (the value on the command line is still visible in `ps`).
- Bool flags take `-debug` / `-debug=false`; `[]string` flags take one comma-separated value that replaces the list.
- `BindFlags` returns an error — never panics — for duplicate `flag:` names, a name already defined on the `FlagSet`, a malformed name, or an unsupported field type; on error no flag is registered.

## Cross-field validation

```go
func (c *AppConfig) Validate() error {
	if c.Debug && !strings.HasPrefix(c.HTTPAddr, "localhost") {
		return errors.New("DEBUG only with a localhost bind")
	}
	return nil
}
```

Called automatically after population — or, with `DeferValidate()`, by you through `config.Validate(cfg)` once flags are parsed.

## The rules that matter more than the API

- **Load once, in `main()`, pass values down.** Constructors take `cfg.DatabaseURL`, not the config struct — and never read `os.Getenv` themselves. Grep-test: `os.Getenv` appears only in `main` packages and the kernel.
- Config is **data, not a service**: no global, no DI container, no re-reading at runtime.

## Testing

```go
func TestLoad(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://test")
	cfg, err := config.Load[AppConfig]("/nonexistent/.env") // point past real files
	// ...
}
```

## Anti-patterns

| Anti-pattern | The right way |
|-------------|---------------|
| `os.Getenv` scattered through features | One struct, loaded once, passed down |
| Global config singleton | Constructor parameters |
| `.env` files in production images | Files are a dev convenience; prod sets real env |
| Secrets with `default:` values | `required:"true"` — fail loud, never ship a default secret |
| Panic-per-missing-var discovery loops | All required vars reported in one error |
| Repeating env names as flag names by hand (`fs.StringVar(&cfg.Addr, "addr", cfg.Addr, "... (HTTP_ADDR)")` per field) | `flag:` / `help:` tags + `BindFlags` — names, defaults and parsing live in one place |
| Validating before flags are parsed, then again after | `DeferValidate()` at load, one `config.Validate(cfg)` after `fs.Parse` |
| Pointing `Load` at `os.DevNull` to skip `.env` files | `LoadWith[T](config.WithoutEnvFiles())` |
| `flag:` on a secret field (`-h` prints the env-loaded token as the default; argv shows in `ps`) | Secrets are `env:`-only; if a flag is unavoidable, `secret:"true"` keeps the value out of `-h` |
