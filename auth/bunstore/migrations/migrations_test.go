package migrations_test

import (
	"testing"

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
