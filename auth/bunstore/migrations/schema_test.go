package migrations_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"

	"github.com/borfast/gorfast/auth/bunstore"
	"github.com/borfast/gorfast/internal/testdb"
)

type idColumn struct {
	table, column string
	nullable      bool
}

var idColumns = []idColumn{
	{"users", "id", false},
	{"sessions", "id", false},
	{"sessions", "user_id", false},
	{"tokens", "id", false},
	{"tokens", "user_id", true},
	{"totp_credentials", "user_id", false},
	{"totp_credentials", "active_id", true},
	{"totp_credentials", "pending_id", true},
	{"passkey_credentials", "id", false},
	{"passkey_credentials", "user_id", false},
	{"recovery_code_sets", "user_id", false},
	{"recovery_codes", "user_id", false},
}

// insertColumns lists, per table, the columns a test insert fills.
var insertColumns = map[string][]string{
	"users":               {"id", "email", "created_at", "updated_at"},
	"sessions":            {"id", "user_id", "token_hash", "expires_at", "created_at", "authenticated_at", "last_seen_at"},
	"tokens":              {"id", "user_id", "token_hash", "purpose", "expires_at", "created_at"},
	"totp_credentials":    {"user_id", "active_id", "pending_id"},
	"passkey_credentials": {"id", "user_id", "credential_id", "public_key", "created_at"},
	"recovery_code_sets":  {"user_id", "replaced_at"},
	"recovery_codes":      {"user_id", "code_hash"},
}

// freshValue is a valid, unused value for column, so inserts never collide.
func freshValue(column string) any {
	switch {
	case column == "credential_id" || column == "public_key":
		return []byte(uuid.NewV7().String())
	case column == "id" || strings.HasSuffix(column, "_id"):
		return bunstore.ID(uuid.NewV7())
	case strings.HasSuffix(column, "_at"):
		return time.Now().UTC()
	default:
		return uuid.NewV7().String()
	}
}

// insertWith inserts one row into c.table with value in c.column.
func insertWith(t *testing.T, db *bun.DB, c idColumn, value any) error {
	t.Helper()

	cols := insertColumns[c.table]
	args := make([]any, len(cols))
	for i, col := range cols {
		if col == c.column {
			args[i] = value
		} else {
			args[i] = freshValue(col)
		}
	}
	marks := strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ")
	q := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", c.table, strings.Join(cols, ", "), marks)
	_, err := db.ExecContext(t.Context(), q, args...)
	return err
}

func TestSchemaIDColumns(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		sqlite := db.Dialect().Name() == dialect.SQLite

		for _, c := range idColumns {
			t.Run(c.table+"."+c.column, func(t *testing.T) {
				if err := insertWith(t, db, c, bunstore.ID(uuid.NewV7())); err != nil {
					t.Fatalf("a 16-byte ID was refused: %v", err)
				}
				err := insertWith(t, db, c, nil)
				if c.nullable && err != nil {
					t.Errorf("NULL was refused: %v", err)
				}
				if !c.nullable && err == nil {
					t.Error("NULL was accepted, want it refused")
				}
				if !sqlite {
					return
				}
				for name, v := range map[string]any{
					"15 bytes":          make([]byte, 15),
					"17 bytes":          make([]byte, 17),
					"16-character text": "abcdefghijklmnop",
				} {
					if err := insertWith(t, db, c, v); err == nil {
						t.Errorf("%s was accepted, want it refused", name)
					}
				}
			})
		}

		if !sqlite {
			t.Run("users.id malformed text", func(t *testing.T) {
				if err := insertWith(t, db, idColumns[0], "not-a-uuid"); err == nil {
					t.Error("a malformed UUID was accepted, want it refused")
				}
			})
		}
	})
}
