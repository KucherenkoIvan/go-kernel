package requestid_test

import (
	"context"
	"testing"

	"github.com/KucherenkoIvan/go-kernel/requestid"
)

func TestWithAndFrom(t *testing.T) {
	ctx := context.Background()
	if requestid.From(ctx) != "" {
		t.Error("empty context should have no request id")
	}
	if got := requestid.From(requestid.With(ctx, "req-1")); got != "req-1" {
		t.Errorf("got %q", got)
	}
}

func TestEnsure(t *testing.T) {
	ctx := requestid.With(context.Background(), "req-1")
	if got := requestid.From(requestid.Ensure(ctx)); got != "req-1" {
		t.Errorf("Ensure must keep an existing id, got %q", got)
	}
	if got := requestid.From(requestid.Ensure(context.Background())); got == "" {
		t.Error("Ensure must generate an id when absent")
	}
}
