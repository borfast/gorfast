# Bunstore Core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `sulis.UserStore`, `sulis.SessionStore` and `sulis.TokenStore` over Bun, so a Sulis application can run password login, magic links, password reset and email verification against Postgres or SQLite.

**Architecture:** Each store is a small struct holding a `bun.IDB`, so the same store works against a connection or inside a transaction. Persistence models live beside the store that owns them and carry the `bun` tags; Sulis's own types stay untagged. Conversion between the two is a pair of pure functions per type. Correctness is not judged by hand-written tests: Sulis ships `storetest`, an executable conformance suite for each interface, and that suite is the acceptance criterion.

**Tech Stack:** Go 1.27, [Bun](https://bun.uptrace.dev/) with `pgdialect` and `sqlitedialect`, `pgdriver` for Postgres, `modernc.org/sqlite` (pure Go, no cgo) for SQLite, and `github.com/borfast/sulis`.

**Spec:** `docs/superpowers/specs/2026-09-27-gorfast-design.md` (sections 2, 6.2, 6.3)

## Global Constraints

- Module path is `github.com/borfast/gorfast`. Go 1.27.
- Doctrine rule 2: no Gorfast interface signature mentions Bun. Bun appears only inside `auth/bunstore` in this plan.
- Doctrine rule 7: return Sulis's sentinel errors (`sulis.ErrUserNotFound` and the rest) directly; wrap anything else with `fmt.Errorf("...: %w", err)`. Never map an error to an HTTP status here.
- Doctrine rule 8: tests run against real databases. SQLite always, Postgres when `GORFAST_TEST_POSTGRES_DSN` is set.
- Doctrine rule 9 and `projects/AGENTS.md`: prefer pure functions. Model conversion is pure; stores hold only their `bun.IDB`.
- No em-dashes anywhere, including code comments. Code comments are at most 2 lines.
- Every store method takes `ctx context.Context` as its first argument and passes it to Bun.
- Work happens in the worktree at `/home/borfast/projects/gorfast/.claude/worktrees/bunstore-core`, on branch `worktree-bunstore-core`. Never `cd` to the main checkout.
- **Every commit uses `git commit --no-gpg-sign`.** The repository signs commits with an SSH key whose agent the sandbox cannot reach, so a plain `git commit` fails with `ssh_askpass: exec(/usr/bin/ssh-askpass): No such file or directory`. Do not try to fix that, and do not change any git config: `.git/config` is deliberately not writable here.
- This repository is also a Claude Code plugin. Do not touch `.claude-plugin/`, `skills/` or `evals/`; the Go module coexists with them.
- Entries under `.claude/` that appear as character devices owned by `nobody` are sandbox mounts, not files. Ignore them, and never add them to a commit.

---

## Scope note

The spec's "slice 1" was `crypt` plus all seven Sulis stores. That is too large for one plan and the pieces are not equally coupled, so it is split into three:

- **This plan:** the three core stores, plus the test harness and migrations everything else will reuse. On its own this makes password login, magic links, password reset and email verification work against a real database.
- **Next:** `crypt`. Independent of this plan. The core three stores have no encrypted columns, so `crypt` is not a prerequisite here.
- **After that:** the second-factor stores (`totp`, `passkey`, `recovery`), which need both this plan's harness and `crypt`.

## File Structure

| File | Responsibility |
|---|---|
| `go.mod`, `go.sum` | Module definition and dependencies |
| `internal/testdb/testdb.go` | Open a migrated, empty database on either dialect; reset between factory calls |
| `auth/bunstore/migrations/0001_auth.up.sql` | Schema for `users`, `sessions`, `tokens` (Postgres) |
| `auth/bunstore/migrations/0001_auth.sqlite.up.sql` | The same schema in SQLite dialect |
| `auth/bunstore/migrations/migrations.go` | Embeds both, exposes `Apply(ctx, db)` and the raw SQL |
| `auth/bunstore/bunstore.go` | `isUniqueViolation`, shared helpers |
| `auth/bunstore/user.go` | `userModel`, conversions, `UserStore` |
| `auth/bunstore/session.go` | `sessionModel`, conversions, `SessionStore` |
| `auth/bunstore/token.go` | `tokenModel`, conversions, `TokenStore` |
| `auth/bunstore/user_test.go` | Runs `storetest.RunUserStore` on both dialects |
| `auth/bunstore/session_test.go` | Runs `storetest.RunSessionStore` on both dialects |
| `auth/bunstore/token_test.go` | Runs `storetest.RunTokenStore` on both dialects |
| `docker-compose.yml` | A Postgres for local test runs |

Models live beside the store that owns them because they change together.

---

### Task 1: Module bootstrap, schema and test harness

**Files:**
- Create: `go.mod`, `docker-compose.yml`
- Create: `auth/bunstore/migrations/0001_auth.up.sql`, `auth/bunstore/migrations/0001_auth.sqlite.up.sql`, `auth/bunstore/migrations/migrations.go`
- Create: `internal/testdb/testdb.go`
- Test: `internal/testdb/testdb_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `migrations.Apply(ctx context.Context, db *bun.DB) error`
  - `testdb.Each(t *testing.T, fn func(t *testing.T, db *bun.DB))` runs `fn` once per available dialect as a subtest.
  - `testdb.Reset(t *testing.T, db *bun.DB)` empties every table.

**Dependencies to add.** These come from the public Go module proxy via `go get`:
`github.com/uptrace/bun`, `github.com/uptrace/bun/dialect/pgdialect`, `github.com/uptrace/bun/dialect/sqlitedialect`, `github.com/uptrace/bun/driver/pgdriver`, `modernc.org/sqlite`, `github.com/borfast/sulis`.

- [ ] **Step 1: Initialise the module and add dependencies**

```bash
cd /home/borfast/projects/gorfast/.claude/worktrees/bunstore-core
go mod init github.com/borfast/gorfast
go get github.com/uptrace/bun@latest
go get github.com/uptrace/bun/dialect/pgdialect@latest
go get github.com/uptrace/bun/dialect/sqlitedialect@latest
go get github.com/uptrace/bun/driver/pgdriver@latest
go get modernc.org/sqlite@latest
```

**`go get` needs network access.** Pass these to the Bash tool's `allowed_domains` on every command that fetches modules: `proxy.golang.org`, `sum.golang.org`, `storage.googleapis.com`.

Sulis is deliberately NOT fetched from the proxy. Its only published tag, `v0.1.0`, is 46 commits behind the local checkout this plan was written against, and `storetest` is the acceptance criterion, so a stale copy would test the wrong contract. Point at the local source:

```bash
go mod edit -require=github.com/borfast/sulis@v0.1.0
go mod edit -replace github.com/borfast/sulis=/home/borfast/projects/sulis
go mod tidy
```

The `require` line names `v0.1.0` only to satisfy the module graph; the `replace` means the build reads `/home/borfast/projects/sulis` and the version is never consulted. Do not remove the replace directive, and do not try to `go get` sulis instead: that is a deliberate decision, not an oversight.

- [ ] **Step 2: Write the Postgres schema**

Create `auth/bunstore/migrations/0001_auth.up.sql`:

```sql
CREATE TABLE users (
    id                    TEXT PRIMARY KEY,
    email                 TEXT NOT NULL,
    password_hash         TEXT NOT NULL DEFAULT '',
    created_at            TIMESTAMPTZ NOT NULL,
    updated_at            TIMESTAMPTZ NOT NULL,
    metadata              JSONB,
    email_verified_at     TIMESTAMPTZ,
    pending_email         TEXT NOT NULL DEFAULT '',
    disabled_at           TIMESTAMPTZ,
    disabled_reason       TEXT NOT NULL DEFAULT '',
    locked_until          TIMESTAMPTZ,
    failed_login_attempts INTEGER NOT NULL DEFAULT 0,
    version               BIGINT NOT NULL DEFAULT 0
);

-- Uniqueness is on the live address only. pending_email is staged and may
-- legitimately duplicate another account's live address until confirmed.
CREATE UNIQUE INDEX users_email_key ON users (email);

CREATE TABLE sessions (
    id                TEXT PRIMARY KEY,
    user_id           TEXT NOT NULL,
    token_hash        TEXT NOT NULL,
    expires_at        TIMESTAMPTZ NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL,
    authenticated_at  TIMESTAMPTZ NOT NULL,
    method            TEXT NOT NULL DEFAULT '',
    last_seen_at      TIMESTAMPTZ NOT NULL,
    idle_expires_at   TIMESTAMPTZ,
    ip                TEXT NOT NULL DEFAULT '',
    user_agent        TEXT NOT NULL DEFAULT '',
    metadata          JSONB
);

CREATE UNIQUE INDEX sessions_token_hash_key ON sessions (token_hash);
CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

CREATE TABLE tokens (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL DEFAULT '',
    token_hash  TEXT NOT NULL,
    purpose     TEXT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL,
    used        BOOLEAN NOT NULL DEFAULT FALSE,
    email       TEXT NOT NULL DEFAULT '',
    nonce_hash  TEXT NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX tokens_hash_purpose_key ON tokens (token_hash, purpose);
CREATE INDEX tokens_user_purpose_idx ON tokens (user_id, purpose);
CREATE INDEX tokens_expires_at_idx ON tokens (expires_at);
```

There is no foreign key from `sessions.user_id` or `tokens.user_id` to `users.id`. Sulis issues magic-link tokens before the user exists, so `tokens.user_id` is legitimately empty, and `storetest` exercises sessions for user IDs it never created.

- [ ] **Step 3: Write the SQLite schema**

Create `auth/bunstore/migrations/0001_auth.sqlite.up.sql`: identical to the Postgres file with these substitutions, and nothing else changed.

- `TIMESTAMPTZ` becomes `TIMESTAMP`
- `JSONB` becomes `TEXT`
- `BIGINT` becomes `INTEGER`
- `BOOLEAN NOT NULL DEFAULT FALSE` becomes `BOOLEAN NOT NULL DEFAULT 0`

- [ ] **Step 4: Write the migrations package**

Create `auth/bunstore/migrations/migrations.go`:

```go
// Package migrations holds the schema auth/bunstore expects. The SQL is
// exported so an application can run it with its own migration tool.
package migrations

import (
	"context"
	"embed"
	"fmt"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
)

//go:embed *.sql
var FS embed.FS

// Apply creates the auth schema on an empty database. It is deliberately
// simple: an application with its own migration tool should read FS instead.
func Apply(ctx context.Context, db *bun.DB) error {
	name := "0001_auth.up.sql"
	if db.Dialect().Name() == dialect.SQLite {
		name = "0001_auth.sqlite.up.sql"
	}

	stmts, err := FS.ReadFile(name)
	if err != nil {
		return fmt.Errorf("reading %s: %w", name, err)
	}

	if _, err := db.ExecContext(ctx, string(stmts)); err != nil {
		return fmt.Errorf("applying %s: %w", name, err)
	}

	return nil
}
```

- [ ] **Step 5: Write the failing harness test**

Create `internal/testdb/testdb_test.go`:

```go
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
```

- [ ] **Step 6: Run the test to verify it fails**

Run: `go test ./internal/testdb/ -v`
Expected: FAIL, `undefined: testdb.Each`

- [ ] **Step 7: Write the harness**

Create `internal/testdb/testdb.go`:

```go
// Package testdb opens real databases for tests. SQLite always runs. Postgres
// runs when GORFAST_TEST_POSTGRES_DSN is set, and is skipped otherwise.
package testdb

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/pgdriver"
	_ "modernc.org/sqlite"

	"github.com/borfast/gorfast/auth/bunstore/migrations"
)

// PostgresDSNEnv names the environment variable holding a Postgres DSN.
// Without it the Postgres subtests skip rather than fail.
const PostgresDSNEnv = "GORFAST_TEST_POSTGRES_DSN"

// tables is every table Reset empties, children before parents.
var tables = []string{"sessions", "tokens", "users"}

// Each runs fn once per available dialect, as a subtest named for the dialect.
func Each(t *testing.T, fn func(t *testing.T, db *bun.DB)) {
	t.Helper()

	t.Run("sqlite", func(t *testing.T) {
		fn(t, openSQLite(t))
	})

	t.Run("postgres", func(t *testing.T) {
		dsn := os.Getenv(PostgresDSNEnv)
		if dsn == "" {
			t.Skipf("%s not set, skipping Postgres", PostgresDSNEnv)
		}
		fn(t, openPostgres(t, dsn))
	})
}

// Reset empties every table, so a storetest factory can hand back a fresh,
// empty store on every call.
func Reset(t *testing.T, db *bun.DB) {
	t.Helper()

	for _, table := range tables {
		if _, err := db.NewRaw("DELETE FROM " + table).Exec(t.Context()); err != nil {
			t.Fatalf("emptying %s: %v", table, err)
		}
	}
}

func openSQLite(t *testing.T) *bun.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.db")
	sqldb, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("opening sqlite: %v", err)
	}

	return migrate(t, bun.NewDB(sqldb, sqlitedialect.New()))
}

// openPostgres gives each test its own schema, so tests never collide and
// cleanup is a single DROP.
func openPostgres(t *testing.T, dsn string) *bun.DB {
	t.Helper()

	admin := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn))), pgdialect.New())
	t.Cleanup(func() { _ = admin.Close() })

	schema := fmt.Sprintf("test_%d", os.Getpid()^int(hashName(t.Name())))
	ctx := t.Context()

	if _, err := admin.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); err != nil {
		t.Fatalf("dropping schema: %v", err)
	}
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("creating schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.WithoutCancel(ctx), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	})

	connector := pgdriver.NewConnector(
		pgdriver.WithDSN(dsn),
		pgdriver.WithConnParams(map[string]any{"search_path": schema}),
	)
	db := bun.NewDB(sql.OpenDB(connector), pgdialect.New())
	t.Cleanup(func() { _ = db.Close() })

	return migrate(t, db)
}

func migrate(t *testing.T, db *bun.DB) *bun.DB {
	t.Helper()

	if err := migrations.Apply(t.Context(), db); err != nil {
		t.Fatalf("applying migrations: %v", err)
	}

	return db
}

func hashName(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h = (h ^ uint32(s[i])) * 16777619
	}
	return h
}
```

- [ ] **Step 8: Add docker-compose.yml**

```yaml
services:
  postgres:
    image: postgres:17-alpine
    environment:
      POSTGRES_USER: gorfast
      POSTGRES_PASSWORD: gorfast
      POSTGRES_DB: gorfast_test
    ports:
      - "5433:5432"
```

Local Postgres runs use:
`GORFAST_TEST_POSTGRES_DSN='postgres://gorfast:gorfast@localhost:5433/gorfast_test?sslmode=disable'`

- [ ] **Step 9: Run the tests to verify they pass**

Run: `go test ./internal/testdb/ -v`
Expected: PASS, with `postgres` subtests SKIPped.

Then with Postgres:

```bash
docker compose up -d postgres
GORFAST_TEST_POSTGRES_DSN='postgres://gorfast:gorfast@localhost:5433/gorfast_test?sslmode=disable' go test ./internal/testdb/ -v
```

Expected: PASS, with both subtests running.

- [ ] **Step 10: Commit**

```bash
git add go.mod go.sum docker-compose.yml auth/bunstore/migrations internal/testdb
git commit --no-gpg-sign -m "feat: add auth schema and real-database test harness"
```

---

### Task 2: UserStore

**Files:**
- Create: `auth/bunstore/bunstore.go`, `auth/bunstore/user.go`
- Test: `auth/bunstore/user_test.go`

**Interfaces:**
- Consumes: `testdb.Each`, `testdb.Reset` from Task 1.
- Produces:
  - `bunstore.NewUserStore(db bun.IDB) *bunstore.UserStore`, satisfying `sulis.UserStore`
  - `isUniqueViolation(err error) bool`, unexported
  - `utcPtr(t *time.Time) *time.Time`, unexported, used again in Task 3

Read `sulis.UserStore`'s doc comment in `/home/borfast/projects/sulis/user.go` before starting. Two requirements carry the weight: optimistic concurrency on `Version`, and email uniqueness enforced by the write path on both `CreateUser` and `UpdateUser`.

- [ ] **Step 1: Write the failing test**

Create `auth/bunstore/user_test.go`:

```go
package bunstore_test

import (
	"testing"

	"github.com/borfast/sulis"
	"github.com/borfast/sulis/storetest"
	"github.com/uptrace/bun"

	"github.com/borfast/gorfast/auth/bunstore"
	"github.com/borfast/gorfast/internal/testdb"
)

func TestUserStore(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		storetest.RunUserStore(t, func() sulis.UserStore {
			testdb.Reset(t, db)
			return bunstore.NewUserStore(db)
		})
	})
}
```

The factory calls `Reset` because `storetest` requires a fresh, empty store on every call, and it calls the factory fifteen times.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./auth/bunstore/ -run TestUserStore -v`
Expected: FAIL, `undefined: bunstore.NewUserStore`

- [ ] **Step 3: Write the shared helpers**

Create `auth/bunstore/bunstore.go`:

```go
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

// sqliteConstraintUnique and sqlitePrimaryKey are SQLite's extended result
// codes for a unique and a primary key constraint violation.
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
```

- [ ] **Step 4: Write the model and conversions**

Create `auth/bunstore/user.go`, starting with the model and the two pure conversion functions:

```go
package bunstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/borfast/sulis"
	"github.com/uptrace/bun"
)

type userModel struct {
	bun.BaseModel `bun:"table:users,alias:u"`

	ID                  string         `bun:"id,pk"`
	Email               string         `bun:"email"`
	PasswordHash        string         `bun:"password_hash"`
	CreatedAt           time.Time      `bun:"created_at"`
	UpdatedAt           time.Time      `bun:"updated_at"`
	Metadata            map[string]any `bun:"metadata"`
	EmailVerifiedAt     *time.Time     `bun:"email_verified_at"`
	PendingEmail        string         `bun:"pending_email"`
	DisabledAt          *time.Time     `bun:"disabled_at"`
	DisabledReason      string         `bun:"disabled_reason"`
	LockedUntil         *time.Time     `bun:"locked_until"`
	FailedLoginAttempts int            `bun:"failed_login_attempts"`
	Version             uint64         `bun:"version"`
}

func toUserModel(u *sulis.User) *userModel {
	return &userModel{
		ID:                  u.ID,
		Email:               u.Email,
		PasswordHash:        u.PasswordHash,
		CreatedAt:           u.CreatedAt,
		UpdatedAt:           u.UpdatedAt,
		Metadata:            u.Metadata,
		EmailVerifiedAt:     u.EmailVerifiedAt,
		PendingEmail:        u.PendingEmail,
		DisabledAt:          u.DisabledAt,
		DisabledReason:      u.DisabledReason,
		LockedUntil:         u.LockedUntil,
		FailedLoginAttempts: u.FailedLoginAttempts,
		Version:             u.Version,
	}
}

func fromUserModel(m *userModel) *sulis.User {
	return &sulis.User{
		ID:                  m.ID,
		Email:               m.Email,
		PasswordHash:        m.PasswordHash,
		CreatedAt:           m.CreatedAt.UTC(),
		UpdatedAt:           m.UpdatedAt.UTC(),
		Metadata:            m.Metadata,
		EmailVerifiedAt:     utcPtr(m.EmailVerifiedAt),
		PendingEmail:        m.PendingEmail,
		DisabledAt:          utcPtr(m.DisabledAt),
		DisabledReason:      m.DisabledReason,
		LockedUntil:         utcPtr(m.LockedUntil),
		FailedLoginAttempts: m.FailedLoginAttempts,
		Version:             m.Version,
	}
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
```

`Metadata` needs no aliasing defence: every read scans a fresh map out of the database.

- [ ] **Step 5: Write the store**

Append to `auth/bunstore/user.go`:

```go
// UserStore implements sulis.UserStore.
type UserStore struct {
	db bun.IDB
}

var _ sulis.UserStore = (*UserStore)(nil)

// NewUserStore returns a UserStore reading and writing through db, which may
// be a connection or a transaction.
func NewUserStore(db bun.IDB) *UserStore {
	return &UserStore{db: db}
}

func (s *UserStore) CreateUser(ctx context.Context, user *sulis.User) error {
	_, err := s.db.NewInsert().Model(toUserModel(user)).Exec(ctx)
	if isUniqueViolation(err) {
		return sulis.ErrUserAlreadyExists
	}
	if err != nil {
		return fmt.Errorf("bunstore: creating user: %w", err)
	}

	return nil
}

func (s *UserStore) GetUserByID(ctx context.Context, id string) (*sulis.User, error) {
	return s.getUserBy(ctx, "id", id)
}

func (s *UserStore) GetUserByEmail(ctx context.Context, email string) (*sulis.User, error) {
	return s.getUserBy(ctx, "email", email)
}

func (s *UserStore) getUserBy(ctx context.Context, column, value string) (*sulis.User, error) {
	m := new(userModel)
	err := s.db.NewSelect().Model(m).Where("? = ?", bun.Ident(column), value).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sulis.ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("bunstore: reading user by %s: %w", column, err)
	}

	return fromUserModel(m), nil
}

// UpdateUser applies the write only while the stored version still matches
// user.Version, and leaves user.Version alone, matching memstore.
func (s *UserStore) UpdateUser(ctx context.Context, user *sulis.User) error {
	res, err := s.db.NewUpdate().
		Model(toUserModel(user)).
		Column("email", "password_hash", "updated_at", "metadata",
			"email_verified_at", "pending_email", "disabled_at",
			"disabled_reason", "locked_until", "failed_login_attempts").
		Set("version = version + 1").
		Where("id = ?", user.ID).
		Where("version = ?", user.Version).
		Exec(ctx)
	if isUniqueViolation(err) {
		return sulis.ErrUserAlreadyExists
	}
	if err != nil {
		return fmt.Errorf("bunstore: updating user: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("bunstore: updating user: %w", err)
	}
	if n == 0 {
		return s.explainFailedUpdate(ctx, user.ID)
	}

	return nil
}

// explainFailedUpdate distinguishes the two reasons an update matched no row.
// The UPDATE alone cannot tell them apart, and the contract names a different
// error for each.
func (s *UserStore) explainFailedUpdate(ctx context.Context, id string) error {
	exists, err := s.db.NewSelect().Model((*userModel)(nil)).Where("id = ?", id).Exists(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: updating user: %w", err)
	}
	if !exists {
		return sulis.ErrUserNotFound
	}

	return sulis.ErrConcurrentUpdate
}

func (s *UserStore) DeleteUser(ctx context.Context, id string) error {
	_, err := s.db.NewDelete().Model((*userModel)(nil)).Where("id = ?", id).Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: deleting user: %w", err)
	}

	return nil
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./auth/bunstore/ -run TestUserStore -v`
Expected: PASS on `sqlite`, SKIP on `postgres`.

If the concurrency subtests fail on SQLite with "database is locked", raise the busy timeout in `openSQLite`. If they fail with `ErrConcurrentUpdate` where a success was expected, the `Set("version = version + 1")` clause is being overwritten by `Column(...)`; confirm `version` is not in the column list.

- [ ] **Step 7: Run against Postgres**

```bash
docker compose up -d postgres
GORFAST_TEST_POSTGRES_DSN='postgres://gorfast:gorfast@localhost:5433/gorfast_test?sslmode=disable' \
  go test ./auth/bunstore/ -run TestUserStore -v
```

Expected: PASS on both dialects.

- [ ] **Step 8: Commit**

```bash
git add auth/bunstore/bunstore.go auth/bunstore/user.go auth/bunstore/user_test.go
git commit --no-gpg-sign -m "feat: add Bun-backed sulis.UserStore"
```

---

### Task 3: SessionStore

**Files:**
- Create: `auth/bunstore/session.go`
- Test: `auth/bunstore/session_test.go`

**Interfaces:**
- Consumes: `testdb.Each`, `testdb.Reset`, `utcPtr` from Task 2.
- Produces: `bunstore.NewSessionStore(db bun.IDB) *bunstore.SessionStore`, satisfying `sulis.SessionStore`.

Read `sulis.SessionStore`'s doc comment in `/home/borfast/projects/sulis/session.go` first. The contract that matters: `DeleteSession` must scope its delete to both `id` and `user_id` in one statement, so a guessed or leaked session ID belonging to another user deletes nothing and returns `ErrSessionNotFound`.

- [ ] **Step 1: Write the failing test**

Create `auth/bunstore/session_test.go`:

```go
package bunstore_test

import (
	"testing"

	"github.com/borfast/sulis"
	"github.com/borfast/sulis/storetest"
	"github.com/uptrace/bun"

	"github.com/borfast/gorfast/auth/bunstore"
	"github.com/borfast/gorfast/internal/testdb"
)

func TestSessionStore(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		storetest.RunSessionStore(t, func() sulis.SessionStore {
			testdb.Reset(t, db)
			return bunstore.NewSessionStore(db)
		})
	})
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./auth/bunstore/ -run TestSessionStore -v`
Expected: FAIL, `undefined: bunstore.NewSessionStore`

- [ ] **Step 3: Write the model and conversions**

Create `auth/bunstore/session.go`:

```go
package bunstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/borfast/sulis"
	"github.com/uptrace/bun"
)

type sessionModel struct {
	bun.BaseModel `bun:"table:sessions,alias:s"`

	ID              string         `bun:"id,pk"`
	UserID          string         `bun:"user_id"`
	TokenHash       string         `bun:"token_hash"`
	ExpiresAt       time.Time      `bun:"expires_at"`
	CreatedAt       time.Time      `bun:"created_at"`
	AuthenticatedAt time.Time      `bun:"authenticated_at"`
	Method          string         `bun:"method"`
	LastSeenAt      time.Time      `bun:"last_seen_at"`
	IdleExpiresAt   *time.Time     `bun:"idle_expires_at"`
	IP              string         `bun:"ip"`
	UserAgent       string         `bun:"user_agent"`
	Metadata        map[string]any `bun:"metadata"`
}

func toSessionModel(s *sulis.Session) *sessionModel {
	return &sessionModel{
		ID:              s.ID,
		UserID:          s.UserID,
		TokenHash:       s.TokenHash,
		ExpiresAt:       s.ExpiresAt,
		CreatedAt:       s.CreatedAt,
		AuthenticatedAt: s.AuthenticatedAt,
		Method:          string(s.Method),
		LastSeenAt:      s.LastSeenAt,
		IdleExpiresAt:   s.IdleExpiresAt,
		IP:              s.IP,
		UserAgent:       s.UserAgent,
		Metadata:        s.Metadata,
	}
}

func fromSessionModel(m *sessionModel) *sulis.Session {
	return &sulis.Session{
		ID:              m.ID,
		UserID:          m.UserID,
		TokenHash:       m.TokenHash,
		ExpiresAt:       m.ExpiresAt.UTC(),
		CreatedAt:       m.CreatedAt.UTC(),
		AuthenticatedAt: m.AuthenticatedAt.UTC(),
		Method:          sulis.AuthMethod(m.Method),
		LastSeenAt:      m.LastSeenAt.UTC(),
		IdleExpiresAt:   utcPtr(m.IdleExpiresAt),
		IP:              m.IP,
		UserAgent:       m.UserAgent,
		Metadata:        m.Metadata,
	}
}
```

- [ ] **Step 4: Write the store**

Append to `auth/bunstore/session.go`:

```go
// SessionStore implements sulis.SessionStore.
type SessionStore struct {
	db bun.IDB
}

var _ sulis.SessionStore = (*SessionStore)(nil)

// NewSessionStore returns a SessionStore reading and writing through db.
func NewSessionStore(db bun.IDB) *SessionStore {
	return &SessionStore{db: db}
}

func (s *SessionStore) CreateSession(ctx context.Context, session *sulis.Session) error {
	_, err := s.db.NewInsert().Model(toSessionModel(session)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: creating session: %w", err)
	}

	return nil
}

func (s *SessionStore) GetSessionByTokenHash(ctx context.Context, tokenHash string) (*sulis.Session, error) {
	m := new(sessionModel)
	err := s.db.NewSelect().Model(m).Where("token_hash = ?", tokenHash).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sulis.ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("bunstore: reading session: %w", err)
	}

	return fromSessionModel(m), nil
}

// ListUserSessions returns every session for userID. Matching nothing is not
// an error, and TokenHash is returned exactly as stored.
func (s *SessionStore) ListUserSessions(ctx context.Context, userID string) ([]sulis.Session, error) {
	var models []sessionModel
	err := s.db.NewSelect().Model(&models).Where("user_id = ?", userID).Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("bunstore: listing sessions: %w", err)
	}

	sessions := make([]sulis.Session, 0, len(models))
	for i := range models {
		sessions = append(sessions, *fromSessionModel(&models[i]))
	}

	return sessions, nil
}

// DeleteSession scopes the delete to both columns, so a session ID belonging
// to another user matches no row rather than being checked afterwards.
func (s *SessionStore) DeleteSession(ctx context.Context, userID, id string) error {
	res, err := s.db.NewDelete().
		Model((*sessionModel)(nil)).
		Where("id = ?", id).
		Where("user_id = ?", userID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: deleting session: %w", err)
	}

	return requireOneRow(res, sulis.ErrSessionNotFound, "deleting session")
}

func (s *SessionStore) DeleteUserSessions(ctx context.Context, userID string) error {
	_, err := s.db.NewDelete().Model((*sessionModel)(nil)).Where("user_id = ?", userID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: deleting user sessions: %w", err)
	}

	return nil
}

// DeleteUserSessionsExcept is the "sign out everywhere else" primitive. A
// keepSessionID that does not exist is not an error.
func (s *SessionStore) DeleteUserSessionsExcept(ctx context.Context, userID, keepSessionID string) error {
	_, err := s.db.NewDelete().
		Model((*sessionModel)(nil)).
		Where("user_id = ?", userID).
		Where("id <> ?", keepSessionID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: deleting other user sessions: %w", err)
	}

	return nil
}

func (s *SessionStore) CleanExpired(ctx context.Context) error {
	_, err := s.db.NewDelete().
		Model((*sessionModel)(nil)).
		Where("expires_at < ?", time.Now().UTC()).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: cleaning expired sessions: %w", err)
	}

	return nil
}

// UpdateAuthenticatedAt stamps the session and touches nothing else. It is
// the write path behind ReAuthenticate.
func (s *SessionStore) UpdateAuthenticatedAt(ctx context.Context, id string, at time.Time) error {
	res, err := s.db.NewUpdate().
		Model((*sessionModel)(nil)).
		Set("authenticated_at = ?", at).
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: stamping authenticated_at: %w", err)
	}

	return requireOneRow(res, sulis.ErrSessionNotFound, "stamping authenticated_at")
}

func (s *SessionStore) TouchSession(ctx context.Context, id string, lastSeen time.Time, idleExpires *time.Time) error {
	res, err := s.db.NewUpdate().
		Model((*sessionModel)(nil)).
		Set("last_seen_at = ?", lastSeen).
		Set("idle_expires_at = ?", idleExpires).
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: touching session: %w", err)
	}

	return requireOneRow(res, sulis.ErrSessionNotFound, "touching session")
}
```

- [ ] **Step 5: Add the shared row-count helper**

Append to `auth/bunstore/bunstore.go`, and add `"database/sql"` and `"fmt"` to its imports:

```go
// requireOneRow turns an affected-row count of zero into notFound. Several
// contracts depend on a scoped statement affecting nothing being an error
// rather than a silent success.
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
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./auth/bunstore/ -run TestSessionStore -v`
Expected: PASS on `sqlite`, SKIP on `postgres`.

If `TouchSession` fails because `idle_expires_at` was expected to stay unchanged, re-read the contract: it sets both fields on every call.

- [ ] **Step 7: Run against Postgres**

```bash
GORFAST_TEST_POSTGRES_DSN='postgres://gorfast:gorfast@localhost:5433/gorfast_test?sslmode=disable' \
  go test ./auth/bunstore/ -run TestSessionStore -v
```

Expected: PASS on both dialects.

- [ ] **Step 8: Commit**

```bash
git add auth/bunstore/bunstore.go auth/bunstore/session.go auth/bunstore/session_test.go
git commit --no-gpg-sign -m "feat: add Bun-backed sulis.SessionStore"
```

---

### Task 4: TokenStore

**Files:**
- Create: `auth/bunstore/token.go`
- Test: `auth/bunstore/token_test.go`

**Interfaces:**
- Consumes: `testdb.Each` and `testdb.Reset` from Task 1. Nothing from Tasks 2 or 3: `ConsumeToken` needs its own two-case error mapping rather than `requireOneRow`.
- Produces: `bunstore.NewTokenStore(db bun.IDB) *bunstore.TokenStore`, satisfying `sulis.TokenStore`.

Read `sulis.TokenStore`'s doc comment in `/home/borfast/projects/sulis/token.go` first. `ConsumeToken` must find the unused token and mark it used in one atomic operation, and must distinguish "no such token" from "already used".

- [ ] **Step 1: Write the failing test**

Create `auth/bunstore/token_test.go`:

```go
package bunstore_test

import (
	"testing"

	"github.com/borfast/sulis"
	"github.com/borfast/sulis/storetest"
	"github.com/uptrace/bun"

	"github.com/borfast/gorfast/auth/bunstore"
	"github.com/borfast/gorfast/internal/testdb"
)

func TestTokenStore(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		storetest.RunTokenStore(t, func() sulis.TokenStore {
			testdb.Reset(t, db)
			return bunstore.NewTokenStore(db)
		})
	})
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./auth/bunstore/ -run TestTokenStore -v`
Expected: FAIL, `undefined: bunstore.NewTokenStore`

- [ ] **Step 3: Write the model, conversions and store**

Create `auth/bunstore/token.go`:

```go
package bunstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/borfast/sulis"
	"github.com/uptrace/bun"
)

type tokenModel struct {
	bun.BaseModel `bun:"table:tokens,alias:t"`

	ID        string    `bun:"id,pk"`
	UserID    string    `bun:"user_id"`
	TokenHash string    `bun:"token_hash"`
	Purpose   string    `bun:"purpose"`
	ExpiresAt time.Time `bun:"expires_at"`
	CreatedAt time.Time `bun:"created_at"`
	Used      bool      `bun:"used"`
	Email     string    `bun:"email"`
	NonceHash string    `bun:"nonce_hash"`
}

func toTokenModel(t *sulis.Token) *tokenModel {
	return &tokenModel{
		ID:        t.ID,
		UserID:    t.UserID,
		TokenHash: t.TokenHash,
		Purpose:   string(t.Purpose),
		ExpiresAt: t.ExpiresAt,
		CreatedAt: t.CreatedAt,
		Used:      t.Used,
		Email:     t.Email,
		NonceHash: t.NonceHash,
	}
}

func fromTokenModel(m *tokenModel) *sulis.Token {
	return &sulis.Token{
		ID:        m.ID,
		UserID:    m.UserID,
		TokenHash: m.TokenHash,
		Purpose:   sulis.TokenPurpose(m.Purpose),
		ExpiresAt: m.ExpiresAt.UTC(),
		CreatedAt: m.CreatedAt.UTC(),
		Used:      m.Used,
		Email:     m.Email,
		NonceHash: m.NonceHash,
	}
}

// TokenStore implements sulis.TokenStore.
type TokenStore struct {
	db bun.IDB
}

var _ sulis.TokenStore = (*TokenStore)(nil)

// NewTokenStore returns a TokenStore reading and writing through db.
func NewTokenStore(db bun.IDB) *TokenStore {
	return &TokenStore{db: db}
}

func (s *TokenStore) CreateToken(ctx context.Context, token *sulis.Token) error {
	_, err := s.db.NewInsert().Model(toTokenModel(token)).Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: creating token: %w", err)
	}

	return nil
}

// ConsumeToken finds and marks the token in one statement, so two concurrent
// presentations of the same token cannot both succeed.
func (s *TokenStore) ConsumeToken(ctx context.Context, hash string, purpose sulis.TokenPurpose) (*sulis.Token, error) {
	m := new(tokenModel)
	err := s.db.NewUpdate().
		Model(m).
		Set("used = ?", true).
		Where("token_hash = ?", hash).
		Where("purpose = ?", string(purpose)).
		Where("used = ?", false).
		Returning("*").
		Scan(ctx)
	if err == nil {
		m.Used = true
		return fromTokenModel(m), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("bunstore: consuming token: %w", err)
	}

	return nil, s.explainFailedConsume(ctx, hash, purpose)
}

// explainFailedConsume distinguishes a token that does not exist from one
// already spent. The UPDATE alone cannot, and the contract names an error for
// each. A token never goes back to unused, so this follow-up read cannot be
// wrong about which case it is.
func (s *TokenStore) explainFailedConsume(ctx context.Context, hash string, purpose sulis.TokenPurpose) error {
	exists, err := s.db.NewSelect().
		Model((*tokenModel)(nil)).
		Where("token_hash = ?", hash).
		Where("purpose = ?", string(purpose)).
		Exists(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: consuming token: %w", err)
	}
	if !exists {
		return sulis.ErrTokenNotFound
	}

	return sulis.ErrTokenAlreadyUsed
}

func (s *TokenStore) DeleteExpiredTokens(ctx context.Context) error {
	_, err := s.db.NewDelete().
		Model((*tokenModel)(nil)).
		Where("expires_at < ?", time.Now().UTC()).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: deleting expired tokens: %w", err)
	}

	return nil
}

// DeleteUserTokens removes every token for the user and purpose. Deleting
// zero tokens is not an error.
func (s *TokenStore) DeleteUserTokens(ctx context.Context, userID string, purpose sulis.TokenPurpose) error {
	_, err := s.db.NewDelete().
		Model((*tokenModel)(nil)).
		Where("user_id = ?", userID).
		Where("purpose = ?", string(purpose)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("bunstore: deleting user tokens: %w", err)
	}

	return nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./auth/bunstore/ -run TestTokenStore -v`
Expected: PASS on `sqlite`, SKIP on `postgres`.

If `UPDATE ... RETURNING` fails on SQLite, check the `modernc.org/sqlite` version: `RETURNING` needs SQLite 3.35 or newer. If the installed version is older, replace `ConsumeToken` with a transaction that selects for update, then updates, and keep the same error mapping.

- [ ] **Step 5: Run the whole suite against both dialects**

```bash
docker compose up -d postgres
GORFAST_TEST_POSTGRES_DSN='postgres://gorfast:gorfast@localhost:5433/gorfast_test?sslmode=disable' \
  go test ./... -v
```

Expected: every test passes on both dialects. This is the plan's definition of done.

- [ ] **Step 6: Run go vet and the race detector**

```bash
go vet ./...
GORFAST_TEST_POSTGRES_DSN='postgres://gorfast:gorfast@localhost:5433/gorfast_test?sslmode=disable' \
  go test ./... -race
```

Expected: no vet findings, no data races. `storetest`'s concurrency subtests are the reason this matters.

- [ ] **Step 7: Commit**

```bash
git add auth/bunstore/token.go auth/bunstore/token_test.go
git commit --no-gpg-sign -m "feat: add Bun-backed sulis.TokenStore"
```

---

## Done when

`go test ./... -race` passes with `GORFAST_TEST_POSTGRES_DSN` set, which means `storetest.RunUserStore`, `RunSessionStore` and `RunTokenStore` all pass against real Postgres and real SQLite.
