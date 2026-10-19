# Example to-do application Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A signed-in, encrypted to-do list in `examples/todo`, and the `core/config` and `data` packages extracted from what it needs.

**Architecture:** `core/config` loads layered configuration into the application's own struct. `data` defines a transaction runner (`Runner[R]`, no Bun types) with a conformance suite and a Bun implementation. The application has a domain and service package (`internal/todo`), a Bun storage package that seals values with `crypt` (`internal/storage`), an HTTP package on `net/http` and `html/template` (`internal/web`), and `main`, which wires them.

**Tech Stack:** Go 1.27, Bun v1.2.18 (Postgres and modernc SQLite), Sulis v0.6.0, gorfast `crypt` and `auth/bunstore`, Koanf v2.

**Spec:** `docs/superpowers/specs/2026-10-19-example-todo-design.md`

## Global Constraints

- New dependencies, and only these, with `go get` from the Go module proxy once the owner has approved this plan: `github.com/borfast/sulis@v0.6.0` (Task 3), and `github.com/knadh/koanf/v2`, `github.com/knadh/koanf/providers/confmap`, `github.com/knadh/koanf/providers/env/v2`, `github.com/knadh/koanf/providers/file`, `github.com/knadh/koanf/parsers/dotenv` (Task 1). If a `go get` fails, stop and report; do not try another way.
- `go 1.27.0` and `toolchain go1.27.2` stay as they are. `go mod tidy` leaves no diff after each task.
- Bun is imported only by `data`, `auth/bunstore`, `examples/todo/internal/storage`, `examples/todo/main.go` and tests. `internal/todo` and `internal/web` never import Bun.
- `internal/todo` and `internal/web` use `uuid.UUID`; `bunstore.ID` appears only in `internal/storage`'s Bun models and query arguments.
- Limits: a name is at most 100 characters and a to-do at most 500, counted in runes after trimming spaces; at most 500 to-dos per user.
- Configuration prefix `TODO_`. Defaults: `TODO_SERVER__ADDR=127.0.0.1:8080`, `TODO_DATABASE__URL=todo.db`, `TODO_LOG__LEVEL=info`. `TODO_CRYPT__KEYS` is required, comma separated, current key first, with no default.
- Logs never contain passwords, session or CSRF tokens, key specs, ciphertext, decrypted values, or query strings.
- Postgres tests: `docker compose up -d postgres`, then `GORFAST_TEST_POSTGRES_DSN='postgres://gorfast:gorfast@localhost:5433/gorfast_test?sslmode=disable'`. A skipped Postgres subtest counts as a failure. Every storage, service and HTTP test runs under `testdb.Each`.
- Run `git commit` (SSH signing), `go get` and Postgres tests outside the sandbox.
- Before editing an existing function, run `node .gitnexus/run.cjs impact "<symbol>" --direction upstream --repo .`. Before each commit, run `node .gitnexus/run.cjs detect-changes --scope all --repo .`.
- Code comments at most 2 lines; commit bodies at most 5 lines; no em-dashes, and no en-dash or spaced hyphen in their place. No `Co-Authored-By` or `Claude-Session` trailers.
- The full gate is CI's: `go mod tidy` with no diff, `go vet ./...`, `go test -race -count=1 ./...` with Postgres, `govulncheck ./...`.

## Review Focus

- Concurrent `Add` calls for a user who has 495 to-dos must not take the count above 500. Pinned in Task 4.
- Names and to-dos in multibyte text: 100 `é` is a valid name, 101 is not; whitespace alone is empty. Pinned in Task 4.
- Malformed to-do IDs in URLs (`not-a-uuid`, the nil UUID, another user's ID) give 404, never 500. Pinned in Task 5.
- Two tabs: a form rendered in one tab still submits after a second tab rendered another form. Pinned in Task 5.
- A session revoked elsewhere: the next page load redirects to sign-in instead of failing. Pinned in Task 5.

---

### Task 1: `core/config`

**Files:**
- Create: `core/config/config.go`, `core/config/config_test.go`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Consumes: nothing.
- Produces, in package `config` (`github.com/borfast/gorfast/core/config`):
  - `func Load(target any, prefix string, defaults map[string]any, envFile string) error`: defaults, then `envFile` if it exists (`""` skips it), then environment variables starting with `prefix`, unmarshalled into `target`, a pointer to a struct with `koanf` tags.
  - `func TransformKey(prefix, name string) string`: `TODO_DATABASE__MAX_CONNS` with prefix `TODO_` gives `database.max_conns`.
  - `func Variable(prefix, key string) string`: the inverse, `database.max_conns` gives `TODO_DATABASE__MAX_CONNS`.
  - `type Problem struct{ Key, Variable, Reason string }`, `type Error struct{ Problems []Problem }`, `func (e *Error) Error() string`.
  - `type Problems struct` (unexported fields), `func NewProblems(prefix string) *Problems`, `func (p *Problems) Require(key, value string)`, `func (p *Problems) Invalid(key, reason string)`, `func (p *Problems) Err() error`.

- [ ] **Step 1: Add the Koanf dependencies** with the five `go get` lines in Global Constraints.

- [ ] **Step 2: Write the failing tests** in `config_test.go` (package `config_test`), with a struct `testConfig{ Server struct{ Addr string `koanf:"addr"`; ReadTimeout time.Duration `koanf:"read_timeout"` } `koanf:"server"`; Name string `koanf:"name"` }`:
  - `TestTransformKey`: `("TODO_", "TODO_SERVER__READ_TIMEOUT")` gives `server.read_timeout`; `("TODO_", "TODO_NAME")` gives `name`. `TestVariable` checks the inverse for both.
  - `TestLoadLayers`: defaults `server.addr=a`, `server.read_timeout=1s`, `name=d`; a temp `.env` with `TODO_SERVER__ADDR=b` and `TODO_NAME=e`; `t.Setenv("TODO_NAME", "f")`. Result: `Addr=b`, `ReadTimeout=time.Second`, `Name=f`.
  - `TestLoadParsesDurations`: `t.Setenv("TODO_SERVER__READ_TIMEOUT", "30s")` gives `30*time.Second`.
  - `TestLoadWithoutEnvFile`: a path that does not exist, and `""`, both load the defaults with no error.
  - `TestLoadRefusesUnreadableEnvFile`: an `envFile` that is a directory returns an error.
  - `TestLoadIgnoresOtherPrefixes`: `t.Setenv("OTHER_NAME", "x")` leaves `Name` at its default.
  - `TestProblemsReportsEveryProblem`: `Require("crypt.keys", "")`, `Require("server.addr", "set")`, `Invalid("log.level", "must be debug, info, warn or error")`. `Err()` is a `*config.Error` (`errors.As`) with exactly two problems, and its message is `config: TODO_CRYPT__KEYS is required; TODO_LOG__LEVEL must be debug, info, warn or error`.
  - `TestProblemsNone`: `NewProblems("TODO_").Err()` is `nil`.

- [ ] **Step 3: Run them to verify they fail**

Run: `go test -count=1 ./core/config/`
Expected: build failure, `undefined: config.Load`.

- [ ] **Step 4: Implement `config.go`**, following `skills/loading-configuration/references/implementation.md`: `confmap`, then `file` with `dotenv.ParserEnv`, then `env/v2` with `Prefix` and a `TransformFunc` calling `TransformKey`. A missing env file (`fs.ErrNotExist`) is not an error. `Err` returns `nil` when nothing was recorded.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test -race -count=1 ./core/config/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add core/config go.mod go.sum
git commit -m "Add core/config, layered configuration from the loading-configuration skill"
```

### Task 2: The `data` transaction runner

**Files:**
- Create: `data/runner.go`, `data/bun.go`, `data/datatest/datatest.go`, `data/bun_test.go`
- Modify: `README.md` (one paragraph on `data`)

**Interfaces:**
- Consumes: `internal/testdb`.
- Produces, in package `data`:
  - `type Runner[R any] interface { Run(ctx context.Context, fn func(ctx context.Context, repos R) (outcome, err error)) error }`. Its doc comment states the contract: `Run` owns a new transaction; when `err` is non-nil it rolls back and returns `err`; otherwise it commits and returns the commit error if there is one, else `outcome`; a panic rolls back and panics again; `Run` called with a context from inside another `Run` returns `ErrNested` without calling `fn`.
  - `var ErrNested = errors.New("data: Run inside another Run's transaction")`.
  - `type BunRunner[R any] struct` (unexported fields), `func NewBunRunner[R any](db *bun.DB, bind func(tx bun.IDB) (R, error)) *BunRunner[R]`, `func (r *BunRunner[R]) Run(...)` as above. A `bind` error is returned without calling `fn`.
- Produces, in package `datatest`:
  - `type Probe interface { Add(ctx context.Context, name string) error }`.
  - `func RunRunner(t *testing.T, newRunner func(t *testing.T) (data.Runner[Probe], func(ctx context.Context) ([]string, error)))`: the second result lists every committed name.

- [ ] **Step 1: Write the conformance suite** in `datatest.go`. Each subtest gets a fresh runner; `errOutcome` and `errFail` are distinct errors:
  - `CommitsOnSuccess`: `fn` adds `a`, returns `(nil, nil)`. `Run` returns `nil`; names are `[a]`.
  - `CommitsThenReturnsOutcome`: adds `a`, returns `(errOutcome, nil)`. `errors.Is(err, errOutcome)`; names are `[a]`.
  - `RollsBackOnFailure`: adds `a`, returns `(nil, errFail)`. `errors.Is(err, errFail)`; names are empty.
  - `FailureWinsOverOutcome`: adds `a`, returns `(errOutcome, errFail)`. `errors.Is(err, errFail)` and not `errOutcome`; names are empty.
  - `RollsBackOnPanic`: adds `a`, then panics with `"boom"`. The recovered value is `"boom"`; names are empty.
  - `RefusesNesting`: inside `fn`, a second `Run` with `fn`'s context returns `ErrNested` and its function is never called; the outer `fn` then returns `(nil, nil)` and commits.

- [ ] **Step 2: Write the Bun tests** in `bun_test.go` (package `data_test`), each under `testdb.Each`:
  - `TestBunRunner`: `datatest.RunRunner` with a probe table `data_probe (name TEXT NOT NULL)`, created if missing and emptied by the factory; the probe inserts through the bound `bun.IDB`.
  - `TestBunRunnerSavepointRollsBackWithOuter`: `NewBunRunner[bun.IDB]` binding the transaction itself; `fn` inserts inside `repos.RunInTx` (a savepoint), which returns `nil`, then `fn` returns `(nil, errFail)`. The table is empty.
  - `TestBunRunnerCommitFailureWinsOverOutcome`: a commit that fails. On Postgres, a table with `UNIQUE (name) DEFERRABLE INITIALLY DEFERRED` and two equal inserts. On SQLite, `db.SetMaxOpenConns(1)`, `PRAGMA foreign_keys = ON`, and a child row whose `REFERENCES ... DEFERRABLE INITIALLY DEFERRED` parent does not exist. `fn` returns `(errOutcome, nil)`; `Run`'s error is non-nil and not `errOutcome`, and nothing was written.
  - `TestBunRunnerBindError`: `bind` returns `errBind`; `Run` returns it and `fn` is not called.

- [ ] **Step 3: Run them to verify they fail**

Run: `go test -count=1 ./data/...`
Expected: build failure, `undefined: data.Runner`.

- [ ] **Step 4: Implement `runner.go` and `bun.go`.** `Run` marks its context with an unexported key and checks for it first. The Bun version uses `db.RunInTx`, keeping `outcome` outside the callback.

- [ ] **Step 5: Run the tests on both dialects**

Run: `GORFAST_TEST_POSTGRES_DSN=... go test -race -count=1 -v ./data/...`
Expected: PASS, no `--- SKIP` for `postgres`.

- [ ] **Step 6: Add the README paragraph**: `data.Runner` runs a function in a transaction it owns; return `(outcome, nil)` to commit writes that must survive an expected failure, and `(nil, err)` to roll back. Then commit:

```bash
git add data README.md
git commit -m "Add data.Runner, a transaction runner with a conformance suite and a Bun implementation"
```

### Task 3: Domain types and encrypted storage

**Files:**
- Create: `examples/todo/internal/todo/todo.go`
- Create: `examples/todo/internal/storage/migrate.go`, `migrations/0001_todo.up.sql`, `migrations/0001_todo.sqlite.up.sql`, `profiles.go`, `todos.go`, `repos.go`, `storage_test.go`
- Modify: `go.mod`, `go.sum` (Sulis v0.6.0)

**Interfaces:**
- Consumes: `data.NewBunRunner` (Task 2); `bunstore.ID`, `bunstore.NewUserStore`, `NewSessionStore`, `NewTokenStore`, `migrations.Apply`; `crypt.Encryptor`, `crypt.Bind`, `crypt.Sealed`; `(*sulis.Sulis).WithStores`.
- Produces, in package `todo` (`examples/todo/internal/todo`), with `ctx` a `context.Context` throughout:
  - `type Todo struct { ID, OwnerID uuid.UUID; Text string; Done bool; CreatedAt, UpdatedAt time.Time }`, `type Profile struct { UserID uuid.UUID; Name string }`.
  - `var ErrNotFound = errors.New("todo: not found")`.
  - `type ProfileStore interface { CreateProfile(ctx, Profile) error; GetProfile(ctx, userID uuid.UUID) (Profile, error); LockProfile(ctx, userID uuid.UUID) error }`. Get and Lock return `ErrNotFound` for a missing profile.
  - `type TodoStore interface { CreateTodo(ctx, Todo) error; GetTodo(ctx, ownerID, id uuid.UUID) (Todo, error); UpdateTodo(ctx, Todo) error; DeleteTodo(ctx, ownerID, id uuid.UUID) error; ListTodos(ctx, ownerID uuid.UUID) ([]Todo, error); CountTodos(ctx, ownerID uuid.UUID) (int, error) }`. Get, Update and Delete match on both `id` and `owner_id`, and return `ErrNotFound` when no row matches. `UpdateTodo` writes `text`, `done` and `updated_at`. `ListTodos` orders by `created_at`, then `id`.
  - `type Repos struct { Auth *sulis.Sulis; Profiles ProfileStore; Todos TodoStore }`.
- Produces, in package `storage`:
  - `func Apply(ctx context.Context, db bun.IDB) error`: `migrations.Apply`, then this package's files for the dialect, in one transaction, the way `auth/bunstore/migrations` selects them.
  - `func NewProfileStore(db bun.IDB, enc crypt.Encryptor) *ProfileStore`, `func NewTodoStore(db bun.IDB, enc crypt.Encryptor) *TodoStore`.
  - `func Binder(base *sulis.Sulis, enc crypt.Encryptor) func(tx bun.IDB) (todo.Repos, error)`: bunstore stores and both app stores over `tx`, and `base.WithStores(users, sessions, tokens, sulis.NoSecondFactors{})`.

Tables (`IF NOT EXISTS`; IDs per the README: `UUID` on Postgres, 16-byte `BLOB` with its `CHECK` on SQLite; sealed columns `BYTEA` on Postgres and `BLOB NOT NULL CHECK (typeof(col) = 'blob')` on SQLite; times `TIMESTAMPTZ` or `TIMESTAMP`; no foreign keys, like bunstore):
- `profiles (user_id PRIMARY KEY, name NOT NULL)`
- `todos (id PRIMARY KEY, owner_id NOT NULL, text NOT NULL, done BOOLEAN NOT NULL DEFAULT FALSE, created_at NOT NULL, updated_at NOT NULL)`, index `todos_owner_created (owner_id, created_at, id)`.

Bindings, exactly: `crypt.Bind("profiles", "name", userID.String())` and `crypt.Bind("todos", "text", id.String(), ownerID.String())`.

- [ ] **Step 1: Move to Sulis v0.6.0** with `go get github.com/borfast/sulis@v0.6.0`, then run `go test -count=1 ./auth/...` with Postgres. Expected: PASS.

- [ ] **Step 2: Write the failing tests** in `storage_test.go` (package `storage_test`), each under `testdb.Each` with `storage.Apply` and a keyring from `crypt.GenerateKey`:
  - `TestApplyTwice`: a second `Apply` returns `nil`.
  - `TestTodoStoreRoundTrip`: create two to-dos; `GetTodo` returns each unchanged (times compared with `Equal` after `Truncate(time.Microsecond)`); `ListTodos` returns them in creation order; `UpdateTodo` changes text and done; `DeleteTodo` removes one; `CountTodos` is 1.
  - `TestTodoStoreScopesByOwner`: with alice's to-do, bob's `GetTodo`, `UpdateTodo` and `DeleteTodo` return `ErrNotFound`, bob's `ListTodos` is empty, and alice's row is unchanged.
  - `TestProfileStore`: create and get; `GetProfile` and `LockProfile` of an unknown user return `ErrNotFound`; a second `CreateProfile` for the same user returns an error.
  - `TestStoredValuesAreNotPlaintext`: the raw `text` and `name` columns do not contain the plaintext bytes.
  - `TestSealedValueMovedElsewhereFailsToOpen`: copying alice's `text` column onto bob's to-do with raw SQL makes bob's `GetTodo` fail with an error that is not `ErrNotFound`; changing a row's `owner_id` to bob's with raw SQL does the same for bob; copying a profile's `name` onto another profile does the same for `GetProfile`.
  - `TestBinderRollsBackEverything`: a `data.NewBunRunner(db, storage.Binder(base, enc))` whose `fn` calls `repos.Auth.Register`, then `repos.Profiles.CreateProfile`, then returns `(nil, errFail)`. Afterwards there is no user (`bunstore.NewUserStore(db).GetUserByEmail` gives `sulis.ErrUserNotFound`), no row in `sessions`, and no row in `profiles`.

- [ ] **Step 3: Run them to verify they fail**

Run: `go test -count=1 ./examples/todo/...`
Expected: build failure on the missing packages.

- [ ] **Step 4: Implement `todo.go` and the storage files.** Bun models use `bunstore.ID` with `type:uuid` and `crypt.Sealed`. Sealing and opening happen in the model conversions; an open failure is wrapped and returned.

- [ ] **Step 5: Run the tests on both dialects**

Run: `GORFAST_TEST_POSTGRES_DSN=... go test -race -count=1 -v ./examples/todo/...`
Expected: PASS, no `--- SKIP` for `postgres`.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum examples/todo/internal
git commit -m "Add the example's encrypted profile and to-do storage"
```

### Task 4: The service

**Files:**
- Create: `examples/todo/internal/todo/service.go`, `examples/todo/internal/todo/service_test.go`

**Interfaces:**
- Consumes: `data.Runner[todo.Repos]`; the Task 3 stores and `storage.Binder` (tests only).
- Produces, in package `todo`:
  - `const MaxNameLength = 100`, `MaxTextLength = 500`, `MaxTodos = 500`.
  - `var ErrNameInvalid`, `ErrTextInvalid`, `ErrTooMany` (distinct sentinels).
  - `type Order string`, `const ByCreated Order = "created"`, `ByText Order = "text"`.
  - `type SignedIn struct { Token string; Expires time.Time }`.
  - `func NewService(run data.Runner[Repos]) *Service`, and on `*Service`:
    - `SignUp(ctx, email, password, name string, ri sulis.RequestInfo) (SignedIn, error)`: checks the name, then in one `Run`: `Register` and `CreateProfile`, with any error rolling back.
    - `SignIn(ctx, email, password string, ri sulis.RequestInfo) (SignedIn, error)`: `Login` in one `Run`. `ErrInvalidCredentials`, `ErrInvalidEmail`, `ErrRateLimited`, `ErrAccountDisabled`, `ErrAccountLocked` and `ErrEmailNotVerified` are outcomes, returned as `(err, nil)` so Sulis's writes commit; any other error rolls back; `NeedsSecondFactor` is a failure.
    - `SignOut(ctx, userID, sessionID uuid.UUID) error`: `RevokeSession`.
    - `Name(ctx, userID uuid.UUID) (string, error)`.
    - `List(ctx, userID uuid.UUID, filter string, order Order) ([]Todo, error)`: keeps to-dos whose text contains the trimmed filter, ignoring case. `ByText` sorts by lowercased text, then creation time, then ID. Anything else, `ByCreated` included, keeps the store's order.
    - `Add(ctx, userID uuid.UUID, text string) (Todo, error)`: checks the text, then in one `Run`: `LockProfile`, `CountTodos`, and `ErrTooMany` at `MaxTodos`, then `CreateTodo` with a new `uuid.NewV7()` and UTC times.
    - `Edit(ctx, userID, id uuid.UUID, text string) error`, `SetDone(ctx, userID, id uuid.UUID, done bool) error`, `Delete(ctx, userID, id uuid.UUID) error`: read and write in one `Run`; `ErrNotFound` passes through.
  - Names and texts are trimmed of surrounding spaces, then must be 1 to the maximum runes, otherwise `ErrNameInvalid` or `ErrTextInvalid`.

- [ ] **Step 1: Write the failing tests** in `service_test.go` (package `todo_test`), each under `testdb.Each`, with a helper building the base Sulis (`WithRequireVerifiedEmail(false)`, `WithoutRateLimiting()`, and for speed `WithArgon2Params(sulis.Argon2Params{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})`), `storage.Binder`, `data.NewBunRunner` and `todo.NewService`:
  - `TestSignUpThenSignIn`: `SignUp` returns a token that `base.ValidateSession` accepts; `Name` returns the name; `SignIn` with the same password returns another valid token.
  - `TestSignUpFailureAfterRegisterLeavesNothing`: a binder whose `Profiles.CreateProfile` fails. `SignUp` returns an error, and afterwards there is no user, session or profile.
  - `TestSignUpChecksTheName`: `""`, `"   "` and 101 `é` give `ErrNameInvalid`; 100 `é` succeeds.
  - `TestSignInKeepsTheFailedAttemptCount`: this test alone builds the base Sulis with `WithFailureLockout(5, time.Minute, time.Hour)`, with a comment that the application leaves lockout off. A wrong password gives `sulis.ErrInvalidCredentials`, and the stored user's `FailedLoginAttempts` is 1.
  - `TestSignInStorageFailureIsNotAWrongPassword`: lockout on as above, and a binder whose user store's `UpdateUser` fails. A wrong password gives an error that is not `ErrInvalidCredentials`.
  - `TestSignOut`: after `SignOut`, `base.ValidateSession` rejects the token.
  - `TestTodosBelongToTheirOwner`: bob's `Edit`, `SetDone` and `Delete` on alice's to-do give `ErrNotFound`, bob's `List` is empty, and alice's to-do is unchanged.
  - `TestAddChecksTheText`: 500 `é` succeeds; 501 `é`, `""` and `"  "` give `ErrTextInvalid`.
  - `TestAddStopsAtTheLimit`: after 500 `Add` calls, the next gives `ErrTooMany`.
  - `TestConcurrentAddsStayWithinTheLimit`: with 495 to-dos, 20 goroutines each `Add` once. Every call succeeds or returns an error; `CountTodos` is at most 500.
  - `TestListSortsAndFilters`: texts added in order `banana`, `Apple`, `cherry pie`, `apple tart`. `ByCreated` gives that order; `ByText` gives `Apple`, `apple tart`, `banana`, `cherry pie`; filter `" APPLE "` with `ByCreated` gives `Apple`, `apple tart`; an unknown order behaves as `ByCreated`.
  - `TestEditAndSetDone`: both change what `List` returns, and `UpdatedAt` moves forward.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -count=1 ./examples/todo/internal/todo/`
Expected: build failure, `undefined: todo.NewService`.

- [ ] **Step 3: Implement `service.go`.**

- [ ] **Step 4: Run the tests on both dialects**

Run: `GORFAST_TEST_POSTGRES_DSN=... go test -race -count=1 -v ./examples/todo/...`
Expected: PASS, no `--- SKIP` for `postgres`.

- [ ] **Step 5: Commit**

```bash
git add examples/todo/internal/todo
git commit -m "Add the example's service, with every write in a transaction"
```

### Task 5: The web interface

**Files:**
- Create: `examples/todo/internal/web/web.go`, `handlers.go`, `middleware.go`, `templates/*.html` (`layout`, `signup`, `signin`, `list`, `edit`, `error`), `static/style.css`, `web_test.go`

**Interfaces:**
- Consumes: `*todo.Service` (Task 4); `*sulis.Sulis` for `ValidateSession`, `SessionCookie`, `ClearSessionCookie`, `IssueCSRFToken`, `RequireSameOrigin`, `RequireCSRFToken`.
- Produces, in package `web`:
  - `const SessionCookieName = "__Host-session"`, which `main` passes to `sulis.WithCookieName`.
  - `func New(svc *todo.Service, auth *sulis.Sulis, log *slog.Logger) (http.Handler, error)`.

Routes: `GET /` (list; `q` and `sort` query parameters), `GET /signup`, `POST /signup`, `GET /signin`, `POST /signin`, `POST /signout`, `POST /todos`, `GET /todos/{id}/edit`, `POST /todos/{id}`, `POST /todos/{id}/done` (field `done`, `true` or `false`), `POST /todos/{id}/delete`, `GET /static/style.css`. Signed-in routes are `/`, `/signout` and `/todos/...`.

Behaviour:
- Every response: `Content-Security-Policy: default-src 'none'; style-src 'self'; img-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`. Every HTML response: `Cache-Control: no-store`.
- Every request body is limited to 64 KiB with `http.MaxBytesHandler`.
- Every POST goes through `auth.RequireSameOrigin(nil)` and `auth.RequireCSRFToken`. Forms carry the token in the `csrf_token` field. A form page reuses the CSRF cookie's value if the request has one, and otherwise issues a new token and sets its cookie.
- A signed-in route with no session cookie, or whose `ValidateSession` error is `sulis.ErrSessionNotFound`, `ErrSessionExpired`, `ErrNotAuthenticated`, `ErrAccountDisabled` or `ErrUserNotFound`, gives `303` to `/signin`. Any other error is a 500.
- Sign-up and sign-in set `auth.SessionCookie(token, expires)` only after the service returns success, then give `303` to `/`. Sign-out revokes the session, sets `auth.ClearSessionCookie()`, and gives `303` to `/signin`.
- Messages: every sign-in outcome except `ErrRateLimited` shows `Wrong email or password.` with status 422; `ErrRateLimited` shows `Too many attempts. Try again later.` with status 429. Sign-up shows a specific message for a taken email, a bad email, a short, long or breached password, and a bad name, with status 422. `ErrTextInvalid` and `ErrTooMany` re-render the list with a message and status 422.
- An ID that does not parse, or `ErrNotFound`, gives the `error` page with status 404. Any other error is logged with method, path (no query) and error, and gives the `error` page saying `Something went wrong.` with status 500.
- `RequestInfo.IP` comes from `r.RemoteAddr`, never a header.
- No inline styles or scripts; no `template.HTML`.

- [ ] **Step 1: Write the failing tests** in `web_test.go` (package `web_test`), each under `testdb.Each`, with the Task 4 service wiring, an `httptest.NewTLSServer`, a client with a cookie jar, a logger writing to a buffer, and a `browser` helper that GETs a form page, reads `csrf_token` from it, and POSTs with `Origin` set to the server's URL:
  - `TestSignUpListAndSignOut`: sign up as `Alice`; `/` shows `Alice` and lets her add, edit, complete and delete a to-do; sign out; `/` then gives `303` to `/signin`.
  - `TestSignInWrongPassword`: status 422 and `Wrong email or password.`; no session cookie set.
  - `TestSignedOutGoesToSignIn`: `GET /` with no cookie gives `303` to `/signin`.
  - `TestRevokedSessionGoesToSignIn`: two clients sign in as alice; the test calls `svc.SignOut` with the second client's session, found with `base.ValidateSession` on its cookie; the second client's next `GET /` gives `303` to `/signin`, and the first client's still gives 200.
  - `TestPostWithoutCSRFTokenIsRefused` and `TestCrossOriginPostIsRefused` (`Origin: https://evil.example`): status 403, and no to-do is created.
  - `TestFormFromAnEarlierTabStillWorks`: render the add form twice, then submit with the first page's token; the to-do is created.
  - `TestOtherUsersTodoIsNotFound`: bob's `GET /todos/{alice's id}/edit` and the three POSTs give 404, and alice's to-do is unchanged.
  - `TestMalformedTodoIDIsNotFound`: `/todos/not-a-uuid/edit` and `/todos/00000000-0000-0000-0000-000000000000/edit` give 404.
  - `TestListQueryParameters`: a 10,000-character `q` gives 200 with no to-dos listed; `sort=nonsense` gives creation order.
  - `TestSecurityHeaders`: the exact headers above on `/signin`, on a signed-in `/`, and on a 404; `Cache-Control: no-store` on both HTML pages.
  - `TestOutputIsEscaped`: a to-do `<script>alert(1)</script>` appears as `&lt;script&gt;alert(1)&lt;/script&gt;`, and the raw `<script>` does not appear.
  - `TestOversizedBodyIsRefused`: a 65 KiB `POST /todos` gives a 4xx status, and no to-do is created.
  - `TestInternalErrorIsGeneric`: after closing the database, `GET /` signed in gives 500 with `Something went wrong.`, and the body does not contain the error's text.
  - `TestLogsHoldNoSecrets`: after the sign-up, add and sign-in flows, the log buffer contains none of: the password, the name, the to-do text, the session cookie's value, the CSRF token, the key spec, the hex of the stored `text` column, or the `q` value used.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -count=1 ./examples/todo/internal/web/`
Expected: build failure, `undefined: web.New`.

- [ ] **Step 3: Implement the package**, embedding templates and the stylesheet with `embed`.

- [ ] **Step 4: Run the tests on both dialects**

Run: `GORFAST_TEST_POSTGRES_DSN=... go test -race -count=1 -v ./examples/todo/...`
Expected: PASS, no `--- SKIP` for `postgres`.

- [ ] **Step 5: Commit**

```bash
git add examples/todo/internal/web
git commit -m "Add the example's web interface"
```

### Task 6: Configuration, `main`, and documentation

**Files:**
- Create: `examples/todo/internal/config/config.go`, `examples/todo/main.go`, `examples/todo/main_test.go`, `examples/todo/.env.dist`, `examples/todo/README.md`
- Modify: `.gitignore` (add `examples/todo/todo.db*`), `README.md` (one line pointing to the example)

**Interfaces:**
- Consumes: `config.Load`, `config.NewProblems` (Task 1); everything above.
- Produces:
  - In package `config` (`examples/todo/internal/config`): `type Config struct { Server struct{ Addr string }; Database struct{ URL string }; Crypt struct{ Keys string }; Log struct{ Level string } }` with `koanf` tags `server.addr`, `database.url`, `crypt.keys`, `log.level`, and `func Load(envFile string) (Config, error)`, which checks:
    - `crypt.keys` set, with the reason `is required; make a key with go run github.com/borfast/gorfast/crypt/cmd/newkey`;
    - every comma-separated spec parsing with `crypt.ParseKey`, with the reason `holds a key that does not parse`, never the value;
    - `log.level` one of `debug`, `info`, `warn`, `error`.
  - In package `main`: `func run(ctx context.Context, envFile string, logOut io.Writer) error`. It loads the config; opens the database (a `postgres://` or `postgresql://` URL with `pgdriver` and `pgdialect`, anything else as a SQLite file with `?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)` and `SetMaxOpenConns(1)`); runs `storage.Apply`; loads the keyring with `crypt.Load(ctx, crypt.StaticKeys(specs...))`; builds the base Sulis, the runner, the service and `web.New`; serves on `net.Listen("tcp", addr)` with `ReadHeaderTimeout` 5s, `ReadTimeout` 10s, `WriteTimeout` 10s and `IdleTimeout` 60s, and logs the address; and on `ctx.Done()` shuts down with a 10s limit and closes the database. `main` calls `run` with a `signal.NotifyContext` for interrupt and SIGTERM, `".env"` and `os.Stderr`, and exits 1 on error.

The base Sulis, with these comments verbatim:

```go
auth, err := sulis.New(users, sessions, tokens, sulis.NoSecondFactors{},
	sulis.WithCookieName(web.SessionCookieName),
	sulis.WithEventSink(sulis.NewSlogSink(logger)),
	// Sign-in is password only, so nothing verifies an email address.
	// Real applications should require email verification.
	sulis.WithRequireVerifiedEmail(false),
	// Failure lockout stays off: it lets anyone lock a person out of their
	// account. Turn it on only for a clear reason.
)
```

- [ ] **Step 1: Write the failing tests** in `main_test.go` (package `main`), each setting every `TODO_` variable with `t.Setenv`, `TODO_DATABASE__URL` to a temp SQLite file, and an env file path that does not exist:
  - `TestRunRefusesToStartWithoutAKey`: the error names `TODO_CRYPT__KEYS` and `crypt/cmd/newkey`.
  - `TestRunRefusesAMalformedKey`: `TODO_CRYPT__KEYS=xchacha20poly1305:not-base64-secret` gives an error naming `TODO_CRYPT__KEYS` and not containing `not-base64-secret`.
  - `TestRunRefusesAnUnknownLogLevel`: `TODO_LOG__LEVEL=loud`, and no key, gives one error naming both variables.
  - `TestRunServesAndStops`: a generated key and `TODO_SERVER__ADDR=127.0.0.1:0`; `run` with a context cancelled once the log shows the address returns `nil`, and a `GET /signin` to that address before cancelling gives 200.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -count=1 ./examples/todo/`
Expected: build failure, `undefined: run`.

- [ ] **Step 3: Implement `config.go` and `main.go`.**

- [ ] **Step 4: Write `.env.dist` and `README.md`.** `.env.dist` documents every variable, with an empty `TODO_CRYPT__KEYS`. The README says, in this order: what the example shows; how to run it (`cd examples/todo`, `cp .env.dist .env`, the `newkey` command, `go run .`); that it runs on localhost only, because elsewhere it needs TLS and a proxy would make every user share one sign-in rate limit; that real applications should require email verification; that lockout is off on purpose; and that Chrome and Firefox accept its `Secure` cookies on `http://localhost` but other browsers may not.

- [ ] **Step 5: Run the whole gate**

Run: `go mod tidy && git diff --exit-code go.mod go.sum && go vet ./... && GORFAST_TEST_POSTGRES_DSN=... go test -race -count=1 ./...`
Expected: no diff, no vet findings, every package `ok`.

- [ ] **Step 6: Commit**

```bash
git add examples/todo .gitignore README.md
git commit -m "Add the example's configuration, main and README"
```

### Task 7: Browser check and security review

**Files:** only those a finding changes.

- [ ] **Step 1: Run the application** as its README says, from `examples/todo`, with a generated key.

- [ ] **Step 2: Go through every flow in a real browser** (Chromium through the Playwright tools): sign up, see the name, add, edit, complete, filter, sort by text, delete, sign out, sign in with a wrong password, sign in. Check the browser console shows no CSP violations.

- [ ] **Step 3: Dispatch a security review** of the branch against the spec's §7, separately from the final code review, on the most capable model. Fix each confirmed finding with a failing test first, then the whole gate from Task 6, then a commit.
