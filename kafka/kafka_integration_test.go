package kafka_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	eventsexamplev1 "github.com/KucherenkoIvan/go-kernel/contracts/gen/events/example/v1"
	"github.com/KucherenkoIvan/go-kernel/ddd"
	"github.com/KucherenkoIvan/go-kernel/internal/testinfra"
	"github.com/KucherenkoIvan/go-kernel/kafka"
	"github.com/KucherenkoIvan/go-kernel/requestid"
)

// Integration tests self-provision Redpanda via testcontainers when
// INTEGRATION=1 (`make integration`), use TEST_KAFKA_BROKERS when set, and
// skip otherwise.
func brokers(t *testing.T) []string {
	t.Helper()
	return testinfra.KafkaBrokers(t)
}

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// uniqueContract gives each test run its own topic so runs don't interfere.
func uniqueContract(t *testing.T) kafka.Contract[*eventsexamplev1.OrderPlacedEvent] {
	t.Helper()
	return kafka.NewContract[*eventsexamplev1.OrderPlacedEvent](
		fmt.Sprintf("test.order-placed.%s", ddd.NewEntityID()))
}

type received struct {
	mu   sync.Mutex
	msgs []*eventsexamplev1.OrderPlacedEvent
	ids  []string
}

func (r *received) wait(t *testing.T, n int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		if len(r.msgs) >= n {
			r.mu.Unlock()
			return
		}
		r.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d messages", n)
}

func runConsumer(t *testing.T, c *kafka.Consumer) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = c.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("consumer did not stop")
		}
	})
}

func TestProduceConsume_RoundTrip(t *testing.T) {
	contract := uniqueContract(t)

	producer, err := kafka.NewProducer(kafka.ProducerConfig{Brokers: brokers(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = producer.Close(context.Background()) })

	if err := producer.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}

	rec := &received{}
	consumer := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers: brokers(t), GroupID: "test-group-" + ddd.NewEntityID(), Logger: discard(),
	})
	kafka.Subscribe(consumer, contract, func(ctx context.Context, msg *eventsexamplev1.OrderPlacedEvent, meta kafka.Meta) error {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		rec.msgs = append(rec.msgs, msg)
		rec.ids = append(rec.ids, requestid.From(ctx))
		return nil
	})

	if err := consumer.Ping(context.Background()); err == nil {
		t.Fatal("Ping must fail before Run starts the consumer")
	}
	runConsumer(t, consumer)

	ctx := requestid.With(context.Background(), "req-kafka-42")
	err = kafka.Send(ctx, producer, contract,
		&eventsexamplev1.OrderPlacedEvent{OrderId: "o-1", UserId: "u-1", TotalCents: 4200},
		kafka.WithKey("o-1"))
	if err != nil {
		t.Fatal(err)
	}

	rec.wait(t, 1, 30*time.Second)

	if err := consumer.Ping(context.Background()); err != nil {
		t.Fatalf("Ping on a running consumer: %v", err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.msgs[0].GetOrderId() != "o-1" || rec.msgs[0].GetTotalCents() != 4200 {
		t.Fatalf("message mismatch: %+v", rec.msgs[0])
	}
	if rec.ids[0] != "req-kafka-42" {
		t.Fatalf("request id not propagated through kafka: %q", rec.ids[0])
	}
}

func TestConsume_RetryUntilSuccess(t *testing.T) {
	contract := uniqueContract(t)

	producer, err := kafka.NewProducer(kafka.ProducerConfig{Brokers: brokers(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = producer.Close(context.Background()) })

	var mu sync.Mutex
	attempts := 0
	done := make(chan struct{})

	consumer := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers: brokers(t), GroupID: "test-group-" + ddd.NewEntityID(), Logger: discard(),
	})
	kafka.Subscribe(consumer, contract, func(context.Context, *eventsexamplev1.OrderPlacedEvent, kafka.Meta) error {
		mu.Lock()
		defer mu.Unlock()
		attempts++
		if attempts < 3 {
			return errors.New("transient failure")
		}
		close(done)
		return nil
	})
	runConsumer(t, consumer)

	if err := kafka.Send(context.Background(), producer, contract,
		&eventsexamplev1.OrderPlacedEvent{OrderId: "o-retry"}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("handler never succeeded")
	}
	mu.Lock()
	defer mu.Unlock()
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3 (same message retried)", attempts)
	}
}
