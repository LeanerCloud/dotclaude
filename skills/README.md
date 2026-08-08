# Skills — the cross-agent guidance layer

Every rule in this repo that isn't needed on *every* turn lives here as an
[Agent Skill](https://agentskills.io): a directory with a `SKILL.md` whose frontmatter is loaded up
front and whose body loads only when the skill is activated. `CLAUDE.md` keeps the always-on core and
routes to these.

The same files drive **Claude Code**, **Codex CLI** and **Gemini CLI**. That portability is a
constraint on how they're written, not a happy accident — the rules below are what keeps it true.

## Discovery

| Tool | Reads |
|------|-------|
| Claude Code | `~/.claude/skills/<name>/SKILL.md` — i.e. this directory directly. Symlinked skill directories are followed. |
| Codex CLI | `$HOME/.agents/skills/<name>/` (user scope), `.agents/skills/` (repo scope) |
| Gemini CLI | `~/.gemini/skills/<name>/` or `~/.agents/skills/<name>/` (alias, higher precedence) |

`~/.agents/skills/` is read natively by both Codex and Gemini, so one canonical copy serves all
three. Run `scripts/setup-agent-symlinks.sh` to create `~/.agents/skills/<name>` symlinks pointing
back here. Nothing is copied and nothing is duplicated.

## Writing a portable skill

**Frontmatter must open the file and must set both fields.** Gemini *silently skips* a `SKILL.md`
that has any text before the opening `---`, or that omits `name:` or `description:` — you get no
warning, the skill just never exists. Claude treats both as optional; write them anyway.

```markdown
---
name: git-commit
description: Conventional-commit format, atomic-commit rules, and the mandatory three-clean-pass
  pre-commit review loop. Invoke before staging or writing a commit message.
---

Body...
```

- `name` must equal the directory name.
- `description` says *what it does and when to invoke it* — that sentence is the only thing the model
  sees before activating. Keep it under 300 characters: Codex budgets the whole skill list to ~8000
  characters and shortens descriptions when it overflows.
- Only `name` and `description` are portable. Claude-specific frontmatter (`allowed-tools`,
  `disable-model-invocation`, `user-invocable`, `context: fork`, `model`, `effort`) is ignored by
  Codex and Gemini — safe to use, but never make a skill's correctness depend on it.
- Bundle supporting files (templates, scripts, long reference material) in the skill directory and
  point at them from the body, so they load only when actually needed.

**Refer to skills by name, never by invocation syntax.** Claude uses `/name`, Codex uses `$name`,
Gemini activates implicitly via `activate_skill`. Prose in this repo always says *"invoke the
`git-commit` skill"*.

`scripts/validate-skills.sh` enforces the mechanical half of this and runs as a pre-commit hook.
