package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/KucherenkoIvan/go-kernel/ddd"
	"github.com/KucherenkoIvan/go-kernel/kafka"
	"github.com/KucherenkoIvan/go-kernel/postgres"
)

// RawProducer is what the relay publishes through — *kafka.Producer in
// production, a fake in tests.
type RawProducer interface {
	SendRaw(ctx context.Context, msgs ...kafka.RawMessage) error
}

// Mode controls overlap-protection between relay iterations (and replicas).
type Mode string

const (
	// ModeSerialized (default): pg_try_advisory_xact_lock elects exactly one
	// relayer per iteration; strict global ORDER BY id. Keep it while one
	// relayer's throughput covers the load.
	ModeSerialized Mode = "serialized"
	// ModeParallel: FOR UPDATE SKIP LOCKED — concurrent relayers take
	// disjoint rows. Ordering holds per Kafka key only.
	ModeParallel Mode = "parallel"
)

// RelayConfig configures the relay worker.
type RelayConfig struct {
	// Interval between iterations. Default 10s.
	Interval time.Duration
	// BatchSize per fetch. Default 100.
	BatchSize int
	// Mode defaults to ModeSerialized.
	Mode Mode
	// Logger defaults to slog.Default().
	Logger *slog.Logger
}

// Relay moves rows from the outbox table to Kafka: fetch → produce → delete,
// all in one transaction — either everything is published and deleted, or
// nothing is. Wire it as an app.Adapter:
//
//	relay := outbox.NewRelay(db, producer, outbox.RelayConfig{})
//	app.Adapter{Name: "outbox-relay", Run: relay.Run}
type Relay struct {
	db       *postgres.Client
	producer RawProducer
	cfg      RelayConfig
}

func NewRelay(db *postgres.Client, producer RawProducer, cfg RelayConfig) *Relay {
	if cfg.Interval <= 0 {
		cfg.Interval = 10 * time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	if cfg.Mode == "" {
		cfg.Mode = ModeSerialized
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Relay{db: db, producer: producer, cfg: cfg}
}

// Run relays on the configured interval until ctx is cancelled. Blocks;
// iteration errors are logged, not fatal — the next tick retries.
func (r *Relay) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if _, err := r.RelayOnce(ctx); err != nil && ctx.Err() == nil {
				r.cfg.Logger.Error("outbox relay iteration failed", "error", err)
			}
		}
	}
}

// RelayOnce drains the outbox (repeated batches until empty) and reports how
// many messages were published. Exported for tests and manual ticks.
func (r *Relay) RelayOnce(ctx context.Context) (int, error) {
	total := 0
	for {
		n, err := r.relayBatch(ctx)
		total += n
		if err != nil || n < r.cfg.BatchSize {
			return total, err
		}
	}
}

type row struct {
	id      string
	msg     kafka.RawMessage
	headers []byte
}

func (r *Relay) relayBatch(ctx context.Context) (int, error) {
	published := 0
	err := r.db.WithinTx(ctx, func(tx ddd.Transaction) error {
		q := r.db.Resolve(tx)

		if r.cfg.Mode == ModeSerialized {
			var locked bool
			if err := q.QueryRow(ctx,
				`SELECT pg_try_advisory_xact_lock(hashtextextended('go-kernel:outbox-relay', 0))`,
			).Scan(&locked); err != nil {
				return fmt.Errorf("acquiring relay lock: %w", err)
			}
			if !locked {
				return nil // another relayer owns this iteration
			}
		}

		// id is UUIDv7 → ORDER BY id is strict creation order
		query := `SELECT id, topic, key, payload, headers FROM outbox ORDER BY id LIMIT $1`
		if r.cfg.Mode == ModeParallel {
			query += ` FOR UPDATE SKIP LOCKED`
		}
		rows, err := q.Query(ctx, query, r.cfg.BatchSize)
		if err != nil {
			return fmt.Errorf("fetching outbox rows: %w", err)
		}
		batch, err := scanRows(rows)
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}

		ids := make([]string, 0, len(batch))
		msgs := make([]kafka.RawMessage, 0, len(batch))
		for _, item := range batch {
			ids = append(ids, item.id)
			msgs = append(msgs, item.msg)
		}

		if err := r.producer.SendRaw(ctx, msgs...); err != nil {
			return fmt.Errorf("publishing: %w", err) // tx rolls back, rows stay
		}
		if _, err := q.Exec(ctx, `DELETE FROM outbox WHERE id = ANY($1)`, ids); err != nil {
			return fmt.Errorf("deleting published rows: %w", err)
		}
		published = len(batch)
		return nil
	})
	return published, err
}

func scanRows(rows interface {
	Next() bool
	Scan(dest ...any) error
	Close()
	Err() error
}) ([]row, error) {
	defer rows.Close()
	var batch []row
	for rows.Next() {
		var item row
		var key *string
		if err := rows.Scan(&item.id, &item.msg.Topic, &key, &item.msg.Payload, &item.headers); err != nil {
			return nil, fmt.Errorf("scanning outbox row: %w", err)
		}
		if key != nil {
			item.msg.Key = *key
		}
		if len(item.headers) > 0 {
			if err := json.Unmarshal(item.headers, &item.msg.Headers); err != nil {
				return nil, fmt.Errorf("decoding headers of outbox row %s: %w", item.id, err)
			}
		}
		batch = append(batch, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading outbox rows: %w", err)
	}
	return batch, nil
}

// PendingStats reports queue depth for monitoring/alerting: row count and
// the age of the oldest pending row (nil when empty). A growing age means
// the relay is down or falling behind.
func (r *Relay) PendingStats(ctx context.Context) (count int, oldestAge *time.Duration, err error) {
	var seconds *float64
	err = r.db.Resolve(ddd.NoTransaction).QueryRow(ctx,
		`SELECT COUNT(*), EXTRACT(EPOCH FROM (NOW() - MIN(created_at)))::float8 FROM outbox`,
	).Scan(&count, &seconds)
	if err != nil {
		return 0, nil, fmt.Errorf("outbox: pending stats: %w", err)
	}
	if seconds != nil {
		age := time.Duration(*seconds * float64(time.Second))
		oldestAge = &age
	}
	return count, oldestAge, nil
}
