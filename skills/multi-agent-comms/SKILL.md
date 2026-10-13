---
name: multi-agent-comms
description: >-
  Stop concurrent agent sessions clobbering one repo: check for peers, message before overlap,
  serialize push/build/install under a per-repo lock. Use when several sessions may work one
  project.
---

# Multi-agent coordination

Concurrent sessions in one checkout cause edits on top of someone else's uncommitted work, builds or installs overwriting each other, and simultaneous pushes to one ref.

This is the protocol between **peer** sessions. For one orchestrator directing implementers and reviewers through PRs see `pr-orchestration`; its interactive agents on the same machine still take the locks below. File locks only serialize processes on one machine; scheduled routines elsewhere coordinate through GitHub state.

## 1. Check for other sessions before touching a shared checkout

Before editing, building, installing or pushing:
- **Claude Code**: `ListAgents` lists peer sessions and whether each is busy.
- **Any tool**: `git status` showing changes you didn't make, an unrecognized branch or commit, or build output newer than your last build means someone else is there.

## 2. Message instead of working in parallel

- **Claude Code**: `SendMessage` to the peer (reply to the incoming message's `from` address). Lead with what you intend to change, where, and whether they already own it.
- If a peer says it owns the work, stop editing, building and installing in that tree and tell it exactly what you already changed.
- **Hosts without session messaging** (Codex, Gemini, opencode, Kimi, scheduled routines; see skills/HOSTS.md): use the section 3 locks, ask the user, or comment on the issue or PR both sessions read.

## 3. Serialize shared steps with a per-repo lock

Run steps that must not happen twice at once under an OS file lock (released on exit, including a crash, so no stale-lock cleanup):

```bash
mkdir -p /tmp/agent-locks
flock -w 600 /tmp/agent-locks/<repo>-<resource>.lock <command>   # Linux (util-linux)
lockf -t 600 /tmp/agent-locks/<repo>-<resource>.lock <command>   # macOS / BSD
```

Name locks by repo, as a single path component: replace every `/` in a branch name with `-` for `<branch-key>` (`feat/foo` -> `feat-foo`), or the lock fails to open. Resources worth locking:
- `git-push-<branch-key>`: every push to a branch from any workflow (`pr-iterate`, `ci-watch`, manual fix) takes `<repo>-git-push-<branch-key>`; same-ref pushes serialize, different PRs don't block.
- `build` / `install`: shared build output or system installs.
- `test-suite`: full runs contending for ports, databases or CPU.

If the lock times out, don't retry blindly: find the holder (section 1) and message it (section 2).

## 4. What a lock doesn't cover

A lock serializes one command, not conflicting edits. For overlapping edits, section 2 applies: one session owns the change, the other stops. Git history is the source of truth for what landed.
