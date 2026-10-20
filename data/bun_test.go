package data_test

import (
	"context"
	"errors"
	"testing"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"

	"github.com/borfast/gorfast/data"
	"github.com/borfast/gorfast/data/datatest"
	"github.com/borfast/gorfast/internal/testdb"
)

var (
	errOutcome = errors.New("test: outcome")
	errFail    = errors.New("test: fail")
	errBind    = errors.New("test: bind")
)

type probe struct{ db bun.IDB }

func (p probe) Add(ctx context.Context, name string) error {
	_, err := p.db.NewRaw("INSERT INTO data_probe (name) VALUES (?)", name).Exec(ctx)
	return err
}

func exec(t *testing.T, db *bun.DB, query string) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), query); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func names(db *bun.DB) func(ctx context.Context) ([]string, error) {
	return func(ctx context.Context) ([]string, error) {
		var out []string
		err := db.NewRaw("SELECT name FROM data_probe ORDER BY name").Scan(ctx, &out)
		return out, err
	}
}

func TestBunRunner(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		datatest.RunRunner(t, func(t *testing.T) (data.Runner[datatest.Probe], func(ctx context.Context) ([]string, error)) {
			exec(t, db, "CREATE TABLE IF NOT EXISTS data_probe (name TEXT NOT NULL)")
			exec(t, db, "DELETE FROM data_probe")
			r := data.NewBunRunner(db, func(tx bun.IDB) (datatest.Probe, error) {
				return probe{tx}, nil
			})
			return r, names(db)
		})
	})
}

func TestBunRunnerSavepointRollsBackWithOuter(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		exec(t, db, "CREATE TABLE IF NOT EXISTS data_probe (name TEXT NOT NULL)")
		exec(t, db, "DELETE FROM data_probe")
		r := data.NewBunRunner(db, func(tx bun.IDB) (bun.IDB, error) { return tx, nil })
		err := r.Run(t.Context(), func(ctx context.Context, repos bun.IDB) (error, error) {
			err := repos.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
				return probe{tx}.Add(ctx, "a")
			})
			if err != nil {
				return nil, err
			}
			return nil, errFail
		})
		if !errors.Is(err, errFail) {
			t.Errorf("Run returned %v, want errFail", err)
		}
		got, err := names(db)(t.Context())
		if err != nil || len(got) != 0 {
			t.Errorf("names = %v, %v; want none", got, err)
		}
	})
}

func TestBunRunnerCommitFailureWinsOverOutcome(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		var insert func(ctx context.Context, tx bun.IDB) error
		var count func() int
		if db.Dialect().Name() == dialect.PG {
			exec(t, db, "CREATE TABLE data_unique (name TEXT NOT NULL, UNIQUE (name) DEFERRABLE INITIALLY DEFERRED)")
			insert = func(ctx context.Context, tx bun.IDB) error {
				for range 2 {
					if _, err := tx.NewRaw("INSERT INTO data_unique (name) VALUES ('a')").Exec(ctx); err != nil {
						return err
					}
				}
				return nil
			}
			count = func() int { return countRows(t, db, "data_unique") }
		} else {
			db.SetMaxOpenConns(1)
			exec(t, db, "PRAGMA foreign_keys = ON")
			exec(t, db, "CREATE TABLE data_parent (id INTEGER PRIMARY KEY)")
			exec(t, db, "CREATE TABLE data_child (parent_id INTEGER REFERENCES data_parent (id) DEFERRABLE INITIALLY DEFERRED)")
			insert = func(ctx context.Context, tx bun.IDB) error {
				_, err := tx.NewRaw("INSERT INTO data_child (parent_id) VALUES (1)").Exec(ctx)
				return err
			}
			count = func() int { return countRows(t, db, "data_child") }
		}
		r := data.NewBunRunner(db, func(tx bun.IDB) (bun.IDB, error) { return tx, nil })
		err := r.Run(t.Context(), func(ctx context.Context, repos bun.IDB) (error, error) {
			return errOutcome, insert(ctx, repos)
		})
		if err == nil || errors.Is(err, errOutcome) {
			t.Errorf("Run returned %v, want a commit error and not errOutcome", err)
		}
		if n := count(); n != 0 {
			t.Errorf("%d rows written, want 0", n)
		}
	})
}

func countRows(t *testing.T, db *bun.DB, table string) int {
	t.Helper()
	var n int
	if err := db.NewRaw("SELECT COUNT(*) FROM "+table).Scan(t.Context(), &n); err != nil {
		t.Fatalf("counting %s: %v", table, err)
	}
	return n
}

func TestBunRunnerBindError(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		r := data.NewBunRunner(db, func(bun.IDB) (struct{}, error) { return struct{}{}, errBind })
		called := false
		err := r.Run(t.Context(), func(context.Context, struct{}) (error, error) {
			called = true
			return nil, nil
		})
		if !errors.Is(err, errBind) {
			t.Errorf("Run returned %v, want errBind", err)
		}
		if called {
			t.Error("Run called fn after a bind error")
		}
	})
}
