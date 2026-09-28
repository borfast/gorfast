// Package migrations holds the schema auth/bunstore expects. The SQL is
// exported so an application can run it with its own migration tool.
package migrations

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
)

//go:embed *.sql
var FS embed.FS

// Apply creates the auth schema on an empty database. It runs every matching
// *.up.sql file, in name order, inside one transaction: a failure partway
// through leaves nothing behind, and a second run is harmless because every
// statement is IF NOT EXISTS. An application with its own migration tool
// should read FS instead.
func Apply(ctx context.Context, db bun.IDB) error {
	names, err := migrationFiles(db.Dialect().Name())
	if err != nil {
		return err
	}

	return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		for _, name := range names {
			stmts, err := FS.ReadFile(name)
			if err != nil {
				return fmt.Errorf("reading %s: %w", name, err)
			}
			if _, err := tx.ExecContext(ctx, string(stmts)); err != nil {
				return fmt.Errorf("applying %s: %w", name, err)
			}
		}

		return nil
	})
}

// migrationFiles selects the *.up.sql files for name, sorted by filename so
// a "000N_..." prefix orders them deterministically. SQLite reads its own
// *.sqlite.up.sql files; every other dialect reads the shared *.up.sql files,
// excluding those.
func migrationFiles(name dialect.Name) ([]string, error) {
	all, err := fs.Glob(FS, "*.up.sql")
	if err != nil {
		return nil, fmt.Errorf("listing migrations: %w", err)
	}

	var selected []string
	for _, f := range all {
		isSQLiteFile := strings.Contains(f, ".sqlite.")
		if isSQLiteFile == (name == dialect.SQLite) {
			selected = append(selected, f)
		}
	}
	sort.Strings(selected)

	return selected, nil
}
