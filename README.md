# go-kernel

An architecture kernel for personal Go projects: one set of docs describing how services are built (DDD + hexagonal architecture, features as bounded contexts, CQRS use-cases, ports & adapters), and one Go module of building blocks implementing everything the docs reference — transactions, events, transports, persistence, runtime.

The point: every project starts with the architecture already decided and the plumbing already written and tested. A feature is domain logic + thin adapters; everything else is imported.

## Possible setups

The same architecture scales down as well as up — a feature's domain and application code cannot tell which setup it runs in:

| | Full fleet | Single binary |
|---|---|---|
| Database | `postgres` (pgx) | `sqlite` (embedded, no container) |
| Cache | `cache.Redis` | `cache.InMemory` |
| Events | `outbox` → `kafka` (at-least-once, cross-service) | `events.ChannelPublisher` (commit-gated, in-process) |
| APIs | `grpckit` + `httpapi` | `httpapi` (and `grpckit` if wanted) |
| Deploy | containers/k8s | one static executable + one file |

Mix freely — e.g. postgres + channel publisher is a fine middle. The runtime four (`app`, `config`, `observability`, `health`) are identical in every setup.

## Docs

### Architecture

Start here — the docs chain into each other:

1. [Service structure](docs/architecture/1-service-structure.md) — `cmd` / `features` / `shared` layout, dependency rules
2. [Domain layer](docs/architecture/2-domain-layer.md) — aggregates, entities, value objects, events, domain errors
3. [Application layer](docs/architecture/3-application-layer.md) — ports, CQRS use-cases (command / query / policy / projector)
4. [Infrastructure layer](docs/architecture/4-infrastructure-layer.md) — adapters, composition root

### Package guides

Each guide links its package's readme; each package readme links back.

- [ddd](docs/guides/ddd.md) — the base building blocks: aggregates' event recorder, typed events, the transaction port/token, determinism ports, domain-error marker (+ `dddtest` fakes)
- [service runtime](docs/guides/service-runtime.md) — how `main.go` composes; the assembly view of the four below
  - [app](docs/guides/app.md) — lifecycle: concurrent adapters, shutdown on signal or first failure, reverse-order cleanup
  - [config](docs/guides/config.md) — typed env config with `.env`/`.env.local`, defaults, required-field validation
  - [observability](docs/guides/observability.md) — the service logger (slog, env-configured or with an explicit writer/level/format); metrics/tracing land here later
  - [health](docs/guides/health.md) — self-diagnosing checks: deep `/healthz` with per-dependency details, dumb `/livez`, gRPC aggregate
- [postgres](docs/guides/postgres.md) — pgx pool as the TxManager: token/`Resolve`, `AfterCommit` hooks, migration runner
- [sqlite](docs/guides/sqlite.md) — the embedded twin for single-binary deployments (modernc, no cgo/container)
- [cache](docs/guides/cache.md) — the caching port; Redis and in-memory stores, cached-Reader pattern
- [events](docs/guides/events.md) — commit-gated in-process event delivery to policies/projectors (`ChannelPublisher`)
- [kafka](docs/guides/kafka.md) — contract-first messaging on franz-go: `Contract[T]`, typed produce/consume, at-least-once semantics
- [outbox](docs/guides/outbox.md) — the transactional outbox: enqueue atomic with the business tx, relay modes, monitoring
- [grpc](docs/guides/grpc.md) — `grpckit` servers/clients with the standard interceptor chain + buf-managed `contracts/`
- [http](docs/guides/http.md) — `httpapi` JSON REST on Gin: uniform error contract, the `gin.Context` containment rule
- [web UI](docs/guides/webui.md) — `webui` server-rendered HTML + htmx: embedded templates, HTML error pages coexisting with the JSON contract
- [middleware & auth](docs/guides/middleware-auth.md) — guards vs gateways, the `auth.Principal` pattern on both transports
- [requestid](docs/guides/requestid.md) — one request id across every hop (HTTP → gRPC → outbox → Kafka)

## Install

Everything is one Go module; packages are subdirectories:

```go
import "github.com/KucherenkoIvan/go-kernel/ddd"
```

The repo is private, so consumers need (once):

```sh
go env -w GOPRIVATE=github.com/KucherenkoIvan/*
git config --global url."git@github.com:".insteadOf "https://github.com/"
```

Then a regular `go get github.com/KucherenkoIvan/go-kernel@latest` works over SSH. Versioning: `v0.x` feature-slice tags; the API may still move between minors.

## Development

No CI by choice — verification is local and encoded in the [Makefile](Makefile):

```sh
make tools        # one-time: buf, protoc plugins, golangci-lint
make test         # unit tests (integration tests self-skip)
make lint         # gofmt + go vet + golangci-lint (.golangci.yml)
make proto        # regenerate contracts
make integration  # full suite; postgres/redpanda/redis self-provision via testcontainers
```

Integration tests resolve their infrastructure in order: explicit `TEST_*_URL` env (your own containers, fastest iteration) → testcontainers when `INTEGRATION=1` → skip. See [internal/testinfra](internal/testinfra/README.md).

The linter also guards one architectural invariant: the `ddd` package must stay zero-dependency (depguard rule).

## License

[0BSD](LICENSE) — do whatever you want with it, no conditions, no attribution required; provided as-is with no warranty or liability.
