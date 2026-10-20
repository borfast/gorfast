// Package datatest provides a conformance suite for data.Runner
// implementations.
package datatest

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/borfast/gorfast/data"
)

// Probe is the repository the suite runs against. Add records a name that
// becomes visible to the lister only after the surrounding Run commits.
type Probe interface {
	Add(ctx context.Context, name string) error
}

var (
	errOutcome = errors.New("datatest: outcome")
	errFail    = errors.New("datatest: fail")
)

// RunRunner checks the contract that every data.Runner must satisfy. It calls
// newRunner once per subtest. Its second result lists every committed name.
func RunRunner(t *testing.T, newRunner func(t *testing.T) (data.Runner[Probe], func(ctx context.Context) ([]string, error))) {
	t.Helper()

	// check runs fn once and compares the error and committed names.
	check := func(t *testing.T, fn func(ctx context.Context, p Probe) (error, error), verify func(t *testing.T, err error), want []string) {
		t.Helper()
		r, list := newRunner(t)
		err := r.Run(t.Context(), func(ctx context.Context, p Probe) (error, error) {
			return fn(ctx, p)
		})
		verify(t, err)
		requireNames(t, list, want)
	}

	t.Run("CommitsOnSuccess", func(t *testing.T) {
		check(t, func(ctx context.Context, p Probe) (error, error) {
			return nil, p.Add(ctx, "a")
		}, func(t *testing.T, err error) {
			if err != nil {
				t.Errorf("Run returned %v, want nil", err)
			}
		}, []string{"a"})
	})
	t.Run("CommitsThenReturnsOutcome", func(t *testing.T) {
		check(t, func(ctx context.Context, p Probe) (error, error) {
			return errOutcome, p.Add(ctx, "a")
		}, func(t *testing.T, err error) {
			if !errors.Is(err, errOutcome) {
				t.Errorf("Run returned %v, want errOutcome", err)
			}
		}, []string{"a"})
	})
	t.Run("RollsBackOnFailure", func(t *testing.T) {
		check(t, func(ctx context.Context, p Probe) (error, error) {
			if err := p.Add(ctx, "a"); err != nil {
				return nil, err
			}
			return nil, errFail
		}, func(t *testing.T, err error) {
			if !errors.Is(err, errFail) {
				t.Errorf("Run returned %v, want errFail", err)
			}
		}, nil)
	})
	t.Run("FailureWinsOverOutcome", func(t *testing.T) {
		check(t, func(ctx context.Context, p Probe) (error, error) {
			if err := p.Add(ctx, "a"); err != nil {
				return nil, err
			}
			return errOutcome, errFail
		}, func(t *testing.T, err error) {
			if !errors.Is(err, errFail) || errors.Is(err, errOutcome) {
				t.Errorf("Run returned %v, want errFail and not errOutcome", err)
			}
		}, nil)
	})
	t.Run("RollsBackOnPanic", func(t *testing.T) {
		r, list := newRunner(t)
		var recovered any
		func() {
			defer func() { recovered = recover() }()
			_ = r.Run(t.Context(), func(ctx context.Context, p Probe) (error, error) {
				if err := p.Add(ctx, "a"); err != nil {
					return nil, err
				}
				panic("boom")
			})
		}()
		if recovered != "boom" {
			t.Errorf("recovered %v, want \"boom\"", recovered)
		}
		requireNames(t, list, nil)
	})
	t.Run("RefusesNesting", func(t *testing.T) {
		r, list := newRunner(t)
		called := false
		err := r.Run(t.Context(), func(ctx context.Context, p Probe) (error, error) {
			inner := r.Run(ctx, func(context.Context, Probe) (error, error) {
				called = true
				return nil, nil
			})
			if !errors.Is(inner, data.ErrNested) {
				t.Errorf("nested Run returned %v, want ErrNested", inner)
			}
			return nil, p.Add(ctx, "a")
		})
		if err != nil {
			t.Errorf("outer Run returned %v, want nil", err)
		}
		if called {
			t.Error("nested Run called its function")
		}
		requireNames(t, list, []string{"a"})
	})
}

func requireNames(t *testing.T, list func(ctx context.Context) ([]string, error), want []string) {
	t.Helper()
	got, err := list(t.Context())
	if err != nil {
		t.Fatalf("listing names: %v", err)
	}
	if !slices.Equal(got, want) && (len(got) != 0 || len(want) != 0) {
		t.Errorf("committed names = %v, want %v", got, want)
	}
}
