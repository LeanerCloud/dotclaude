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
- `description` says *what it does and when to invoke it* — that text is the only thing the model
  sees before activating, so trigger phrases belong here rather than in `when_to_use`, which only
  Claude reads.
- **Budget**: Codex renders the whole skill list into ~8000 characters and silently shortens
  descriptions past that. The validator holds the total under 7000 and any single description under
  400. The total is the real constraint — adding skills is what eats it.
- **Use a `>-` block scalar for any description containing `#`.** In a plain YAML scalar a space
  followed by `#` starts a comment, so a description mentioning `#NNN` or an issue ref is silently
  truncated there and the skill advertises half of what it does. This bit `pr-iterate`. The
  validator now fails on it.
- Only `name` and `description` are portable. Claude-specific frontmatter (`allowed-tools`,
  `disable-model-invocation`, `user-invocable`, `context: fork`, `model`, `effort`) is ignored by
  Codex and Gemini — safe to use, but never make a skill's correctness depend on it.
- Bundle supporting files (templates, scripts, long reference material) in the skill directory and
  point at them from the body, so they load only when actually needed.

**Refer to skills by name, never by invocation syntax.** Claude uses `/name`, Codex uses `$name`,
Gemini activates implicitly via `activate_skill`. Prose in this repo always says *"invoke the
`git-commit` skill"*.

`scripts/validate-skills.sh` enforces the mechanical half of this and runs as a pre-commit hook.

## The one thing skills cost you

A flat file that `CLAUDE.md` said to always read was in context unconditionally. A skill is in
context only if something invokes it, and nothing enforces that. For most guidance this is the point
of the exercise — but for a few rules, missing them is expensive.

Two mitigations, in order of strength:

1. **The headline rule stays in `CLAUDE.md`.** A skill holds the procedure, never the fact that the
   procedure exists. `CLAUDE.md` says "before staging a commit, invoke `git-commit`" and states the
   3-clean-pass requirement inline, so skipping the skill still leaves the rule visible.
2. **`scripts/hooks/skill-reminder.sh`** (Claude Code only, registered as a `PreToolUse` hook on
   `Bash`) injects a one-line reminder next to `git commit`, `git push`, and `gh pr create`. Those
   three are rare enough that a per-invocation nudge is cheap and high-stakes enough to be worth it.
   It deliberately does not fire on every Bash call: a reminder that always appears stops being read.

Gemini CLI has its own hook system (`gemini hooks`) that could carry the same reminders; Codex does
not currently expose an equivalent. Neither is wired up.
