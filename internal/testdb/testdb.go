// Package testdb opens real databases for tests. SQLite always runs. Postgres
// runs when GORFAST_TEST_POSTGRES_DSN is set, and is skipped otherwise.
package testdb

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/pgdriver"
	_ "modernc.org/sqlite"

	"github.com/borfast/gorfast/auth/bunstore/migrations"
)

// PostgresDSNEnv names the environment variable holding a Postgres DSN.
// Without it the Postgres subtests skip rather than fail.
const PostgresDSNEnv = "GORFAST_TEST_POSTGRES_DSN"

// tables is every table Reset empties, children before parents.
var tables = []string{"sessions", "tokens", "users"}

// Each runs fn once per available dialect, as a subtest named for the dialect.
func Each(t *testing.T, fn func(t *testing.T, db *bun.DB)) {
	t.Helper()

	t.Run("sqlite", func(t *testing.T) {
		fn(t, openSQLite(t))
	})

	t.Run("postgres", func(t *testing.T) {
		dsn := os.Getenv(PostgresDSNEnv)
		if dsn == "" {
			t.Skipf("%s not set, skipping Postgres", PostgresDSNEnv)
		}
		fn(t, openPostgres(t, dsn))
	})
}

// Reset empties every table, so a storetest factory can hand back a fresh,
// empty store on every call.
func Reset(t *testing.T, db *bun.DB) {
	t.Helper()

	for _, table := range tables {
		if _, err := db.NewRaw("DELETE FROM " + table).Exec(t.Context()); err != nil {
			t.Fatalf("emptying %s: %v", table, err)
		}
	}
}

func openSQLite(t *testing.T) *bun.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.db")
	sqldb, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("opening sqlite: %v", err)
	}

	db := bun.NewDB(sqldb, sqlitedialect.New())
	t.Cleanup(func() { _ = db.Close() })
	return migrate(t, db)
}

// openPostgres gives each test its own schema, so tests never collide and
// cleanup is a single DROP.
func openPostgres(t *testing.T, dsn string) *bun.DB {
	t.Helper()

	admin := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn))), pgdialect.New())
	t.Cleanup(func() { _ = admin.Close() })

	schema := fmt.Sprintf("test_%d", os.Getpid()^int(hashName(t.Name())))
	ctx := t.Context()

	if _, err := admin.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); err != nil {
		t.Fatalf("dropping schema: %v", err)
	}
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("creating schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.WithoutCancel(ctx), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	})

	connector := pgdriver.NewConnector(
		pgdriver.WithDSN(dsn),
		pgdriver.WithConnParams(map[string]any{"search_path": schema}),
	)
	db := bun.NewDB(sql.OpenDB(connector), pgdialect.New())
	t.Cleanup(func() { _ = db.Close() })

	return migrate(t, db)
}

func migrate(t *testing.T, db *bun.DB) *bun.DB {
	t.Helper()

	if err := migrations.Apply(t.Context(), db); err != nil {
		t.Fatalf("applying migrations: %v", err)
	}

	return db
}

func hashName(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h = (h ^ uint32(s[i])) * 16777619
	}
	return h
}
