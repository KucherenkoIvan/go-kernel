# go-kernel development targets. `make help` lists them.

GOBIN := $(shell go env GOPATH)/bin

.PHONY: help test lint fmt proto tools integration

help: ## list targets
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-14s %s\n", $$1, $$2}'

test: ## unit tests (integration tests self-skip)
	go test ./...

lint: ## gofmt check + go vet + golangci-lint
	@fmtout=$$(gofmt -l .); if [ -n "$$fmtout" ]; then echo "gofmt needed:"; echo "$$fmtout"; exit 1; fi
	go vet ./...
	$(GOBIN)/golangci-lint run ./...

fmt: ## format everything
	gofmt -w .

proto: ## lint + regenerate contracts (buf)
	cd contracts && $(GOBIN)/buf lint && PATH="$(GOBIN):$$PATH" $(GOBIN)/buf generate

tools: ## install dev tools into GOPATH/bin
	go install github.com/bufbuild/buf/cmd/buf@latest
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

integration: ## full suite against real postgres/redpanda/redis (testcontainers; needs docker)
	INTEGRATION=1 go test ./... -count=1

# To iterate against your own long-running containers instead (faster than
# per-run testcontainers), set the env explicitly — it takes precedence:
#   TEST_POSTGRES_URL=... TEST_KAFKA_BROKERS=... TEST_REDIS_URL=... go test ./...
