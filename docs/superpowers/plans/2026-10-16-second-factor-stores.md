# Second-Factor Stores Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement Sulis's `totp.Store`, `passkey.Store`, `passkey.ChallengeStore` and `recovery.Store` in `auth/bunstore`, so their `storetest` suites pass on SQLite and Postgres.

**Architecture:** Each store is one file in `auth/bunstore` with its own storage model, in the style of `token.go`. Migration `0002_second_factors` adds the tables, one store at a time. `UserStore.DeleteUser` then removes a user's second-factor rows in its existing transaction.

**Tech Stack:** Go 1.27, Bun v1.2.18, Sulis v0.3.0 (`totp`, `passkey`, `recovery`, `storetest`), `github.com/go-webauthn/webauthn/protocol` for transports, Postgres 18 and SQLite through `internal/testdb`.

**Spec:** `docs/superpowers/specs/2026-10-16-second-factor-stores-design.md`. "§N" below means a section of it.

## Global Constraints

- Branch `second-factor-stores` from `main` at `ba71b3b`. `main` requires the `test` check, signed commits and linear history.
- Commits are SSH-signed. The executor stages each task's files; the user runs the commit command unless they have asked the executor to commit. No Co-Authored-By or other trailers.
- No em-dashes in code, comments, docs or commit messages. Code comments at most 2 lines. Commit subjects in the repo style ("Add ...", sentence case, no prefix); bodies at most 5 lines.
- Per `CLAUDE.md`: run `node .gitnexus/run.cjs impact "<symbol>" --direction upstream --repo .` before editing `DeleteUser` (Task 6) or `testdb.Reset`'s `tables` list (Tasks 2 to 5), and `node .gitnexus/run.cjs detect-changes --scope all --repo .` before each commit. It must list only the task's files.
- Storage models stay separate from Sulis types, stores take `bun.IDB`, and the schema has no foreign keys (gorfast design §6.2).
- Wrapped errors read `bunstore: <doing what>: %w`, as in `token.go`. Sulis sentinels and the new `bunstore` sentinels are returned unwrapped.
- Write times as UTC; read them back through `.UTC()` or `utcPtr`.
- Migration statements are all `IF NOT EXISTS`. Postgres types: `TIMESTAMPTZ`, `JSONB`, `BYTEA`, `BIGINT`, `BOOLEAN ... DEFAULT FALSE`. SQLite types, as in 0001: `TIMESTAMP`, `TEXT` for JSON, `BLOB`, `INTEGER`, `BOOLEAN ... DEFAULT 0`.
- Postgres tests need `docker compose up -d postgres` and `GORFAST_TEST_POSTGRES_DSN='postgres://gorfast:gorfast@localhost:5433/gorfast_test?sslmode=disable'`. A task is done only when `go test -race -count=1 -v ./... 2>&1 | grep -E -- '--- SKIP: .*/postgres'` prints nothing.
- In this session's sandbox, prefix Go commands with `GOCACHE=$TMPDIR/gocache GOPATH=$TMPDIR/gopath GOMODCACHE=$TMPDIR/gopath/pkg/mod GOFLAGS=-modcacherw XDG_CACHE_HOME=$TMPDIR/xdg` and allow proxy.golang.org, sum.golang.org and storage.googleapis.com. Commands below leave the prefix out.
- Task 1 downloads Sulis v0.3.0 from the Go module proxy. Tell the user before running it (`AGENTS.md`, "Installing things").

## Decisions this plan adds

- **Migration 0002 grows across Tasks 2 to 5.** Each task adds the tables its store uses, so each table is reviewed with the code that uses it. The files are new on this branch, so no database has applied a partial version.
- **Counters are `int64` in the models.** `active_counter` and `sign_count` convert to and from `uint64` and `uint32` in the model functions. Bun's `pgdialect.WithAppendUintAsInt` would otherwise write `math.MaxUint32` as `-1`.
- **`SaveCredential` uses `ON CONFLICT DO NOTHING`** and treats zero affected rows as `ErrPasskeyCredentialExists`, as `CreateUser` does. A failed insert would abort a caller's Postgres transaction.
- **The TOTP upserts' `DO UPDATE` sets only the three pending columns.** The `EnrollPending` guard is `WHERE ?TableAlias.active_id IS NULL`: Bun aliases the insert target, and on Postgres the alias hides the table name that §4 writes.
- **A counter above `math.MaxInt64`** returns `fmt.Errorf("bunstore: TOTP counter %d does not fit in BIGINT", counter)`, not a sentinel. Nothing can recover from it.
- **`WithChallengeTTL` panics when called** with a TTL of zero or less: `panic("bunstore: challenge TTL must be positive")`.
- **Tests set the challenge clock** through `SetChallengeClock(s *ChallengeStore, now func() time.Time)` in `auth/bunstore/export_test.go`, which only test builds compile.
- **New sentinels**, each declared in its store's file:
  - `ErrTOTPCounterRegressed = errors.New("bunstore: TOTP counter would go down")`
  - `ErrPasskeyCredentialExists = errors.New("bunstore: passkey credential already exists")`
  - `ErrChallengeNotFound = errors.New("bunstore: challenge not found or expired")`

## Review Focus

1. **`SaveTOTP` after `DeleteTOTP`** must return `totp.ErrTOTPNotEnrolled` and must not bring the credential back, which `memstore` does. Test: Task 2 `TestSaveTOTPAfterDeleteTOTP`.
2. **A duplicate `SaveCredential` inside a caller's Postgres transaction** returns `ErrPasskeyCredentialExists` and leaves the transaction usable. Test: Task 3 `TestSaveCredentialDuplicateInsideCallerTransaction`.
3. **Duplicate hashes passed to `ReplaceCodes`** are stored once, not rejected by the primary key. Test: Task 5 `TestReplaceCodesWithDuplicateHashes`.
4. **A sign count of `math.MaxUint32`** survives `SaveCredential` and `UpdateCredentialAfterLogin`. Test: Task 3 `TestPasskeySignCountAtMaxUint32`.
5. **A challenge at exactly its expiry time is expired**, and the default TTL is 5 minutes. Test: Task 4 `TestChallengeDefaultTTL`.

---

### Task 1: Require Sulis v0.3.0

**Files:**
- Modify: `go.mod`, `go.sum`
- Modify: `README.md` (the paragraph starting "`go.mod` requires Sulis")

**Interfaces:**
- Consumes: nothing.
- Produces: `passkey.Store.RenameCredential(ctx context.Context, userID, id, name string) error`, used by Task 3.

This is a dependency change, so the existing suites are the test.

- [ ] **Step 1: Update the module**

Tell the user about the download first. Run: `go get github.com/borfast/sulis@v0.3.0 && go mod tidy`
Expected: `go.mod` requires `github.com/borfast/sulis v0.3.0`.

- [ ] **Step 2: Run the suite on both dialects**

Run: `go test -race -count=1 ./...` with the Postgres DSN set.
Expected: every package `ok`.

- [ ] **Step 3: Update the README**

Replace "`go.mod` requires Sulis `v0.2.1`. The store interfaces these stores implement first shipped in `v0.2.0`." with "`go.mod` requires Sulis `v0.3.0`, the first release where `passkey.Store.RenameCredential` takes the owner's user ID."

- [ ] **Step 4: Commit**

```bash
node .gitnexus/run.cjs detect-changes --scope all --repo .
git add go.mod go.sum README.md
git commit -m "Require Sulis v0.3.0"
```

---

### Task 2: TOTP store

**Files:**
- Create: `auth/bunstore/migrations/0002_second_factors.up.sql`, `auth/bunstore/migrations/0002_second_factors.sqlite.up.sql`
- Create: `auth/bunstore/migrations/migrations_test.go`
- Modify: `internal/testdb/testdb.go` (`tables`)
- Create: `auth/bunstore/totp.go`, `auth/bunstore/totp_test.go`

**Interfaces:**
- Consumes: `utcPtr` from `bunstore.go`.
- Produces: `type TOTPStore struct`, `func NewTOTPStore(db bun.IDB) *TOTPStore`, `var _ totp.Store = (*TOTPStore)(nil)`, `ErrTOTPCounterRegressed`, and the model `totpModel` (`table:totp_credentials,alias:tc`, primary key `user_id`), which Task 6 deletes through.

- [ ] **Step 1: Write the failing tests**

`auth/bunstore/migrations/migrations_test.go` (package `migrations_test`):

```go
// Apply must stay safe on a database that already has every migration.
func TestApplyTwice(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		if err := migrations.Apply(t.Context(), db); err != nil {
			t.Fatalf("second Apply: %v", err)
		}
	})
}
```

This one passes today. It guards the `IF NOT EXISTS` rule for every file added from here on.

`auth/bunstore/totp_test.go` (package `bunstore_test`). `activeTOTP` is a test helper that calls `ReplacePending` then `ConfirmEnrollment` and returns the promoted credential; it works whether or not the user already has an active credential.

```go
func TestTOTPStore(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		storetest.RunTOTPStore(t, func() totp.Store {
			testdb.Reset(t, db)
			return bunstore.NewTOTPStore(db)
		})
	})
}

func activeTOTP(t *testing.T, s totp.Store, userID, id string, counter uint64) *totp.Credential
```

Each test below runs through `testdb.Each`:

- `TestSaveTOTPWithAReplacedCredentialID`: `old := activeTOTP(t, s, "alice", "old", 10)`, then `activeTOTP(t, s, "alice", "new", 20)`. Set `old.LastUsedCounter = 30` and call `SaveTOTP(ctx, old)`. Want `errors.Is(err, totp.ErrTOTPNotEnrolled)`. `GetActiveTOTP` then returns `ID == "new"`, `LastUsedCounter == 20` and the new secret.
- `TestSaveTOTPAfterDeleteTOTP`: `cred := activeTOTP(..., 10)`, `DeleteTOTP`, `cred.LastUsedCounter = 11`, `SaveTOTP`. Want `totp.ErrTOTPNotEnrolled`, and `GetActiveTOTP` still returns `totp.ErrTOTPNotEnrolled`.
- `TestSaveTOTPLoweredCounterReturnsErrTOTPCounterRegressed`: active at 10, save at 9. Want `errors.Is(err, bunstore.ErrTOTPCounterRegressed)`.
- `TestTOTPCounterAboveMaxInt64`:
  - `ConfirmEnrollment` with `uint64(math.MaxInt64) + 1` returns a non-nil error. `GetPendingTOTP` still returns the pending credential, and `GetActiveTOTP` returns `totp.ErrTOTPNotEnrolled`.
  - `ConfirmEnrollment` with `math.MaxInt64` succeeds and returns `LastUsedCounter == math.MaxInt64`.
  - `SaveTOTP` with `uint64(math.MaxInt64) + 1` then returns an error, and the stored counter is still `math.MaxInt64`.
- `TestConfirmEnrollmentKeepsThePendingCreatedAt`: enroll with `CreatedAt: t0`, where `t0 := time.Now().UTC().Truncate(time.Second)`. The promoted credential and `GetActiveTOTP` both have `CreatedAt.Equal(t0)`.
- `TestTOTPStoreInsideCallerTransaction`: begin `tx`, call `activeTOTP` through `bunstore.NewTOTPStore(tx)`, and check that `GetActiveTOTP` through `tx` succeeds. After `tx.Rollback()`, `GetActiveTOTP` through `db` returns `totp.ErrTOTPNotEnrolled`.

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./auth/bunstore/ -run 'TOTP' -count=1`
Expected: build failure, `undefined: bunstore.NewTOTPStore`.

- [ ] **Step 3: Create the migration with `totp_credentials`**

Postgres, in `0002_second_factors.up.sql`:

```sql
CREATE TABLE IF NOT EXISTS totp_credentials (
    user_id            TEXT PRIMARY KEY,
    active_id          TEXT,
    active_secret      TEXT,
    active_counter     BIGINT,
    active_created_at  TIMESTAMPTZ,
    pending_id         TEXT,
    pending_secret     TEXT,
    pending_created_at TIMESTAMPTZ
);
```

The SQLite file has the same table, with `INTEGER` and `TIMESTAMP`. Add `"totp_credentials"` to `tables` in `internal/testdb/testdb.go`.

- [ ] **Step 4: Implement `TOTPStore` in `auth/bunstore/totp.go`**

Every method is one statement, following §4:

- `GetActiveTOTP` and `GetPendingTOTP` select the row with `active_id IS NOT NULL` or `pending_id IS NOT NULL`. `sql.ErrNoRows` returns `totp.ErrTOTPNotEnrolled`. `Verified` is `true` for active and `false` for pending.
- `EnrollPending` and `ReplacePending` insert `user_id` and the pending columns, with `On("CONFLICT (user_id) DO UPDATE")` setting `pending_id`, `pending_secret` and `pending_created_at` from `EXCLUDED`. `EnrollPending` adds `Where("?TableAlias.active_id IS NULL")`; zero affected rows returns `totp.ErrTOTPAlreadyEnrolled`.
- `ConfirmEnrollment` checks the counter range first, then runs this statement. `sql.ErrNoRows` returns `totp.ErrTOTPNotEnrolled`.

```sql
UPDATE totp_credentials SET
    active_id = pending_id, active_secret = pending_secret,
    active_created_at = pending_created_at,
    active_counter = CASE WHEN active_counter IS NULL OR active_counter < ? THEN ? ELSE active_counter END,
    pending_id = NULL, pending_secret = NULL, pending_created_at = NULL
WHERE user_id = ? AND pending_id = ?
RETURNING active_id, active_secret, active_counter, active_created_at
```

- `SaveTOTP` checks the counter range, then runs the `UPDATE` from §4. On zero rows it reads `active_id`: if it equals `cred.ID`, return `ErrTOTPCounterRegressed`; otherwise, or when there is no row, return `totp.ErrTOTPNotEnrolled`.
- `DeleteTOTP` deletes the row by `user_id`.

- [ ] **Step 5: Run the tests to see them pass**

Run: `go test -race -count=1 -v ./auth/bunstore/... ./internal/testdb/ 2>&1 | grep -E -- '^(ok|FAIL)|--- (FAIL|SKIP: .*/postgres)'` with the Postgres DSN set.
Expected: `ok` for each package, no `FAIL` lines, no `SKIP` lines.

- [ ] **Step 6: Commit**

```bash
node .gitnexus/run.cjs detect-changes --scope all --repo .
git add auth/bunstore/migrations/0002_second_factors.up.sql auth/bunstore/migrations/0002_second_factors.sqlite.up.sql auth/bunstore/migrations/migrations_test.go internal/testdb/testdb.go auth/bunstore/totp.go auth/bunstore/totp_test.go
git commit -m "Add the TOTP store"
```

---

### Task 3: Passkey store

**Files:**
- Modify: both `0002_second_factors` files, `internal/testdb/testdb.go` (`tables`)
- Create: `auth/bunstore/passkey.go`, `auth/bunstore/passkey_test.go`
- Modify: `go.mod`, `go.sum` (`go mod tidy` makes `github.com/go-webauthn/webauthn` a direct requirement)

**Interfaces:**
- Consumes: `isUniqueViolation`, `requireOneRow`, `utcPtr` from `bunstore.go`; Sulis v0.3.0 from Task 1.
- Produces: `type PasskeyStore struct`, `func NewPasskeyStore(db bun.IDB) *PasskeyStore`, `var _ passkey.Store = (*PasskeyStore)(nil)`, `ErrPasskeyCredentialExists`, and the model `passkeyModel` (`table:passkey_credentials,alias:pc`) for Task 6.

- [ ] **Step 1: Write the failing tests**

`auth/bunstore/passkey_test.go`. `newPasskey(userID, id string, createdAt time.Time) *passkey.Credential` is a test helper that fills `CredentialID` with `[]byte("raw-" + id)`, `PublicKey` and `AttestationType: "none"`.

```go
func TestPasskeyStore(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		storetest.RunPasskeyStore(t, func() passkey.Store {
			testdb.Reset(t, db)
			return bunstore.NewPasskeyStore(db)
		})
	})
}
```

Each test below runs through `testdb.Each`:

- `TestSaveCredentialDuplicate`: after saving `a`, saving another credential with `a.CredentialID` and a new `ID` returns `errors.Is(err, bunstore.ErrPasskeyCredentialExists)`. So does one with `a.ID` and a new `CredentialID`. `GetCredentialsByUserID` returns 1 credential.
- `TestSaveCredentialDuplicateInsideCallerTransaction`: in `tx`, save `a`, then save a duplicate of it, and want `ErrPasskeyCredentialExists`. Then `GetCredentialsByUserID` through `tx` returns 1 credential and `nil` error, which proves Postgres did not abort the transaction.
- `TestPasskeySignCountAtMaxUint32`: save with `SignCount: math.MaxUint32 - 1`, read it back equal, then `UpdateCredentialAfterLogin(..., math.MaxUint32, ...)`. `GetCredentialByID` returns `SignCount == math.MaxUint32`.
- `TestGetCredentialsByUserIDOrder`: save `"b"` at `t0`, `"a"` at `t0`, and `"c"` at `t0.Add(-time.Hour)`, all for one user. IDs come back as `["c", "a", "b"]`.
- `TestPasskeyNotFoundIsErrPasskeyNotFound`: for unknown IDs, `GetCredentialByID` and `UpdateCredentialAfterLogin` both return `errors.Is(err, passkey.ErrPasskeyNotFound)`.
- `TestPasskeyStoreInsideCallerTransaction`: in `tx`, save two credentials and `DeleteCredential` one with `allowLast` false (it runs as a savepoint). Through `tx` one remains. After rollback, `GetCredentialsByUserID` through `db` returns 0.

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./auth/bunstore/ -run 'Passkey|SaveCredential|GetCredentials' -count=1`
Expected: build failure, `undefined: bunstore.NewPasskeyStore`.

- [ ] **Step 3: Add `passkey_credentials` to the migration**

```sql
CREATE TABLE IF NOT EXISTS passkey_credentials (
    id               TEXT PRIMARY KEY,
    user_id          TEXT NOT NULL,
    credential_id    BYTEA NOT NULL,
    public_key       BYTEA NOT NULL,
    attestation_type TEXT NOT NULL DEFAULT '',
    aaguid           BYTEA,
    sign_count       BIGINT NOT NULL DEFAULT 0,
    name             TEXT NOT NULL DEFAULT '',
    transports       JSONB,
    backup_eligible  BOOLEAN NOT NULL DEFAULT FALSE,
    backup_state     BOOLEAN NOT NULL DEFAULT FALSE,
    last_used_at     TIMESTAMPTZ,
    discoverable     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at       TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS passkey_credentials_credential_id_key ON passkey_credentials (credential_id);
CREATE INDEX IF NOT EXISTS passkey_credentials_user_id_idx ON passkey_credentials (user_id);
```

Add the SQLite translation, and add `"passkey_credentials"` to `tables`.

- [ ] **Step 4: Implement `PasskeyStore` in `auth/bunstore/passkey.go`**

Follow §5. The model's `Transports` field is `[]protocol.AuthenticatorTransport` with no tag option, so Bun stores it as JSON. `UpdateCredentialAfterLogin` matches on `credential_id`, and `RenameCredential` on `id` and `user_id`; both turn zero rows into `passkey.ErrPasskeyNotFound` through `requireOneRow`. `DeleteCredential` runs the four steps of §5 inside `s.db.RunInTx`, and the lock in step 1 is `Set("user_id = user_id")`.

- [ ] **Step 5: Run the tests to see them pass**

Run: `go mod tidy`, then the Task 2 Step 5 command.
Expected: the same as Task 2. A Postgres error with SQLSTATE `40P01` (deadlock) in the race subtests means the lock design is wrong: stop and report it, do not add retries.

- [ ] **Step 6: Commit**

```bash
node .gitnexus/run.cjs detect-changes --scope all --repo .
git add auth/bunstore/migrations/0002_second_factors.up.sql auth/bunstore/migrations/0002_second_factors.sqlite.up.sql internal/testdb/testdb.go auth/bunstore/passkey.go auth/bunstore/passkey_test.go go.mod go.sum
git commit -m "Add the passkey store"
```

---

### Task 4: Passkey challenge store

**Files:**
- Modify: both `0002_second_factors` files, `internal/testdb/testdb.go` (`tables`)
- Create: `auth/bunstore/challenge.go`, `auth/bunstore/challenge_test.go`, `auth/bunstore/export_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks beyond the migration files.
- Produces:
  - `type ChallengeStore struct` with unexported fields `db bun.IDB`, `ttl time.Duration` and `now func() time.Time`
  - `type ChallengeOption func(*ChallengeStore)` and `func WithChallengeTTL(d time.Duration) ChallengeOption`
  - `func NewChallengeStore(db bun.IDB, opts ...ChallengeOption) *ChallengeStore`, with a default TTL of `5 * time.Minute` and `time.Now` as the default clock
  - `func (s *ChallengeStore) DeleteExpiredChallenges(ctx context.Context) (int, error)`
  - `var _ passkey.ChallengeStore = (*ChallengeStore)(nil)` and `ErrChallengeNotFound`
  - in `export_test.go` (package `bunstore`): `func SetChallengeClock(s *ChallengeStore, now func() time.Time)`

- [ ] **Step 1: Write the failing tests**

`auth/bunstore/challenge_test.go`. Tests that set the clock use `t0 := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)` and a variable `now` read by the clock function.

```go
func TestChallengeStore(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		storetest.RunPasskeyChallengeStore(t, func() passkey.ChallengeStore {
			testdb.Reset(t, db)
			return bunstore.NewChallengeStore(db)
		})
	})
}
```

Each test below runs through `testdb.Each`, except the panic test:

- `TestChallengeDefaultTTL`: save `"inside"` and `"boundary"` at `t0`. At `t0+5m-1s`, `ConsumeChallenge("inside")` returns the saved bytes. At `t0+5m`, `ConsumeChallenge("boundary")` returns `errors.Is(err, bunstore.ErrChallengeNotFound)`.
- `TestWithChallengeTTL`: with `WithChallengeTTL(time.Minute)`, a challenge saved at `t0` returns `ErrChallengeNotFound` at `t0+1m`.
- `TestWithChallengeTTLPanicsOnNonPositive`: `WithChallengeTTL(0)` and `WithChallengeTTL(-time.Second)` each panic. Check with `recover()` in a deferred function, with no database needed.
- `TestDeleteExpiredChallenges`: save `"old1"` and `"old2"` at `t0` and `"new"` at `t0+3m`. At `t0+5m`, `DeleteExpiredChallenges` returns `2, nil`. After that, `"new"` consumes successfully and `"old1"` returns `ErrChallengeNotFound`.
- `TestChallengeStoreInsideCallerTransaction`:
  - Save `"k"` through `db`, consume it through a `tx` store, and roll back. `"k"` then consumes through `db`, because the rollback undid the delete.
  - Save `"j"` through a `tx` store and roll back. `"j"` then returns `ErrChallengeNotFound` through `db`.

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./auth/bunstore/ -run 'Challenge' -count=1`
Expected: build failure, `undefined: bunstore.NewChallengeStore`.

- [ ] **Step 3: Add `passkey_challenges` to the migration**

```sql
CREATE TABLE IF NOT EXISTS passkey_challenges (
    challenge_key TEXT PRIMARY KEY,
    session_data  BYTEA NOT NULL,
    expires_at    TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS passkey_challenges_expires_at_idx ON passkey_challenges (expires_at);
```

Add the SQLite translation, and add `"passkey_challenges"` to `tables`.

- [ ] **Step 4: Implement `ChallengeStore` in `auth/bunstore/challenge.go`**

Follow §5. `SaveChallenge` upserts on `challenge_key`, with `expires_at = s.now().UTC().Add(s.ttl)`. `ConsumeChallenge` is a `NewDelete` with `Returning("session_data, expires_at")`. `sql.ErrNoRows`, or an `expires_at` that is not after `s.now()`, returns `ErrChallengeNotFound`. `DeleteExpiredChallenges` deletes `expires_at <= s.now().UTC()` and returns `RowsAffected` as an `int`.

- [ ] **Step 5: Run the tests to see them pass**

Run: the Task 2 Step 5 command.
Expected: the same as Task 2.

- [ ] **Step 6: Commit**

```bash
node .gitnexus/run.cjs detect-changes --scope all --repo .
git add auth/bunstore/migrations/0002_second_factors.up.sql auth/bunstore/migrations/0002_second_factors.sqlite.up.sql internal/testdb/testdb.go auth/bunstore/challenge.go auth/bunstore/challenge_test.go auth/bunstore/export_test.go
git commit -m "Add the passkey challenge store"
```

---

### Task 5: Recovery code store

**Files:**
- Modify: both `0002_second_factors` files, `internal/testdb/testdb.go` (`tables`)
- Create: `auth/bunstore/recovery.go`, `auth/bunstore/recovery_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks beyond the migration files.
- Produces:
  - `type RecoveryStore struct`, `func NewRecoveryStore(db bun.IDB) *RecoveryStore` and `var _ recovery.Store = (*RecoveryStore)(nil)`
  - the models `recoverySetModel` (`table:recovery_code_sets,alias:rs`) and `recoveryCodeModel` (`table:recovery_codes,alias:rc`), for Task 6
  - the test helper `countRows(t *testing.T, db bun.IDB, table, userID string) int` in `recovery_test.go`, which runs `SELECT count(*) FROM <table> WHERE user_id = ?`. Task 6 uses it.

- [ ] **Step 1: Write the failing tests**

`auth/bunstore/recovery_test.go`:

```go
func TestRecoveryStore(t *testing.T) {
	testdb.Each(t, func(t *testing.T, db *bun.DB) {
		storetest.RunRecoveryStore(t, func() recovery.Store {
			testdb.Reset(t, db)
			return bunstore.NewRecoveryStore(db)
		})
	})
}
```

Each test below runs through `testdb.Each`:

- `TestConcurrentReplaceCodesForANewUser`: run 20 iterations, each with a fresh user ID. Start `ReplaceCodes(ctx, user, setA)` and `ReplaceCodes(ctx, user, setB)` from two goroutines released together by a closed channel, where `setA = {"a1","a2","a3"}` and `setB = {"b1","b2","b3"}`. Both calls return `nil`. `SELECT code_hash ... ORDER BY code_hash` then returns exactly `setA` or exactly `setB`, never six rows.
- `TestReplaceCodesWithDuplicateHashes`: `ReplaceCodes(ctx, "alice", []string{"h1", "h1", "h2"})` returns `nil` and `CountCodes` returns 2. `ConsumeCode("h1")` returns `1, nil`, and a second `ConsumeCode("h1")` returns `0` and `recovery.ErrCodeNotFound`.
- `TestReplaceCodesWithNoCodesKeepsTheSetRow`: `ReplaceCodes(ctx, "alice", nil)` leaves `countRows(t, db, "recovery_code_sets", "alice") == 1` and `CountCodes == 0`. `ConsumeCode` then returns `0` and `recovery.ErrCodeNotFound`.
- `TestDeleteCodesRemovesTheSetRow`: after `ReplaceCodes` and `DeleteCodes`, both `countRows(... "recovery_code_sets" ...)` and `countRows(... "recovery_codes" ...)` are 0.
- `TestRecoveryStoreInsideCallerTransaction`: in `tx`, `ReplaceCodes` three codes and `ConsumeCode` one, which returns `2`. After rollback, `CountCodes` through `db` is 0.

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./auth/bunstore/ -run 'Recovery|ReplaceCodes|DeleteCodes' -count=1`
Expected: build failure, `undefined: bunstore.NewRecoveryStore`.

- [ ] **Step 3: Add the two recovery tables to the migration**

```sql
CREATE TABLE IF NOT EXISTS recovery_code_sets (
    user_id     TEXT PRIMARY KEY,
    replaced_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS recovery_codes (
    user_id   TEXT NOT NULL,
    code_hash TEXT NOT NULL,
    PRIMARY KEY (user_id, code_hash)
);
```

Add the SQLite translation, and add `"recovery_code_sets"` and `"recovery_codes"` to `tables`.

- [ ] **Step 4: Implement `RecoveryStore` in `auth/bunstore/recovery.go`**

Follow §6. Each write method runs in `s.db.RunInTx` and writes the `recovery_code_sets` row first. `ReplaceCodes` upserts it with `replaced_at = time.Now().UTC()`, and `ConsumeCode` uses `Set("user_id = user_id")`. `ReplaceCodes` removes duplicate hashes before a single bulk insert, and skips the insert when no hashes remain, because Bun rejects an empty slice model. `ConsumeCode` returns `0, recovery.ErrCodeNotFound` when either step 1 or step 2 affects no rows.

- [ ] **Step 5: Run the tests to see them pass**

Run: the Task 2 Step 5 command.
Expected: the same as Task 2.

- [ ] **Step 6: Commit**

```bash
node .gitnexus/run.cjs detect-changes --scope all --repo .
git add auth/bunstore/migrations/0002_second_factors.up.sql auth/bunstore/migrations/0002_second_factors.sqlite.up.sql internal/testdb/testdb.go auth/bunstore/recovery.go auth/bunstore/recovery_test.go
git commit -m "Add the recovery code store"
```

---

### Task 6: Delete a user's second factors with the user

**Files:**
- Modify: `auth/bunstore/user.go` (`DeleteUser` and its doc comment)
- Modify: `auth/bunstore/user_delete_test.go`
- Modify: `README.md` ("Go module" section)

**Interfaces:**
- Consumes: `totpModel`, `passkeyModel`, `recoverySetModel` and `recoveryCodeModel` from Tasks 2, 3 and 5; the stores' constructors; `countRows` from Task 5.
- Produces: nothing new.

- [ ] **Step 1: Run impact on `DeleteUser`**

Run: `node .gitnexus/run.cjs impact "DeleteUser" --direction upstream --repo .`
Expected: callers are tests only. Report anything else before editing.

- [ ] **Step 2: Write the failing tests**

In `user_delete_test.go`, add `seedSecondFactors(t *testing.T, db bun.IDB)`, which gives alice and bob each an active TOTP credential, one passkey and the recovery codes `{"h1", "h2"}`. Then:

- `TestDeleteUserRemovesSecondFactors`: seed with `seedDeleteUser` and `seedSecondFactors`, then `DeleteUser(ctx, "alice")`.
  - For alice: `GetActiveTOTP` returns `totp.ErrTOTPNotEnrolled`, `GetCredentialsByUserID` returns 0 credentials, `CountCodes` is 0, and `countRows(t, db, "recovery_code_sets", "alice")` is 0.
  - For bob: `GetActiveTOTP` succeeds, there is 1 credential, `CountCodes` is 2, and his set row count is 1.
- Turn `TestDeleteUserRemovesTheUserRowFirst` into `TestDeleteUserDeleteOrder`. It seeds both helpers, records the first `DELETE FROM` index per table, and requires this order: `users`, `sessions`, `tokens`, `totp_credentials`, `passkey_credentials`, `recovery_code_sets`, `recovery_codes`. Keep the comment on why the user row goes first, and add one line: the set row goes before the codes to keep §6's lock order.
- In `TestDeleteUserInsideCallerTransaction`, also seed second factors. After the rollback, alice's `GetActiveTOTP` succeeds again.

- [ ] **Step 3: Run the tests to see them fail**

Run: `go test ./auth/bunstore/ -run 'DeleteUser' -count=1`
Expected: `TestDeleteUserRemovesSecondFactors` fails on alice's `GetActiveTOTP`, and `TestDeleteUserDeleteOrder` fails because it sees only three tables.

- [ ] **Step 4: Extend `DeleteUser`**

After the tokens delete, in the same `RunInTx`, delete by `user_id = ?` from `totpModel`, `passkeyModel`, `recoverySetModel` and `recoveryCodeModel`, in that order. Update the doc comment to say second factors go too, and that challenges are left to expire.

- [ ] **Step 5: Update the README**

- In the "Go module" section, list all seven interfaces: Sulis's `UserStore`, `SessionStore` and `TokenStore`, and the `totp.Store`, `passkey.Store`, `passkey.ChallengeStore` and `recovery.Store` interfaces.
- Add: "`DeleteUser` also removes the user's TOTP credential, passkeys and recovery codes."
- Add: "Passkey challenges expire after five minutes, which `WithChallengeTTL` changes. Expired rows stay until the application calls `DeleteExpiredChallenges`, so run it on a schedule."

- [ ] **Step 6: Run the full check**

Run, with the Postgres DSN set:

```bash
go mod tidy && git diff --exit-code go.mod go.sum
go vet ./...
go test -race -count=1 -v ./... 2>&1 | tee "$TMPDIR/test.log" | grep -E -- '^(ok|FAIL)'
grep -E -- '--- SKIP: .*/postgres' "$TMPDIR/test.log"
```

Expected: no diff, no vet output, `ok` for every package, and no output from the last `grep`.

- [ ] **Step 7: Commit**

```bash
node .gitnexus/run.cjs detect-changes --scope all --repo .
git add auth/bunstore/user.go auth/bunstore/user_delete_test.go README.md
git commit -m "Delete a user's second factors with the user"
```

---

## Not in this plan

- Reporting the two Sulis gaps in §8 (the racy reference SQL, and `memstore.SaveTOTP`) upstream.
- Opening the pull request. CI runs `govulncheck` and the Postgres skip check on it.
