// Package testinfra provisions real infrastructure for integration tests.
//
// Resolution order, per dependency:
//
//  1. TEST_<DEP>_URL / TEST_KAFKA_BROKERS set → use it (your own container,
//     fastest iteration)
//  2. INTEGRATION set → start a throwaway container via testcontainers
//     (cleaned up by its reaper after the test process exits)
//  3. otherwise → skip the test, keeping plain `go test ./...` instant and
//     docker-free
//
// Containers are per-process singletons: one postgres/redpanda/redis per
// test binary, shared by all tests in the package.
package testinfra

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
	tcredpanda "github.com/testcontainers/testcontainers-go/modules/redpanda"
)

const enableVar = "INTEGRATION"

func provision[T any](tb testing.TB, envVar string, once *sync.Once, value *T, err *error, start func(ctx context.Context) (T, error)) T {
	tb.Helper()
	if raw := os.Getenv(envVar); raw != "" {
		var fromEnv T
		switch p := any(&fromEnv).(type) {
		case *string:
			*p = raw
		case *[]string:
			*p = strings.Split(raw, ",")
		}
		return fromEnv
	}
	if os.Getenv(enableVar) == "" {
		tb.Skipf("integration test: set %s=1 (testcontainers) or %s", enableVar, envVar)
	}
	once.Do(func() { *value, *err = start(context.Background()) })
	if *err != nil {
		tb.Fatalf("testinfra: %v", *err)
	}
	return *value
}

var (
	pgOnce sync.Once
	pgURL  string
	pgErr  error
)

// PostgresURL returns a connection string to a real PostgreSQL.
func PostgresURL(tb testing.TB) string {
	return provision(tb, "TEST_POSTGRES_URL", &pgOnce, &pgURL, &pgErr,
		func(ctx context.Context) (string, error) {
			container, err := tcpostgres.Run(ctx, "postgres:17-alpine",
				tcpostgres.WithPassword("test"),
				tcpostgres.BasicWaitStrategies(),
			)
			if err != nil {
				return "", fmt.Errorf("starting postgres container: %w", err)
			}
			url, err := container.ConnectionString(ctx, "sslmode=disable")
			if err != nil {
				return "", fmt.Errorf("postgres connection string: %w", err)
			}
			return url, nil
		})
}

var (
	kafkaOnce    sync.Once
	kafkaBrokers []string
	kafkaErr     error
)

// KafkaBrokers returns seed brokers of a real Kafka-compatible broker
// (Redpanda), with topic auto-creation enabled.
func KafkaBrokers(tb testing.TB) []string {
	return provision(tb, "TEST_KAFKA_BROKERS", &kafkaOnce, &kafkaBrokers, &kafkaErr,
		func(ctx context.Context) ([]string, error) {
			container, err := tcredpanda.Run(ctx, "redpandadata/redpanda:latest",
				tcredpanda.WithAutoCreateTopics(),
			)
			if err != nil {
				return nil, fmt.Errorf("starting redpanda container: %w", err)
			}
			broker, err := container.KafkaSeedBroker(ctx)
			if err != nil {
				return nil, fmt.Errorf("redpanda seed broker: %w", err)
			}
			return []string{broker}, nil
		})
}

var (
	redisOnce sync.Once
	redisURL  string
	redisErr  error
)

// RedisURL returns a connection URL to a real Redis.
func RedisURL(tb testing.TB) string {
	return provision(tb, "TEST_REDIS_URL", &redisOnce, &redisURL, &redisErr,
		func(ctx context.Context) (string, error) {
			container, err := tcredis.Run(ctx, "redis:7-alpine")
			if err != nil {
				return "", fmt.Errorf("starting redis container: %w", err)
			}
			url, err := container.ConnectionString(ctx)
			if err != nil {
				return "", fmt.Errorf("redis connection string: %w", err)
			}
			return url, nil
		})
}
