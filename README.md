# gorfast

Claude Code skills for building web applications in Go.

This repository is a Claude Code plugin. It is distributed through
[borfast/claude-plugins-marketplace](https://github.com/borfast/claude-plugins-marketplace).

## Install

```
/plugin marketplace add borfast/claude-plugins-marketplace
/plugin install gorfast@borfast
```

The marketplace only needs adding once; further plugins from it install as
`<name>@borfast`.

## Skills

| Skill | Use it for |
|---|---|
| `gorfast:loading-configuration` | Typed, layered configuration with Koanf: defaults → `.env` → environment variables |

More to follow.

A working application that uses these pieces is in [`examples/todo`](examples/todo).

## Layout

```
.claude-plugin/
  plugin.json           the plugin manifest
skills/
  loading-configuration/
    SKILL.md            overview, conventions, checklist
    references/
      implementation.md complete working code
```

Skills use progressive disclosure: `SKILL.md` carries the concepts and the
naming conventions, and the heavy implementation lives under `references/`,
read only when it is actually needed.

## Go module

This repository also holds a Go module, `github.com/borfast/gorfast`.
`auth/bunstore` provides [Bun](https://bun.uptrace.dev/)-backed implementations
of Sulis's `UserStore`, `SessionStore` and `TokenStore` interfaces, and of the
`totp.Store`, `passkey.Store`, `passkey.ChallengeStore` and `recovery.Store`
interfaces, for Postgres and SQLite.
`DeleteUser` also removes the user's TOTP credential, passkeys and recovery
codes.
`migrations.Apply` creates every table; an application that runs `migrations.FS`
with its own tool must also apply `0002_second_factors`, which adds the
second-factor tables.

IDs are version 7 UUIDs, stored as `uuid` on Postgres and as 16-byte `BLOB` on
SQLite. Databases created before this schema must be recreated: there is no
migration from text IDs.
An application table that refers to a user stores the ID the same way, through
`bunstore.ID`. Wrap IDs in query arguments too: Bun sends a bare `uuid.UUID` as
binary, which Postgres refuses and SQLite accepts, so tests that run only on
SQLite will not catch a missing wrap. The `type:uuid` tag makes Bun's
`CreateTable` and bulk updates use `uuid` on Postgres instead of `bytea`:

```go
type Unit struct {
	bun.BaseModel `bun:"table:units"`

	ID      bunstore.ID `bun:"id,pk,type:uuid"`
	OwnerID bunstore.ID `bun:"owner_id,type:uuid"`
}

err := db.NewSelect().Model(&units).Where("owner_id = ?", bunstore.ID(userID)).Scan(ctx)
```

In the application's own migration, `owner_id` is `UUID` on Postgres, and on
SQLite `BLOB` with `CHECK (typeof(owner_id) = 'blob' AND length(owner_id) = 16)`.
A nullable column uses `*bunstore.ID`, with nil meaning NULL, and its SQLite
check becomes `CHECK (owner_id IS NULL OR (typeof(owner_id) = 'blob' AND length(owner_id) = 16))`.
Keep `bunstore.ID` in Bun models and query arguments, and convert to `uuid.UUID`
where the model is read or written, as bunstore's own stores do. A model sent
as JSON still writes its IDs as UUID text.

Passkey challenges expire after five minutes, which `WithChallengeTTL` changes.
Expired rows stay until the application calls `DeleteExpiredChallenges`, so run
it on a schedule.

`crypt` encrypts column values at rest with a rotating keyring.
Generate a key with `go run github.com/borfast/gorfast/crypt/cmd/newkey@f9e66db3e375288edb3fa9fb23dd6d55740f1456`,
pinned to a reviewed commit.
See the [crypt spec](docs/superpowers/specs/2026-10-02-crypt-design.md#6-keys-bindings-and-rotation) for rotation.

`data.Runner` runs a function in a transaction it owns. Return `(outcome, nil)` to
commit writes that must survive an expected failure, and `(nil, err)` to roll back.

Run the tests with:

```bash
go test ./...
```

SQLite runs with no setup. Postgres tests skip unless a database is
available: `docker compose up -d postgres`, then set
`GORFAST_TEST_POSTGRES_DSN='postgres://gorfast:gorfast@localhost:5433/gorfast_test?sslmode=disable'`.

`go.mod` requires Sulis `v0.5.0`, the release whose IDs are `uuid.UUID` and
whose `totp.Store.SaveTOTP` refuses a save that does not raise the counter. A
local Sulis checkout is not required. For local development against a different
Sulis checkout, use a personal, untracked `go.work` override rather than
changing the committed module dependency.

## Development

This repo follows the [Superpowers](https://github.com/obra/superpowers)
workflow.

Validate changes before committing:

```bash
claude plugin validate .
```

## License

MIT
