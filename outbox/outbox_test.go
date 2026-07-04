package outbox_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	eventsexamplev1 "github.com/KucherenkoIvan/go-kernel/contracts/gen/events/example/v1"
	"github.com/KucherenkoIvan/go-kernel/contracts/kafkacontracts"
	"github.com/KucherenkoIvan/go-kernel/ddd"
	"github.com/KucherenkoIvan/go-kernel/internal/testinfra"
	"github.com/KucherenkoIvan/go-kernel/kafka"
	"github.com/KucherenkoIvan/go-kernel/outbox"
	"github.com/KucherenkoIvan/go-kernel/postgres"
	"github.com/KucherenkoIvan/go-kernel/requestid"
)

// Integration tests self-provision postgres via testinfra (no Kafka needed —
// the relay publishes through a fake producer).
func integrationClient(t *testing.T) *postgres.Client {
	t.Helper()
	c, err := postgres.Connect(context.Background(), testinfra.PostgresURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)

	ctx := context.Background()
	if _, err := c.Resolve(ddd.NoTransaction).Exec(ctx, outbox.MigrationSQL); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Resolve(ddd.NoTransaction).Exec(ctx, `TRUNCATE outbox`); err != nil {
		t.Fatal(err)
	}
	return c
}

type fakeProducer struct {
	mu     sync.Mutex
	sent   []kafka.RawMessage
	failNo int // fail while len(sent) < failNo
}

func (f *fakeProducer) SendRaw(_ context.Context, msgs ...kafka.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) < f.failNo {
		return errors.New("broker unavailable")
	}
	f.sent = append(f.sent, msgs...)
	return nil
}

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func pendingCount(t *testing.T, c *postgres.Client) int {
	t.Helper()
	var n int
	if err := c.Resolve(ddd.NoTransaction).QueryRow(context.Background(),
		`SELECT count(*) FROM outbox`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestEnqueueAndRelay_EndToEnd(t *testing.T) {
	c := integrationClient(t)
	ctx := requestid.With(context.Background(), "req-outbox-1")

	// enqueue two messages atomically with a business tx
	err := c.WithinTx(ctx, func(tx ddd.Transaction) error {
		q := c.Resolve(tx)
		if err := outbox.Enqueue(ctx, q, kafkacontracts.ExampleOrderPlaced,
			&eventsexamplev1.OrderPlacedEvent{OrderId: "o-1", TotalCents: 100},
			outbox.WithKey("o-1"), outbox.WithHeader("x-custom", "v")); err != nil {
			return err
		}
		return outbox.Enqueue(ctx, q, kafkacontracts.ExampleOrderPlaced,
			&eventsexamplev1.OrderPlacedEvent{OrderId: "o-2", TotalCents: 200})
	})
	if err != nil {
		t.Fatal(err)
	}

	producer := &fakeProducer{}
	relay := outbox.NewRelay(c, producer, outbox.RelayConfig{Logger: discard()})

	n, err := relay.RelayOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || len(producer.sent) != 2 {
		t.Fatalf("published %d / sent %d, want 2", n, len(producer.sent))
	}

	// order preserved (UUIDv7 ids), payload/key/headers intact
	first := producer.sent[0]
	if first.Key != "o-1" || first.Topic != kafkacontracts.ExampleOrderPlaced.Topic {
		t.Fatalf("first message: %+v", first)
	}
	if first.Headers["x-custom"] != "v" || first.Headers[requestid.Header] != "req-outbox-1" {
		t.Fatalf("headers lost: %+v", first.Headers)
	}
	msg, err := kafka.Decode(kafkacontracts.ExampleOrderPlaced, first.Payload)
	if err != nil || msg.GetOrderId() != "o-1" || msg.GetTotalCents() != 100 {
		t.Fatalf("payload mismatch: %+v err=%v", msg, err)
	}

	// table drained; second run is a no-op
	if pendingCount(t, c) != 0 {
		t.Fatal("outbox not drained")
	}
	if n, _ := relay.RelayOnce(context.Background()); n != 0 {
		t.Fatalf("second run published %d, want 0", n)
	}
}

func TestRelay_RollbackOnPublishFailure(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	if err := outbox.Enqueue(ctx, c.Resolve(ddd.NoTransaction), kafkacontracts.ExampleOrderPlaced,
		&eventsexamplev1.OrderPlacedEvent{OrderId: "o-fail"}); err != nil {
		t.Fatal(err)
	}

	producer := &fakeProducer{failNo: 1} // first publish fails
	relay := outbox.NewRelay(c, producer, outbox.RelayConfig{Logger: discard()})

	if _, err := relay.RelayOnce(ctx); err == nil {
		t.Fatal("expected publish failure")
	}
	if pendingCount(t, c) != 1 {
		t.Fatal("row must survive a failed publish (tx rollback)")
	}

	// broker recovers → the same row is delivered (at-least-once)
	producer.failNo = 0
	n, err := relay.RelayOnce(ctx)
	if err != nil || n != 1 {
		t.Fatalf("recovery run: n=%d err=%v", n, err)
	}
	if pendingCount(t, c) != 0 {
		t.Fatal("outbox not drained after recovery")
	}
}

func TestRelay_DrainsMultipleBatches(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	for i := 0; i < 7; i++ {
		if err := outbox.Enqueue(ctx, c.Resolve(ddd.NoTransaction), kafkacontracts.ExampleOrderPlaced,
			&eventsexamplev1.OrderPlacedEvent{OrderId: "o-batch"}); err != nil {
			t.Fatal(err)
		}
	}

	producer := &fakeProducer{}
	relay := outbox.NewRelay(c, producer, outbox.RelayConfig{BatchSize: 3, Logger: discard()})

	n, err := relay.RelayOnce(ctx)
	if err != nil || n != 7 {
		t.Fatalf("n=%d err=%v, want 7 (3+3+1 across batches)", n, err)
	}
}

func TestPendingStats(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	relay := outbox.NewRelay(c, &fakeProducer{}, outbox.RelayConfig{Logger: discard()})

	count, age, err := relay.PendingStats(ctx)
	if err != nil || count != 0 || age != nil {
		t.Fatalf("empty stats: count=%d age=%v err=%v", count, age, err)
	}

	_ = outbox.Enqueue(ctx, c.Resolve(ddd.NoTransaction), kafkacontracts.ExampleOrderPlaced,
		&eventsexamplev1.OrderPlacedEvent{OrderId: "o-stats"})

	count, age, err = relay.PendingStats(ctx)
	if err != nil || count != 1 || age == nil {
		t.Fatalf("stats after enqueue: count=%d age=%v err=%v", count, age, err)
	}
}
