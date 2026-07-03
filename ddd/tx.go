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
