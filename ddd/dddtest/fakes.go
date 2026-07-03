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
}

func (*FakeTx) DDDTransaction() {}

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

func (m *FakeTxManager) WithinTx(_ context.Context, fn func(tx ddd.Transaction) error) error {
	m.Calls++
	tx := &FakeTx{Seq: m.Calls}
	m.LastTransaction = tx
	return fn(tx)
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
