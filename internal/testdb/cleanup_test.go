package testdb_test

import (
	"testing"

	"github.com/borfast/gorfast/internal/testdb"
	"github.com/uptrace/bun"
)

func TestSQLiteHarnessClosesDatabase(t *testing.T) {
	var opened *bun.DB
	t.Run("owner", func(t *testing.T) {
		testdb.Each(t, func(t *testing.T, db *bun.DB) {
			if t.Name() == "TestSQLiteHarnessClosesDatabase/owner/sqlite" {
				opened = db
			}
		})
	})
	if opened == nil {
		t.Fatal("SQLite did not run")
	}
	defer opened.Close()
	if n := opened.Stats().OpenConnections; n != 0 {
		t.Errorf("after test cleanup: %d open SQLite connections; want 0", n)
	}
}
