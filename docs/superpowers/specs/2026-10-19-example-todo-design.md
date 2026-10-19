# Example to-do application: design

Date: 2026-10-19. Roadmap step 2 in `2026-09-27-gorfast-design.md` §8.

## 1. Intent

The example application is gorfast's first real user. It is not only
documentation: §8 says each package is extracted from what a real consumer
needed, and §10 says the example application decides the transaction runner's
contract before `data` offers a reusable one. Step 2 builds this application
and extracts `core/config` and the `data` transaction runner from it.

It is a to-do list a person signs up for, signs in to and uses in a browser.
It is deliberately small. What matters is that it uses `auth/bunstore`,
`crypt` and transactions together, because problems in how packages fit
together only show when they are wired up.

People copy example code. The owner's main test of done is that the
application has no security holes.

## 2. Goals, non-goals, anti-goals

Goals:

- A person can sign up, sign in, sign out, and add, edit, complete and delete
  their own to-dos, in a browser.
- The user's name and each to-do's text are encrypted with `crypt`.
- The to-do list can be sorted and filtered by a word. Both happen in memory,
  after decryption, because the database cannot read encrypted values.
- Sign-up, sign-in and every write run through a transaction runner that meets
  every rule in §10.
- It runs on SQLite and Postgres, with tests on both.
- `core/config` and the `data` transaction runner exist, extracted from what
  the application needed. The runner ships a conformance suite (doctrine
  rule 3). `core/config` defines no interface, so its tests in §8 are enough.

Non-goals for this step:

- Email verification, password reset, magic links and second factors. Sign-in
  is by password only.
- An encrypted label with an exact-match filter, which needs HMAC blind
  indexes in `crypt`, and key rotation. Both are the next step.
- Editing the name, deleting the account, a JSON API.
- gorfast's `http` and `view` packages. The application uses `net/http` and
  `html/template` directly, so those packages can later be extracted from it.
- Logging, lifecycle and shutdown in `core`. The application writes them
  plainly in `main`; they move into `core` when a second application needs
  them.
- Fixtures, seeding and connection helpers in `data`.
- More than one running instance. Sulis's rate limiter is per process.
- Deployment. The application runs on localhost only, and its README says
  so: anywhere else needs TLS, and behind a proxy every user would share the
  proxy's IP and so its sign-in rate limit.

Anti-goals, which would make this a failure even if it works:

- A security hole a reader would copy.
- Library API designed ahead of need: a runner or config feature that the
  application does not use.
- Bun types in a service contract (§10, doctrine rule 2).

## 3. What the person sees

Plain server-rendered HTML forms, no JavaScript, one small stylesheet.

- **Sign-up:** email, password and name. On success the person is signed in and
  sees their list.
- **Sign-in:** email and password. A wrong email or password gives one message
  that does not say which was wrong.
- **To-do list** (signed in only): a greeting with the decrypted name, a form
  to add a to-do, and the person's to-dos. Each can be marked done or not done,
  edited or deleted. A word filter keeps only to-dos whose text contains the
  word, ignoring case. Sorting is by text or by creation time; creation time is
  the default.
- **Sign-out:** a button, which deletes the session on the server.

A signed-out request for a signed-in page goes to the sign-in page.

## 4. Data

The application keeps its own tables beside the bunstore tables, and its own
migrations in the bunstore style: embedded SQL, one file set per dialect,
applied at startup after `migrations.Apply`.

- `profiles`: `user_id` (the user's ID, primary key), `name` (sealed).
- `todos`: `id`, `owner_id`, `text` (sealed), `done`, `created_at`,
  `updated_at`, with an index on `owner_id`.

ID columns follow the README: `bunstore.ID` in Bun models and query
arguments, `uuid` on Postgres, 16-byte `BLOB` with the README's `CHECK` on
SQLite. Application code outside the models uses `uuid.UUID`. New to-do IDs
are version 7 UUIDs made by the application before sealing, because the
binding needs the ID.

Every sealed value is bound with `crypt.Bind` to its table, column, row ID and
owner, so a ciphertext copied to another row or another user fails to open.

Limits: a name is at most 100 characters, a to-do at most 500 characters, and
a person may have at most 500 to-dos. The last limit keeps the in-memory sort
and filter bounded.

## 5. Transactions

The application owns a runner, as §10 describes: the service calls it with a
function, and the runner supplies the stores bound to one transaction. Only a
Bun adapter, wired in `main`, sees `bun.Tx`.

Sign-up is the first operation that needs it. `Register` creates the user and
a session, and the application then creates the profile. All three commit
together or not at all, and the session cookie is set only after the commit.
To-do writes run through the runner too, with the ownership check inside the
same transaction as the write. Sign-in runs through the runner as an operation
that owns its transaction.

The application leaves Sulis's failure lockout off, its default, and says why
in a comment: people copy example code, and lockout lets anyone lock a person
out of their account, so an application should turn it on only for a clear
reason. With lockout off, a failed sign-in writes nothing. The §10 check on
keeping failed-attempt bookkeeping therefore runs in a test that turns lockout
on for that test only.

A composition problem the application exposes: `sulis.New` takes the stores,
so running `Register` inside a transaction needs a Sulis whose stores use that
transaction. `New` computes an Argon2 dummy hash (64 MiB) and, by default,
creates a new in-process rate limiter and password blocklist. A Sulis made per
transaction therefore costs an extra Argon2 hash and loses rate-limiting state
unless the limiter is shared. See §11.

## 6. Configuration

`core/config` provides what §5 of the gorfast design lists: the key transform,
a layered `Load` helper taking a prefix and defaults, and the validation error
type. It follows `skills/loading-configuration`. The application declares its
own `Config` in `examples/todo/internal/config`, which only `main` imports.

Variables, with the prefix `TODO_`:

- `TODO_SERVER__ADDR`, default `127.0.0.1:8080`.
- `TODO_DATABASE__URL`, default a SQLite file `todo.db`. A `postgres://` URL
  selects Postgres.
- `TODO_CRYPT__KEYS`, required: key specs, current key first. There is no
  default key. Without one the application refuses to start and names the
  command that makes one, `go run ./crypt/cmd/newkey`.
- `TODO_LOG__LEVEL`, default `info`.

`.env.dist` documents each one. `.env` is ignored by git.

New dependencies, from the skill: `github.com/knadh/koanf/v2` and its
`confmap`, `env/v2` and `file` providers and `dotenv` parser, added with
`go get` once the owner approves.

## 7. Security requirements

- **Access:** every to-do query filters by the signed-in user. A to-do ID
  belonging to someone else behaves exactly like one that does not exist.
- **Encryption:** bindings as in §4. No default key.
- **Sessions:** Sulis's cookies (HttpOnly, Secure, SameSite=Lax, `__Host-`
  name). A new session at sign-up and sign-in. Sign-out revokes the session
  on the server and clears the cookie.
- **Forged requests:** every POST, including sign-up and sign-in, goes through
  Sulis's `RequireSameOrigin` and `RequireCSRFToken` (the methods, so
  rejections reach the event sink). Forms carry the CSRF token.
- **Requests:** a body size limit on every handler, and server read, write,
  header and idle timeouts. The client IP comes from the connection, never
  from a forwarded header.
- **Pages:** a Content-Security-Policy allowing only same-origin resources and
  forms, `frame-ancestors 'none'`, `X-Content-Type-Options: nosniff`, a strict
  referrer policy, and `Cache-Control: no-store` on signed-in pages. All output
  goes through `html/template`; nothing is marked as trusted HTML.
- **Passwords:** Sulis defaults: Argon2id, 12 characters minimum, the breach
  blocklist, rate limiting.
- **Email verification:** Sulis requires it by default, and with it on every
  sign-in after sign-up fails. Sign-in is password only, so the application
  turns it off with the visible `WithRequireVerifiedEmail(false)`. See §12.
- **Logs and errors:** no passwords, session or CSRF tokens, keys, ciphertext
  or decrypted values in logs. Sulis events go to `slog` through
  `NewSlogSink`. A person never sees an internal error, only a generic page.
- **Review:** the finished branch gets its own security review before merge,
  like the `crypt` work did.

## 8. Tests

All storage and HTTP tests run on SQLite and Postgres with `internal/testdb`.

- Every §10 required check, in the runner's conformance suite and the Bun
  adapter's integration tests. The response stands in for the post-commit
  effect; the runner gains an after-commit hook only if the application
  needs one.
- Each flow in §3 through `httptest`, with cookies and CSRF tokens, as a
  browser sends them.
- A second user cannot see, edit, complete or delete the first user's to-dos.
- A POST without a valid CSRF token, or from another origin, is refused.
- A sealed value moved to another row or owner fails to open.
- Sign-up that fails after `Register` leaves no user, session or profile.
- The application refuses to start without a key, and with a malformed one.
- Logs captured during the flows contain none of the values listed in §7.
- The limits in §4 are enforced.
- `core/config`: the transform, layering, and validation reporting every
  missing field at once.

Before handing over, the builder runs the application and goes through every
flow in a real browser.

## 9. What is left to the builder

- The runner's exact API, within §10 and §5 here.
- The `core/config` API, within §6.
- Page layout, routes and wording.
- How expired sessions are cleaned up.

## 10. Calls made without the owner

- The application lives in `examples/todo`, in the root module.
- Sulis's CSRF protection instead of Go's `http.CrossOriginProtection`, because
  Sulis already has it and adds a token check that does not depend on headers.
- No password pepper, which is Sulis's default.
- The limits in §4 and the defaults in §6.
- Logging and shutdown stay in `main`; connection opening stays in the
  application.
- Localhost only, as in §2.
- No conformance suite for `core/config`, as in §2.

## 11. Decided by the owner

1. **A Sulis bound to a transaction (§5).** A new Sulis per transaction runs
   a 64 MiB Argon2 hash before the rate limiter is checked, so a flood of
   requests forces unlimited Argon2 work. Stores that read the transaction
   from the context hide it from the reader. Decided: a Sulis change that
   returns a copy using other stores and sharing everything else.
2. **A failed-attempt write that fails.** Sulis drops that error on purpose
   (`recordFailedLogin`), so the caller still gets `ErrInvalidCredentials`,
   and bunstore's savepoint lets the outer transaction commit. §10 requires
   the storage failure to be reported instead. Decided: Sulis returns it, in
   the same release as question 1, in a form that lets the caller tell a
   storage failure from a wrong password.
3. **Failure lockout stays off** in the application, as §5 explains.

Changes 1 and 2 are a prerequisite: they ship in one Sulis release, planned
in the Sulis repository, and `go.mod` moves to it before this work starts.

## 12. Risks

- Sulis's cookies are always `Secure` with a `__Host-` name. Chrome and
  Firefox accept these on `http://localhost`; other browsers may not, which
  would make local sign-in fail there.
- Sign-up says when an email is already registered, and without email
  verification anyone can sign up with an address they do not own. Both are
  fixed properly only by email verification, a non-goal here.
