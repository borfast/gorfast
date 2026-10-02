# Gorfast: design

**Date:** 2026-09-27
**Status:** Approved, pending implementation plan for slice 1

## 1. What Gorfast is

Gorfast is a Go library for building web applications. It takes from Rails,
Laravel and Django only the ideas that are worth taking:

- A safe-by-default starting point, so a new application is secure before the
  author configures anything.
- Batteries included, so most projects do not have to assemble the same ten
  dependencies again.
- Cohesive pieces that are designed to work together.

It deliberately rejects the mechanics those frameworks use to deliver those
ideas:

- No convention-over-configuration magic.
- No framework that calls your code. Your `main()` calls Gorfast.
- No DSLs.
- No global application singleton.
- No implicit registration and no init-time side effects.

The test for every API is: can the reader see what happens by reading their own
code, without knowing a convention?

**Audience:** primarily the author's own applications. Public on GitHub, but
APIs may break until they settle. Not yet a project that promises stability to
strangers.

**Module:** one repository, one Go module, `github.com/borfast/gorfast`. Split
into submodules later only if dependency weight becomes a real complaint.

Gorfast is also the umbrella name. The Claude Code skills that currently live in
this repository are one of its features, not a separate product.

## 2. Doctrine

These rules apply to every package. They are the reason the library is a library
and not a framework.

1. **Feature packages do not import each other.** `auth` does not import `http`.
   `data` does not import `view`. A user can take one feature package and leave
   the rest.

   Two packages are exempt because they are primitives rather than features:
   `core` (config, logging, lifecycle) and `crypt`. Any package may depend on
   those, and they depend on nothing in Gorfast. The dependency graph is
   therefore two layers deep and has no cycles by construction.

2. **Contracts are interfaces. Third-party libraries are implementation details
   behind them.** Gorfast defines the interface; an adapter package implements
   it against a specific library. No exported, consumer-reachable Gorfast API
   mentions Bun, Redis or any other dependency in its signature. Packages under
   `internal/` are exempt, because no consumer can import them, so internal test
   infrastructure naming a driver type is not a violation.

3. **Every interface Gorfast defines ships an executable conformance suite as
   public API.** This is the pattern Sulis established with its `storetest`
   package, and it is Gorfast's signature move. An interface's doc comment
   states the requirements no compiler can check (atomicity, scoping, error
   sentinels); the conformance suite proves an implementation meets them. This
   is what makes "swap the backend" a real promise rather than a hopeful one.

4. **Constructor plus functional options**, matching Sulis:
   `data.New(db, data.WithEncryption(k))`.

5. **Safe by default, opt out visibly.** Every default is the secure choice.
   Turning one off is a visible call a reviewer can find, never the silent
   result of forgetting something. Inherited from Sulis and applied everywhere:
   fields declared encrypted are encrypted, CSRF is on, transactions roll back
   on panic, a backend that cannot provide a requested guarantee refuses at
   startup instead of degrading quietly.

6. **Layering.** Pure `domain` types at the centre, `service` around them,
   `storage` and `transport` at the edges. Presentation is a peer choice, not a
   core assumption, so HTML and JSON are two adapters over the same service.
   There is no content negotiation: the author picks the adapter explicitly.

7. **Errors.** Sentinel values and `%w` wrapping. Mapping an error to an HTTP
   status happens only at the transport edge, never in a service or a
   repository.

8. **Storage tests run against real databases, never mocks.** Postgres and
   SQLite.

9. **Functional style where Go allows it.** Prefer pure functions and values
   over methods that mutate shared state.

## 3. Packages

```
gorfast/              github.com/borfast/gorfast
  core/               config (koanf), logging (slog), lifecycle, graceful shutdown
  crypt/              encryptors, self-describing envelope, encrypted column types
  data/               Bun integration: connection, transaction-per-request, fixtures, seeding
  auth/bunstore/      Sulis store interfaces implemented over Bun
  http/               router, middleware stack, request binding and validation, error mapping
  view/               html/template layouts and partials, forms, flash, CSRF, HTMX helpers
  api/                JSON presenters, problem+json
  jobs/               queue interface, worker loop, backends
  mail/               templating, sending, development mailbox
  cmd/gorfast/        the CLI
  examples/           reference application
  docs/
```

The repository is also a Claude Code plugin, and stays one. The Go module sits
alongside that packaging rather than replacing it:

```
  .claude-plugin/     the plugin manifest, distributed through
                      borfast/claude-plugins-marketplace
  skills/             the skills, named by what they do
                      (loading-configuration, not gorfast-config)
  evals/              the eval harness that grades those skills
```

So `go.mod` is at the repository root and `go get github.com/borfast/gorfast`
works, while `/plugin install gorfast@borfast` keeps working unchanged. The
skills describe this library and live next to it, which is the arrangement that
stops them drifting from it.

Deferred until there is a reason: observability (OpenTelemetry), i18n, an asset
pipeline.

## 4. Decisions

| Decision | Choice | Reason |
|---|---|---|
| Data access | Adopt [Bun](https://bun.uptrace.dev/) and build the integration around it | Struct-mapped ORM for simple CRUD, full query builder for complex work, raw SQL escape hatch, migrations and fixtures included, multi-dialect. Sits on `database/sql`, so encrypted column types work unchanged. Does not shape domain types. |
| Where Bun appears | Only inside `data` and `auth/bunstore` | Gorfast's own contracts stay at the repository interface, so an application can swap the implementation. |
| Configuration | Koanf, with the rules already established in `skills/loading-configuration` | Already proven in `lastmessage`. See section 5. |
| Presentation | `view` and `api` as peer packages over the same services | Keeps the core presentation-agnostic. No content negotiation. |
| Job backends | Small interface, declared capabilities, several backends | See section 7. |
| Module layout | One module, many packages | Simplest to develop and version while APIs are unstable. |
| First slice | `auth/bunstore` and `crypt` | See section 6. |

## 5. `core/config`

Carried over from `skills/loading-configuration`, which is already proven in
`lastmessage`:

- Layered loading, later sources overriding earlier: defaults, then `.env`, then
  environment variables.
- Environment variable naming `PREFIX_SECTION__FIELD`. A double underscore
  becomes a dot for nesting; a single underscore is preserved.
- Typed structs with `koanf` tags, and the tags are **snake_case**. This is not
  cosmetic: a squashed `koanf:"maxconns"` silently discards
  `MYAPP_DATABASE__MAX_CONNS` and uses the default instead, which is one of the
  four defects commit `ef1bafb` fixed. No stringly-typed access.
- One central `validate()` reporting all missing required fields together, each
  naming the environment variable that sets it.
- Loaded once at startup and passed as a typed value. No global koanf instance,
  no reloading per request.
- `.env.dist` committed and documented, `.env` ignored.

One further rule, added upstream in `be3d78a` and adopted here because it says
the same thing doctrine rule 1 says: the config package lives at
`internal/config` and **only `main` imports it**. `main` destructures the config
and hands each component the values it needs, rather than passing
`*config.Config` around. Passing the whole config everywhere makes it a hub that
every package depends on, which is the coupling this library exists to avoid.

Gorfast's contribution on top of the skill is the reusable parts: the key
transform function, the layered `Load` helper parameterised by prefix and
defaults, and the validation error type. The application still declares its own
`Config` struct, because that struct is the application's, not the library's.

## 6. Slice 1: `crypt` and `auth/bunstore`

The first thing built. Chosen because it is small, sharply specified, has an
acceptance test that already exists, and makes Sulis immediately more useful
whether or not the rest of Gorfast gets built.

### 6.1 `crypt`

Generalises `totp.AESEncryptor` from Sulis
(`/home/borfast/projects/sulis/totp/encrypt.go`), which is the best existing
design of this in the author's projects:

- AES-256-GCM with a random nonce per call, so encrypting the same plaintext
  twice gives unrelated ciphertexts.
- A short fingerprint of the encrypting key prefixed onto every ciphertext, and
  a map of fingerprint to key. Decryption selects the key from the prefix. This
  gives key rotation without re-encrypting stored data and without any external
  key bookkeeping.
- Fails closed. A wrong key, truncated ciphertext or any other anomaly returns
  an error, never plausible-looking wrong plaintext.

It borrows one idea from `lastmessage/internal/repositories/encryption`: a
self-describing envelope that stores the algorithm alongside the ciphertext, so
the algorithm can change later. It does **not** borrow that package's key
derivation, which runs Argon2id per record on every read. That is slow and makes
encrypted columns unqueryable.

Public surface:

- `Encryptor` interface: `Encrypt([]byte) ([]byte, error)`,
  `Decrypt([]byte) ([]byte, error)`. Byte slices, not strings, so binary values
  can be encrypted.
- `AESEncryptor`, constructed with a current key and any number of retired keys.
- `EncryptedString` and `EncryptedBytes` column types implementing
  `driver.Valuer` and `sql.Scanner`, so an encrypted column needs no special
  handling in a query.
- An adapter satisfying `totp.Encryptor`, so Sulis TOTP secrets get the same
  treatment without Sulis changing.
- Deferred, documented as a known gap: HMAC blind index columns for looking up
  encrypted values. Not needed until something needs to query one.

### 6.2 `auth/bunstore`

Implements all seven Sulis store interfaces over Bun:

| Sulis interface | Conformance suite |
|---|---|
| `sulis.UserStore` | `storetest.RunUserStore` |
| `sulis.SessionStore` | `storetest.RunSessionStore` |
| `sulis.TokenStore` | `storetest.RunTokenStore` |
| `totp.Store` | `storetest.RunTOTPStore` |
| `passkey.Store` | `storetest.RunPasskeyStore` |
| `passkey.ChallengeStore` | `storetest.RunPasskeyChallengeStore` |
| `recovery.Store` | `storetest.RunRecoveryStore` |

Design points:

- **Storage models are separate from Sulis types.** `bunstore.userModel` carries
  the `bun` tags; `sulis.User` stays clean. This is forced on us, because Sulis's
  structs cannot be tagged from here, and it is also the domain/storage split
  the layering doctrine asks for. Slice 1 therefore proves that split on a real
  case before anything else depends on it.

- **Stores accept `bun.IDB`, not `*bun.DB`.** The same store then works against
  a connection or inside a transaction unchanged. This is the seam that
  transaction-per-request will use in `data`.

- **The hard contracts map to single statements**, so none of them needs an
  explicit transaction:
  - `UpdateUser`: `UPDATE ... SET ..., version = version + 1 WHERE id = ? AND
    version = ?`. Zero rows affected is ambiguous, because it means either that
    another writer won or that no such user exists, and `memstore` returns a
    different error for each. So a follow-up existence check on the id chooses
    between `sulis.ErrConcurrentUpdate` and `sulis.ErrUserNotFound`. A unique
    constraint violation on the live email returns `sulis.ErrUserAlreadyExists`.
    The caller's own `user.Version` is left untouched, matching `memstore`.
  - `ConsumeToken`: one `UPDATE ... WHERE hash = ? AND purpose = ? AND used =
    false ... RETURNING *`. No match means either not found or already used, so
    the implementation distinguishes them with a follow-up read to choose
    between `ErrTokenNotFound` and `ErrTokenAlreadyUsed`.
  - `DeleteSession`: scoped to both columns, `DELETE FROM sessions WHERE id = ?
    AND user_id = ?`. Zero rows affected returns `ErrSessionNotFound`, which is
    what makes cross-user revocation impossible.
  - `ListUserSessions` and every other read returns independent copies, never
    aliases of stored state. Reconstructing rows from a database read gives this
    for free, but the conformance suite checks it.

- **Migrations ship two ways**: embedded Bun migrations for anyone who wants
  them, and the same statements as plain `.sql` files for anyone who runs goose
  or anything else. The library does not insist on owning migrations.

- **Dialects**: Postgres first, SQLite second. SQLite matters mostly because it
  makes the conformance suite fast to run. MySQL only if something needs it.

### 6.3 Definition of done for slice 1

All seven `storetest.Run*` suites pass against a real Postgres and against
SQLite, and `crypt` has its own round-trip, rotation and fail-closed tests. This
is not a judgement call: the acceptance criteria already exist as executable
code in Sulis.

## 7. Jobs

Deferred to a later slice, recorded here because the design shapes the
interface.

Backends have genuinely different capabilities, so neither the intersection nor
the union of their features is an honest interface:

| Capability | Postgres (`SKIP LOCKED`) | Redis | RabbitMQ |
|---|---|---|---|
| Transactional enqueue | yes | no | no |
| Delayed jobs | yes | yes | plugin only |
| Unique jobs | yes | partial | no |
| Job state introspection | yes | partial | no |
| Throughput | lowest | high | highest |

The design that follows from this:

- **The interface stays small**: enqueue, subscribe, ack, nack.
- **Backends declare their capabilities.** An application that asks for a
  capability the configured backend does not have fails at startup with a clear
  message. It never degrades silently, per doctrine rule 5.
- **Retry, backoff and dead-lettering live in Gorfast's worker loop**, not in
  each backend, so behaviour is identical across backends. Only the primitives
  differ.
- **Postgres is the default.** Its enqueue accepts a `bun.IDB`, so a job can be
  queued in the same transaction as the business write that justifies it, and
  can never fire for work that rolled back. That is the single strongest reason
  to prefer it, and it composes directly with the seam from slice 1.
- A conformance suite proves every backend behaves identically for the
  capabilities it claims.

## 8. Sequencing

Each step is justified by what the previous step actually needed, so nothing is
designed on paper ahead of a real consumer.

1. `auth/bunstore` and `crypt`. Slice 1.
2. `core` and `data`, extracted from what slice 1 turned out to need.
3. `http`.
4. `view` and `api`, as peers.
5. `examples/`: one signed-in CRUD application proving steps 1 to 4, built
   HTML-first with JSON endpoints beside it.
6. `jobs` and `mail`.
7. `cmd/gorfast` and the skills rewrite. Last, because a generator can only be
   written once the API it generates against is real.

`skills/gorfast-auth` was already deleted upstream in `ef1bafb`, so there is
nothing to drop. `skills/gorfast-config` became `skills/loading-configuration`
in the same commit, split into a `SKILL.md` carrying the concepts and a
`references/` directory carrying the code.

## 9. Known gaps

Recorded so they are choices rather than oversights.

- HMAC blind indexes for querying encrypted columns. Deferred until something
  needs to query one.
- Sulis's `totp` and `passkey` subpackages have no event sink, so security
  events from them cannot be captured yet. This is Sulis's gap, not Gorfast's.
- Sulis's default rate limiter is per-process, not shared across instances. A
  multi-instance deployment needs a shared limiter, which Gorfast should provide
  once `data` exists.
- Observability, i18n and asset pipeline are all unaddressed.
