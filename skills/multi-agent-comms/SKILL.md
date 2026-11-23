---
name: multi-agent-comms
description: >-
  Stop concurrent agent sessions clobbering one repo: check for peers, message before overlap,
  serialize push/build/install under a per-project lock. Use when several sessions may work one
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

## 3. Serialize shared steps with a per-project lock

A lock is scoped to one project and shared only by that project's workstreams. A **project** is the unit that owns its checkout, build tree and runtime prefix (one app bring-up, one feature repo); its **workstreams** are the parallel sessions working inside it. Never take, wait on or invent a lock that spans projects: a session of project A must not queue behind project B, even when both use the same toolchain or runtime.

Resources projects share (host RAM, swap, disk) get no lock. Check the live numbers before a heavy step (MemAvailable, free swap, free disk), bound the work with a timeout, and tell the user if it runs short. A physically single resource (one display, a licence) is the user's call; do not lock around it silently.

Run steps that must not happen twice at once within a project under an OS file lock (released on exit, including a crash, so no stale-lock cleanup):

```bash
mkdir -p /tmp/agent-locks
flock -w 600 /tmp/agent-locks/<project>-<resource>.lock <command>   # Linux (util-linux)
lockf -t 600 /tmp/agent-locks/<project>-<resource>.lock <command>   # macOS / BSD
```

Name locks by a project name that is unique on this machine (a repo is one project unless the user says otherwise; use its `owner-repo` slug, not the checkout directory name, which two unrelated checkouts can share), as a single path component: replace every `/` in a branch name with `-` for `<branch-key>` (`feat/foo` -> `feat-foo`), or the lock fails to open. Resources worth locking:
- `git-push-<branch-key>`: every push to a branch from any workflow (`pr-iterate`, `ci-watch`, manual fix) takes `<project>-git-push-<branch-key>` (a branch belongs to one project, so only that project's workstreams take its push lock); same-ref pushes serialize, different PRs don't block.
- `build` / `install`: the project's own shared build output or install target (a build tree or prefix owned by another project is not yours to lock or wait for).
- `test-suite`: full runs of the project that contend for ports, databases or CPU it owns.

If the lock times out, don't retry blindly: find the holder (section 1) and message it (section 2).

## 4. What a lock doesn't cover

A lock serializes one command, not conflicting edits. For overlapping edits, section 2 applies: one session owns the change, the other stops. Git history is the source of truth for what landed.
