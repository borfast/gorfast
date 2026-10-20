// Package storage keeps the example's profiles and to-dos with Bun. Names and
// to-do text are sealed with crypt before they reach the database.
package storage

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"

	"github.com/borfast/gorfast/auth/bunstore/migrations"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Apply creates the auth tables, then this application's tables, in one
// transaction. Every statement is IF NOT EXISTS, so a second run is harmless.
func Apply(ctx context.Context, db bun.IDB) error {
	names, err := migrationFiles(db.Dialect().Name())
	if err != nil {
		return err
	}

	return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := migrations.Apply(ctx, tx); err != nil {
			return err
		}
		for _, name := range names {
			stmts, err := migrationFS.ReadFile(name)
			if err != nil {
				return fmt.Errorf("storage: reading %s: %w", name, err)
			}
			if _, err := tx.ExecContext(ctx, string(stmts)); err != nil {
				return fmt.Errorf("storage: applying %s: %w", name, err)
			}
		}
		return nil
	})
}

// migrationFiles selects the files for name in name order, as
// auth/bunstore/migrations does: SQLite reads *.sqlite.up.sql, others the rest.
func migrationFiles(name dialect.Name) ([]string, error) {
	all, err := fs.Glob(migrationFS, "migrations/*.up.sql")
	if err != nil {
		return nil, fmt.Errorf("storage: listing migrations: %w", err)
	}

	var selected []string
	for _, f := range all {
		if strings.Contains(f, ".sqlite.") == (name == dialect.SQLite) {
			selected = append(selected, f)
		}
	}
	sort.Strings(selected)
	return selected, nil
}
