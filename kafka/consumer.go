package kafka

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"

	"github.com/KucherenkoIvan/go-kernel/requestid"
)

// Meta is the message envelope available to handlers.
type Meta struct {
	Topic     string
	Partition int32
	Offset    int64
	Key       string
	Headers   map[string]string
	Timestamp time.Time
}

// ConsumerConfig configures a consumer group.
type ConsumerConfig struct {
	Brokers []string
	// GroupID identifies the consumer group; offsets are tracked per group.
	GroupID string
	// FromLatest starts a NEW group at the end of the topic instead of the
	// beginning. The default (earliest) is the safe one: accumulated events
	// are not lost, idempotent handlers reprocess them. Set it only for
	// topics where history is noise (metrics, ephemeral fan-out).
	FromLatest bool
	// Logger for handler errors. Defaults to slog.Default().
	Logger *slog.Logger
}

// Consumer is a consumer group dispatching messages to per-contract
// handlers. Wire it as an app.Adapter:
//
//	c := kafka.NewConsumer(kafka.ConsumerConfig{Brokers: ..., GroupID: "myservice"})
//	kafka.Subscribe(c, kafkacontracts.OrderPlaced, handler.Handle)
//	app.Adapter{Name: "kafka-consumer", Run: c.Run}
type Consumer struct {
	cfg      ConsumerConfig
	handlers map[string]func(ctx context.Context, m Meta, payload []byte) error
}

func NewConsumer(cfg ConsumerConfig) *Consumer {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Consumer{
		cfg:      cfg,
		handlers: map[string]func(ctx context.Context, m Meta, payload []byte) error{},
	}
}

// Subscribe registers the handler for a contract's topic. Call during
// composition, before Run. One handler per topic.
//
// Offset semantics (at-least-once): the offset is committed only after the
// handler returns nil. An error means "redeliver" — the consumer retries the
// same message with backoff and does not advance past it, so handlers must
// be idempotent. A payload that cannot be decoded is logged and skipped —
// retrying cannot fix it.
func Subscribe[T proto.Message](c *Consumer, contract Contract[T], handle func(ctx context.Context, msg T, meta Meta) error) {
	if _, dup := c.handlers[contract.Topic]; dup {
		panic(fmt.Sprintf("kafka: duplicate subscription for topic %s", contract.Topic))
	}
	c.handlers[contract.Topic] = func(ctx context.Context, meta Meta, payload []byte) error {
		msg, err := Decode(contract, payload)
		if err != nil {
			c.cfg.Logger.Error("kafka: undecodable message skipped",
				"topic", meta.Topic, "partition", meta.Partition, "offset", meta.Offset, "error", err)
			return nil // poison pill: retry cannot fix a bad payload
		}
		return handle(ctx, msg, meta)
	}
}

// retryBackoff returns the delay before the attempt-th retry (1-based),
// capped at 30s.
func retryBackoff(attempt int) time.Duration {
	backoff := time.Second << min(attempt-1, 5) // 1s, 2s, 4s, 8s, 16s, 32s→30s cap
	return min(backoff, 30*time.Second)
}

// Run consumes until ctx is cancelled. Blocks; returns nil on graceful stop.
func (c *Consumer) Run(ctx context.Context) error {
	if len(c.handlers) == 0 {
		return fmt.Errorf("kafka: consumer %q has no subscriptions", c.cfg.GroupID)
	}

	topics := make([]string, 0, len(c.handlers))
	for topic := range c.handlers {
		topics = append(topics, topic)
	}

	resetOffset := kgo.NewOffset().AtStart()
	if c.cfg.FromLatest {
		resetOffset = kgo.NewOffset().AtEnd()
	}

	client, err := kgo.NewClient(
		kgo.SeedBrokers(c.cfg.Brokers...),
		kgo.ConsumerGroup(c.cfg.GroupID),
		kgo.ConsumeTopics(topics...),
		kgo.ConsumeResetOffset(resetOffset),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		return fmt.Errorf("kafka: creating consumer: %w", err)
	}
	defer client.Close()

	for {
		fetches := client.PollFetches(ctx)
		if ctx.Err() != nil {
			return nil
		}
		for _, fetchErr := range fetches.Errors() {
			c.cfg.Logger.Error("kafka: fetch error",
				"topic", fetchErr.Topic, "partition", fetchErr.Partition, "error", fetchErr.Err)
		}

		var done []*kgo.Record
		fetches.EachRecord(func(record *kgo.Record) {
			if c.handleWithRetry(ctx, record) {
				done = append(done, record)
			}
		})

		if len(done) > 0 {
			if err := client.CommitRecords(ctx, done...); err != nil && ctx.Err() == nil {
				c.cfg.Logger.Error("kafka: committing offsets", "error", err)
			}
		}
	}
}

// handleWithRetry processes one record, retrying with backoff until it
// succeeds or ctx is cancelled. Reports whether the offset may be committed.
func (c *Consumer) handleWithRetry(ctx context.Context, record *kgo.Record) bool {
	meta := recordMeta(record)
	handle := c.handlers[record.Topic]
	if handle == nil {
		return true // topic without handler (should not happen) — skip
	}

	msgCtx := ctx
	if id := meta.Headers[requestid.Header]; id != "" {
		msgCtx = requestid.With(ctx, id)
	}

	for attempt := 1; ; attempt++ {
		err := handle(msgCtx, meta, record.Value)
		if err == nil {
			return true
		}
		c.cfg.Logger.Error("kafka: handler failed, will retry",
			"topic", meta.Topic, "partition", meta.Partition, "offset", meta.Offset,
			"attempt", attempt, "error", err)

		select {
		case <-ctx.Done():
			return false // shutting down — offset stays uncommitted, message redelivers
		case <-time.After(retryBackoff(attempt)):
		}
	}
}

func recordMeta(record *kgo.Record) Meta {
	headers := make(map[string]string, len(record.Headers))
	for _, h := range record.Headers {
		headers[h.Key] = string(h.Value)
	}
	return Meta{
		Topic:     record.Topic,
		Partition: record.Partition,
		Offset:    record.Offset,
		Key:       string(record.Key),
		Headers:   headers,
		Timestamp: record.Timestamp,
	}
}
