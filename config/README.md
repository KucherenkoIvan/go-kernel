# config

Typed configuration from environment variables, with `.env`-file support for development and optional command-line flags. Zero dependencies. Loaded once in `main()`, passed down as a value — never a global.

- `Load[T]` / `MustLoad[T]` — populate a struct from the environment (`.env.local`, `.env` by default)
- `LoadWith[T](opts...)` / `MustLoadWith[T]` — the same with options: `WithEnvFiles(paths...)`, `WithoutEnvFiles()`, `DeferValidate()`
- `BindFlags(fs, cfg)` — one `flag.FlagSet` flag per `flag:"name"` field; the flag's default is the loaded value, its usage names the env var
- `Validate(cfg)` — run the `Validate()` hook yourself, after flags are parsed (with `DeferValidate`)
- Tags: `env:"NAME"`, `default:"..."`, `required:"true"`, `flag:"name"`, `help:"..."`, `secret:"true"` (no default in `-h`) — all missing required vars reported at once
- Types: string, bool, ints, floats, `time.Duration`, `[]string` (named string and slice types too); nested structs group sections
- Secrets stay `env:`-only: a flag's default would print them in `-h`, and argv is visible in `ps`
- `required:` checks the environment at load time; a value that may come from a flag is checked in `Validate()` instead
- Precedence: **flags > OS env > `.env.local` > `.env` > `default:`** — files never override real env
- Optional `Validate() error` method for cross-field rules

```go
type AppConfig struct {
	HTTPAddr    string        `env:"HTTP_ADDR" default:":8080" flag:"addr" help:"listen address"`
	DatabaseURL string        `env:"DATABASE_URL" required:"true"`
	Timeout     time.Duration `env:"TIMEOUT" default:"5s"`
}

cfg := config.MustLoad[AppConfig]()
```

With flags (a CLI or a daemon that reads no `.env` files):

```go
cfg, err := config.LoadWith[AppConfig](config.WithoutEnvFiles(), config.DeferValidate())
if err != nil { ... }
fs := flag.NewFlagSet("serve", flag.ContinueOnError)
if err := config.BindFlags(fs, cfg); err != nil { ... }
if err := fs.Parse(args); err != nil { ... }
if err := config.Validate(cfg); err != nil { ... }
```

Guide: [docs/guides/config.md](../docs/guides/config.md)
