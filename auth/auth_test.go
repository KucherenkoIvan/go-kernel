package auth_test

import (
	"context"
	"testing"

	"github.com/KucherenkoIvan/go-kernel/auth"
)

func TestPrincipalRoundTrip(t *testing.T) {
	if _, ok := auth.PrincipalFrom(context.Background()); ok {
		t.Error("empty context should have no principal")
	}

	ctx := auth.WithPrincipal(context.Background(), auth.Principal{UserID: "u-1", Roles: []string{"admin"}})
	p, ok := auth.PrincipalFrom(ctx)
	if !ok || p.UserID != "u-1" {
		t.Fatalf("got %+v ok=%v", p, ok)
	}
	if !p.HasRole("admin") || p.HasRole("root") {
		t.Errorf("HasRole misbehaves: %+v", p)
	}
}
