package ddd_test

import (
	"context"
	"errors"
	"testing"

	"github.com/KucherenkoIvan/go-kernel/ddd"
	"github.com/KucherenkoIvan/go-kernel/ddd/dddtest"
)

func TestAfterCommit_NoTransactionRunsImmediately(t *testing.T) {
	ran := false
	ddd.AfterCommit(context.Background(), ddd.NoTransaction, func(context.Context) { ran = true })
	if !ran {
		t.Fatal("must run immediately without a transaction")
	}
}

func TestAfterCommit_RunsOnCommitOnly(t *testing.T) {
	txm := &dddtest.FakeTxManager{}

	ran := false
	err := txm.WithinTx(context.Background(), func(tx ddd.Transaction) error {
		ddd.AfterCommit(context.Background(), tx, func(context.Context) { ran = true })
		if ran {
			t.Fatal("must not run inside the transaction")
		}
		return nil
	})
	if err != nil || !ran {
		t.Fatalf("must run after commit: err=%v ran=%v", err, ran)
	}

	ran = false
	_ = txm.WithinTx(context.Background(), func(tx ddd.Transaction) error {
		ddd.AfterCommit(context.Background(), tx, func(context.Context) { ran = true })
		return errors.New("rollback")
	})
	if ran {
		t.Fatal("must not run after rollback")
	}
}
