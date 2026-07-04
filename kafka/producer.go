package kafka

import (
	"context"
	"fmt"

	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"

	"github.com/KucherenkoIvan/go-kernel/requestid"
)

// RawMessage is a serialized message — what the outbox relay works with.
type RawMessage struct {
	Topic   string
	Key     string // "" = no key (round-robin partitioning)
	Payload []byte
	Headers map[string]string
}

// Producer sends messages to Kafka. One Producer per service, shared by all
// features and by the outbox relay.
type Producer struct {
	client *kgo.Client
}

// ProducerConfig is the minimal connection config; TLS/SASL become options
// when a deployment needs them.
type ProducerConfig struct {
	Brokers []string
}

func NewProducer(cfg ProducerConfig) (*Producer, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Brokers...),
		// dev-friendliness: create topics on first produce where the broker
		// allows it (auto.create.topics.enable); production topics should
		// still be provisioned explicitly
		kgo.AllowAutoTopicCreation(),
	)
	if err != nil {
		return nil, fmt.Errorf("kafka: creating producer: %w", err)
	}
	return &Producer{client: client}, nil
}

// Ping verifies broker connectivity — for health checks.
func (p *Producer) Ping(ctx context.Context) error {
	return p.client.Ping(ctx)
}

// Close flushes buffered messages and closes the client.
func (p *Producer) Close(ctx context.Context) error {
	if err := p.client.Flush(ctx); err != nil {
		p.client.Close()
		return fmt.Errorf("kafka: flushing producer: %w", err)
	}
	p.client.Close()
	return nil
}

// SendRaw synchronously produces pre-serialized messages, preserving order
// per partition. Used by the outbox relay; typed code uses Send.
func (p *Producer) SendRaw(ctx context.Context, msgs ...RawMessage) error {
	if len(msgs) == 0 {
		return nil
	}
	records := make([]*kgo.Record, 0, len(msgs))
	for _, m := range msgs {
		records = append(records, rawToRecord(m))
	}
	if err := p.client.ProduceSync(ctx, records...).FirstErr(); err != nil {
		return fmt.Errorf("kafka: producing: %w", err)
	}
	return nil
}

// SendOption customizes a single send.
type SendOption func(*RawMessage)

// WithKey sets the partitioning key — messages with the same key keep their
// order.
func WithKey(key string) SendOption {
	return func(m *RawMessage) { m.Key = key }
}

// WithHeader adds a header.
func WithHeader(key, value string) SendOption {
	return func(m *RawMessage) {
		if m.Headers == nil {
			m.Headers = map[string]string{}
		}
		m.Headers[key] = value
	}
}

// Send produces one typed message (direct, at-most-once relative to your DB
// state — business-critical events go through the outbox instead). The
// context's request id is propagated as a header.
func Send[T proto.Message](ctx context.Context, p *Producer, c Contract[T], msg T, opts ...SendOption) error {
	raw, err := buildRaw(ctx, c, msg, opts...)
	if err != nil {
		return err
	}
	return p.SendRaw(ctx, raw)
}

func buildRaw[T proto.Message](ctx context.Context, c Contract[T], msg T, opts ...SendOption) (RawMessage, error) {
	payload, err := Encode(c, msg)
	if err != nil {
		return RawMessage{}, err
	}
	raw := RawMessage{Topic: c.Topic, Payload: payload}
	for _, opt := range opts {
		opt(&raw)
	}
	if id := requestid.From(ctx); id != "" {
		if raw.Headers == nil {
			raw.Headers = map[string]string{}
		}
		raw.Headers[requestid.Header] = id
	}
	return raw, nil
}

func rawToRecord(m RawMessage) *kgo.Record {
	record := &kgo.Record{Topic: m.Topic, Value: m.Payload}
	if m.Key != "" {
		record.Key = []byte(m.Key)
	}
	for k, v := range m.Headers {
		record.Headers = append(record.Headers, kgo.RecordHeader{Key: k, Value: []byte(v)})
	}
	return record
}
