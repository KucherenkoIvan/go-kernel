# config

Typed configuration from environment variables, with `.env`-file support for development. Zero dependencies. Loaded once in `main()`, passed down as a value — never a global.

- `Load[T]` / `MustLoad[T]` — populate a struct from the environment
- Tags: `env:"NAME"`, `default:"..."`, `required:"true"` — all missing required vars reported at once
- Types: string, bool, ints, float64, `time.Duration`, `[]string`; nested structs group sections
- Precedence: **OS env > `.env.local` > `.env`** — files never override real env
- Optional `Validate() error` method for cross-field rules

```go
type AppConfig struct {
	HTTPAddr    string        `env:"HTTP_ADDR" default:":8080"`
	DatabaseURL string        `env:"DATABASE_URL" required:"true"`
	Timeout     time.Duration `env:"TIMEOUT" default:"5s"`
}

cfg := config.MustLoad[AppConfig]()
```

Guide: [docs/guides/config.md](../docs/guides/config.md)
