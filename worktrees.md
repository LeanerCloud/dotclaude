# Worktree Isolation Per Change

Non-trivial work happens in a dedicated git worktree branched off the current branch — never commit in-progress work directly on the branch you started from. The base branch stays clean until the change is fully implemented and verified, so a broken or abandoned attempt never pollutes it.

This file is the full worktree-isolation protocol: when to create one, how to persist the plan, the PID/ownership lifecycle, crash recovery, the merge gate, and cleanup. CLAUDE.md §1b carries a short headline + pointer here.

## Preconditions and creation

- **Precondition — plan has passed the §1 three-pass review gate.** The worktree is the commitment to implement. Don't create one while the plan is still being iterated on, or it becomes a dumping ground for exploratory edits made on an unverified plan (and once commits start landing, reviewing the plan becomes fighting the code's momentum instead of shaping its design). If the plan needs more revision, stay on the base branch, revise, re-review, then come back.
- **Record the base branch** (the branch checked out when the task starts — e.g., `feat/multicloud-web-frontend`, `main`) in the plan. That's what you'll rebase/merge onto at the end. If the base branch is `main` or another protected branch, still use a worktree — PR discipline from `~/.claude/git-workflow.md` applies on top.
- **Create the worktree after the plan review gate passes**, before the first commit:
  ```bash
  git worktree add ../<repo>-<slug> -b <type>/<slug> <base-branch>
  ```
  where `<type>` matches conventional commit types (`feat`, `fix`, `refactor`, `chore`, etc.) and `<slug>` is a short kebab-case name for the change. All implementation, commits, tests, and reviews run inside the worktree.

## Persisting the plan

Persist the plan outside the worktree so it survives crashes: write the authoritative plan to `~/.claude/projects/<project>/plans/<slug>.md` (create the dir if missing). The plan file has three mandatory sections in order: (1) a YAML header, (2) an embedded workflow checklist (so the process travels with the plan even if a reader skips CLAUDE.md), and (3) the task breakdown itself.

### Header block (YAML frontmatter)

```yaml
---
worktree: /absolute/path/to/<repo>-<slug>
base_branch: <base-branch>
feature_branch: <type>/<slug>
started: <ISO-8601 timestamp>
status: in-progress   # in-progress | verifying | merged | abandoned
pid: <PID of the owning Claude process — $$ from the shell the session runs in>
host: <hostname — $(hostname) output>
pid_updated: <ISO-8601 timestamp of the last pid write>
---
```

### Embedded workflow checklist

Paste verbatim below the header — copy-paste, don't paraphrase, so every plan carries the same rules:

```markdown
## Workflow (embedded from ~/.claude/worktrees.md — DO NOT SKIP)

**Before touching any file in the worktree, resolve ownership**:
1. Read `pid:`, `host:`, and `pid_updated:` from the header.
2. If `host:` equals the current hostname, run `kill -0 <pid> 2>/dev/null`. Exit code 0 → another session owns this plan. STOP and coordinate via `~/.claude/agent-comms/` (see `~/.claude/multi-agent-comms.md`) — do not adopt.
3. If `host:` differs OR `kill -0` fails OR `pid_updated:` is older than 24h, the plan is orphaned. Adopt it: overwrite `pid:` with your own PID, `host:` with your hostname, `pid_updated:` with now (ISO-8601). Save the header BEFORE any code edit. The adoption write is the lock — whichever session writes last wins; the other must abandon if it discovers the change.
4. Re-read the header after a short delay (~2s) to detect a competing adopter. If your PID is still there, you own the plan; otherwise back off.

**While working**: refresh `pid_updated:` at least every 30 min (or at each task checkpoint) so a watchdog can tell a live session from a stuck one.

**Merge gate — ALL must hold before rebasing onto `base_branch:`**:
- Every item in this plan is implemented (tick each line).
- The CLAUDE.md §1 post-implementation review is clean.
- **Three consecutive verification passes find no gaps.** A pass covers tests, lint/typecheck, the §4 per-change-type verification (UI smoke, API curl, etc.), and a re-read of the diff against the plan. Any finding → fix and restart the count at zero. Partial credit does not exist.

**On completion**: rebase onto `base_branch:`, push, `git worktree remove` the worktree, flip `status:` to `merged`, then delete (or archive) this plan file.

**On abandonment**: flip `status:` to `abandoned`, clear `pid:`, then remove the worktree and delete the plan file.
```

### Task breakdown

The actual plan — atomic tasks with file paths and verification steps per CLAUDE.md §1.

### Symlink and exclusion

Then symlink the plan into the worktree:

```bash
ln -s ~/.claude/projects/<project>/plans/<slug>.md <worktree>/plan.md
```

Add `plan.md` to the worktree's `.git/info/exclude` (per-clone, local-only — keeps it untracked without touching the committed `.gitignore`). Update the plan file in place as the work evolves — it's the single source of truth; `plan.md` inside the worktree is just a convenient handle.

## PID lifecycle — writes are the ownership protocol

- On plan creation: set `pid:` to the current Claude process PID (the shell's `$$` from the same terminal the session runs in), `host:` to `$(hostname)`, `pid_updated:` to now.
- On every task checkpoint or at least every 30 min while actively editing: rewrite `pid_updated:` (and `pid:` if it changed). This is the liveness heartbeat.
- On adoption by a new session: rewrite `pid:`, `host:`, `pid_updated:` in one atomic write BEFORE any code change. Then verify after ~2s that your PID is still there — if not, another adopter raced you; yield.
- On clean exit (merge or abandon): clear `pid:` (set to empty or omit) so the plan is trivially identifiable as not-owned even before file deletion.

## Crash recovery and orphan detection

The next session enumerates `~/.claude/projects/<project>/plans/` and reads each header. For every plan with `status: in-progress` or `verifying`:

- `host:` matches current hostname AND `kill -0 <pid>` succeeds → **active**, leave alone.
- `host:` matches AND `kill -0` fails → **orphaned locally**, safe to adopt.
- `host:` differs → can't verify PID across machines; treat as orphaned only if `pid_updated:` is older than 24h (stale heartbeat). Otherwise leave alone and coordinate via the multi-agent comms bus.
- After adoption, `cd` to the `worktree:` path, re-read the embedded workflow, run `git status` and `git log <base_branch>..HEAD` to see progress, and resume from the first unchecked task.

## Subagent worktrees

Prefer the `Agent` tool's `isolation: "worktree"` parameter when delegating the implementation to a subagent — it creates and cleans up the worktree automatically. For subagent worktrees, still persist the plan to `~/.claude/projects/<project>/plans/` with the subagent's PID in the header so the parent session can recover if the subagent crashes.

## Merge gate

ALL of these must hold before rebasing/merging back onto the base branch:

1. Every item in the plan is implemented (cross-check the plan line-by-line).
2. The §1 post-implementation review is complete and clean.
3. **Three consecutive verification passes find no gaps.** A pass covers: tests, lint/typecheck, the §4 per-change-type verification (UI smoke, API curl, etc.), and a re-read of the diff against the plan. If any pass surfaces anything — missing behaviour, regression, hack, duplication, security concern — fix it and **restart the count at zero**. Partial credit does not exist.

## Rebase and cleanup

- **Rebase, don't merge, by default**: `git rebase <base-branch>` inside the worktree to keep history linear, then fast-forward the base branch. Use a merge commit only if the base branch protects against force-pushes or the team convention demands it.
- **After the merge**: push the base branch (triggering the post-push CI watcher per `~/.claude/git-workflow.md`), then `git worktree remove ../<repo>-<slug>`, delete the feature branch if it's no longer needed, and delete the plan file at `~/.claude/projects/<project>/plans/<slug>.md` (or flip its `status:` header to `merged` and move it to a `plans/archive/` subdir if you want an audit trail). Crash-recovery enumeration should only surface active work — stale plan files and worktrees confuse future sessions.
- **If a worktree is abandoned** (idea didn't pan out, approach superseded): flip the plan's `status:` to `abandoned` before removing the worktree, so recovery doesn't try to resume dead work. Then delete the plan file and worktree as above.

## When to skip

- **Skip the worktree only for trivially mechanical edits** — a single-line typo fix, a rename with no logic change, a comment tweak — the same bar as "skip the plan". When in doubt, create the worktree; the overhead is seconds and the isolation is worth it.
- **If a plan turns out to require multiple independent changes**, create one worktree per change. Land them one at a time onto the base branch in dependency order, re-running the 3-pass verification for each.
