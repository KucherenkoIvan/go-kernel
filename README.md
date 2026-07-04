# go-kernel

Architecture kernel for personal Go projects: DDD + hexagonal service structure, shared building blocks, and docs.

## Docs

- [Service structure](docs/architecture/1-service-structure.md) — `cmd` / `features` / `shared` layout, dependency rules
- [Domain layer](docs/architecture/2-domain-layer.md) — aggregates, entities, value objects, domain events, domain errors
- [Application layer](docs/architecture/3-application-layer.md) — ports, CQRS use-cases (command / query / policy / projector)
- [Infrastructure layer](docs/architecture/4-infrastructure-layer.md) — adapters, composition root

## Packages

Everything lives in a single Go module:

```go
import "github.com/KucherenkoIvan/go-kernel/ddd"
```

- [`ddd`](ddd/) — base building blocks the docs reference: `Event[T]` + `EventRecorder[E]`, `TxManager` / `Transaction` / `NoTransaction`, `Clock`, `IDGenerator`, `NewEntityID` (UUIDv7), `DomainError` marker. Zero dependencies.
- [`ddd/dddtest`](ddd/dddtest/) — test fakes for the ports: `FakeTxManager`, `FixedClock`, `SequenceIDGenerator`.
- [`postgres`](postgres/) — pgx v5 pool implementing `ddd.TxManager`: `Connect` / `MustConnect`, transaction token + `Resolve(tx)` returning a `Querier` (shaped to fit both scany and sqlc). See the [PostgreSQL guide](docs/guides/postgres.md).
- [`grpckit`](grpckit/) — gRPC servers/clients with the standard interceptor chain: panic recovery, request-id propagation, domain errors encoded into rich statuses and decoded back (`RemoteDomainError`), custom interceptors via `WithUnaryInterceptor`, internals never leaked. See the [gRPC guide](docs/guides/grpc.md).
- [`httpapi`](httpapi/) — JSON REST on Gin with the same guarantees: standard middleware chain, uniform `{"error":{"code",...}}` contract, `WithErrorStatus` mapping, graceful `Run`. See the [HTTP guide](docs/guides/http.md).
- [`contracts`](contracts/) — contract-first protos (buf) with committed generated code under `contracts/gen/`.
- [`auth`](auth/) — transport-agnostic `Principal` + context helpers; verification stays per-project. See the [middleware & auth guide](docs/guides/middleware-auth.md).
- [`requestid`](requestid/) — the shared request-id context, so ids survive HTTP → gRPC hops.
- [`app`](app/) — service lifecycle: adapters run concurrently, shutdown on signal or first failure, reverse-order cleanup. See the [service runtime guide](docs/guides/service-runtime.md).
- [`config`](config/) — typed env config with `.env`/`.env.local` support, defaults, required-field validation. Zero dependencies.
- [`observability`](observability/) — `NewLogger` (slog, env-configured); metrics/tracing land here later.
- [`health`](health/) — self-diagnosing checks: detailed `/healthz`, dumb `/livez`, aggregate gRPC health service.
- [`events`](events/) — commit-gated in-process event delivery (`ChannelPublisher`) for policies/projectors; outbox comes next behind the same port. See the [events guide](docs/guides/events.md).
- [`grpckit/grpckittest`](grpckit/grpckittest/) — in-memory gRPC round-trips for tests (`Serve`).

### Installing from a private repo

This repo is private, so consumers need:

```sh
go env -w GOPRIVATE=github.com/KucherenkoIvan/*
git config --global url."git@github.com:".insteadOf "https://github.com/"
```

Then a regular `go get github.com/KucherenkoIvan/go-kernel@latest` works over SSH.
