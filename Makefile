# go-kernel development targets. `make help` lists them.

GOBIN := $(shell go env GOPATH)/bin

# Pinned dev tool that needs a pinned version (govulncheck). Installed per
# checkout into bin/tools via a stamp file, mirroring the per-checkout tool
# pinning used in downstream projects. Bumping the version reinstalls it.
# govulncheck needs Go 1.26+ to build; an older local Go must leave
# GOTOOLCHAIN at auto, or the install fails.
GOVULNCHECK_VERSION := v1.9.0

TOOLS := $(CURDIR)/bin/tools
GOVULNCHECK_STAMP := $(TOOLS)/.govulncheck-$(GOVULNCHECK_VERSION)

.PHONY: help test lint fmt vulncheck vulncheck-tools proto tools integration

help: ## list targets
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-14s %s\n", $$1, $$2}'

test: ## unit tests (integration tests self-skip)
	go test ./...

lint: ## gofmt check + go vet + golangci-lint + govulncheck
	@fmtout=$$(gofmt -l .); if [ -n "$$fmtout" ]; then echo "gofmt needed:"; echo "$$fmtout"; exit 1; fi
	go vet ./...
	$(GOBIN)/golangci-lint run ./...
	@$(MAKE) --no-print-directory vulncheck

fmt: ## format everything
	gofmt -w .

# govulncheck fetches the Go vulnerability database (vuln.go.dev) on every
# run, so lint needs the network; a scan takes seconds. It reports only
# vulnerabilities our code can reach, and fails on any reachable one.
vulncheck: $(GOVULNCHECK_STAMP) ## govulncheck: fail on a known vulnerability in reachable code (needs the network)
	@$(TOOLS)/govulncheck ./...

proto: ## lint + regenerate contracts (buf)
	cd contracts && $(GOBIN)/buf lint && PATH="$(GOBIN):$$PATH" $(GOBIN)/buf generate

tools: ## install dev tools into GOPATH/bin (buf, protoc plugins, golangci-lint); govulncheck is pinned per checkout
	go install github.com/bufbuild/buf/cmd/buf@latest
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	@$(MAKE) --no-print-directory vulncheck-tools

vulncheck-tools: $(GOVULNCHECK_STAMP) ## install the pinned govulncheck into bin/tools

$(GOVULNCHECK_STAMP):
	@mkdir -p $(TOOLS)
	GOBIN=$(TOOLS) go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	touch $@

integration: ## full suite against real postgres/redpanda/redis (testcontainers; needs docker)
	INTEGRATION=1 go test ./... -count=1

# To iterate against your own long-running containers instead (faster than
# per-run testcontainers), set the env explicitly — it takes precedence:
#   TEST_POSTGRES_URL=... TEST_KAFKA_BROKERS=... TEST_REDIS_URL=... go test ./...
