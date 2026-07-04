# testinfra (internal)

Provisions real infrastructure for the kernel's integration tests via [testcontainers-go](https://golang.testcontainers.org). Module-internal — not part of the public API.

Resolution order per dependency (`PostgresURL`, `KafkaBrokers`, `RedisURL`):

1. `TEST_POSTGRES_URL` / `TEST_KAFKA_BROKERS` / `TEST_REDIS_URL` set → used as-is (your own long-running container; fastest iteration)
2. `INTEGRATION` set (`make integration`) → a throwaway container per test binary, reaped automatically after the process exits
3. neither → the test skips; plain `go test ./...` stays instant and docker-free

Containers are per-process singletons — one postgres/redpanda/redis shared by all tests in a package binary.
