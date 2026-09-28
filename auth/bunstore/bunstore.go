// Package bunstore implements Sulis's store interfaces over Bun.
//
// Every store takes a bun.IDB rather than a *bun.DB, so the same store works
// against a connection or inside a transaction.
package bunstore

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun/driver/pgdriver"
)

// sqliteConstraintUnique and sqliteConstraintPrimary are SQLite's extended
// result codes for a unique and a primary key constraint violation.
const (
	sqliteConstraintUnique  = 2067
	sqliteConstraintPrimary = 1555
)

// coder matches modernc.org/sqlite's error type without importing it, so this
// package does not depend on a driver it never opens.
type coder interface{ Code() int }

// sqlStater matches PostgreSQL errors from drivers such as pgx.
type sqlStater interface{ SQLState() string }

// isUniqueViolation reports whether err is a unique constraint violation, on
// either dialect. Callers turn it into the sentinel their contract names.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}

	var pgErr pgdriver.Error
	if errors.As(err, &pgErr) {
		return pgErr.Field('C') == "23505"
	}
	var pgState sqlStater
	if errors.As(err, &pgState) {
		return pgState.SQLState() == "23505"
	}

	var c coder
	if errors.As(err, &c) {
		return c.Code() == sqliteConstraintUnique || c.Code() == sqliteConstraintPrimary
	}

	// modernc.org/sqlite has not always exposed Code() on every error path.
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// requireOneRow turns an affected-row count of zero into notFound.
// Several contracts require a scoped statement matching nothing to be an error.
func requireOneRow(res sql.Result, notFound error, what string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("bunstore: %s: %w", what, err)
	}
	if n == 0 {
		return notFound
	}

	return nil
}

// utcPtr normalises a nullable timestamp. SQLite returns local times, so
// without this the same value compares unequal across dialects.
func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
