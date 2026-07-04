package app_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/KucherenkoIvan/go-kernel/app"
)

type order struct {
	mu    sync.Mutex
	items []string
}

func (o *order) add(s string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.items = append(o.items, s)
}

func blockUntilCancel(o *order, name string) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		<-ctx.Done()
		o.add("run-done:" + name)
		return nil
	}
}

func closer(o *order, name string) func(ctx context.Context) error {
	return func(context.Context) error {
		o.add("close:" + name)
		return nil
	}
}

func TestRun_ErrorShutsEverythingDown_ClosesInReverse(t *testing.T) {
	o := &order{}
	boom := errors.New("boom")

	err := app.Run(context.Background(),
		app.Adapter{Name: "db", Close: closer(o, "db")},
		app.Adapter{Name: "worker", Run: blockUntilCancel(o, "worker"), Close: closer(o, "worker")},
		app.Adapter{Name: "failing", Run: func(ctx context.Context) error {
			time.Sleep(10 * time.Millisecond)
			return boom
		}},
	)

	if !errors.Is(err, boom) {
		t.Fatalf("run error not propagated: %v", err)
	}

	o.mu.Lock()
	defer o.mu.Unlock()
	want := []string{"run-done:worker", "close:worker", "close:db"}
	if len(o.items) != len(want) {
		t.Fatalf("got %v, want %v", o.items, want)
	}
	for i := range want {
		if o.items[i] != want[i] {
			t.Fatalf("wrong order: got %v, want %v", o.items, want)
		}
	}
}

func TestRun_ContextCancelIsGraceful(t *testing.T) {
	o := &order{}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := app.Run(ctx,
		app.Adapter{Name: "worker", Run: blockUntilCancel(o, "worker")},
	)
	if err != nil {
		t.Fatalf("graceful shutdown must return nil, got %v", err)
	}
}

func TestRun_CloseErrorsAreReported(t *testing.T) {
	closeErr := errors.New("close failed")
	err := app.Run(context.Background(),
		app.Adapter{Name: "db", Close: func(context.Context) error { return closeErr }},
		app.Adapter{Name: "oneshot", Run: func(ctx context.Context) error { return nil }},
	)
	if !errors.Is(err, closeErr) {
		t.Fatalf("close error not reported: %v", err)
	}
}
