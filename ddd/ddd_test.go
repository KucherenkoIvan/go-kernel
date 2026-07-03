package ddd_test

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/KucherenkoIvan/go-kernel/ddd"
	"github.com/KucherenkoIvan/go-kernel/ddd/dddtest"
)

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewEntityID_Format(t *testing.T) {
	id := ddd.NewEntityID()
	if !uuidRe.MatchString(id) {
		t.Fatalf("not a UUIDv7: %q", id)
	}
}

func TestNewEntityID_TimestampPrefix(t *testing.T) {
	before := time.Now().UnixMilli()
	id := ddd.NewEntityID()
	after := time.Now().UnixMilli()

	ms, err := strconv.ParseInt(id[0:8]+id[9:13], 16, 64)
	if err != nil {
		t.Fatalf("parsing timestamp prefix: %v", err)
	}
	if ms < before || ms > after {
		t.Fatalf("timestamp %d outside [%d, %d]", ms, before, after)
	}
}

func TestNewEntityID_Unique(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for range 1000 {
		id := ddd.NewEntityID()
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id: %q", id)
		}
		seen[id] = struct{}{}
	}
}

type couponUsedData struct {
	CouponID string
	Code     string
}

func TestNewEvent(t *testing.T) {
	before := time.Now()
	event := ddd.NewEvent("CouponUsedEvent", couponUsedData{CouponID: "c-1", Code: "SUMMER10"})

	if !uuidRe.MatchString(event.ID) {
		t.Errorf("ID is not a UUIDv7: %q", event.ID)
	}
	if event.Name != "CouponUsedEvent" {
		t.Errorf("Name = %q", event.Name)
	}
	if event.OccurredAt.Before(before) || event.OccurredAt.After(time.Now()) {
		t.Errorf("OccurredAt = %v, out of range", event.OccurredAt)
	}
	if event.Data.Code != "SUMMER10" {
		t.Errorf("Data = %+v", event.Data)
	}
}

func TestNewEventAt(t *testing.T) {
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	event := ddd.NewEventAt("CouponUsedEvent", couponUsedData{CouponID: "c-1"}, "known-id", at)

	if event.ID != "known-id" || !event.OccurredAt.Equal(at) {
		t.Errorf("overrides not applied: %+v", event)
	}
}

func TestEvent_ImplementsDomainEvent(t *testing.T) {
	event := ddd.NewEvent("CouponUsedEvent", couponUsedData{CouponID: "c-1"})

	var de ddd.DomainEvent = event
	if de.EventID() != event.ID || de.EventName() != "CouponUsedEvent" || !de.EventOccurredAt().Equal(event.OccurredAt) {
		t.Errorf("envelope accessors mismatch: %+v", event)
	}
}

func TestEventRecorder(t *testing.T) {
	var r ddd.EventRecorder[ddd.DomainEvent]
	r.PushEvent(ddd.NewEvent("A", struct{}{}))
	r.PushEvent(ddd.NewEvent("B", 42))

	events := r.PopEvents()
	if len(events) != 2 || events[0].EventName() != "A" || events[1].EventName() != "B" {
		t.Fatalf("PopEvents = %+v", events)
	}
	if again := r.PopEvents(); len(again) != 0 {
		t.Fatalf("second PopEvents should be empty, got %+v", again)
	}
}

type couponExhaustedError struct{ ddd.DomainError }

func (e *couponExhaustedError) Error() string { return "coupon_exhausted" }

func TestIsDomainError(t *testing.T) {
	if !ddd.IsDomainError(&couponExhaustedError{}) {
		t.Error("embedded marker not detected")
	}
	if !ddd.IsDomainError(fmt.Errorf("saving coupon: %w", &couponExhaustedError{})) {
		t.Error("wrapped domain error not detected")
	}
	if ddd.IsDomainError(errors.New("db is down")) {
		t.Error("plain error misdetected as domain error")
	}
	if ddd.IsDomainError(nil) {
		t.Error("nil misdetected as domain error")
	}
}

func TestFakeTxManager(t *testing.T) {
	m := &dddtest.FakeTxManager{}

	var got ddd.Transaction
	err := m.WithinTx(context.Background(), func(tx ddd.Transaction) error {
		got = tx
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got != m.LastTransaction || m.Calls != 1 {
		t.Fatalf("token not recorded: got=%v last=%v calls=%d", got, m.LastTransaction, m.Calls)
	}

	wantErr := errors.New("boom")
	if err := m.WithinTx(context.Background(), func(ddd.Transaction) error { return wantErr }); !errors.Is(err, wantErr) {
		t.Fatalf("callback error not propagated: %v", err)
	}
	if m.Calls != 2 || m.LastTransaction == got {
		t.Fatalf("second call should produce a new token")
	}
}

func TestSequenceIDGenerator(t *testing.T) {
	g := &dddtest.SequenceIDGenerator{}
	if a, b := g.NewID(), g.NewID(); a != "id-1" || b != "id-2" {
		t.Fatalf("got %q, %q", a, b)
	}
	p := &dddtest.SequenceIDGenerator{Prefix: "coupon"}
	if a := p.NewID(); a != "coupon-1" {
		t.Fatalf("got %q", a)
	}
}

func TestFixedClock(t *testing.T) {
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := dddtest.FixedClock{Time: at}
	if !c.Now().Equal(at) {
		t.Fatalf("got %v", c.Now())
	}
}
