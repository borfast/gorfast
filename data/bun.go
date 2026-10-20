package data

import (
	"context"

	"github.com/uptrace/bun"
)

// BunRunner is a Runner over a Bun database.
type BunRunner[R any] struct {
	db   *bun.DB
	bind func(tx bun.IDB) (R, error)
}

// NewBunRunner returns a BunRunner. For each Run, bind builds the repositories
// from the transaction.
func NewBunRunner[R any](db *bun.DB, bind func(tx bun.IDB) (R, error)) *BunRunner[R] {
	return &BunRunner[R]{db: db, bind: bind}
}

// Run implements Runner. An error from bind is returned without calling fn.
func (r *BunRunner[R]) Run(ctx context.Context, fn func(ctx context.Context, repos R) (outcome, err error)) error {
	if ctx.Value(inTx{}) != nil {
		return ErrNested
	}
	ctx = context.WithValue(ctx, inTx{}, true)

	var outcome error
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		repos, err := r.bind(tx)
		if err != nil {
			return err
		}
		outcome, err = fn(ctx, repos)
		return err
	})
	if err != nil {
		return err
	}
	return outcome
}
