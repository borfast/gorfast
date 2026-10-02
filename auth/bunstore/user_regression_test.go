package bunstore

import (
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/borfast/gorfast/auth/bunstore/migrations"
	"github.com/borfast/sulis"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

type sqlStateError string

func (e sqlStateError) Error() string    { return "UNIQUE constraint failed" }
func (e sqlStateError) SQLState() string { return string(e) }

func TestUniqueViolationSQLState(t *testing.T) {
	if !isUniqueViolation(fmtWrapped(sqlStateError("23505"))) {
		t.Fatal("SQLSTATE 23505 must be a unique violation")
	}
	if isUniqueViolation(fmtWrapped(sqlStateError("23503"))) {
		t.Fatal("SQLSTATE 23503 must not be a unique violation")
	}
}

func fmtWrapped(err error) error { return errors.Join(errors.New("wrapped"), err) }

func postgresRegressionTx(t *testing.T, driver string) bun.Tx {
	t.Helper()
	dsn := os.Getenv("GORFAST_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("GORFAST_TEST_POSTGRES_DSN not set")
	}

	var raw *sql.DB
	var err error
	if driver == "pgx" {
		raw, err = sql.Open("pgx", dsn)
		if err != nil {
			t.Fatal(err)
		}
	} else {
		raw = sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	}
	db := bun.NewDB(raw, pgdialect.New())
	t.Cleanup(func() { _ = db.Close() })
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	ddl, err := migrations.FS.ReadFile("0001_auth.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), strings.ReplaceAll(string(ddl), "CREATE TABLE IF NOT EXISTS", "CREATE TEMP TABLE IF NOT EXISTS")); err != nil {
		t.Fatal(err)
	}
	return tx
}

func regressionUser(id, email string) *sulis.User {
	now := time.Now().UTC()
	return &sulis.User{ID: id, Email: email, CreatedAt: now, UpdatedAt: now}
}

func TestPostgresDuplicateUserDrivers(t *testing.T) {
	for _, driver := range []string{"pgdriver", "pgx"} {
		for _, operation := range []string{"duplicate_email_create", "duplicate_id_create", "duplicate_email_update"} {
			t.Run(driver+"/"+operation, func(t *testing.T) {
				store := NewUserStore(postgresRegressionTx(t, driver))
				ctx := t.Context()
				first := regressionUser("first", "One@Example.test")
				second := regressionUser("second", "two@example.test")
				if err := store.CreateUser(ctx, first); err != nil {
					t.Fatal(err)
				}
				var err error
				switch operation {
				case "duplicate_email_create":
					second.Email = "one@example.test"
					err = store.CreateUser(ctx, second)
				case "duplicate_id_create":
					second.ID = first.ID
					err = store.CreateUser(ctx, second)
				case "duplicate_email_update":
					if err := store.CreateUser(ctx, second); err != nil {
						t.Fatal(err)
					}
					second.Email = "one@example.test"
					err = store.UpdateUser(ctx, second)
				}
				if !errors.Is(err, sulis.ErrUserAlreadyExists) {
					t.Fatalf("%s: got %v, want ErrUserAlreadyExists", operation, err)
				}
				got, err := store.GetUserByEmail(ctx, "ONE@example.test")
				if err != nil {
					t.Fatalf("lookup after duplicate create: %v", err)
				}
				if got.ID != first.ID {
					t.Fatalf("lookup returned %q, want %q", got.ID, first.ID)
				}
			})
		}
	}
}
