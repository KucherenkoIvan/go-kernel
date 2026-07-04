package ddd

import "context"

// Transaction is a lightweight token. It does not carry a native DB
// transaction — it serves as a key for obtaining one in adapters
// (the infra package that owns the DB client type-asserts the token back
// to its concrete implementation).
//
// Infra packages implement it by declaring the DDDTransaction marker method
// on their own transaction type.
type Transaction interface {
	DDDTransaction()
}

type noTx struct{}

func (noTx) DDDTransaction() {}

// NoTransaction is passed where a transaction is deliberately not needed.
// Repositories and producers take Transaction as a leading parameter so that
// every call site explicitly answers the question "does this need a
// transaction?" — if not, NoTransaction is passed consciously instead of the
// transaction being forgotten by accident.
var NoTransaction Transaction = noTx{}

// TxManager is the port for running work inside a single DB transaction.
// Everything inside fn runs in one transaction; if fn returns an error, the
// transaction is rolled back.
type TxManager interface {
	WithinTx(ctx context.Context, fn func(tx Transaction) error) error
}

// AfterCommitter is implemented by Transaction tokens that support
// post-commit callbacks (the postgres token and the dddtest fake do).
type AfterCommitter interface {
	AfterCommit(fn func(ctx context.Context))
}

// AfterCommit schedules fn to run after the transaction commits
// successfully; if the transaction rolls back, fn never runs. With
// NoTransaction (or any token without hook support) fn runs immediately.
//
// This is what makes in-process event dispatch safe: events buffered on the
// transaction are delivered only for state that actually exists — no ghost
// events from rolled-back transactions, no handlers reading uncommitted
// state.
func AfterCommit(ctx context.Context, tx Transaction, fn func(ctx context.Context)) {
	if h, ok := tx.(AfterCommitter); ok {
		h.AfterCommit(fn)
		return
	}
	fn(ctx)
}
