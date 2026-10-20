// Package data defines the transaction runner that services use to make work
// atomic without depending on a database library.
package data

import (
	"context"
	"errors"
)

// ErrNested is returned by Run when it is called with a context from inside
// another Run.
var ErrNested = errors.New("data: Run inside another Run's transaction")

// Runner runs a function in a transaction that it owns, and gives the function
// repositories bound to that transaction.
//
// If err is non-nil, Run rolls back and returns err. Otherwise it commits, and
// returns the commit error if there is one, else outcome. Return (outcome, nil)
// to keep writes that must survive an expected failure. A panic in fn rolls
// back and panics again. Run called with a context from inside another Run
// returns ErrNested without calling fn.
type Runner[R any] interface {
	Run(ctx context.Context, fn func(ctx context.Context, repos R) (outcome, err error)) error
}

// inTx marks a context passed to a Run function.
type inTx struct{}
