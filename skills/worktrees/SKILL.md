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
- **Durable root**: `~/.claude/worktrees/<repo-name>-<slug>` (`<repo-name>` is the main checkout's directory name). Never under `/tmp`, `$TMPDIR` or another directory cleared at reboot.
- **Record the base branch** (the branch checked out at task start) in the plan; that is what you rebase onto. A `main`/protected base still gets a worktree.
- **Create after the review gate, before the first commit**:
  ```bash
  mkdir -p "$HOME/.claude/worktrees"
  git worktree add "$HOME/.claude/worktrees/<repo-name>-<slug>" -b <type>/<slug> <base-branch>
  ```
  `<type>` is a conventional-commit type, `<slug>` short kebab-case. All implementation, commits, tests and reviews run inside the worktree.

## Persisting the plan

Write the authoritative plan to `~/.claude/projects/<project>/plans/<slug>.md` (outside the worktree, so it survives crashes), with three sections in order: YAML header, embedded workflow checklist, task breakdown.

### Header block (YAML frontmatter)

```yaml
---
worktree: /absolute/path/to/.claude/worktrees/<repo-name>-<slug>
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

**On completion**: rebase onto `base_branch:`, push, then (inside the AGENTS.md tenet 9 exception only when the branch tip is an ancestor of the pushed base, `git merge-base --is-ancestor <branch> origin/<base>`; otherwise ask) `git worktree remove` the worktree, flip `status:` to `merged`, then archive this plan file (`plans/archive/`). If the PR merges out-of-band (a human or another agent's `merge-watch` merges it after this session is gone), any later session reclaims this worktree via the sweep in the `worktrees` skill ("Reclaiming worktrees after the PR merges or closes").

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

- Plan creation: set `pid:` (the session shell's `$$`), `host:` (`$(hostname)`), `pid_updated:` (now).
- Every task checkpoint or at least every 30 min while editing: rewrite `pid_updated:` (and `pid:` if changed). This is the heartbeat.
- Adoption: rewrite `pid:`, `host:`, `pid_updated:` in one write BEFORE any code change; verify after ~2s that your PID is still there, else yield.
- Clean exit (merge or abandon): clear `pid:`.

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

ALL must hold before rebasing/merging onto the base branch:

1. Every plan item is implemented (cross-check line by line).
2. The §1 post-implementation review is complete and clean.
3. **2 verification passes are done and acted on** (two, whatever the stakes), as defined in the embedded checklist. Fix what a pass surfaces but don't restart a counter: after the second pass, go with it.

## Rebase and cleanup

- **Rebase, don't merge, by default**: `git rebase <base-branch>` in the worktree for linear history, then fast-forward the base. Merge commit only if the base protects against force-pushes or the team convention demands it.
- **Immediately after the merge**: push the base branch (post-push CI watcher per `ci-watch`). The rest of this bullet is inside the AGENTS.md tenet 9 exception only when the branch tip is an ancestor of the pushed base (`git merge-base --is-ancestor <branch> origin/<base>`); otherwise ask first. Then `git worktree remove "$HOME/.claude/worktrees/<repo-name>-<slug>"`, delete the build dirs recorded in the plan's `build_dirs:` (exact paths only, only if no other plan lists them; never glob by number; never shared dirs such as `build-master`), delete the feature branch if no longer needed, and archive the plan (flip `status:` to `merged`, move to `plans/archive/`).
- **If a worktree is abandoned**: flip `status:` to `abandoned` first, archive the plan, and remove the worktree once the user confirms.

## Reclaiming worktrees after the PR merges or closes (run the sweep)

The creating session usually isn't the one that sees its PR merge or close (a human merges, or another agent's `merge-watch`, see `git-commit`). So every session treats a worktree whose PR has merged or closed as reclaimable, and sweeps proactively; otherwise merged-branch worktrees pile up and poison crash-recovery enumeration.

**When** (cheap, run liberally): at session start and before ending; right after observing any PR merge (yours or a peer's) or closing one as wontfix.

**The sweep**: enumerate `git worktree list`, resolve each branch's PR state, reclaim only terminal + clean ones:

1. **Classify by PR state**: `gh pr list --head <branch> --state all --json number,state,mergedAt,headRefOid` (or `gh pr view <branch>`). Reclaim `MERGED` without asking (AGENTS.md tenet 9 exception) only when the branch tip equals `headRefOid` or is its ancestor (`git merge-base --is-ancestor <branch> <headRefOid>`); otherwise it is a reused branch name, outside the exception: ask. A `CLOSED` (wontfix/superseded) PR's worktree and branches need per-item user confirmation. Leave `OPEN` PRs and branches with **no** PR (possibly pre-PR work) alone.
2. **Safety gate before removal** (clean, nothing unpushed):
   - `git -C <worktree> status --porcelain` is empty, AND
   - `git -C <worktree> status --porcelain --ignored` lists only build output (the plan's `build_dirs:` plus standard caches: `node_modules`, `target`, `dist`, `__pycache__`; no `.env`, credentials, logs or local data; `git worktree remove` deletes ignored files silently), AND
   - `git -C <worktree> log --oneline @{u}..` is empty (if the upstream is gone from origin, the commits are on the merged PR / base).
   - If **any** is non-empty, do NOT remove: this is the `feedback_recover_stranded_fix_work` case (a dead agent's finished-but-uncommitted or unpushed work). Recover first (commit, gate, fresh PR, or cherry-pick), then remove.
3. **Never reclaim a `locked` worktree** (a running agent owns it) or one whose plan header shows a live PID on this host; coordinate (`multi-agent-comms`).
4. **Remove**: `git worktree remove <worktree>` (never `--force` without user confirmation; if git balks, report why). For a `MERGED` PR also delete the build dirs in the plan's `build_dirs:` (exact paths, only if no other plan lists them, never a glob by number, never shared dirs such as `build-master`); the tenet 9 exception covers this. Then delete BOTH sides of the orphaned branch and archive the plan:
   - **Local**: `git branch -D <branch>`.
   - **Remote**: `git push origin --delete <branch>` (a merged/wontfix head branch is dead; stale remote refs accumulate by the hundreds). Skip if origin already lacks it (GitHub auto-deleted it; check `git ls-remote --heads origin <branch>` or ignore "remote ref does not exist"). NEVER delete the remote of an `OPEN`-PR branch, `main`, or a protected/base branch. Skip it while an open PR has it as base (`gh pr list --base <branch> --state open`).
   - **Plan file**: flip `status:` to `merged`/`abandoned`, move to `plans/archive/`.

**Never bulk-remove blindly**: enumerate, classify, safety-gate each, remove only confirmed-terminal-and-clean. A worktree that fails the gate is a signal (stranded work), not an obstacle to force past.

## Sweep merged/closed branches that have NO worktree

Branch refs outlive their worktrees, so pair the worktree sweep with a **branch sweep** at session start/end:

1. **Build the merged set from PR state**, not `git branch --merged` (squash-merges change the SHA): `gh pr list --state merged --limit 3000 --json headRefName,headRefOid --jq '.[] | [.headRefName, .headRefOid] | @tsv' | sort -u > merged.txt` (`headRefName<TAB>headRefOid`). Delete a branch only when its tip equals that SHA or is its ancestor (`git merge-base --is-ancestor`); a mismatch is a reused name, so ask. Closed-unmerged branches are outside the tenet 9 exception: list them for the user to confirm.
2. **Delete sets by intersection, with hard exclusions**: also fetch open-PR head AND base branches (`--state open --json headRefName,baseRefName`; `open` below is their union, so a branch a stacked PR still targets survives) and a protected list (`main`, long-lived bases). `local_del = local ∩ merged − open − protected`; `remote_del = origin ∩ merged − open − protected`. Print the counts and assert the open-PR branches and `main` are absent from both sets before deleting.
3. **Delete**: `git branch -D` each local; `git push origin --delete` each remote **in batches** (~25 refs per push, a few seconds apart) to stay under GitHub's secondary rate limit (`feedback_pace_merges_secondary_ratelimit`). A per-branch fallback handles refs GitHub already auto-deleted.
4. **Dirty-worktree safety still applies**: a to-be-deleted branch whose worktree has uncommitted, unpushed or non-build ignored files is outside the exception. Snapshot it (`git diff HEAD > <preserved-path>.patch`) and ask the user first.

## When to use one

- **Use a worktree when the work is multi-commit or long-running, or another session may be using the checkout.** A small single-commit change can stay on a feature branch in the main checkout if no other session uses it.
- **If a plan needs multiple independent changes**, one worktree per change, landed one at a time in dependency order, re-verifying each.
