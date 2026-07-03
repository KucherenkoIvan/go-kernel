# go-kernel

Architecture kernel for personal Go projects: DDD + hexagonal service structure, shared building blocks, and docs.

## Docs

- [Service structure](docs/architecture/1-service-structure.md) — `cmd` / `features` / `shared` layout, dependency rules
- [Domain layer](docs/architecture/2-domain-layer.md) — aggregates, entities, value objects, domain events, domain errors
- [Application layer](docs/architecture/3-application-layer.md) — ports, CQRS use-cases (command / query / policy / projector)
- [Infrastructure layer](docs/architecture/4-infrastructure-layer.md) — adapters, composition root

## Packages

Planned (not published yet). Everything lives in a single Go module, so once a package lands it is importable directly:

```go
import "github.com/KucherenkoIvan/go-kernel/ddd"
```

- `ddd` — base building blocks the docs reference: `EventRecorder`, `Event`, `TxManager` / `Transaction` / `NoTransaction`, `Clock`, `IDGenerator`, `NewEntityID` (UUIDv7), `DomainError` marker + test fakes.

### Installing from a private repo

This repo is private, so consumers need:

```sh
go env -w GOPRIVATE=github.com/KucherenkoIvan/*
git config --global url."git@github.com:".insteadOf "https://github.com/"
```

Then a regular `go get github.com/KucherenkoIvan/go-kernel@latest` works over SSH.
