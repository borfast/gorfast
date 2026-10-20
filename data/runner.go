// Package data defines Runner, which services use to make work atomic, and
// BunRunner, its implementation over Bun. Runner's contract has no Bun types.
package data

import (
	"context"
	"errors"
)

// ErrNested is returned by Run when it is called with a context from inside
// another Run.
var ErrNested = errors.New("data: Run inside another Run's transaction")

// Runner runs fn in a new transaction: a non-nil err rolls back and is returned, else it commits and returns the commit error or outcome.
// A panic rolls back and panics again. Run inside another Run's context returns ErrNested without calling fn.
type Runner[R any] interface {
	Run(ctx context.Context, fn func(ctx context.Context, repos R) (outcome, err error)) error
}

// inTx marks a context passed to a Run function.
type inTx struct{}
