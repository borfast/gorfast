# Second-factor stores in `auth/bunstore`: design

Date: 2026-10-16. Roadmap step 1a in `2026-09-27-gorfast-design.md` §8.

## 1. Purpose

`auth/bunstore` implements Sulis's user, session and token stores. This spec
adds the other four:

| Sulis interface | Constructor | Conformance suite |
|---|---|---|
| `totp.Store` | `NewTOTPStore(db bun.IDB)` | `storetest.RunTOTPStore` |
| `passkey.Store` | `NewPasskeyStore(db bun.IDB)` | `storetest.RunPasskeyStore` |
| `passkey.ChallengeStore` | `NewChallengeStore(db bun.IDB, opts ...ChallengeOption)` | `storetest.RunPasskeyChallengeStore` |
| `recovery.Store` | `NewRecoveryStore(db bun.IDB)` | `storetest.RunRecoveryStore` |

Done means all four suites pass against SQLite and a real Postgres in CI, with
no Postgres subtest skipped. That completes slice 1 (gorfast design §6.3).

The work targets Sulis v0.3.0, where `passkey.Store.RenameCredential` takes the
owner's user ID. gorfast's `go.mod` moves from v0.2.1 to v0.3.0 first.

The existing design points in gorfast design §6.2 still apply: storage models
are separate from Sulis types, stores take `bun.IDB`, and the schema has no
foreign keys.

## 2. Decisions and the alternatives rejected

- **One spec and one plan, one task per store.** The four stores share a
  migration, `testdb` and `DeleteUser`. Four plans would repeat that shared
  work four times.
- **One row per user for TOTP**, holding an active slot and a pending slot.
  Rejected: separate `totp_active` and `totp_pending` tables, as in Sulis's
  reference SQL. With two tables, the active-credential check and the pending
  write touch different rows, so on Postgres under READ COMMITTED two
  transactions can both pass the check (section 8). With one row, every TOTP
  operation is a single statement on that row.
- **One lock row per user for recovery codes** (`recovery_code_sets`).
  Rejected: locking the code rows themselves. A user with no codes has no rows
  to lock, so two concurrent `ReplaceCodes` calls for a new user could both
  insert, and leave the union of two code sets.
- **Challenges expire by TTL, with a cleanup method.** `expires_at` is set when
  a challenge is saved, `ConsumeChallenge` treats an expired row as missing, and the
  application schedules `DeleteExpiredChallenges`. Rejected: no expiry (Sulis
  asks for about five minutes), and a background goroutine in the store (a
  store must not start work its caller cannot see or stop).
- **`DeleteUser` removes second-factor rows in its existing transaction.**
  Rejected: leaving it to the application. A deleted account's TOTP secret,
  passkeys and recovery codes have no use, and removing them in the same
  transaction as the user row matches how sessions and tokens are handled.
- **Keep Sulis's one-TOTP-per-user model.** `totp.Store` allows one active
  secret per user. The same secret can be added to several authenticator
  apps, but separate secrets per device need a change to Sulis (section 8).

## 3. Schema

Migration `0002_second_factors`, as `0002_second_factors.up.sql` (Postgres)
and `0002_second_factors.sqlite.up.sql` (SQLite). Every statement is
`IF NOT EXISTS`, so `migrations.Apply` on a database that already has 0001
adds only the new tables. Type mapping follows 0001: `TIMESTAMPTZ` and `JSONB`
on Postgres, `TIMESTAMP` and `TEXT` on SQLite; byte columns are `BYTEA` and
`BLOB`.

**`totp_credentials`**: one row per user.

| Column | Type | Notes |
|---|---|---|
| `user_id` | TEXT | primary key |
| `active_id` | TEXT, nullable | NULL means no active credential |
| `active_secret` | TEXT, nullable | |
| `active_counter` | BIGINT, nullable | `LastUsedCounter` |
| `active_created_at` | timestamp, nullable | |
| `pending_id` | TEXT, nullable | NULL means no pending enrollment |
| `pending_secret` | TEXT, nullable | |
| `pending_created_at` | timestamp, nullable | |

**`passkey_credentials`**: one row per credential. Primary key `id`. A unique
index on `credential_id`, and an index on `user_id`. The other columns map
`passkey.Credential` field by field: `public_key` and `aaguid` as bytes,
`attestation_type` and `name` as TEXT, `sign_count` as BIGINT (a `uint32` does
not fit Postgres `INTEGER`), `backup_eligible`, `backup_state` and
`discoverable` as BOOLEAN, `last_used_at` nullable, `created_at`, and
`transports` as JSON, stored the same way as `users.metadata`.

**`passkey_challenges`**: primary key `challenge_key`, `session_data` as
bytes, `expires_at` with an index.

**`recovery_code_sets`**: primary key `user_id`, and `replaced_at`. It exists
to be locked (section 6).

**`recovery_codes`**: primary key `(user_id, code_hash)`.

`testdb.Reset` empties all five new tables.

## 4. TOTP

Every method is a single statement on the user's row, so none needs an
explicit transaction.

- **`GetActiveTOTP` and `GetPendingTOTP`** read one slot. A missing row or an
  empty slot returns `totp.ErrTOTPNotEnrolled`. `Verified` is true for the
  active slot and false for the pending slot; it is not stored.
- **`EnrollPending`**: an upsert that writes the pending slot, `ON CONFLICT
  (user_id) DO UPDATE ... WHERE totp_credentials.active_id IS NULL`. Postgres
  locks the conflicting row and evaluates the `WHERE` against its latest
  version, so the check and the write are one atomic step. Zero rows affected
  returns `totp.ErrTOTPAlreadyEnrolled`.
- **`ReplacePending`**: the same upsert without the `WHERE`. The active slot is
  not touched.
- **`ConfirmEnrollment`**: one `UPDATE ... WHERE user_id = ? AND pending_id = ?
  RETURNING ...`. It copies the pending slot into the active slot, including
  `pending_created_at` (as `memstore` does), and clears the pending slot. The
  counter is `CASE WHEN active_counter IS NULL OR active_counter < ? THEN ?
  ELSE active_counter END`, which takes the higher of the two on both dialects
  without `GREATEST`. Zero rows returns `totp.ErrTOTPNotEnrolled`.
- **`SaveTOTP`**: `UPDATE ... SET active_secret = ?, active_counter = ? WHERE
  user_id = ? AND active_id = ? AND active_counter <= ?`. On zero rows, a
  follow-up read chooses the error: if the active slot still has this ID, the
  counter would have gone down, and the store returns a new
  `bunstore.ErrTOTPCounterRegressed`; otherwise it returns
  `totp.ErrTOTPNotEnrolled`. Unlike `memstore`, a late save for a credential
  that was replaced or deleted cannot bring it back.
- **`DeleteTOTP`** deletes the row, which removes both slots at once.
- A counter above `math.MaxInt64` cannot be stored in BIGINT. `ConfirmEnrollment`
  and `SaveTOTP` return an error for it before running any SQL. Real
  time-step counters are near 6×10⁷.

## 5. Passkeys and challenges

### `passkey.Store`

- **`SaveCredential`** inserts. A duplicate `credential_id` or `id` returns a
  new `bunstore.ErrPasskeyCredentialExists`.
- **`GetCredentialsByUserID`** is ordered by `created_at, id`, so the order is
  the same on every call and on both dialects.
- **`GetCredentialByID`** looks up by `credential_id` (the WebAuthn bytes). No
  match returns `passkey.ErrPasskeyNotFound`.
- **`UpdateCredentialAfterLogin`** and **`RenameCredential`** are single
  `UPDATE` statements. `RenameCredential` matches on `id` and `user_id`. Zero
  rows returns `passkey.ErrPasskeyNotFound`.
- **`DeleteCredentialsByUserID`** is one `DELETE ... WHERE user_id = ?`.
- **`DeleteCredential`** runs in one transaction:
  1. `UPDATE passkey_credentials SET user_id = user_id WHERE user_id = ?`.
     This changes nothing, but on Postgres it locks every credential row the
     user has, and on SQLite it takes the database write lock.
  2. Read the user's credential IDs.
  3. In Go: if `id` is not among them, return `passkey.ErrPasskeyNotFound`; if
     it is the only one and `allowLast` is false, return
     `passkey.ErrLastCredential`.
  4. `DELETE ... WHERE id = ? AND user_id = ?`.

  Two concurrent deletes of a user's last two credentials run one after the
  other: the second waits at step 1, then reads one remaining credential and
  refuses.

### `passkey.ChallengeStore`

- **`NewChallengeStore(db, opts...)`** takes `WithChallengeTTL(d)`. The default
  is 5 minutes. A TTL of zero or less panics, because it is a programming
  error and every saved challenge would already be expired.
- **`SaveChallenge`** is an upsert that sets `session_data` and `expires_at =
  now + TTL`. Saving under an existing key replaces it.
- **`ConsumeChallenge`** is one `DELETE ... WHERE challenge_key = ? RETURNING
  session_data, expires_at`, so only one caller can receive a challenge. A
  missing row, or one whose `expires_at` is not after now, returns a new
  `bunstore.ErrChallengeNotFound`. Sulis turns any error here into
  `passkey.ErrChallengeExpired`.
- **`DeleteExpiredChallenges(ctx) (int, error)`** deletes rows whose
  `expires_at` is not after now and returns how many. The application runs it
  on a schedule; until gorfast has `jobs` (design §8, step 6), that is the
  application's own ticker or cron.
- The store gets the current time from an unexported clock, which tests
  replace. Times are compared in Go, not with the database's clock, so both
  dialects behave the same.

## 6. Recovery codes and `DeleteUser`

Every recovery-code transaction starts by writing the user's row in
`recovery_code_sets`. On Postgres this locks the row; on SQLite the first
write takes the database write lock. Either way, writes for one user run one
after another, and SQLite never has to upgrade a read lock to a write lock,
which fails with `SQLITE_BUSY` instead of waiting.

- **`ReplaceCodes`**, in one transaction: upsert the set row (setting
  `replaced_at`), delete the user's codes, then insert the new hashes with
  duplicates removed. The upsert takes the lock even for a user with no
  codes. An empty list keeps the set row and leaves zero codes.
- **`ConsumeCode`**, in one transaction:
  1. `UPDATE recovery_code_sets SET user_id = user_id WHERE user_id = ?`. Zero
     rows means the user has no codes: return 0 and
     `recovery.ErrCodeNotFound`.
  2. `DELETE FROM recovery_codes WHERE user_id = ? AND code_hash = ?`. Zero
     rows returns 0 and `recovery.ErrCodeNotFound`.
  3. `COUNT` the user's remaining codes, and return that number.
- **`DeleteCodes`** deletes the set row, then the codes, in one transaction.
- **`CountCodes`** is a plain count, with no lock.

**`UserStore.DeleteUser`** keeps its transaction and its order (user row
first). After sessions and tokens, it deletes the user's `totp_credentials`
row, `passkey_credentials` rows, `recovery_code_sets` row and
`recovery_codes`, in that order. Challenges are not included: their keys are
opaque, and they expire. An empty ID still deletes nothing.

Lock order is the same in every `bunstore` transaction that takes more than
one lock: the recovery set row comes before the codes in `ReplaceCodes`,
`ConsumeCode`, `DeleteCodes` and `DeleteUser`. `DeleteCredential` locks only
passkey rows. So these transactions wait for each other but cannot deadlock.
A caller's own transaction that spans several stores is outside this
guarantee.

## 7. Testing

**Conformance.** The four `storetest` suites run through `testdb.Each`, so
they run on SQLite always and on Postgres in CI, where the existing CI check
fails the build if a Postgres subtest skips.

**Integration tests**, for behaviour the suites do not check:

- `SaveTOTP` with a stale credential ID, after a `ReplacePending` and
  `ConfirmEnrollment`, returns `totp.ErrTOTPNotEnrolled` and leaves the new
  credential unchanged.
- A counter above `math.MaxInt64` is rejected by `ConfirmEnrollment` and
  `SaveTOTP`.
- A second credential with the same `credential_id` returns
  `ErrPasskeyCredentialExists`.
- An expired challenge returns `ErrChallengeNotFound`, and
  `DeleteExpiredChallenges` removes only expired rows and reports the count.
- `WithChallengeTTL(0)` panics.
- On Postgres, two concurrent `ReplaceCodes` calls for a user with no codes
  leave exactly one of the two sets.
- `DeleteUser` removes the user's second-factor rows and leaves another user's
  rows in place.
- Each store works inside a caller's transaction, and a rollback of that
  transaction undoes its writes, as `UserStore` already shows.

## 8. Known gaps

- **Several TOTP devices per user** with separate secrets need a change to
  `totp.Store` in Sulis. This design keeps one active secret per user.
- **Sulis's reference SQL is not atomic on Postgres under READ COMMITTED.**
  The `EnrollPending` example checks one table with `NOT EXISTS` and writes
  another, and the `DeleteCredential` example counts with a subquery. In both,
  two transactions can pass the check before either commits. To be reported
  to Sulis.
- **`memstore.SaveTOTP` can bring back a replaced credential.** It writes the
  active slot when the IDs differ or no active credential exists. `bunstore`
  refuses. To be reported to Sulis.
- **Expired challenges stay** until the application runs
  `DeleteExpiredChallenges`, or until someone consumes them.
