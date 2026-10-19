# Host mapping

The harness (`AGENTS.md`, `skills/`, `scripts/`) runs on five hosts. Skills and `AGENTS.md` name rules by
role; this table says how each host does them. `scripts/setup-agent-symlinks.sh` creates the links.

## Where each host reads the harness

| Host | Global rules | Skills |
|---|---|---|
| Claude Code | `~/.claude/CLAUDE.md` (`@AGENTS.md`) and `~/.claude/AGENTS.md`, loaded once | `~/.claude/skills/` |
| Codex CLI | `~/.codex/AGENTS.md`, 32 KiB cap (`project_doc_max_bytes`, docs) | `~/.agents/skills/`, listing capped near 8000 chars |
| Gemini CLI | `~/.gemini/GEMINI.md` | `~/.agents/skills/` (alias of `~/.gemini/skills/`) |
| Kimi Code | `~/.agents/AGENTS.md` (or `~/.kimi-code/AGENTS.md`); truncates very large files (docs) | `~/.agents/skills/` |
| Antigravity CLI (`agy`) | `~/.gemini/GEMINI.md` (also `~/.gemini/AGENTS.md`) | `~/.gemini/antigravity-cli/skills/` (app/IDE: `~/.gemini/config/skills/`); not `~/.agents/skills` (docs) |
| opencode | `~/.config/opencode/AGENTS.md` (first match wins, falls back to `~/.claude/CLAUDE.md`: docs) | `~/.agents/skills/`, `~/.claude/skills/`, `~/.config/opencode/skills/` |

Global-rule loading was verified live on 2026-10-13/14 for Claude Code, Codex, Kimi, opencode and
Antigravity CLI (each
saw `AGENTS.md` once); Codex and Gemini do not read `~/.agents/AGENTS.md`. opencode also loads an
`AGENTS.md` from a parent directory such as `~` as project rules, so keep no stale copy there.

opencode also sees the Claude-only imported skills through `~/.claude/skills/`; set
`OPENCODE_DISABLE_CLAUDE_CODE_SKILLS=1` to limit it to `~/.agents/skills/`.

## Primitives

| Role | Claude Code | Hosts without it |
|---|---|---|
| Subagent at a tier | `Agent` tool with `model` (state effort in the brief) | Run it yourself, or start an independent headless session (below) |
| Continue the same subagent | `SendMessage` | Re-brief a new session with the prior findings; independence matters more than warmth |
| Background job | Bash `run_in_background: true` | `nohup ... &` with output to a file, then check it when the work is due |
| Wake later | `ScheduleWakeup`, `CronCreate` | The host's scheduler, or a background `sleep N; <check>` |
| Isolated checkout | `isolation: "worktree"` | `git worktree add` per the `worktrees` skill |
| Peer messaging | `SendMessage` across sessions | The per-repo locks in `multi-agent-comms`, or a comment on the shared issue or PR |
| Ask the user | `ask-user.sh DOC` (decision doc) | `ask-user.sh DOC` (decision doc) |

**Headless session as an independent reviewer**: `codex exec --sandbox read-only`, `kimi --plan -p`
(plan mode), `opencode run`, `gemini -p` and `agy -p --sandbox`
(none of these four documents a read-only mode; agy's `--sandbox` only restricts the terminal, so the
brief is the guard). Give it the diff or a worktree path, the cold brief and an explicit
read-only instruction (use the host's sandbox or read-only mode where it has one). It cannot be
continued, so a second pass re-sends the brief plus the first findings. A different vendor's model
gives stronger independence than the same model in a fresh session.

## Reviewer models

Use an independent non-author for every review gate, review-findings triage and independent
verification. This mapping takes precedence over the general tiers below and review-tier examples in
other skills; an exact project-specific reviewer pin still wins.

| Host | Reviewer selection |
|---|---|
| Codex CLI | `gpt-6.1-sol`, medium effort (`-m gpt-6.1-sol -c model_reasoning_effort=medium`) |
| Claude Code | Opus (`model: opus`); retain it as the default Claude reviewer, not a mandatory gate |
| opencode using Claude | Opus; choose the available provider/model ID from `opencode models` |
| Other hosts | Their existing high-tier reviewer mapping below |

Keep the same reviewer across the two passes of its loop. Fresh exact-head independent verification
uses a cold brief and no author transcript. Planning, diagnosis, implementation and prescribed
mechanical checks keep their existing tiers; model selection grants no merge or approval-bypass authority.

## Tiers

`AGENTS.md` §2 names three tiers by role: low (mechanical), medium (writes code), high (plans,
diagnoses). Reviews and verification use §Reviewer models.

| Host | low | medium | high |
|---|---|---|---|
| Claude Code | `sonnet`, low effort | `sonnet`, medium effort | `sonnet`, high effort |
| Codex CLI | default model, `-c model_reasoning_effort=low` | `=medium` | `=high` |
| opencode | `-m opencode/claude-sonnet-5-5`, low effort in the brief | same, medium | same, high |
| Kimi Code | `-m kimi-code/k3`, effort low | `kimi-code/k3`, effort high | `kimi-code/k3`, effort max |
| Gemini CLI | Flash-Lite | Flash | Pro |
| Antigravity CLI | `agy -p --effort low` | `--effort medium` | `--effort high` (pick `--model` from `agy models`; unverified) |

The Sonnet-only general tiers in `AGENTS.md` §2 cover Claude models outside the reviewer role above;
other hosts use their own models. Kimi's medium row is effort high because `k3` offers only low, high and max.
Exercised live: `kimi-code/k3` (the default) and opencode's free model; the opencode Sonnet ID is from
`opencode models` (that account had no funds to run it).
opencode lists no effort variants for Sonnet (`provider/model#variant`), so state the effort in the brief.
Kimi has no per-call effort flag: effort is `default_effort` (low, high, max) in
`~/.kimi-code/config.toml`, so a Kimi session runs at one effort; pick the session's tier up front.

Model IDs come from each host's model list on 2026-10-13; re-check with the host's model list command
when a name stops resolving. An exact model pinned by a project rule always wins. Gemini CLI was not
verifiable live (its individual tier was retired in favour of Antigravity), so its rows follow the docs.
Antigravity CLI is its successor (`brew install --cask antigravity-cli`, browser sign-in); its rules and
skills paths are verified live, its tier flags are from the docs. Link files, never whole
config directories: each host keeps its own runtime config, system skills and credentials.
