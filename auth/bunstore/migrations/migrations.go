// Package migrations holds the schema auth/bunstore expects. The SQL is
// exported so an application can run it with its own migration tool.
package migrations

import (
	"context"
	"embed"
	"fmt"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
)

//go:embed *.sql
var FS embed.FS

// Apply creates the auth schema on an empty database. It is deliberately
// simple: an application with its own migration tool should read FS instead.
func Apply(ctx context.Context, db *bun.DB) error {
	name := "0001_auth.up.sql"
	if db.Dialect().Name() == dialect.SQLite {
		name = "0001_auth.sqlite.up.sql"
	}

	stmts, err := FS.ReadFile(name)
	if err != nil {
		return fmt.Errorf("reading %s: %w", name, err)
	}

	if _, err := db.ExecContext(ctx, string(stmts)); err != nil {
		return fmt.Errorf("applying %s: %w", name, err)
	}

	return nil
}
