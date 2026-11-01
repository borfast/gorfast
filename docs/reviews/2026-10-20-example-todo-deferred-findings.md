# Example to-do: rulings and deferred findings

From building `examples/todo` (PR #12). Recovered from the session record after
the working ledger was deleted. Not rechecked against the current code, so line
numbers may have moved.

## Rulings made without the owner

1. **`crypt.keys` error text.** It reads `is required (make one with go run
   github.com/borfast/gorfast/crypt/cmd/newkey)`, not the plan's `; ` form,
   because `; ` already separates problems in the message. Cost if wrong: one
   string.
2. **`LockProfile`.** It takes a write lock as the transaction's first statement
   on both dialects, with a no-op `UPDATE`, where 0 rows gives `ErrNotFound`.
   `FOR UPDATE` does nothing on SQLite, and a SQLite transaction that reads first
   can fail as busy. Cost if wrong: one query.
3. **Task 4, Delete.** It is one owner-scoped `DeleteTodo` with no read first,
   though the brief said "read and write". The result is the same, with one
   statement fewer. Cost if wrong: one read added.
4. **Task 5, plan-mandated review items.** Origin-only browsers being refused,
   the taken-email message and the missing sign-up limit went to the Task 7
   security review, not a fix round. Cost if wrong: Task 7 fixes them instead.
5. **Final review, first fix round.** Fixed inside this repository: the skill's
   `.env` leak, session cleanup, Postgres TLS docs, config values in errors,
   opening values with the caller's owner ID, three one-line minors and the Bun
   comment. Cost if wrong: small reverts.
6. **Final review, items sent to the owner.** The sign-up DoS, the account bucket
   order, same-site requests and events before commit, because their best fixes
   were in Sulis. Cost if wrong: a delay.

## Open minor findings

### `core/config`

- `config.go:129`: `Err()` hands out the collector's own slice. `slices.Clone`
  would stop a later `Invalid` from sharing it.
- `config.go:93`: an `Error` with no problems formats as `config: `. `Err` cannot
  reach this.
- `config_test.go`: the `Load` tests assume no `TODO_*` variables are set in the
  environment.
- `skills/loading-configuration/SKILL.md:3,45,66,84`: has dashes from before this
  branch.

### `data`

- `datatest`: nesting is checked only on one runner instance, not across
  `BunRunner[A]` and `BunRunner[B]`.
- The conformance suite has no concurrent independent `Run` subtest, so a
  per-instance busy flag would pass.
- On Postgres, a runner that leaves a panicked transaction open still passes
  `RollsBackOnPanic`.
- `datatest.go:34-36`: the wrapper closure adds nothing.

### `examples/todo`: storage

- `storage_test.go:120-136`: the `ListTodos` order test cannot catch a wrong
  `ORDER BY`. A later `CreatedAt` and a later v7 ID coincide, and there is no ID
  tie-break case.
- The binding strings (`todos`, `text`, `profiles`, `name`) are not pinned by a
  test that seals with `crypt.Bind` directly.
- Postgres server logs record ciphertext in a failing statement's text, because
  Bun inlines arguments.

### `examples/todo`: service

- `service_test.go:69-77`: the test binder copies `storage.Binder`.
- `service_test.go:148`: the table name is concatenated into SQL. `bun.Ident`
  would be the form to copy.
- `service.go:28-29`: error messages repeat 100 and 500 instead of using the
  constants.
- `service_test.go:210`: the sign-in `Expires` is not compared with the session's
  expiry.
- `service_test.go:461-478`: the `ByText` tie-break is untested, and `SortFunc` is
  not stable.
- `service_test.go:505`: `Edit` above 500 runes is untested.
- `service.go:242-246`: invalid UTF-8 is accepted in names and texts.

### `examples/todo`: web

- `web_test.go:366`: tests 3 of 5 wrong-credential errors. Disabled, locked and
  unverified are untested.
- `web_test.go:638`: headers are not checked on a 303 or a 500.
- Nothing asserts that a same-origin rejection reaches the event sink. The
  package-level `RequireSameOrigin` would pass.
- `handlers.go:49` and `signup.html:10`: the minimum password length of 12 is
  hard-coded.
- `handlers.go:152`: the edit page lists and decrypts up to 500 to-dos to show one.
- `web.go:54`: `New` issues a throwaway CSRF token to learn the cookie name.
- `handlers.go:186-208`: the local `done` next to the method `h.done` reads
  confusingly.
- Sulis's 403 page is a bare "Forbidden" with no link back, which a restored tab
  shows after the CSRF cookie expires.
- `hashSlots` is package-level, so the web tests must not run in parallel, and no
  comment says so.
- `/favicon.ico` returns 404, which logs a console error on first load.

### `examples/todo`: main

- `main.go:37-38`: a second interrupt during the 10-second shutdown is ignored,
  because `stop` runs only after `run` returns.
- `main.go:116-120`: `srv.Close` is not called when `Shutdown` times out, so
  handlers may run while `db.Close` runs.
- `main.go:69-71`: a duplicate key passes `config.Load` and then fails without
  naming `TODO_CRYPT__KEYS`.
- `main.go:134`: a SQLite URL that already holds `?` gives a broken DSN.
- `main_test.go:121`: `http.Get` with the default client has no timeout.
- `run` in `main.go`: no test covers the wiring of `WithLimiter`, `events.NewSink`
  and `events.Hold`. Deleting `WithLimiter` brings back blocking by one IP with
  the tests still passing.
- `CleanExpired` removes only sessions past their absolute expiry. Idle-expired
  rows wait until then.

## Deferred, then fixed

- The commit-failure test did not assert that the insert succeeded (16972d2).
- `open` used the row's own owner, not the caller's (a3c794f).
- The `ProfileStore` interface did not say `LockProfile` must be the first
  statement (99151b5).
- A rate-limited sign-up logged `transaction=rolled_back` (2fd3caa).
