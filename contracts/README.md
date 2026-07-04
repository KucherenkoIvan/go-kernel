# contracts

Shared gRPC contracts (contract-first). Protos live in `proto/`, generated Go code is committed under `gen/` — consumers import it directly from the module and never run codegen themselves:

```go
import examplev1 "github.com/KucherenkoIvan/go-kernel/contracts/gen/example/v1"
```

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

- One directory per bounded context, versioned from day one: `proto/<domain>/v1/`.
- Breaking changes require a new version directory (`v2`), not edits to `v1`.
- `gen/` is never edited by hand.
