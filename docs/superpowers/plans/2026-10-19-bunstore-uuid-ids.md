# auth/bunstore UUID identifiers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move `auth/bunstore` to Sulis v0.5.0, with IDs stored as native `uuid` on Postgres and 16-byte `BLOB` on SQLite.

**Architecture:** A new exported `bunstore.ID` type (the standard library `uuid.UUID` with Bun's `QueryAppender` and `sql.Scanner`) writes each ID in the form its dialect expects. Migrations `0001` and `0002` are edited in place. `SaveTOTP` moves to the v0.5.0 contract first, on its own, while the IDs are still strings.

**Tech Stack:** Go 1.27 standard library `uuid`, Bun v1.2.18 (`pgdialect`, `sqlitedialect`, `pgdriver`), modernc SQLite, Postgres 18, Sulis `storetest`.

**Spec:** `docs/superpowers/specs/2026-10-19-bunstore-uuid-ids-design.md`

## Global Constraints

- `go.mod` requires `github.com/borfast/sulis v0.5.0` (from Task 3). No other dependency is added or changed; `go mod tidy` leaves `go.mod` and `go.sum` unchanged after Task 3.
- `go 1.27.0` and `toolchain go1.27.2` stay as they are.
- Postgres tests: `docker compose up -d postgres`, then `GORFAST_TEST_POSTGRES_DSN='postgres://gorfast:gorfast@localhost:5433/gorfast_test?sslmode=disable'`. A skipped Postgres subtest counts as a failure, as in CI.
- Before editing a function, run `node .gitnexus/run.cjs impact "<symbol>" --direction upstream --repo .`. Before each commit, run `node .gitnexus/run.cjs detect-changes --scope all --repo .`. Treat `UNKNOWN` risk as unresolved and confirm with a text search.
- Code comments are at most 2 lines; commit bodies at most 5 lines; no em-dashes, and no en-dash or spaced hyphen in their place.
- No `Co-Authored-By` or `Claude-Session` trailers. Commits are signed by the repo's git config.
- The full gate is CI's: `go mod tidy` with no diff, `go vet ./...`, `go test -race -count=1 ./...` with Postgres, and `govulncheck ./...`.

## Review Focus

- A store built over a caller's `bun.Tx`: `SaveTOTP`'s new `RunInTx` must run inside that transaction, so a rollback undoes the save. Pinned in Task 2.
- `DeleteUserSessionsExcept(ctx, userID, uuid.Nil())`: its contract says every session of the user goes, because `uuid.Nil()` names no session. Pinned in Task 3.
- `DeleteUser(ctx, uuid.Nil())`: must match no row and keep its current return value. Pinned in Task 3 by the updated `TestDeleteUserMissingAndEmptyID`.
- A user ID read back through the pgx driver, not only `pgdriver`: `ID.Scan` must accept what pgx returns. Already pinned by `TestPostgresDuplicateUserDrivers` once its IDs are UUIDs (Task 3).

---

### Task 1: The `bunstore.ID` type

**Files:**
- Create: `auth/bunstore/id.go`
- Create: `auth/bunstore/id_test.go`

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces, in package `bunstore`:
  - `type ID uuid.UUID`
  - `func (id ID) AppendQuery(gen schema.QueryGen, b []byte) ([]byte, error)`
  - `func (id *ID) Scan(src any) error`
  - `func (id ID) String() string`

- [ ] **Step 1: Write the failing tests** in `id_test.go` (package `bunstore_test`):
  - `TestIDAppendQuery`, table-driven, `u := uuid.NewV7()`:
    - `schema.NewQueryGen(pgdialect.New())`: `bunstore.ID(u)` gives `"'" + u.String() + "'"`, and `bunstore.ID(uuid.Nil())` gives `"'00000000-0000-0000-0000-000000000000'"`.
    - `schema.NewQueryGen(sqlitedialect.New())`: `"X'" + hex.EncodeToString(u[:]) + "'"`, and for `uuid.Nil()`, `"X'" + strings.Repeat("0", 32) + "'"`.
  - `TestIDAppendQueryRefusesOtherDialects`: `AppendQuery(schema.NewNopQueryGen(), nil)` returns a non-nil error.
  - `TestIDScan`, table-driven:
    - accepted: `nil` gives `uuid.Nil()`; `u[:]` gives `u`; `u.String()` gives `u`; `[]byte(u.String())` gives `u`.
    - refused (non-nil error): `make([]byte, 15)`, `make([]byte, 17)`, `"not-a-uuid"`, `int64(1)`.
  - `TestIDRoundTrip`, with `testdb.Each`:
    - create table `id_round_trip (id T PRIMARY KEY, ref T)`, where `T` is `UUID` when `db.Dialect().Name() == dialect.PG` and `BLOB` otherwise;
    - use the model `struct { bun.BaseModel \`bun:"table:id_round_trip"\`; ID bunstore.ID \`bun:"id,pk"\`; Ref *bunstore.ID \`bun:"ref"\` }`;
    - insert one row with `Ref` nil and one with `Ref` set, then read each back with `Where("id = ?", bunstore.ID(x))`;
    - both rows come back equal, and the nil `Ref` stays nil;
    - on SQLite only, `SELECT typeof(id), length(id)` gives `blob` and `16`.
  - `TestIDsSortByCreationTime`, with `testdb.Each` and the same table:
    - generate 5 IDs with `time.Sleep(2 * time.Millisecond)` between them;
    - insert them in reverse order;
    - `SELECT id FROM id_round_trip ORDER BY id` returns them in generation order.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -count=1 -run 'TestID' ./auth/bunstore/`
Expected: build failure, `undefined: bunstore.ID`.

- [ ] **Step 3: Implement `ID` in `id.go`**

`AppendQuery` switches on `gen.Dialect().Name()`: `dialect.PG` uses `gen.Dialect().AppendString(b, uuid.UUID(id).String())`, `dialect.SQLite` uses `gen.Dialect().AppendBytes(b, id[:])`, and anything else returns an error naming the dialect. `Scan` accepts exactly the inputs in Step 1. The type's doc comment, verbatim from the spec: `// ID is a sulis ID as a Bun value: a native uuid on Postgres, 16 bytes on SQLite.`

- [ ] **Step 4: Run the tests to verify they pass, on both dialects**

Run: `GORFAST_TEST_POSTGRES_DSN=... go test -race -count=1 -v -run 'TestID' ./auth/bunstore/`
Expected: PASS, with no `--- SKIP` for `postgres`.

- [ ] **Step 5: Commit**

```bash
git add auth/bunstore/id.go auth/bunstore/id_test.go
git commit -m "Add bunstore.ID, a UUID column value for Postgres and SQLite"
```

### Task 2: `SaveTOTP` follows the Sulis v0.5.0 contract

The IDs are still strings here; only the save path changes.

**Files:**
- Modify: `auth/bunstore/totp.go` (`ErrTOTPCounterRegressed` at lines 15-17; `SaveTOTP` and `explainFailedSave` at lines 187-233)
- Modify: `auth/bunstore/totp_test.go`

**Interfaces:**
- Consumes: `totp.ErrTOTPReplayed`, `totp.ErrTOTPNotEnrolled` (both exist in the current Sulis).
- Produces: `bunstore.ErrTOTPCounterRegressed` no longer exists.

- [ ] **Step 1: Write the failing tests** in `totp_test.go`:
  - Rename `TestSaveTOTPLoweredCounterReturnsErrTOTPCounterRegressed` to `TestSaveTOTPLoweredCounterReturnsErrTOTPReplayed`, and `TestSaveTOTPAtTheStoredCounterReturnsErrTOTPCounterRegressed` to `TestSaveTOTPAtTheStoredCounterReturnsErrTOTPReplayed`. Both check `errors.Is(err, totp.ErrTOTPReplayed)`.
  - `TestSaveTOTPWritesOnlyTheCounter`:
    - `cred := activeTOTP(t, s, "alice", "c1", 10)`;
    - set `cred.LastUsedCounter = 11` and `cred.Secret = "changed"`;
    - `SaveTOTP` returns nil;
    - `GetActiveTOTP` gives counter `11` and secret `"secret-c1"`.
  - Extend `TestTOTPStoreInsideCallerTransaction`:
    - after `activeTOTP` in the transaction, save the credential with counter `11` through the transaction's store;
    - after the rollback, `GetActiveTOTP` on `db` still gives `ErrTOTPNotEnrolled`, as now.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -count=1 -run 'TOTP' ./auth/bunstore/`
Expected: FAIL. The two renamed tests get `bunstore: TOTP counter would not go up`, and `TestSaveTOTPWritesOnlyTheCounter` gets secret `"changed"`.

- [ ] **Step 3: Implement the change in `totp.go`**
  - `SaveTOTP` runs in `s.db.RunInTx`. The `UPDATE` sets only `active_counter`, keeping its three `WHERE` conditions.
  - On 0 rows, `explainFailedSave(ctx, tx, cred)` reads `active_id` in the same transaction and returns:
    - `totp.ErrTOTPReplayed` when it equals `cred.ID`;
    - `totp.ErrTOTPNotEnrolled` otherwise, including when no row exists.
  - Delete `ErrTOTPCounterRegressed` and its comment.
  - The doc comment on `SaveTOTP` becomes at most 2 lines.

- [ ] **Step 4: Run the tests to verify they pass, on both dialects**

Run: `GORFAST_TEST_POSTGRES_DSN=... go test -race -count=1 -v ./auth/bunstore/`
Expected: PASS, with no Postgres skips.

- [ ] **Step 5: Commit**

```bash
git add auth/bunstore/totp.go auth/bunstore/totp_test.go
git commit -m "Write only the counter in SaveTOTP and report a replay as ErrTOTPReplayed"
```

### Task 3: Sulis v0.5.0, the UUID schema and the stores

Everything here changes in one step, because the package compiles only once every store matches the new interfaces.

**Files:**
- Modify: `go.mod`, `go.sum`
- Modify: `auth/bunstore/migrations/0001_auth.up.sql`, `0001_auth.sqlite.up.sql`, `0002_second_factors.up.sql`, `0002_second_factors.sqlite.up.sql`
- Create: `auth/bunstore/migrations/schema_test.go`
- Modify: `auth/bunstore/user.go`, `session.go`, `token.go`, `passkey.go`, `totp.go`, `recovery.go`, and `challenge.go` only if its interface changed
- Modify: every `*_test.go` in `auth/bunstore` and `auth/bunstore/migrations/migrations_test.go`

**Interfaces:**
- Consumes: `bunstore.ID` from Task 1, and the Sulis v0.5.0 interfaces `sulis.UserStore`, `sulis.SessionStore`, `sulis.TokenStore`, `totp.Store`, `passkey.Store`, `passkey.ChallengeStore` and `recovery.Store`. Read them with `go doc` once the module is bumped.
- Produces: stores whose signatures match those interfaces. The existing `var _ X = (*Y)(nil)` lines check this.

- [ ] **Step 1: Write the failing schema test** `TestSchemaIDColumns` in `migrations/schema_test.go`, with `testdb.Each`. It is table-driven over these 12 columns, with `nullable` true only where marked:

  `users.id`, `sessions.id`, `sessions.user_id`, `tokens.id`, `tokens.user_id` (nullable), `totp_credentials.user_id`, `totp_credentials.active_id` (nullable), `totp_credentials.pending_id` (nullable), `passkey_credentials.id`, `passkey_credentials.user_id`, `recovery_code_sets.user_id`, `recovery_codes.user_id`.

  Each case inserts one row whose other required columns hold fresh values, and checks:
  - **both dialects:** a 16-byte ID is accepted (on Postgres, `bunstore.ID(uuid.NewV7())`); NULL is accepted when `nullable` and refused otherwise.
  - **SQLite:** `make([]byte, 15)`, `make([]byte, 17)` and the text `"abcdefghijklmnop"` are refused.
  - **Postgres, `users.id` only:** the text `"not-a-uuid"` is refused.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -count=1 -run TestSchemaIDColumns ./auth/bunstore/migrations/`
Expected: FAIL. The `TEXT` columns accept the short blobs and the text.

- [ ] **Step 3: Edit the four migration files** to match spec section 5:
  - **Postgres:** `UUID`. `tokens.user_id` is `UUID` with no `NOT NULL` and no default.
  - **SQLite:** `BLOB`, with `CHECK (typeof(col) = 'blob' AND length(col) = 16)`. Nullable columns use `CHECK (col IS NULL OR (typeof(col) = 'blob' AND length(col) = 16))` instead. Every primary key column also says `NOT NULL`.
  - `passkey_challenges.challenge_key` stays `TEXT`.

- [ ] **Step 4: Bump Sulis**

Run: `go get github.com/borfast/sulis@v0.5.0 && go mod tidy`
Expected: `go.mod` requires `github.com/borfast/sulis v0.5.0`. The build now fails in `auth/bunstore` with type errors.

- [ ] **Step 5: Update the stores**
  - Model ID fields become `ID`. `tokenModel.UserID`, `totpModel.ActiveID` and `totpModel.PendingID` become `*ID`.
  - The `to...Model` and `from...Model` functions convert between the models and the Sulis structs. A `Token.UserID` of `uuid.Nil()` becomes a nil `*ID`, and nil becomes `uuid.Nil()`.
  - Every query argument that is an ID is wrapped as `ID(x)`, about 45 places: run `grep -n 'Where(\|Set(' auth/bunstore/*.go` and check each one.
  - Method signatures follow the v0.5.0 interfaces.

- [ ] **Step 6: Update the existing tests**
  - Replace string IDs such as `"alice"`, `"c1"`, `"p1"` and `"u1"` with variables holding `uuid.NewV7()` values. Keep each test's meaning.
  - In `TestDeleteUserMissingAndEmptyID`, the empty ID becomes `uuid.Nil()`.
  - In `migrations_test.go`, insert the user with `bunstore.ID(id)` and look it up the same way.
  - Add `TestDeleteUserSessionsExceptNilDeletesEverySession` to `session_test.go`, with `testdb.Each`: create two sessions for one user, call `DeleteUserSessionsExcept(ctx, userID, uuid.Nil())`, and `ListUserSessions` gives none.

- [ ] **Step 7: Run the whole suite, on both dialects**

Run: `GORFAST_TEST_POSTGRES_DSN=... go test -race -count=1 -v ./... 2>&1 | tee $TMPDIR/test.log; grep -E -- '--- (FAIL|SKIP).*' $TMPDIR/test.log`
Expected: every package `ok`, and the `grep` prints nothing. This includes the v0.5.0 `storetest` cases such as `ConcurrentSaveTOTPOfOneCounterHasOneWinner` and `DeleteUserTokensForNilUserDeletesNothing`.

- [ ] **Step 8: Commit**

```bash
git add go.mod go.sum auth/bunstore
git commit -m "Store IDs as native UUIDs and move to Sulis v0.5.0" -m "Postgres columns are uuid, SQLite columns 16-byte BLOB with a type and length check. Migrations 0001 and 0002 are edited in place. Closes #8."
```

### Task 4: README and the full gate

**Files:**
- Modify: `README.md` (the "Go module" section, lines 43-80)

**Interfaces:**
- Consumes: `bunstore.ID` from Task 1; the schema from Task 3.

- [ ] **Step 1: Update the README** per spec section 8:
  - `go.mod` requires Sulis v0.5.0. This replaces the v0.3.0 paragraph; keep the `go.work` advice.
  - IDs are `uuid` on Postgres and 16-byte `BLOB` on SQLite, and existing databases must be recreated.
  - One short example of an application table:
    - a Bun model field `OwnerID bunstore.ID \`bun:"owner_id"\``;
    - the column as `UUID`, or as `BLOB` with the length check;
    - `Where("owner_id = ?", bunstore.ID(userID))`.

- [ ] **Step 2: Check that the example compiles**

Paste the example's Go into a throwaway `_test.go` file in `auth/bunstore`, run `go vet ./auth/bunstore/`, then delete the file.
Expected: no errors.

- [ ] **Step 3: Run the full gate**

Run: `go mod tidy && git diff --exit-code go.mod go.sum && go vet ./... && GORFAST_TEST_POSTGRES_DSN=... go test -race -count=1 ./... && go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./... && claude plugin validate .`
Expected: no diff, vet clean, all tests pass, govulncheck reports no called vulnerabilities, and the plugin validates.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "Document UUID IDs and bunstore.ID in the README"
```
