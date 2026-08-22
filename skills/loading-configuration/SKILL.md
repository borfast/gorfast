---
name: loading-configuration
description: Use when writing, reviewing, or changing how a Go service loads configuration - reading env vars, parsing .env files, defining config structs, calling os.Getenv, wiring up a config package, or adding a new setting to an existing app. Use this even when the user doesn't mention Koanf by name.
---

# Loading Configuration in Go

## Overview

Load configuration in layers into one typed struct, then pass that struct
around. Use [Koanf](https://github.com/knadh/koanf) rather than hand-rolled
`os.Getenv` calls.

**Core principle:** configuration is read once, at startup, into a value the
compiler can check. Everything after that is ordinary Go.

Layers, lowest priority first: **defaults → .env file → environment variables.**
Each layer overrides the one before it, so a container can override a `.env`
that overrides a built-in default without any conditional logic.

## When to Use

- Creating a config package for a new service
- Adding a setting to an app that already has config
- Replacing scattered `os.Getenv` calls with something typed
- Setting up `.env` support for local development

Skip this for single-value throwaway scripts, where one `os.Getenv` is honest.

## Where Config Lives

Put the package at `internal/config/config.go`, and let **only `main` import
it.** `main` destructures the config and hands each component what it needs:

```go
pool, err := db.Open(cfg.Database.URL, cfg.Database.MaxConns)
srv := server.New(cfg.Server.Host, cfg.Server.Port, pool)
```

Passing `*config.Config` into every component turns config into a hub: every
package depends on it, and it knows every package's settings. That is the same
coupling as a global koanf instance, one indirection removed.

Unexported types in `package main` (`cmd/web/config.go`) are fine while the
binary *is* the whole application — one `cmd/`, no `internal/` packages, no test
helpers that need the values. Switch to `internal/config` as soon as a second
binary (`cmd/worker`, `cmd/migrate`) or any package outside `main` needs the
type. Duplicating the struct into a second `package main` is the failure mode
to avoid.

## Environment Variable Naming

The separator convention is the part people get wrong. Double underscore nests;
single underscore is just a word break inside a name.

| Env variable | Koanf path | Struct field |
|---|---|---|
| `MYAPP_SERVER__HOST` | `server.host` | `Config.Server.Host` |
| `MYAPP_SERVER__READ_TIMEOUT` | `server.read_timeout` | `Config.Server.ReadTimeout` |
| `MYAPP_DATABASE__MAX_CONNS` | `database.max_conns` | `Config.Database.MaxConns` |

Pattern: `PREFIX_SECTION__FIELD` → `section.field`.

The koanf tag must match the *transformed* path exactly. A tag of
`koanf:"maxconns"` will silently ignore `MYAPP_DATABASE__MAX_CONNS` and leave
the default in place — no error, just the wrong value at runtime. When a field
name is two words, the tag is snake_case.

## Implementation

Read **`references/implementation.md`** for the complete `config.go`, the
`.env.dist` template, `.gitignore` patterns, and the exact `go get` lines.

## Adding a New Field

1. Add the struct field with a `koanf` tag matching its path
2. Add a default to the `defaults` map if the field is optional
3. Add a check to `validate()` if it is required
4. Document it in `.env.dist`
5. Set `PREFIX_SECTION__FIELD` to match

## Common Mistakes

**Tag doesn't match the path.** The failure is silent — the default wins. If a
variable seems ignored, print the transformed key before blaming the provider.

**Wrong env provider version.** `providers/env/v2` takes
`Provider(delim, env.Opt{...})`. The three-argument form is v1 and will not
compile against a v2 import.

**Prefix mismatch.** `EnvPrefix` must exactly match the variables you set,
trailing underscore included.

**Case.** Environment variables are uppercase; the transform lowercases them.

**Durations** parse from strings like `30s`, `5m`, `1h` straight into
`time.Duration`. No manual conversion.

## Anti-Patterns

**Global koanf instance.** Load once into a typed struct in `main`, then hand
components the values they need. A package level `*koanf.Koanf` is mutable
global state.

**Loading more than once.** `Load()` belongs in `main()`, not in a getter called
per request.

**Stringly-typed access.** `k.String("database.url")` scattered through the
codebase discards the type safety that motivated the struct. Read fields off
the config value instead.

## Quality Checklist

- [ ] Every struct field has a `koanf` tag, including nested struct fields
- [ ] Tags match koanf paths exactly (snake_case for multi-word names)
- [ ] Required fields are checked in `validate()`; optional ones have defaults
- [ ] `.env.dist` documents every variable and is committed
- [ ] `.gitignore` excludes `.env` but keeps `.env.dist`
- [ ] `EnvPrefix` matches the application
- [ ] Config is loaded once at startup and its *values* passed to components
- [ ] `internal/config` is imported by `main` only
