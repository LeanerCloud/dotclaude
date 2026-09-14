---
name: multi-agent-comms
description: How peer agent sessions sharing one machine or repo avoid clobbering each other -
  check for other sessions, message them before overlapping work, and serialize shared steps (push,
  build, install, full test suite) with a per-repo OS file lock. Invoke when several agents or
  sessions may work the same project concurrently.
---

# Multi-agent coordination

Several sessions on one machine regularly end up in the same checkout: one diagnosing a crash while
another fixes it, two CI watchers pushing fixes to one branch. The failures are concrete: edits
landing on top of someone else's uncommitted work, two builds or installs overwriting each other,
simultaneous pushes to one ref.

This is the protocol between **peer** sessions. For one orchestrator directing implementers and
reviewers through a queue of PRs, see the `pr-orchestration` skill; its agents still take the locks
below.

## 1. Check for other sessions before touching a shared checkout

Before editing, building, installing or pushing in a repo:

- **Claude Code**: `ListAgents` lists peer sessions and whether each is busy.
- **Any tool**: `git status` showing changes you didn't make, a branch or commit you don't
  recognize, or build output newer than your last build means someone else is working there.

## 2. Message instead of working in parallel

- **Claude Code**: `SendMessage` to the peer (to reply, use the incoming message's `from` address).
  Lead with the point: what you intend to change and where, and whether they already own it.
- When a peer says it owns the work, stop editing, building and installing in that tree, and tell it
  exactly what you already changed so it can decide what to keep.
- **Tools without session messaging** (Codex CLI, Gemini CLI, scheduled routines): ask the user, or
  comment on the issue or PR both sessions read.

## 3. Serialize shared steps with a per-repo lock

Run any step that must not happen twice at once under an OS file lock. The lock is released when the
command exits, including on a crash, so there is no stale-lock cleanup.

```bash
mkdir -p /tmp/agent-locks
flock -w 600 /tmp/agent-locks/<repo>-<resource>.lock <command>   # Linux (util-linux)
lockf -t 600 /tmp/agent-locks/<repo>-<resource>.lock <command>   # macOS / BSD
```

Name locks by repo so different repos never block each other. Resources worth locking:

- `git-push`: pushes, especially force-pushes (`<repo>-git-push-<branch>` when PRs push in parallel)
- `build` / `install`: anything writing shared build output or installing onto the system
- `test-suite`: a full run that contends for ports, databases or CPU

If the lock times out, don't retry blindly: find the holder (section 1) and message it (section 2).

## 4. What a lock doesn't cover

A lock serializes one command; it doesn't stop two sessions making conflicting edits. For
overlapping edits, section 2 is the protocol: one session owns the change and the other stops. Git
history stays the source of truth for what landed.
