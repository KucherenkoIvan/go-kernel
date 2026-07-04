package events_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/KucherenkoIvan/go-kernel/ddd"
	"github.com/KucherenkoIvan/go-kernel/ddd/dddtest"
	"github.com/KucherenkoIvan/go-kernel/events"
)

type recorder struct {
	mu   sync.Mutex
	seen []string
}

func (r *recorder) handler(name string, eventNames ...string) events.Handler {
	return events.Handler{
		Name:   name,
		Events: eventNames,
		Handle: func(_ context.Context, e ddd.DomainEvent) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.seen = append(r.seen, name+":"+e.EventName())
			return nil
		},
	}
}

func (r *recorder) wait(t *testing.T, n int) []string {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		r.mu.Lock()
		if len(r.seen) >= n {
			out := append([]string(nil), r.seen...)
			r.mu.Unlock()
			return out
		}
		r.mu.Unlock()
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d events", n)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func runPublisher(t *testing.T, pub *events.ChannelPublisher) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = pub.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-done
		_ = pub.Close(context.Background())
	})
}

func TestPublish_AfterCommitOnly(t *testing.T) {
	rec := &recorder{}
	pub := events.NewChannelPublisher(events.WithLogger(discardLogger()))
	pub.Subscribe(rec.handler("h"))
	runPublisher(t, pub)

	txm := &dddtest.FakeTxManager{}

	// committed tx → delivered
	err := txm.WithinTx(context.Background(), func(tx ddd.Transaction) error {
		return pub.Publish(context.Background(), tx, ddd.NewEvent("CouponUsedEvent", 1))
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := rec.wait(t, 1); got[0] != "h:CouponUsedEvent" {
		t.Fatalf("got %v", got)
	}

	// rolled-back tx → no ghost events
	boom := errors.New("boom")
	_ = txm.WithinTx(context.Background(), func(tx ddd.Transaction) error {
		_ = pub.Publish(context.Background(), tx, ddd.NewEvent("GhostEvent", 2))
		return boom
	})
	time.Sleep(50 * time.Millisecond)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.seen) != 1 {
		t.Fatalf("rolled-back event was delivered: %v", rec.seen)
	}
}

func TestPublish_NoTransactionIsImmediate(t *testing.T) {
	rec := &recorder{}
	pub := events.NewChannelPublisher(events.WithLogger(discardLogger()))
	pub.Subscribe(rec.handler("h"))
	runPublisher(t, pub)

	if err := pub.Publish(context.Background(), ddd.NoTransaction, ddd.NewEvent("PingEvent", 1)); err != nil {
		t.Fatal(err)
	}
	rec.wait(t, 1)
}

func TestSubscribe_EventNameFilter(t *testing.T) {
	rec := &recorder{}
	pub := events.NewChannelPublisher(events.WithLogger(discardLogger()))
	pub.Subscribe(rec.handler("used-only", "CouponUsedEvent"))
	pub.Subscribe(rec.handler("all"))
	runPublisher(t, pub)

	_ = pub.Publish(context.Background(), ddd.NoTransaction,
		ddd.NewEvent("CouponUsedEvent", 1), ddd.NewEvent("CouponCreatedEvent", 2))

	got := rec.wait(t, 3) // used-only sees 1, all sees 2
	counts := map[string]int{}
	for _, s := range got {
		counts[s]++
	}
	if counts["used-only:CouponUsedEvent"] != 1 || counts["used-only:CouponCreatedEvent"] != 0 ||
		counts["all:CouponUsedEvent"] != 1 || counts["all:CouponCreatedEvent"] != 1 {
		t.Fatalf("got %v", got)
	}
}

func TestHandlerPanicAndError_DoNotStopThePublisher(t *testing.T) {
	rec := &recorder{}
	pub := events.NewChannelPublisher(events.WithLogger(discardLogger()))
	pub.Subscribe(events.Handler{Name: "panicky", Handle: func(context.Context, ddd.DomainEvent) error { panic("boom") }})
	pub.Subscribe(events.Handler{Name: "failing", Handle: func(context.Context, ddd.DomainEvent) error { return errors.New("nope") }})
	pub.Subscribe(rec.handler("ok"))
	runPublisher(t, pub)

	_ = pub.Publish(context.Background(), ddd.NoTransaction, ddd.NewEvent("E", 1))
	if got := rec.wait(t, 1); got[0] != "ok:E" {
		t.Fatalf("got %v", got)
	}
}

func TestClose_DrainsPending(t *testing.T) {
	rec := &recorder{}
	pub := events.NewChannelPublisher(events.WithLogger(discardLogger()))
	pub.Subscribe(rec.handler("h"))
	// no Run — events stay buffered until Close drains them

	_ = pub.Publish(context.Background(), ddd.NoTransaction, ddd.NewEvent("E1", 1), ddd.NewEvent("E2", 2))
	if err := pub.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.seen) != 2 {
		t.Fatalf("Close did not drain: %v", rec.seen)
	}
}
