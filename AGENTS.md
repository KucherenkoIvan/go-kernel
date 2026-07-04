# Agent instructions

go-kernel is a personal architecture kernel: one Go module of building blocks (ddd, postgres/sqlite, grpckit/httpapi, kafka/outbox, events, cache, runtime) plus the docs describing the architecture they implement. Start with [README.md](README.md) for the full doc tree; [docs/architecture/](docs/architecture/1-service-structure.md) is the source of truth for design decisions — don't contradict it in code without flagging the conflict.

## Hard rules

1. **Privacy.** This is a personal project. Never reference the author's employer, work projects, internal repository/package names, or copy content from other private codebases — in code, docs, comments, commit messages, or examples. A local pre-commit hook scans staged changes for known-sensitive terms; never bypass it (`--no-verify`). When unsure whether a term is safe, don't write it.
2. **`ddd` stays zero-dependency** — stdlib and itself only (depguard-enforced). It is the one package domain code imports; a dependency there infects every project.
3. **`contracts/gen/` is generated** — never hand-edit. Change protos under `contracts/proto/`, then `make proto`.
4. **There is no CI — you are the CI.** `make lint` and `make test` must pass before every commit. Run `make integration` when touching anything with an integration surface (postgres, sqlite, kafka, outbox, cache, grpckit) — it needs docker; containers self-provision via testcontainers.
5. **Docs completeness contract.** Every package has a `README.md` and a guide in `docs/guides/` (code examples + an anti-patterns table); the guide links the package README; the main README links every guide; architecture docs chain via next-links. A change isn't done until the docs match it.
6. **Versioning.** Conventional commits. Tag releases `v0.MINOR.PATCH`: new API → minor bump, fixes/docs/tooling → patch.

## Conventions

- English everywhere. Transparent naming — call things what they are (`ChannelPublisher`, not `Bus`); the mechanism should be readable from the name.
- Expected business failures are `ddd.DomainError` values; transports map them and mask everything else. Never leak internals to clients.
- Tests assert behavior, not call sequences; integration infra comes from `internal/testinfra` (`TEST_*_URL` env > `INTEGRATION=1` testcontainers > skip). Prefer real components over mocks (`:memory:` sqlite, `cache.InMemory`, `dddtest`, `grpckittest`).
- Comments state non-obvious constraints only; match the existing density.
- Keep the `go.mod` floor as low as dependencies allow — it is imposed on every consumer; never bump it for taste.

## Workflow

```sh
make help          # list targets
make test          # unit tests (integration self-skips)
make lint          # gofmt + go vet + golangci-lint — required before commit
make integration   # full suite against real infra (docker)
make proto         # regenerate contracts after proto changes
make tools         # one-time dev tooling install
```
