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
