# go-kernel development targets. `make help` lists them.

GOBIN        := $(shell go env GOPATH)/bin
PG_PORT      := 15433
KAFKA_PORT   := 19092
REDIS_PORT   := 16380
PG_URL       := postgres://postgres:test@localhost:$(PG_PORT)/postgres
KAFKA_ADDR   := localhost:$(KAFKA_PORT)
REDIS_URL    := redis://localhost:$(REDIS_PORT)/0

.PHONY: help test lint fmt vet proto tools integration integration-up integration-down

help: ## list targets
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-18s %s\n", $$1, $$2}'

test: ## unit tests (integration tests skip without TEST_* env)
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

integration-up: ## start postgres + redpanda + redis test containers
	docker run --rm -d --name gokernel-test-pg -p $(PG_PORT):5432 -e POSTGRES_PASSWORD=test postgres:17-alpine
	docker run --rm -d --name gokernel-test-rp -p $(KAFKA_PORT):$(KAFKA_PORT) redpandadata/redpanda:latest \
		redpanda start --smp 1 --overprovisioned \
		--kafka-addr PLAINTEXT://0.0.0.0:$(KAFKA_PORT) \
		--advertise-kafka-addr PLAINTEXT://localhost:$(KAFKA_PORT) \
		--set redpanda.auto_create_topics_enabled=true
	docker run --rm -d --name gokernel-test-redis -p $(REDIS_PORT):6379 redis:7-alpine
	@sleep 8

integration-down: ## stop test containers
	-docker stop gokernel-test-pg gokernel-test-rp gokernel-test-redis 2>/dev/null

integration: ## full test suite against real postgres/kafka/redis (needs docker)
	@$(MAKE) integration-up
	TEST_POSTGRES_URL='$(PG_URL)' TEST_KAFKA_BROKERS='$(KAFKA_ADDR)' TEST_REDIS_URL='$(REDIS_URL)' \
		go test ./... -count=1; status=$$?; $(MAKE) integration-down; exit $$status
