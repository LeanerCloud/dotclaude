---
name: worktrees
description: Worktree isolation per change - creation preconditions, plan persistence, PID
  lifecycle, crash recovery, staleness detection, the merge gate and post-merge reclaim. Invoke when
  starting any non-trivial change.
---

# Worktree Isolation Per Change

Multi-commit or long-running work, and any work in a checkout another session may be using, happens in a dedicated git worktree branched off the current branch; never commit in-progress work on the branch you started from. This file is the full protocol (AGENTS.md §1b carries the headline).

## Preconditions and creation

- **Precondition: the plan has passed the §1 review gate** (2 adversarial passes, acted on). The worktree is the commitment to implement; while the plan is still being revised, stay on the base branch.
- **Durable root**: `~/.claude/worktrees/` (`<repo-name>` is the main checkout's directory name). Never under `/tmp`, `$TMPDIR` or another directory cleared at reboot.
- **New PR worktree names**: with a linked issue, use `<repo-name>-issue-<issue-number>-<slug>`. For multiple issues, use the primary issue being implemented and list the others in the plan and PR. With no associated issue, use `<repo-name>-no-issue-<slug>`, including work started before a PR exists; keep that name after creating the PR. Existing worktrees keep their paths, including during recovery.
- **Record the base branch** (the branch checked out at task start) in the plan; that is what you rebase onto. A `main`/protected base still gets a worktree.
- **Create after the review gate, before the first commit**:
  ```bash
  mkdir -p "$HOME/.claude/worktrees"
  git worktree add "$HOME/.claude/worktrees/<repo-name>-issue-<issue-number>-<slug>" -b <type>/<slug> <base-branch>
  ```
  This example has a linked issue; for no-issue work, use `<repo-name>-no-issue-<slug>` instead. `<type>` is a conventional-commit type, `<slug>` short kebab-case. All implementation, commits, tests and reviews run inside the worktree.

## Persisting the plan

Write the authoritative plan to `~/.claude/projects/<project>/plans/<slug>.md` (outside the worktree, so it survives crashes), with three sections in order: YAML header, embedded workflow checklist, task breakdown.

### Header block (YAML frontmatter)

```yaml
---
worktree: /absolute/path/to/.claude/worktrees/<repo-name>-issue-<issue-number>-<slug>
base_branch: <base-branch>
feature_branch: <type>/<slug>
started: <ISO-8601 timestamp>
status: in-progress   # in-progress | verifying | merged | abandoned
pid: <PID of the owning Claude process - $$ from the shell the session runs in>
host: <$(hostname)>
pid_updated: <ISO-8601 timestamp of the last pid write>
build_dirs: []   # absolute paths of every build dir created for this branch; the only dirs cleanup may delete
---
```

### Embedded workflow checklist

Paste verbatim below the header (copy, don't paraphrase):

```markdown
## Workflow (embedded from ~/.claude/skills/worktrees/SKILL.md - DO NOT SKIP)

**Before touching any file in the worktree, resolve ownership**:
1. Read `pid:`, `host:`, and `pid_updated:` from the header.
2. If `host:` equals the current hostname, run `kill -0 <pid> 2>/dev/null`. Exit code 0 -> another session owns this plan. STOP and coordinate with that session (see the `multi-agent-comms` skill); do not adopt.
3. If `host:` differs OR `kill -0` fails OR `pid_updated:` is older than 24h, the plan is orphaned. Adopt it: overwrite `pid:` with your own PID, `host:` with your hostname, `pid_updated:` with now (ISO-8601). Save the header BEFORE any code edit. The adoption write is the lock: whichever session writes last wins; the other must abandon if it discovers the change.
4. Re-read the header after ~2s to detect a competing adopter. If your PID is still there, you own the plan; otherwise back off.

**While working**: refresh `pid_updated:` at least every 30 min (or at each task checkpoint) so a watchdog can tell a live session from a stuck one.

**Merge gate - ALL must hold before rebasing onto `base_branch:`**:
- Every item in this plan is implemented (tick each line).
- The AGENTS.md §1 post-implementation review is clean.
- **2 verification passes are done and acted on** (two, whatever the stakes). A pass covers tests, lint/typecheck, the §4 per-change-type verification (UI smoke, API curl, etc.), and a re-read of the diff against the plan. Fix what each pass finds; don't keep re-verifying for a pass that finds nothing. A clean pass on the exact same SHA counts for every gate that sees it (`AGENTS.md` §1, same-hash rule).

**On completion**: rebase onto `base_branch:`, push, then (inside the AGENTS.md tenet 9 exception only when the branch tip is an ancestor of the pushed base, `git merge-base --is-ancestor <branch> origin/<base>`; otherwise ask) `git worktree remove` the worktree, flip `status:` to `merged`, then archive this plan file (`plans/archive/`). If the PR merges out-of-band (a human or another agent's `merge-watch` merges it after this session is gone), any later session reclaims this worktree with `reclaim-merged-worktrees.sh` ("Reclaiming merged worktrees, branches and build dirs" below).

**On abandonment**: flip `status:` to `abandoned`, clear `pid:`, archive the plan file, and remove the worktree only after user confirmation (abandoned work is outside the AGENTS.md tenet 9 exception).
```

### Task breakdown

Atomic tasks with file paths and verification steps per AGENTS.md §1.

### Symlink and exclusion

```bash
ln -s ~/.claude/projects/<project>/plans/<slug>.md <worktree>/plan.md
```

Add `plan.md` to the worktree's `.git/info/exclude` (local-only). Update the plan file in place; it is the single source of truth.

### Submodules are not isolated

Submodule gitdirs live in the main clone's `.git/modules`, so `git submodule update` inside a linked worktree moves the **main clone's** submodule HEADs for every worktree, and `init`/`sync` change shared config (submodule URL in the common `.git/config`). Superproject-only edits and index-only pointer fixes (`git update-index --cacheinfo 160000,<sha>,<path>`) are fine. Anything needing populated submodules, a build, or a recursive operation gets an **independent clone with `--reference`** (alternates share the superproject's objects; refs, HEAD and submodule gitdirs stay its own). The reference covers the superproject only: pass it to submodules explicitly (`git submodule update --init --recursive --reference <path>`, or `--dissociate`). The clone depends on the reference repo's object store, so never `git gc`/`prune`/`repack` the tree it borrows from.

## PID lifecycle - writes are the ownership protocol

Defined in the embedded checklist above (resolve, adopt, heartbeat every 30 min). Plan creation sets `pid:` (`$$`), `host:` (`$(hostname)`), `pid_updated:` (now); clean exit (merge or abandon) clears `pid:`.

## Crash recovery and orphan detection

The next session enumerates `~/.claude/projects/<project>/plans/` and reads each header with `status: in-progress` or `verifying`:

- `host:` matches AND `kill -0 <pid>` succeeds: **active**, leave alone.
- `host:` matches AND `kill -0` fails: **orphaned locally**, safe to adopt.
- `host:` differs: PIDs can't be verified across machines; orphaned only if `pid_updated:` is older than 24h. Otherwise coordinate (`multi-agent-comms`).
- After adoption, `cd` to `worktree:`, re-read the embedded workflow, run `git status` and `git log <base_branch>..HEAD`, resume from the first unchecked task.

## Staleness and disappearance

Check before doing any work in a pre-existing worktree; both failure modes let you verify code that isn't the code under review.

- **Stale = a pre-rebase copy.** After the branch is rebased (by you, another session, or a CR-loop force-push), an old worktree holds the *old* commits (same subjects, different SHAs): tests pass against code not on the branch and the push fails non-fast-forward at the end. Detect it only with the ahead/behind count (mtimes and byte-identical build artifacts mislead):
  ```bash
  git fetch origin <branch>
  git rev-list --left-right --count origin/<branch>...HEAD   # left = origin-only
  ```
  If behind: branch from the origin tip, cherry-pick your commits across, and **re-run every gate** (the base changed). Never `--force` (`--force-with-lease` only, per `git-commit`).
- **Gone entirely.** Worktrees get pruned mid-session (`git worktree prune`, a cleanup sweep, a reboot clearing `$TMPDIR`); the symptom is `fatal: not a git repository` from a directory that worked minutes ago. Recreate at the origin tip and re-run the gates; prior verification no longer stands. The plan file can be intact while the tree is gone.

## Subagent worktrees

Don't rely on a host-managed subagent worktree (Claude Code: `Agent` `isolation: "worktree"`; other hosts: skills/HOSTS.md) for crash-recoverable work. Create the worktree explicitly under `~/.claude/worktrees/` and pass its absolute path to the subagent. Still persist the plan under `~/.claude/projects/<project>/plans/` with the subagent's PID in the header so the parent can recover.

## Merge gate

The gate is the "Merge gate" block in the embedded checklist above (all plan items implemented, clean §1 post-implementation review, 2 verification passes acted on). After the second pass, go with it.

## Rebase and cleanup

- **Rebase, don't merge, by default**: `git rebase <base-branch>` in the worktree, then fast-forward the base. Merge commit only if the base protects against force-pushes or the team convention demands it.
- **Immediately after the merge**: push the base branch (post-push CI watcher per `ci-watch`), then clean up per "On completion" in the embedded checklist and delete the feature branch if no longer needed; build dirs are handled by the reclaim script below.
- **Abandoned worktree**: see "On abandonment" in the checklist.

## Reclaiming merged worktrees, branches and build dirs

`~/.claude/scripts/reclaim-merged-worktrees.sh [--apply] [--repo PATH] [--branch NAME]` is the implementation; never hand-roll the sweep. Without `--apply` it only reports, one line per item: `reclaimed|reclaimable|blocked|ask|skip|prunable <name> <reason>`.

**When**: report mode at session start and before ending; `--apply --branch <b>` right after each merge, from the primary checkout. The session that performs or first observes the merge (a human merge, a merge driver, a peer's `merge-watch`) owns the reclaim, and any session may reclaim another session's merged worktree because the gates below protect live work.

**What it checks** (it fails closed: a `gh` error or a full result page aborts before any deletion):
- **Merged**: the branch tip equals or is an ancestor of the head of a same-repo merged PR into the default branch (fork PRs ignored). A reused name, extra commits or a stacked base gives `ask`. Branches with an open PR as head or base, `main` and the default branch are `skip`; branches with no merged PR are left alone.
- **Gate** (`blocked` leaves the worktree untouched): not locked; clean tree; no ignored file outside the build allowlist (a path with any component named `node_modules`, `target`, `dist`, `build`, `.terraform`, `__pycache__`, `coverage`, `.next`, `test-results` or `playwright-report`, at any depth; or a top-level `plan.md` symlink), and only when nothing under that prefix is tracked; secret-like names (`.env*`, keys, certs, `*.tfvars`, `*.tfstate*`, `.npmrc`, `secrets.*`, `credentials*`) always block, even inside build dirs (except node_modules, whose packages ship such fixtures); no process with its cwd inside; no `in-progress`/`verifying` plan with a live owner.
- **Actions** (`--apply`, in order): `git worktree remove` (never `--force`); the plan's `build_dirs:` (each path must be absolute, not a symlink, strictly below `~/.claude/worktrees/`, `$TMPDIR` or `/private/tmp/`, canonical, not a git clone, with no live cwd inside, clear of every worktree and the repo, and listed by no other plan); local branch delete pinned to the verified tip; remote delete only when the remote equals or is an ancestor of a merged head (`ask: remote ahead` otherwise); plan to `status: merged` in `plans/archive/`. A full run also covers local branches that have no worktree.

`blocked` is a signal of stranded work (recover first: commit, gate, fresh PR), not an obstacle to force past. `ask` needs the user. CLOSED-unmerged and abandoned work is outside the AGENTS.md tenet 9 exception and is never touched by the script.

## When to use one

- **Use a worktree when the work is multi-commit or long-running, or another session may be using the checkout.** A small single-commit change can stay on a feature branch in the main checkout if no other session uses it.
- **If a plan needs multiple independent changes**, one worktree per change, landed one at a time in dependency order, re-verifying each.
