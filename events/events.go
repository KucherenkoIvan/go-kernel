// Package events provides in-process delivery of domain events to policies
// and projectors within the same service.
//
// Delivery is commit-gated: Publish called inside a transaction buffers the
// events on the transaction token (ddd.AfterCommit) and hands them to the
// dispatch queue only after a successful commit. Rolled-back transactions
// produce no events, and no handler ever observes uncommitted state.
//
// Semantics are at-most-once and in-memory: events die with the process.
// That is correct for same-service reactions; cross-service (or durable)
// delivery is the transactional-outbox's job, behind the same producer port
// shape — swapping a feature from the channel publisher to the outbox is a
// composition root change only.
package events

import (
	"context"
	"log/slog"
	"runtime/debug"
	"slices"
	"sync/atomic"

	"github.com/KucherenkoIvan/go-kernel/ddd"
)

// Producer is the generic shape of an event producer port. Feature ports
// (e.g. CouponEventProducer) follow it with their concrete event types;
// ChannelPublisher implements it for ddd.DomainEvent.
type Producer interface {
	Publish(ctx context.Context, tx ddd.Transaction, events ...ddd.DomainEvent) error
}

// Handler is a subscription — a policy or a projector adapter.
type Handler struct {
	// Name identifies the handler in logs.
	Name string
	// Events filters by event name; empty means all events.
	Events []string
	// Handle processes one event. Errors are logged, not retried —
	// handlers that need retries manage them internally.
	Handle func(ctx context.Context, event ddd.DomainEvent) error
}

func (h Handler) matches(eventName string) bool {
	return len(h.Events) == 0 || slices.Contains(h.Events, eventName)
}

const defaultBuffer = 1024

// ChannelPublisher is the in-process Producer implementation: a buffered Go
// channel with a dispatch loop. Wire it as an app.Adapter:
//
//	pub := events.NewChannelPublisher(events.WithLogger(logger))
//	// features subscribe their policies/projectors, then:
//	app.Adapter{Name: "events", Run: pub.Run, Close: pub.Close}
type ChannelPublisher struct {
	logger   *slog.Logger
	ch       chan ddd.DomainEvent
	handlers []Handler
	closed   atomic.Bool
}

// Option customizes NewChannelPublisher.
type Option func(*ChannelPublisher)

// WithLogger sets the logger for handler errors. Defaults to slog.Default().
func WithLogger(logger *slog.Logger) Option {
	return func(p *ChannelPublisher) { p.logger = logger }
}

// WithBuffer sets the queue size (default 1024). When the buffer is full,
// Publish dispatches inline instead of blocking.
func WithBuffer(n int) Option {
	return func(p *ChannelPublisher) { p.ch = make(chan ddd.DomainEvent, n) }
}

func NewChannelPublisher(opts ...Option) *ChannelPublisher {
	p := &ChannelPublisher{
		logger: slog.Default(),
		ch:     make(chan ddd.DomainEvent, defaultBuffer),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Subscribe registers a handler. Call during composition (before Run) —
// subscriptions are not synchronized against a running publisher.
func (p *ChannelPublisher) Subscribe(h Handler) {
	p.handlers = append(p.handlers, h)
}

// Publish implements Producer. Inside a transaction, delivery waits for the
// commit; with ddd.NoTransaction it is enqueued immediately.
func (p *ChannelPublisher) Publish(ctx context.Context, tx ddd.Transaction, events ...ddd.DomainEvent) error {
	if len(events) == 0 {
		return nil
	}
	ddd.AfterCommit(ctx, tx, func(ctx context.Context) {
		for _, e := range events {
			p.enqueue(ctx, e)
		}
	})
	return nil
}

func (p *ChannelPublisher) enqueue(ctx context.Context, e ddd.DomainEvent) {
	if p.closed.Load() {
		p.logger.Warn("channel publisher is closed, dropping event", "event", e.EventName())
		return
	}
	select {
	case p.ch <- e:
	default:
		// Never block the publisher: a full buffer during shutdown would
		// deadlock against the draining order. Dispatch inline instead.
		p.logger.Warn("channel publisher buffer full, dispatching inline", "event", e.EventName())
		p.dispatch(ctx, e)
	}
}

// Run dispatches events until ctx is cancelled. Blocks; always returns nil.
func (p *ChannelPublisher) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case e := <-p.ch:
			p.dispatch(ctx, e)
		}
	}
}

// Close drains the remaining events and stops accepting new ones. In the
// app adapter list the publisher is registered before the servers, so it
// drains after all in-flight requests have finished publishing.
func (p *ChannelPublisher) Close(ctx context.Context) error {
	p.closed.Store(true)
	for {
		select {
		case e := <-p.ch:
			p.dispatch(ctx, e)
		default:
			return nil
		}
	}
}

func (p *ChannelPublisher) dispatch(ctx context.Context, e ddd.DomainEvent) {
	for _, h := range p.handlers {
		if !h.matches(e.EventName()) {
			continue
		}
		p.dispatchOne(ctx, h, e)
	}
}

func (p *ChannelPublisher) dispatchOne(ctx context.Context, h Handler, e ddd.DomainEvent) {
	defer func() {
		if r := recover(); r != nil {
			p.logger.Error("panic in event handler",
				"handler", h.Name, "event", e.EventName(),
				"panic", r, "stack", string(debug.Stack()))
		}
	}()
	if err := h.Handle(ctx, e); err != nil {
		p.logger.Error("event handler failed",
			"handler", h.Name, "event", e.EventName(), "error", err)
	}
}
