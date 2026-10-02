# PR #1 review

- PR: https://github.com/borfast/gorfast/pull/1
- Title: Add Bun-backed Sulis stores for Postgres and SQLite
- Reviewed: 2026-09-28
- Head: `9e0df81b25125974241e6f3a542b5a90219a36d4`
- Base: `f67028c5013877acefce695ac15f873bcc6969a5` (`origin/main` at review time)
- Recommendation: **Request changes**

## Scope and branch comparison

The worktree matched the PR head exactly. The review used the PR's actual
remote base, rather than local `main`. At the initial review, local `main`
was one commit ahead and four commits behind `origin/main`; its design-document
commit had been replayed into the PR.

The review covered the three stores, schema and migration runner, test harness,
module definition, and relevant Sulis callers and interface contracts. No
implementation files were changed. Additional reproduction tests were written
outside the repository, under `/tmp/gorfast-pr1-review`.

## Findings

### 1. [P1] The committed Sulis dependency cannot be resolved independently

**Location:** [`go.mod:5–8`](../../go.mod#L5)

The module replaces Sulis with `/home/borfast/projects/sulis` and requires
`v0.0.0-00010101000000-000000000000`. A fresh checkout needs that exact local
path. Downstream consumers do not inherit dependency-module replacements, so
they instead encounter the nonexistent all-zero version.

This limitation is already acknowledged in the PR description. It also means
the passing tests depend on the contents of an independently changing checkout,
rather than a reproducible dependency pinned by this PR. The checkout used for
validation included local Sulis commit `55f975e`, which adjusts session test
timestamps to microsecond precision and was ahead of its remote branch.

**Recommended change:** Publish the required Sulis commit and pin a valid
pseudo-version or release. Keep the local development override in an untracked
`go.work`. Creating a Sulis tag alone does not repair the current `go.mod`;
the requirement and replacement must also be updated.

### 2. [P2] Duplicate-email errors are not mapped correctly with pgx

**Location:** [`auth/bunstore/bunstore.go:35–46`](../../auth/bunstore/bunstore.go#L35)

The constructors accept `bun.IDB`, but PostgreSQL uniqueness detection only
recognizes `pgdriver.Error`. A Bun connection backed by pgx reports a different
error type, so both `CreateUser` and `UpdateUser` return wrapped database errors
instead of the required `sulis.ErrUserAlreadyExists`.

**Reproduction:** With pgx v5.11.0 and the PR's PostgreSQL schema, create one user
and then either create a second user with the same email or update a second
user to that email. In both cases,
`errors.Is(err, sulis.ErrUserAlreadyExists)` is false. The error contains
`duplicate key value violates unique constraint "users_email_lower_key"`
and SQLSTATE `23505`. The equivalent cases using pgdriver pass.

This breaks callers that branch on the sentinel, including Sulis's recovery
from a concurrent passwordless-account creation.

**Recommended change:** Recognize PostgreSQL errors exposing
`SQLState() string`, alongside the existing pgdriver handling, and cover both
drivers. Alternatively, explicitly document and enforce a pgdriver-only
restriction rather than accepting an apparently unrestricted Bun connection.

**Reproduction file:** `/tmp/gorfast-pr1-review/driver_test.go`,
`TestDuplicateEmailDrivers`.

### 3. [P2] A duplicate-user error leaves a supplied PostgreSQL transaction aborted

**Location:** [`auth/bunstore/user.go:82–85`](../../auth/bunstore/user.go#L82)

When the store receives a `bun.Tx`, a uniqueness violation returns
`sulis.ErrUserAlreadyExists` but leaves the transaction aborted. Further
queries through that store fail with SQLSTATE `25P02`.

This is a concrete problem for the advertised transaction support. Sulis's
`getOrCreatePasswordlessUser` catches `ErrUserAlreadyExists` and retries
`GetUserByEmail` when another request won the account-creation race. With a
transaction-backed PostgreSQL store, that recovery lookup cannot succeed.
The relevant caller is `magiclink.go:212–234` in the Sulis checkout used for
the review.

**Reproduction:** Begin a PostgreSQL transaction using pgdriver, migrate an
isolated schema, and construct `NewUserStore(tx)`. Create a user, attempt a
second user with the same email, and confirm `ErrUserAlreadyExists`. A
subsequent `GetUserByEmail` fails with:

```text
bunstore: reading user by email: ERROR: current transaction is aborted,
commands ignored until end of transaction block (SQLSTATE=25P02)
```

This finding is independent of the pgx error-mapping issue: it reproduces with
the driver used by the PR's own tests.

**Recommended change:** Protect the insert with a savepoint and roll back to
it on failure, or use `ON CONFLICT DO NOTHING` and translate zero affected
rows into `ErrUserAlreadyExists`. Add a test that performs the recovery lookup
through the same transaction after a duplicate insert.

**Reproduction file:** `/tmp/gorfast-pr1-review/extra_test.go`,
`TestPostgresDuplicateDoesNotPoisonTransaction`.

### 4. [P3] The SQLite test helper does not close its database

**Location:** [`internal/testdb/testdb.go:58–67`](../../internal/testdb/testdb.go#L58)

`openSQLite` creates a database pool but never registers `Close` with
`t.Cleanup`. The PostgreSQL helper does register cleanup. Removing the SQLite
temporary directory does not close the pool or its open file handles.

**Reproduction:** Capture the SQLite `*bun.DB` returned through `testdb.Each`
inside a child test. After the child test and its cleanup finish,
`db.Stats().OpenConnections` remains `1`, rather than `0`. Repeated tests
therefore accumulate connections and file handles until the test process exits.

**Recommended change:** Construct the Bun database, register a cleanup that
closes it, and then run migrations. Register cleanup before migration so it
also runs if migration fails.

**Reproduction file:** `/tmp/gorfast-pr1-review/extra_test.go`,
`TestSQLiteHarnessClosesDatabase`.

## Validation and coverage

| Check | Result |
| --- | --- |
| Existing `go test -race -count=1 ./...`, with PostgreSQL DSN configured | Passed against both SQLite and PostgreSQL |
| `go vet ./...` | Passed |
| Duplicate-email create and update through pgdriver | Passed |
| Duplicate-email create and update through pgx | Failed sentinel assertions; finding 2 |
| Lookup after duplicate create in a pgdriver transaction | Failed with SQLSTATE `25P02`; finding 3 |
| SQLite database closed after test cleanup | Failed; one connection remained open; finding 4 |
| Repeated migration application preserves existing data | Passed against both SQLite and PostgreSQL |
| Migration failure rolls back earlier table creation | Passed against both SQLite and PostgreSQL |

The migration checks are in `/tmp/gorfast-pr1-review/migrations_test.go`:
`TestMigrationRepeatPreservesData` and
`TestMigrationFailureRollsBackEarlierTables`. The rollback check uses an
incompatible pre-existing `tokens` table to cause index creation to fail,
then verifies that the earlier `users` and `sessions` table creations did
not survive.

The existing suite exercises standalone database-backed stores. It does not
cover the transaction recovery case, the pgx error type, or SQLite pool cleanup.
The passing conformance results also depend on the local Sulis checkout noted
in finding 1.

## Graph-assisted follow-up

CodeGraph provided the current store source and the Sulis caller that retries
after a duplicate-user error. The newly supplied GitNexus index initially
described an earlier `main` checkout and omitted the PR's Go packages. It was
refreshed with `analyze --index-only`; subsequent queries used the CLI because
the MCP connection retained the old index.

The refreshed graph confirmed that `isUniqueViolation` affects both user write
paths and traced the migration runner through the test harness. Graph risk
labels and missing caller/test edges were not treated as proof of correctness
or absence of coverage; findings above are grounded in source and focused
reproductions.

## Previously acknowledged limitations

The PR also documents the narrow follow-up-read race in `ConsumeToken` and
`UpdateUser`, timestamp precision differences from Sulis's reference store,
JSON `null` for nil metadata, non-STRICT SQLite tables, and the test harness's
hardcoded table list. These were considered during review and were not raised
as additional findings. The local Sulis dependency is acknowledged too, but
remains finding 1 because it prevents an independently reproducible build.

The reproduction files under `/tmp` are temporary review artifacts, not
committed regression tests. The descriptions above preserve the conditions
and observed results if those files are later removed.

## Resolution (2026-09-28)

All four findings were addressed after this review. The original findings and
red results above remain as the review record.

1. Sulis commit `55f975e76870d9208b33c408558876a2ea097421` was published on
   `origin/example-webapp`. `go.mod` now requires the resolvable pseudo-version
   `v0.1.1-0.20260928114120-55f975e76870` and has no local Sulis replacement.
   A temporary downstream module with `GOWORK=off` and only a local replacement
   for gorfast completed `go mod tidy` and `go test ./...`. Its Sulis module
   listing showed this version with no replacement.
2. `isUniqueViolation` now recognizes PostgreSQL errors exposing
   `SQLState() string`, including pgx, while preserving pgdriver and SQLite
   handling. A negative SQLSTATE `23503` test confirms an unrelated error is
   not mapped to `ErrUserAlreadyExists` even if its message contains the old
   SQLite fallback text.
3. `CreateUser` now inserts with `ON CONFLICT DO NOTHING` and maps zero affected
   rows to `ErrUserAlreadyExists`. Real PostgreSQL tests cover duplicate IDs
   and mixed-case duplicate emails through pgdriver and pgx; a lookup through
   the same transaction succeeds after each rejected create. Existing user
   store conformance passes for SQLite and PostgreSQL.
4. `openSQLite` registers database close cleanup before migration. A regression
   checks that its connection count is zero after subtest cleanup.

The new focused regressions failed before their fixes with the errors recorded
in findings 2–4 and passed afterward. With
`GORFAST_TEST_POSTGRES_DSN=postgres://gorfast:gorfast@localhost:5433/gorfast_test?sslmode=disable`,
`go test -race -count=1 ./...` passed for both databases; `go vet ./...`
also passed. The previously acknowledged limitations remain outside this
repair's scope.
