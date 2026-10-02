package testdb_test

import (
	"testing"

	"github.com/uptrace/bun"

	"github.com/borfast/gorfast/internal/testdb"
)

func TestEachGivesAMigratedEmptyDatabase(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		var n int
		err := db.NewRaw("SELECT count(*) FROM users").Scan(t.Context(), &n)
		if err != nil {
			t.Fatalf("counting users: %v", err)
		}
		if n != 0 {
			t.Fatalf("users count = %d, want 0", n)
		}
	})
}

func TestResetEmptiesTables(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()
		_, err := db.NewRaw(
			"INSERT INTO users (id, email, created_at, updated_at) VALUES (?, ?, ?, ?)",
			"u1", "a@example.com", "2026-01-01 00:00:00", "2026-01-01 00:00:00",
		).Exec(ctx)
		if err != nil {
			t.Fatalf("inserting: %v", err)
		}

		testdb.Reset(t, db)

		var n int
		if err := db.NewRaw("SELECT count(*) FROM users").Scan(ctx, &n); err != nil {
			t.Fatalf("counting users: %v", err)
		}
		if n != 0 {
			t.Fatalf("users count after Reset = %d, want 0", n)
		}
	})
}
