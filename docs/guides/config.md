# config: Environment Configuration

Typed config from env vars with `.env`-file support. Package readme: [`config/README.md`](../../config/README.md).

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

Supported types: `string`, `bool`, all int sizes, `float64`, `time.Duration`, `[]string`. A field without an `env` tag is ignored (nested structs recurse).

## Loading

```go
cfg := config.MustLoad[AppConfig]() // panic on error — this is main()'s first act
```

- **All** missing required variables are reported at once — one failed start, not one per restart.
- Precedence: **OS environment > `.env.local` > `.env`**. Files never override real env, so production is file-proof. Convention: `.env` holds shared dev defaults (committed), `.env.local` personal overrides (gitignored).
- File format: `KEY=VALUE`, `#` comments, optional `export ` prefix and surrounding quotes.

## Cross-field validation

```go
func (c *AppConfig) Validate() error {
	if c.Debug && !strings.HasPrefix(c.HTTPAddr, "localhost") {
		return errors.New("DEBUG only with a localhost bind")
	}
	return nil
}
```

Called automatically after population.

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
