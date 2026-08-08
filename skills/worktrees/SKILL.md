---
name: worktrees
description: Worktree isolation per change - creation preconditions, plan persistence, PID
  lifecycle, crash recovery, staleness detection, the merge gate and post-merge reclaim. Invoke when
  starting any non-trivial change.
---

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

**On completion**: rebase onto `base_branch:`, push, `git worktree remove` the worktree, flip `status:` to `merged`, then delete (or archive) this plan file. If the PR merges out-of-band (a human or another agent's `merge-watch` merges it after this session is gone), any later session reclaims this worktree via the sweep in `~/.claude/worktrees.md` ("Reclaiming worktrees after the PR merges or closes").

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

## Staleness and disappearance

A worktree that has been around a while is not necessarily current, and is not necessarily still there. Both failure modes let you verify code that isn't the code under review, so check before doing any work in a pre-existing worktree.

- **Stale = a pre-rebase copy.** After the branch is rebased (by you elsewhere, by another session, or by a CR-loop force-push), a long-lived worktree still holds the *old* commits: same commit subjects, different SHAs. Tests there pass against code that is not on the branch, and the push fails as non-fast-forward at the very end, after all the work. Detect it with the ahead/behind count, never by eyeballing:
  ```bash
  git fetch origin <branch>
  git rev-list --left-right --count origin/<branch>...HEAD   # left = origin-only
  ```
  If behind: branch from the origin tip, cherry-pick your commits across, and **re-run every gate** — the base changed, so earlier verification no longer applies. Never resolve this with `--force` (`--force-with-lease` only, per `~/.claude/git-workflow.md`).
  What does *not* detect staleness: file mtimes merely look "old", and externally-visible artifacts (generated schemas, build outputs) can be byte-identical across versions. Only the ahead/behind count is conclusive.
- **Gone entirely.** Worktrees get pruned mid-session (`git worktree prune`, a cleanup sweep, a reboot clearing `$TMPDIR`). The symptom is `fatal: not a git repository` from a directory that worked minutes ago. Recreate it at the origin tip and re-run the gates; do not assume the prior run's verification still stands. This is distinct from the plan-file orphan detection above: the plan can be perfectly intact while the tree it described is gone.

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

## Reclaiming worktrees after the PR merges or closes (run the sweep)

The creating session is usually **not** the one that observes its PR reach a terminal state: in a PR-based repo, merges happen through GitHub (a human clicking merge, or another agent's `merge-watch`, see `~/.claude/git-workflow.md`), and the creating session may be long dead by then. Likewise a PR can be closed as wontfix/superseded by someone else entirely. So worktree cleanup **cannot** rely only on the creating session's "on completion" step above, or worktrees pile up (a stale `git worktree list` full of merged branches poisons crash-recovery enumeration and wastes disk). Every session treats a worktree whose PR has merged or closed as reclaimable, and sweeps proactively.

**When to sweep** (cheap, so run it liberally):
- At session start and before ending a session.
- Immediately after observing any PR merge (yours, or a peer's via `merge-watch`) or any PR you close as wontfix.

**The sweep**: enumerate `git worktree list`, and for each worktree branch resolve its PR state, then reclaim only the terminal + clean ones:

1. **Classify by PR state**: `gh pr list --head <branch> --state all --json number,state,mergedAt` (or `gh pr view <branch>`). Reclaim only when the PR is `MERGED` or `CLOSED` (closed = wontfix/superseded). Leave `OPEN` PRs and any branch with **no** PR (could be pre-PR work) alone.
2. **Safety gate before removal** (the worktree must be clean and hold nothing unpushed):
   - `git -C <worktree> status --porcelain` is empty (no uncommitted changes), AND
   - `git -C <worktree> log --oneline @{u}..` is empty (nothing ahead of upstream); if the upstream branch is already gone from origin, the commits are on the merged PR / base branch.
   - If **either** check is non-empty, do NOT remove; this is the `feedback_recover_stranded_fix_work` case (a dead agent left finished-but-uncommitted or unpushed work). Recover it first (commit, gate, fresh PR, or cherry-pick), then remove.
3. **Never reclaim a `locked` worktree** (a running agent owns it) or one whose plan header shows a live PID on this host; coordinate via `~/.claude/agent-comms/` first.
4. **Remove**: `git worktree remove <worktree>` (add `--force` only if git balks on a lock/submodule *after* the safety gate confirmed it clean). Then delete BOTH sides of the now-orphaned branch and the plan file:
   - **Local branch**: `git branch -D <branch>`.
   - **Remote branch**: `git push origin --delete <branch>` — the head branch of a `MERGED` or wontfix-`CLOSED` PR serves no further purpose, and leaving it strands a remote ref that clutters `git branch -r`, breaks branch pickers, and (over a busy repo) accumulates into hundreds of dead refs. Skip only if origin already lacks it (GitHub auto-deleted it on merge — check `git ls-remote --heads origin <branch>` first, or just ignore the "remote ref does not exist" error). NEVER delete the remote of an `OPEN`-PR branch, `main`, or a protected/base branch.
   - **Plan file**: delete `~/.claude/projects/<project>/plans/<slug>.md` (or flip `status:` to `merged`/`abandoned` and move it to `plans/archive/`).

**Never bulk-remove blindly**: enumerate, classify by PR state, safety-gate each, remove only the confirmed-terminal-and-clean. A worktree that fails the safety gate is a signal (stranded work to recover), not an obstacle to force past.

## Sweep merged/closed branches that have NO worktree

The worktree sweep above only reaches branches that still have a worktree. Local and remote **branch refs outlive their worktrees** — every merged PR leaves a `refs/heads/<branch>` (and often a `refs/remotes/origin/<branch>`) behind, and these pile into the hundreds on a busy repo, poisoning branch pickers and `gh`/`git` autocompletion. So pair the worktree sweep with a **branch sweep** at session start/end (also cheap):

1. **Build the authoritative merged set** — squash-merges mean `git branch --merged` MISSES most merges (the squashed commit has a different SHA), so do NOT gate on it. Use the PR state instead: `gh pr list --state merged --limit 3000 --json headRefName --jq '.[].headRefName' | sort -u > merged.txt`. Build a `closed.txt` the same way for wontfix/superseded closes if you want to sweep those too.
2. **Compute delete sets by intersection, with hard exclusions**: also fetch the current **open**-PR head branches (`--state open`) and a protected list (`main`, any long-lived base). `local_del = local ∩ merged − open − protected`; `remote_del = origin ∩ merged − open − protected`. Print the counts and explicitly assert the open-PR branches and `main` are absent from both delete sets before deleting anything.
3. **Delete**: `git branch -D` each local; `git push origin --delete` each remote **in batches** (~25 refs per push) with a few seconds between batches, to stay under GitHub's secondary/abuse rate limit (see `feedback_pace_merges_secondary_ratelimit`). A per-branch fallback handles refs GitHub already auto-deleted without failing the batch.
4. **Dirty-worktree safety still applies**: if a to-be-deleted branch has a worktree with uncommitted/unpushed work, snapshot it (`git diff HEAD > <preserved-path>.patch`) before removing — never `--force` away un-snapshotted local edits even on a merged branch.

This is exactly the cleanup that keeps a repo from reaching hundreds of stale worktrees/branches; run it as routine hygiene, not a one-off rescue.

## When to skip

- **Skip the worktree only for trivially mechanical edits** — a single-line typo fix, a rename with no logic change, a comment tweak — the same bar as "skip the plan". When in doubt, create the worktree; the overhead is seconds and the isolation is worth it.
- **If a plan turns out to require multiple independent changes**, create one worktree per change. Land them one at a time onto the base branch in dependency order, re-running the 3-pass verification for each.
