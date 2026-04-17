# Multi-Agent Communication Protocol

Claude instances and agents working on the same project coordinate through a shared filesystem-based message bus at `~/.claude/agent-comms/`. This avoids conflicts, duplicated work, and broken commits when multiple sessions run concurrently.

## Directory Layout

```
~/.claude/agent-comms/
  messages/          # Timestamped message files (the bus)
  locks/             # Advisory lock files for exclusive operations
  status/            # Per-agent status beacons
```

Each agent picks a unique `AGENT_ID` at session start — use the format `<role>-<short-hash>`, e.g. `coder-a3f1`, `tester-9bc2`.

## Message Format

Messages are plain-text files named `<unix-ms>-<from>-<to>.msg` (or `<unix-ms>-<from>-broadcast.msg` for all-agent messages). For the millisecond timestamp on macOS, use `python3 -c 'import time; print(int(time.time()*1000))'` (macOS `date` lacks `%N`).

```
FROM: coder-a3f1
TO: tester-9bc2          # or "all"
TIMESTAMP: 2026-04-11T14:30:00Z
TYPE: sync | claim | release | intent | result | question | ack
---
<free-form body — keep under 20 lines>
```

### Message Types

| Type | Purpose |
|------|---------|
| `sync` | Share current state — what you're working on, what you just finished |
| `claim` | Announce exclusive ownership of a file or task (check locks/ first) |
| `release` | Relinquish a previous claim |
| `intent` | Declare what you're about to do — gives others a chance to object |
| `result` | Report outcome of a task (pass/fail, commit ref, summary) |
| `question` | Ask another agent for information or a decision |
| `ack` | Acknowledge receipt of a message |

## Workflows

### Before Committing

1. Write an `intent` message: which files you changed, proposed commit message.
2. Wait ~5 seconds and check for objections or conflicting `intent` messages.
3. If another agent has a conflicting intent, coordinate via `question`/`ack` to decide ordering.
4. After committing, write a `result` message with the commit SHA.

### Before Running Tests

1. Check `status/` — if another agent is mid-test-run, wait or coordinate.
2. Write a `claim` on the test runner (only one agent should run the full suite at a time to avoid resource contention).
3. After tests finish, write a `result` with pass/fail and any failing test names.
4. `release` the test runner claim.

### Fixing Tests

1. When a `result` message reports test failures, any agent can `claim` the fix.
2. The fixing agent writes an `intent` with which tests it's addressing.
3. After fixing, it runs the relevant tests and posts a `result`.
4. Other agents re-sync before resuming their own work.

### General Sync

Agents should post a `sync` message:
- At session start (announce presence and planned work)
- After completing a logical unit of work
- Before ending a session (handoff summary)

## Advisory Locks

Locks are directories at `locks/<resource-name>.lock/` — use `mkdir` to acquire (atomic on POSIX; fails if already exists) and `rmdir` to release. Place a file inside (`info`) with the `AGENT_ID` and timestamp. Locks older than 10 minutes without a corresponding status beacon are considered stale and can be removed.

> **Note**: the `&&` chaining below is intentional — the `echo`/`rm` must only run if `mkdir`/`rmdir` succeeds to preserve lock atomicity. This is an acceptable exception to the "avoid `&&`" rule in `tool-usage.md`. Write these as a script file per the multiline-shell rule.

```bash
# Acquire lock (atomic — fails if already held)
mkdir ~/.claude/agent-comms/locks/git-commit.lock && \
  echo "coder-a3f1 $(date -u +%Y-%m-%dT%H:%M:%SZ)" > ~/.claude/agent-comms/locks/git-commit.lock/info

# Release lock
rm ~/.claude/agent-comms/locks/git-commit.lock/info && \
  rmdir ~/.claude/agent-comms/locks/git-commit.lock
```

Resources worth locking:
- `test-runner` — full test suite execution
- `git-commit` — staging + committing (short-lived, seconds)
- `git-push` — pushing to remote
- Specific file paths when doing large refactors

## Status Beacons

Each agent writes `status/<AGENT_ID>.status` on a regular cadence (every few minutes or after significant actions):

```
AGENT_ID: coder-a3f1
ROLE: coder
PROJECT: /path/to/project
WORKING_ON: refactoring auth middleware
LAST_ACTION: committed 3a1b2c3
TIMESTAMP: 2026-04-11T14:35:00Z
```

Stale beacons (>15 min without update) indicate the agent is no longer active.

## Discovering Sibling Instances

Claude Code stores per-session data under `/private/tmp/claude-<uid>/<mangled-cwd>/`. To find other instances working on the same directory:

```bash
# Your UID
UID=$(id -u)

# The mangled path for the current project (slashes become dashes, leading dash)
MANGLED=$(echo "$PWD" | tr '/' '-')

# Check if a session directory exists (means another instance may be active)
ls -d /private/tmp/claude-${UID}/${MANGLED}/ 2>/dev/null

# List ALL active Claude CLI sessions and their working directories
ls /private/tmp/claude-${UID}/
```

Additionally, you can find running `claude` processes and their working directories:

```bash
# Find claude CLI processes and their PIDs
pgrep -fl "^claude" | grep -v Helper
```

When you discover siblings working on the same project, check `~/.claude/agent-comms/status/` for their beacons and `messages/` for recent activity before starting work.

## Reading Messages

When starting work or resuming after a pause, read all messages newer than your last read timestamp. Use `find` here — the `Glob` tool doesn't support `-newer` filtering:

```bash
# Find messages from the last 30 minutes
find ~/.claude/agent-comms/messages/ -name '*.msg' -newer <last-read-marker> -type f | sort
```

Process them in timestamp order. Acknowledge any `question` messages directed at you.

## Garbage Collection

Messages older than 24 hours can be deleted. Status beacons for inactive agents (>1 hour stale) can be removed. Lock files for inactive agents can be force-released after checking the agent is truly gone.

## Integration with Git Workflow

This protocol supplements — not replaces — normal git workflow:
- Still use feature branches for independent work streams
- Use this bus for real-time coordination that git can't provide (pre-commit sync, test orchestration)
- The bus is ephemeral; git history is the source of truth

## Quick Reference

```bash
# Bootstrap the comms directory
mkdir -p ~/.claude/agent-comms/{messages,locks,status}

# Millisecond timestamp (macOS-compatible)
MS=$(python3 -c 'import time; print(int(time.time()*1000))')

# Post a sync message
TS=$(date -u +%Y-%m-%dT%H:%M:%SZ)
cat > ~/.claude/agent-comms/messages/${MS}-myagent-broadcast.msg <<EOF
FROM: myagent
TO: all
TIMESTAMP: ${TS}
TYPE: sync
---
Starting work on auth refactor. Will touch api/auth.go and api/middleware.go.
EOF

# Check for active agents
ls ~/.claude/agent-comms/status/

# Check for locks
ls ~/.claude/agent-comms/locks/

# Read recent messages
ls -lt ~/.claude/agent-comms/messages/ | head -20
```
