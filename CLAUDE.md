# gorfast

A Claude Code plugin holding skills for building web applications in Go. The
repository is also its own marketplace: `.claude-plugin/marketplace.json` lists
the plugin with `"source": "./"`.

## Workflow

This project uses Superpowers, not spec-kit. Before creative work, use
`superpowers:brainstorming`; for multi-step work, `superpowers:writing-plans`
then `superpowers:executing-plans`. Design docs belong in
`docs/superpowers/specs/`.

When adding or editing a skill, use `superpowers:writing-skills` and
`skill-creator:skill-creator`.

## Conventions for skills here

Skill directories are named for what the reader is doing, verb-first
(`loading-configuration`), and are invoked as `gorfast:<directory>`.

The `description` frontmatter field states **only when to use the skill** —
triggering conditions, symptoms, and context. It must never summarise the
skill's process, because agents will follow that summary instead of reading the
body, which defeats the point of writing the body.

Keep `SKILL.md` to concepts, conventions, and checklists. Put complete code and
long templates under `references/`, and point to them from `SKILL.md`.

## Verifying Go code in skills

Go that appears in a skill is load-bearing: readers copy it. It must compile.
Extract the code block and build it against the real dependencies rather than
reviewing it by eye — a wrong provider version or a mismatched struct tag reads
as correct and fails at runtime.

```bash
claude plugin validate .
```

## Versioning

`plugin.json` and the matching entry in `marketplace.json` must agree on
version. `claude plugin tag` checks this and creates the release tag.
