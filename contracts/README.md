# contracts

Shared contracts (contract-first), organized by kind:

```
proto/
  grpc/<domain>/v1/     # gRPC service contracts    (package grpc.<domain>.v1)
  events/<domain>/v1/   # Kafka event contracts     (package events.<domain>.v1)
gen/                    # committed generated Go code, mirrors proto/
kafkacontracts/         # topic ↔ message-type pairings for the events
```

Consumers import the generated code directly from the module and never run codegen themselves:

```go
import examplev1 "github.com/KucherenkoIvan/go-kernel/contracts/gen/grpc/example/v1"
import eventsexamplev1 "github.com/KucherenkoIvan/go-kernel/contracts/gen/events/example/v1"
```

The `changeme` contract belongs to [go-tinycore-template](https://github.com/KucherenkoIvan/go-tinycore-template) — it lives here so the template compiles out of the box.

## Adding or changing a contract

One-time tooling setup (buf executes these as local plugins — no protoc needed):

```sh
go install github.com/bufbuild/buf/cmd/buf@latest
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

```sh
cd contracts
buf lint                # style/naming rules (STANDARD)
buf breaking --against '.git#subdir=contracts'   # no breaking changes vs committed state
buf generate            # regenerate gen/
```

Commit `proto/` and `gen/` together.

## Rules

- One directory per bounded context under its kind, versioned from day one: `proto/grpc/<domain>/v1/`, `proto/events/<domain>/v1/`.
- Breaking changes require a new version directory (`v2`), not edits to `v1`.
- `gen/` is never edited by hand.
