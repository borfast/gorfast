package migrations_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/borfast/gorfast/auth/bunstore/migrations"
	"github.com/borfast/gorfast/internal/testdb"
)

// Apply must stay safe on a database that already has every migration.
func TestApplyTwice(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		if err := migrations.Apply(t.Context(), db); err != nil {
			t.Fatalf("second Apply: %v", err)
		}
	})
}

// Apply on a database that has only 0001 must add the second-factor tables
// and keep the existing rows.
func TestApplyAfterOnly0001(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		ctx := t.Context()
		tables := []string{
			"totp_credentials", "passkey_credentials", "passkey_challenges",
			"recovery_code_sets", "recovery_codes",
		}
		for _, name := range tables {
			if _, err := db.ExecContext(ctx, "DROP TABLE "+name); err != nil {
				t.Fatalf("dropping %s: %v", name, err)
			}
		}
		_, err := db.ExecContext(ctx,
			"INSERT INTO users (id, email, created_at, updated_at) VALUES (?, ?, ?, ?)",
			"u1", "u1@example.test", time.Now().UTC(), time.Now().UTC())
		if err != nil {
			t.Fatalf("inserting user: %v", err)
		}

		if err := migrations.Apply(ctx, db); err != nil {
			t.Fatalf("Apply: %v", err)
		}

		for _, name := range tables {
			var n int
			if err := db.NewRaw(fmt.Sprintf("SELECT count(*) FROM %s", name)).Scan(ctx, &n); err != nil {
				t.Errorf("table %s missing after Apply: %v", name, err)
			}
		}
		var n int
		if err := db.NewRaw("SELECT count(*) FROM users WHERE id = ?", "u1").Scan(ctx, &n); err != nil || n != 1 {
			t.Errorf("users row count = %d, err = %v, want 1", n, err)
		}
	})
}
