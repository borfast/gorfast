package events_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/borfast/sulis"

	"github.com/borfast/gorfast/data"
	"github.com/borfast/gorfast/examples/todo/internal/events"
)

// logBuffer collects log lines, which tests write from several goroutines.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

// lines returns the lines logged so far.
func (l *logBuffer) lines() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	text := strings.TrimSuffix(l.buf.String(), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

// inFake marks a context passed to a fakeRunner's fn.
type inFake struct{}

// fakeRunner follows the data.Runner contract without a database. A non-nil
// commitErr is returned as a commit that failed.
type fakeRunner struct{ commitErr error }

func (f fakeRunner) Run(ctx context.Context, fn func(ctx context.Context, repos struct{}) (outcome, err error)) error {
	if ctx.Value(inFake{}) != nil {
		return data.ErrNested
	}
	outcome, err := fn(context.WithValue(ctx, inFake{}, true), struct{}{})
	if err != nil {
		return err
	}
	if f.commitErr != nil {
		return f.commitErr
	}
	return outcome
}

var (
	errFail    = errors.New("test: fail")
	errOutcome = errors.New("test: an outcome that commits")
)

type fixture struct {
	logs *logBuffer
	sink sulis.EventSink
	run  data.Runner[struct{}]
}

func setup(inner fakeRunner) fixture {
	logs := new(logBuffer)
	logger := slog.New(slog.NewTextHandler(logs, nil))
	return fixture{logs: logs, sink: events.NewSink(logger), run: events.Hold[struct{}](inner, logger)}
}

func (f fixture) emit(ctx context.Context, kind sulis.EventKind) {
	f.sink.Emit(ctx, sulis.Event{Kind: kind})
}

// requireLogged checks that the log holds exactly kinds, in order, each line
// with the rolled-back label when rolledBack is true and without it otherwise.
func requireLogged(t *testing.T, f fixture, rolledBack bool, kinds ...sulis.EventKind) {
	t.Helper()
	lines := f.logs.lines()
	if len(lines) != len(kinds) {
		t.Fatalf("logged %d lines, want %d:\n%s", len(lines), len(kinds), strings.Join(lines, "\n"))
	}
	for i, line := range lines {
		fields := strings.Fields(line)
		if !slices.Contains(fields, "kind="+string(kinds[i])) {
			t.Errorf("line %d = %q, want kind=%s", i, line, kinds[i])
		}
		if rolledBack && !slices.Contains(fields, "transaction=rolled_back") {
			t.Errorf("line %d = %q, want transaction=rolled_back", i, line)
		}
		if !rolledBack && strings.Contains(line, "transaction=") {
			t.Errorf("line %d = %q, want no transaction label", i, line)
		}
	}
}

func TestCommittedRunLogsItsEventsWhenItEnds(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outcome error
	}{
		{"nil outcome", nil},
		{"an outcome", errOutcome},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(fakeRunner{})
			err := f.run.Run(t.Context(), func(ctx context.Context, _ struct{}) (error, error) {
				f.emit(ctx, sulis.EventAccountRegistered)
				f.emit(ctx, sulis.EventSessionIssued)
				if lines := f.logs.lines(); len(lines) != 0 {
					t.Errorf("logged before the transaction ended:\n%s", strings.Join(lines, "\n"))
				}
				return tc.outcome, nil
			})
			if !errors.Is(err, tc.outcome) || (tc.outcome == nil && err != nil) {
				t.Fatalf("Run = %v, want %v", err, tc.outcome)
			}
			requireLogged(t, f, false, sulis.EventAccountRegistered, sulis.EventSessionIssued)
		})
	}
}

func TestRolledBackRunLogsItsEventsWithTheLabel(t *testing.T) {
	for _, tc := range []struct {
		name          string
		inner         fakeRunner
		outcome, fail error
		want          error
	}{
		{"fn's error", fakeRunner{}, nil, errFail, errFail},
		{"a failed commit", fakeRunner{commitErr: errFail}, nil, nil, errFail},
		{"a failed commit of an outcome", fakeRunner{commitErr: errFail}, errOutcome, nil, errFail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(tc.inner)
			err := f.run.Run(t.Context(), func(ctx context.Context, _ struct{}) (error, error) {
				f.emit(ctx, sulis.EventAccountRegistered)
				f.emit(ctx, sulis.EventSessionIssued)
				return tc.outcome, tc.fail
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("Run = %v, want %v", err, tc.want)
			}
			requireLogged(t, f, true, sulis.EventAccountRegistered, sulis.EventSessionIssued)
		})
	}
}

func TestPanicLogsTheEventsAsRolledBack(t *testing.T) {
	f := setup(fakeRunner{})
	defer func() {
		if p := recover(); p != "test: panic" {
			t.Errorf("recovered %v, want the panic from fn", p)
		}
		requireLogged(t, f, true, sulis.EventAccountRegistered)
	}()
	_ = f.run.Run(t.Context(), func(ctx context.Context, _ struct{}) (error, error) {
		f.emit(ctx, sulis.EventAccountRegistered)
		panic("test: panic")
	})
	t.Error("Run returned after fn panicked")
}

func TestEventOutsideRunIsLoggedAtOnce(t *testing.T) {
	f := setup(fakeRunner{})
	f.emit(t.Context(), sulis.EventCSRFRejected)
	requireLogged(t, f, false, sulis.EventCSRFRejected)
}

func TestEventAfterRunEndsIsLoggedAtOnce(t *testing.T) {
	f := setup(fakeRunner{})
	var held context.Context
	if err := f.run.Run(t.Context(), func(ctx context.Context, _ struct{}) (error, error) {
		held = ctx
		return nil, nil
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	f.emit(held, sulis.EventSessionIssued)
	requireLogged(t, f, false, sulis.EventSessionIssued)
}

func TestNestedRunIsRefused(t *testing.T) {
	f := setup(fakeRunner{})
	err := f.run.Run(t.Context(), func(ctx context.Context, _ struct{}) (error, error) {
		f.emit(ctx, sulis.EventAccountRegistered)
		err := f.run.Run(ctx, func(context.Context, struct{}) (error, error) {
			t.Error("the nested Run called fn")
			return nil, nil
		})
		if !errors.Is(err, data.ErrNested) {
			t.Errorf("nested Run = %v, want ErrNested", err)
		}
		return nil, nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	requireLogged(t, f, false, sulis.EventAccountRegistered)
}

func TestConcurrentEmitsAreAllLogged(t *testing.T) {
	const n = 20
	f := setup(fakeRunner{})
	err := f.run.Run(t.Context(), func(ctx context.Context, _ struct{}) (error, error) {
		var wg sync.WaitGroup
		for range n {
			wg.Go(func() { f.emit(ctx, sulis.EventLoginFailed) })
		}
		wg.Wait()
		return nil, nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	kinds := make([]sulis.EventKind, n)
	for i := range kinds {
		kinds[i] = sulis.EventLoginFailed
	}
	requireLogged(t, f, false, kinds...)
}
