# auth/bunstore UUID identifiers: design

Date: 2026-10-19. Issue: borfast/gorfast#8.

## 1. Intent

Sulis v0.4.0 changed every ID it generates from `string` to a version 7
`uuid.UUID` from the Go standard library, and v0.5.0 tightened the
`totp.Store.SaveTOTP` contract. `auth/bunstore` still implements the v0.3.0
interfaces with text IDs. This work moves it to Sulis v0.5.0.

The owner wants the database's own UUID handling: a native `uuid` column on
Postgres, so indexing, sorting, filtering and validation come from the
database, and IDs that sort by creation time (version 7 does this, because it
starts with a millisecond timestamp). The first application on gorfast targets
both SQLite and Postgres, and its own tables will reference Sulis user IDs, so
they need the same column type.

## 2. Goals, non-goals, anti-goals

Goals:

- Every bunstore store implements the Sulis v0.5.0 interfaces and passes the
  v0.5.0 `storetest` suite on SQLite and Postgres.
- ID columns are `uuid` on Postgres and 16-byte `BLOB` on SQLite.
- An exported column type lets application tables store user IDs in the same
  form.

Non-goals:

- Converting existing databases with text IDs. Nothing uses gorfast or Sulis
  yet, so the schema is written as if UUIDs had been used from the start.
- The small follow-ups in borfast/gorfast#7.
- Releasing a new gorfast version.

Anti-goals:

- An ID column that silently stores text on one dialect and bytes on the
  other.
- A save path that can bring back a replaced or deleted TOTP credential, or
  accept one code twice.

## 3. Decisions

The owner decided:

- Keep the standard library `uuid.UUID` in Sulis. Rejected: switching Sulis to
  `github.com/google/uuid`. Its `Value()` always sends text, so SQLite `BLOB`
  would still need a conversion.
- SQLite stores IDs as 16-byte `BLOB`. Rejected: 36-character `TEXT`.
- An exported `bunstore.ID` type, accepting that IDs passed as query
  arguments must be wrapped in it (section 4).
- Migrations `0001` and `0002` are edited in place. Existing databases must be
  recreated.

Calls made while writing this design, open to the owner's objection:

- `uuid.Nil()` is written as the all-zero UUID, not NULL. A nullable column
  uses `*ID`, with nil meaning NULL. This matches Sulis's own SQL stores and
  keeps `DeleteUserSessionsExcept(ctx, user, uuid.Nil())` deleting every
  session, as its contract requires.
- `ID` implements only Bun's `schema.QueryAppender` and `sql.Scanner`, not
  `driver.Valuer`. A `Value()` method cannot know the dialect, so it would
  write the wrong form on one of them.

## 4. The `ID` type

In `auth/bunstore/id.go`:

```go
// ID is a sulis ID as a Bun value: a native uuid on Postgres, 16 bytes on SQLite.
type ID uuid.UUID
```

- `func (id ID) AppendQuery(gen schema.QueryGen, b []byte) ([]byte, error)`.
  It chooses by `gen.Dialect().Name()`:
  - `dialect.PG`: the UUID's text, quoted, via `gen.Dialect().AppendString`.
    Postgres converts it to its native form.
  - `dialect.SQLite`: the 16 bytes, via `gen.Dialect().AppendBytes`.
  - Any other dialect: an error naming it.
- `func (id *ID) Scan(src any) error` accepts:
  - `nil`, which gives `uuid.Nil()`;
  - a 16-byte `[]byte`, copied as it is;
  - a 36-character `string` or `[]byte`, parsed as text.
  Anything else is an error.
- `func (id ID) String() string` returns the text form.

Callers convert with `bunstore.ID(u)` and `uuid.UUID(id)`. Bun sees model
fields of type `ID` and `*ID` without help. Query arguments are different:
Bun sends a bare `uuid.UUID` as binary, which Postgres rejects, so every ID
argument is written `Where("user_id = ?", ID(userID))`. On SQLite a missed
wrap does no harm, because the binary form is the correct 16 bytes. On
Postgres it fails the query. The `storetest` suite calls every store method
on Postgres, and CI fails when the Postgres tests are skipped, so a missed
wrap in a store method cannot pass CI.

## 5. Schema

Both dialects, in `0001_auth` and `0002_second_factors`:

| Column | Postgres | SQLite | Null |
|---|---|---|---|
| `users.id` | `UUID` | `BLOB` | primary key |
| `sessions.id` | `UUID` | `BLOB` | primary key |
| `sessions.user_id` | `UUID` | `BLOB` | `NOT NULL` |
| `tokens.id` | `UUID` | `BLOB` | primary key |
| `tokens.user_id` | `UUID` | `BLOB` | nullable, no default; NULL means no user |
| `totp_credentials.user_id` | `UUID` | `BLOB` | primary key |
| `totp_credentials.active_id` | `UUID` | `BLOB` | nullable |
| `totp_credentials.pending_id` | `UUID` | `BLOB` | nullable |
| `passkey_credentials.id` | `UUID` | `BLOB` | primary key |
| `passkey_credentials.user_id` | `UUID` | `BLOB` | `NOT NULL` |
| `recovery_code_sets.user_id` | `UUID` | `BLOB` | primary key |
| `recovery_codes.user_id` | `UUID` | `BLOB` | `NOT NULL`, part of the primary key |

`passkey_challenges.challenge_key` stays `TEXT`: Sulis passes it as a string.

Every SQLite ID column has `CHECK (typeof(col) = 'blob' AND length(col) = 16)`.
A nullable column has `CHECK (col IS NULL OR (typeof(col) = 'blob' AND
length(col) = 16))` instead, because `typeof(NULL)` is `'null'`, so the first
form refuses NULL. The tables are not STRICT, so without the `typeof` part a
16-character text would pass the length check. Every SQLite primary key
column also says `NOT NULL`, because a non-STRICT table otherwise accepts NULL
in a primary key that is not an integer.

The schema keeps having no foreign keys, as now.

## 6. Store changes

- Every model's ID field becomes `ID`, or `*ID` where the column is nullable
  (`tokens.user_id`, `totp_credentials.active_id` and `pending_id`). The
  existing `to...Model` and `from...Model` functions convert to and from the
  Sulis structs. A Sulis `Token.UserID` of `uuid.Nil()` becomes a nil `*ID`,
  and a NULL becomes `uuid.Nil()`.
- Every query argument that is an ID is wrapped in `ID(...)`.
- Store method signatures follow the Sulis v0.5.0 interfaces.
- `TOTPStore.SaveTOTP` follows the v0.5.0 contract:
  - it sets only `active_counter`, no longer `active_secret`;
  - when the conditional `UPDATE` matches nothing, the read that explains why
    runs in the same transaction (both inside `s.db.RunInTx`), and gives `totp.ErrTOTPReplayed` when the
    active ID is still `cred.ID`, and `totp.ErrTOTPNotEnrolled` otherwise;
  - `bunstore.ErrTOTPCounterRegressed` is removed.
- `go.mod` requires `github.com/borfast/sulis v0.5.0`.

## 7. Tests

- `ID`, written first, since these tests confirm how Bun treats the type:
  - the appended form for Postgres and SQLite;
  - the error for another dialect;
  - each accepted and each refused `Scan` input;
  - a round trip of `ID` and of a nil and non-nil `*ID` through a real
    database on both dialects, with `testdb.Each`.
- Schema, table-driven over every ID column in section 5:
  - SQLite refuses a 15-byte and a 17-byte blob and a 16-character text;
  - both dialects accept NULL in each nullable ID column, and SQLite refuses
    it in each `NOT NULL` one;
  - Postgres refuses a malformed UUID text (one column is enough there,
    since the type does it).
- Ordering: IDs generated at least 2 ms apart come back in creation order
  from `ORDER BY id` on both dialects.
- The `storetest` v0.5.0 suite for every store on both dialects, through the
  existing wiring. Its `ConcurrentSaveTOTPOfOneCounterHasOneWinner` subtest
  checks the replay anti-goal: of 8 concurrent saves of one counter, exactly
  one succeeds.
- Existing bunstore tests move to the new types, and the TOTP tests expect
  `totp.ErrTOTPReplayed` where they expected `ErrTOTPCounterRegressed`.

`go test -race ./...` passes with `GORFAST_TEST_POSTGRES_DSN` set, as CI runs
it.

## 8. Documentation

The README's Go module section:

- `go.mod` requires Sulis v0.5.0.
- IDs are `uuid` on Postgres and 16-byte `BLOB` on SQLite.
- Existing databases must be recreated.
- How an application table stores a user ID: an `ID` field in its Bun model,
  `UUID` or `BLOB` with the length check in its own migration, and IDs in
  query arguments wrapped in `bunstore.ID(...)`. One short example.

## 9. Left to the builder

- Error texts, test and helper names, and how the tests are split across files.
- Whether the `ID` tests read the appended SQL through a query's `String()` or
  through a formatter.
- Which existing tests need more than a type change.
