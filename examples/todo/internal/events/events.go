// Package events logs the Sulis events emitted inside a transaction when it
// ends, with a label on those from a transaction that rolled back.
package events

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/borfast/sulis"

	"github.com/borfast/gorfast/data"
)

// hold keeps the events emitted during one Run. Once released, it keeps no more.
type hold struct {
	mu       sync.Mutex
	events   []sulis.Event
	released bool
}

type holdKey struct{}

// add keeps e and reports true, or reports false once the hold is released.
func (h *hold) add(e sulis.Event) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.released {
		return false
	}
	h.events = append(h.events, e)
	return true
}

// release returns the events kept, in the order they came, and keeps no more.
func (h *hold) release() []sulis.Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.released = true
	return h.events
}

type sink struct{ log sulis.EventSink }

// NewSink returns an event sink for sulis.WithEventSink. Inside a Run of a
// runner from Hold it keeps each event for that Run; otherwise it logs it at once.
func NewSink(logger *slog.Logger) sulis.EventSink {
	return sink{log: sulis.NewSlogSink(logger)}
}

func (s sink) Emit(ctx context.Context, e sulis.Event) {
	if h, ok := ctx.Value(holdKey{}).(*hold); ok && h.add(e) {
		return
	}
	s.log.Emit(ctx, e)
}

type holdingRunner[R any] struct {
	inner                 data.Runner[R]
	committed, rolledBack sulis.EventSink
}

// Hold returns a runner whose Run logs the events NewSink kept during it once
// inner's Run ends, those from a rollback with transaction=rolled_back.
func Hold[R any](inner data.Runner[R], logger *slog.Logger) data.Runner[R] {
	return holdingRunner[R]{
		inner:      inner,
		committed:  sulis.NewSlogSink(logger),
		rolledBack: sulis.NewSlogSink(logger.With("transaction", "rolled_back")),
	}
}

// Run implements data.Runner. Inside another Run it adds no hold, and inner
// returns data.ErrNested.
func (r holdingRunner[R]) Run(ctx context.Context, fn func(ctx context.Context, repos R) (outcome, err error)) error {
	if ctx.Value(holdKey{}) != nil {
		return r.inner.Run(ctx, fn)
	}
	h := new(hold)
	var called, committed bool
	var outcome, fnErr error
	// Deferred, so on a panic the events are logged as rolled back before the
	// panic continues.
	defer func() {
		log := r.rolledBack
		if committed {
			log = r.committed
		}
		for _, e := range h.release() {
			log.Emit(ctx, e)
		}
	}()

	runErr := r.inner.Run(context.WithValue(ctx, holdKey{}, h), func(ctx context.Context, repos R) (error, error) {
		called = true
		outcome, fnErr = fn(ctx, repos)
		return outcome, fnErr
	})
	// By the data.Runner contract, inner committed only if fn ran and returned a
	// nil err, and inner then returned fn's outcome.
	committed = called && fnErr == nil && ((outcome == nil && runErr == nil) || (outcome != nil && errors.Is(runErr, outcome)))
	return runErr
}
