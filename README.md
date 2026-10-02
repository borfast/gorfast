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
of Sulis's `UserStore`, `SessionStore` and `TokenStore` interfaces, for
Postgres and SQLite.

`crypt` encrypts column values at rest with a rotating keyring.
Generate a key with `go run github.com/borfast/gorfast/crypt/cmd/newkey@latest`.
See the [crypt spec](docs/superpowers/specs/2026-10-02-crypt-design.md#6-keys-bindings-and-rotation) for rotation.

Run the tests with:

```bash
go test ./...
```

SQLite runs with no setup. Postgres tests skip unless a database is
available: `docker compose up -d postgres`, then set
`GORFAST_TEST_POSTGRES_DSN='postgres://gorfast:gorfast@localhost:5433/gorfast_test?sslmode=disable'`.

`go.mod` requires Sulis `v0.2.1`. The store interfaces these stores implement
first shipped in `v0.2.0`. A local Sulis checkout is not required. For local
development against a different Sulis checkout, use a personal, untracked
`go.work` override rather than changing the committed module dependency.

## Development

This repo follows the [Superpowers](https://github.com/obra/superpowers)
workflow. See `CLAUDE.md` (symlinked as `AGENTS.md` for Codex, Copilot CLI and
Gemini CLI).

Validate changes before committing:

```bash
claude plugin validate .
```

## License

MIT
