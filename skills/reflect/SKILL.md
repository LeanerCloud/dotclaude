---
name: reflect
description: Spawn three parallel review subagents over the active transcript, surface learnings, and route each to a concrete edit on an existing skill. Use when the user says reflect.
---

<!-- GENERATED-TRAMPOLINE by scripts/import-upstream-skills.sh - do not edit; see skills/UPSTREAM.md -->

# reflect

Read `~/.claude/upstreams/cursor-plugins/pstack/skills/reflect/SKILL.md` and follow it exactly, including anything it references from its
own `references/` or `scripts/` directory (paths there are relative to that directory).

This file exists only because the upstream skill carries `disable-model-invocation: true`, which
stops Claude Code's Skill tool from reaching it. The body is deliberately not copied, so
`git submodule update --remote` keeps this skill current.

Resolve every Cursor primitive the upstream names through `~/.claude/upstreams/HOST-MAPPING.md`.
Where upstream and `~/.claude/CLAUDE.md` disagree, CLAUDE.md wins.
