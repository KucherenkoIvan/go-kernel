// Package outbox implements the transactional outbox: events are written to
// the outbox table in the same transaction as the business data, and a relay
// worker publishes them to Kafka — at-least-once, in id order.
//
//	use-case tx:  INSERT INTO orders ...; outbox.Enqueue(...)   ← atomic
//	relay loop:   SELECT ... FOR UPDATE SKIP LOCKED → produce → DELETE
//
// Compared to events.ChannelPublisher (same-service, at-most-once) the
// outbox survives crashes and crosses service boundaries; both sit behind
// the feature's event-producer port, so the choice is a composition-root
// concern.
package outbox

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/protobuf/proto"

	"github.com/KucherenkoIvan/go-kernel/ddd"
	"github.com/KucherenkoIvan/go-kernel/kafka"
	"github.com/KucherenkoIvan/go-kernel/requestid"
)

// MigrationSQL is the outbox table schema — the table is this package's
// contract, applying the migration is the service's duty. Append it to the
// service's migrations verbatim.
const MigrationSQL = `
CREATE TABLE IF NOT EXISTS outbox (
  id         uuid PRIMARY KEY,
  topic      text NOT NULL,
  key        text,
  payload    bytea NOT NULL,
  headers    jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS outbox_created_at_idx ON outbox (created_at);
`

// Exec is the write surface Enqueue needs — postgres.Querier satisfies it,
// so pass client.Resolve(tx).
type Exec interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Option customizes an enqueued message.
type Option func(*kafka.RawMessage)

// WithKey sets the partitioning key — messages with the same key keep their
// order end to end (id order in the table, partition order in Kafka).
func WithKey(key string) Option {
	return func(m *kafka.RawMessage) { m.Key = key }
}

// WithHeader adds a header, delivered as a Kafka header by the relay.
func WithHeader(key, value string) Option {
	return func(m *kafka.RawMessage) {
		if m.Headers == nil {
			m.Headers = map[string]string{}
		}
		m.Headers[key] = value
	}
}

// Enqueue writes one message into the outbox table. Call it inside the same
// transaction that saves the aggregate:
//
//	err := c.txManager.WithinTx(ctx, func(tx ddd.Transaction) error {
//		if err := c.orderRepo.Save(ctx, tx, order); err != nil { return err }
//		return outbox.Enqueue(ctx, c.db.Resolve(tx), kafkacontracts.OrderPlaced,
//			&eventsv1.OrderPlacedEvent{...}, outbox.WithKey(orderID))
//	})
//
// The context's request id is stored in headers and restored by consumers —
// the id survives the outbox hop. Row ids are UUIDv7, so the relay's
// ORDER BY id is creation order.
func Enqueue[T proto.Message](ctx context.Context, q Exec, c kafka.Contract[T], msg T, opts ...Option) error {
	payload, err := kafka.Encode(c, msg)
	if err != nil {
		return fmt.Errorf("outbox: %w", err)
	}

	raw := kafka.RawMessage{Topic: c.Topic, Payload: payload}
	for _, opt := range opts {
		opt(&raw)
	}
	if id := requestid.From(ctx); id != "" {
		if raw.Headers == nil {
			raw.Headers = map[string]string{}
		}
		raw.Headers[requestid.Header] = id
	}

	var headers []byte
	if len(raw.Headers) > 0 {
		if headers, err = json.Marshal(raw.Headers); err != nil {
			return fmt.Errorf("outbox: encoding headers: %w", err)
		}
	}

	var key *string
	if raw.Key != "" {
		key = &raw.Key
	}

	if _, err := q.Exec(ctx,
		`INSERT INTO outbox (id, topic, key, payload, headers) VALUES ($1, $2, $3, $4, $5)`,
		ddd.NewEntityID(), raw.Topic, key, raw.Payload, headers,
	); err != nil {
		return fmt.Errorf("outbox: enqueueing to %s: %w", raw.Topic, err)
	}
	return nil
}
