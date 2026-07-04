// Package dddtest provides test implementations of the ddd ports.
package dddtest

import (
	"context"
	"fmt"
	"time"

	"github.com/KucherenkoIvan/go-kernel/ddd"
)

// FakeTx is the Transaction token produced by FakeTxManager.
type FakeTx struct {
	Seq int

	afterCommit []func(ctx context.Context)
}

func (*FakeTx) DDDTransaction() {}

// AfterCommit implements ddd.AfterCommitter with the same semantics as the
// postgres token: callbacks run only when the callback given to WithinTx
// returns nil, and are dropped on error ("rollback").
func (t *FakeTx) AfterCommit(fn func(ctx context.Context)) {
	t.afterCommit = append(t.afterCommit, fn)
}

// FakeTxManager does not create real transactions — it just invokes the
// callback and records the token, so tests can assert that a repository was
// called inside the transaction:
//
//	txManager := &dddtest.FakeTxManager{}
//	command := usecoupon.NewUseCouponCommand(txManager, couponRepo, events, permission)
//
//	_ = command.Execute(ctx, userID, couponID)
//
//	// assert couponRepo.Save received txManager.LastTransaction
type FakeTxManager struct {
	Calls           int
	LastTransaction ddd.Transaction
}

func (m *FakeTxManager) WithinTx(ctx context.Context, fn func(tx ddd.Transaction) error) error {
	m.Calls++
	tx := &FakeTx{Seq: m.Calls}
	m.LastTransaction = tx
	if err := fn(tx); err != nil {
		return err
	}
	for _, hook := range tx.afterCommit {
		hook(ctx)
	}
	return nil
}

// FixedClock always returns Time.
type FixedClock struct {
	Time time.Time
}

func (c FixedClock) Now() time.Time { return c.Time }

// SequenceIDGenerator returns "id-1", "id-2", … (or "<Prefix>-1", … when
// Prefix is set) for deterministic assertions.
type SequenceIDGenerator struct {
	Prefix string
	n      int
}

func (g *SequenceIDGenerator) NewID() string {
	g.n++
	prefix := g.Prefix
	if prefix == "" {
		prefix = "id"
	}
	return fmt.Sprintf("%s-%d", prefix, g.n)
}
