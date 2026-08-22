# Eval: config hub coupling

Checks that `gorfast:loading-configuration` stops agents from wiring every
package to the config package.

## What it measures

One fixture service (`fixture/`) with everything hardcoded, one task
(`task.md`): add layered configuration and wire it through. Two outcomes get
graded, both read off the generated Go files rather than the agent's own
summary:

- **who imports `internal/config`** — must be `cmd/web` alone
- **what `db.Open` and `server.New` take** — plain values, not a `config.*`
  struct

## Running it

```bash
./run.sh --arm without --reps 5   # no guidance; expected to FAIL
./run.sh --arm with    --reps 5   # SKILL.md in the prompt; expected to PASS
./grade.sh <run-work-dir>         # score one run on its own
```

`run.sh` inlines the current `SKILL.md` and `references/implementation.md` into
the prompt and drives a headless agent per rep. Results land in `results/`,
which is gitignored. Always run the `without` arm too: if it passes, the eval
has stopped discriminating and no longer proves the skill does anything.

## Recorded results

2026-08-23, `claude -p --model sonnet`, 5 reps per arm.

| Arm | Guidance | Importers of `internal/config` | `server.New` | Score |
|---|---|---|---|---|
| control | none | 3 (`main`, `db`, `server`) | `cfg config.ServerConfig` | 0/5 |
| baseline | SKILL.md at `ef1bafb` | 3 (`main`, `db`, `server`) | `cfg config.ServerConfig` | 0/5 |
| current | SKILL.md with "Where Config Lives" | 1 (`main`) | `host, port, readTimeout, writeTimeout, pool` | 5/5 |

Every rep in an arm produced the same signatures — that convergence is the
signal the wording binds rather than merely suggests.

Two findings worth keeping:

- The control arm chose `internal/config` unprompted in 5/5 runs, so the
  location advice codifies what agents already do. The load-bearing rule is
  "only `main` imports it".
- The baseline arm failed identically 5/5 while following a checklist that said
  config is *"passed to components"* — read as "pass the struct". That line now
  says its **values** are passed, and the anti-pattern entry no longer says
  "pass it". Re-running the `with` arm after both edits stayed 5/5.

## Not covered

The fixture has `internal/` packages, so the branch where config types belong in
`package main` never fires. Nothing here tests it.

`claude plugin eval` is early-access gated on this machine, so these cases are
not in its `case.yaml` format yet.
