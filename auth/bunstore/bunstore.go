// Package bunstore implements Sulis's store interfaces over Bun.
//
// Every store takes a bun.IDB rather than a *bun.DB, so the same store works
// against a connection or inside a transaction.
package bunstore

import (
	"errors"
	"strings"

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

	var c coder
	if errors.As(err, &c) {
		return c.Code() == sqliteConstraintUnique || c.Code() == sqliteConstraintPrimary
	}

	// modernc.org/sqlite has not always exposed Code() on every error path.
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
