# Multi-agent PR harness

How a change gets from "found" to "merged" when **one interactive session is
driving several PRs and several agents at once**, and why each step exists.
Every rule here was written after the failure it prevents actually happened,
so treat the "Trap" sections as incident reports rather than theory.

This file owns only what is specific to running that harness: the role split,
the independence rule, subsystem agent pooling, the merge gate as a
four-way check, the trap list, and the agent briefing template. Everything
else it needs already has an owner, and is cross-referenced rather than
restated:

| Topic | Owner |
|-------|-------|
| Commit conventions, CodeRabbit loop, CI watchers, merge mechanics, rate limits | `git-workflow.md` |
| Worktree isolation, plan persistence, crash recovery, post-merge reclaim | `worktrees.md` |
| Model-tier selection, agent reuse via `SendMessage`, background-first execution | `subagent-strategy.md` |
| Concurrent sessions on one repo (locks, sync messages) | `multi-agent-comms.md` |
| Priority ordering of issues and PRs | `triage.md` |
| Verification standards, adversarial verification | `CLAUDE.md` §4, §6 |

Paths below use placeholders: `<repo>` is the main checkout, `<wt>` a
worktree root (this repo conventionally puts them under a scratch dir such
as `$TMPDIR/claude/`).

---

## Interactive or scheduled?

Two harnesses solve the same problem (drive issues and PRs to merged) under
different constraints. Pick by which constraints you are under, not by
preference.

| | **This file** (interactive) | **`issue-pr-autopilot.md`** (scheduled) |
|---|---|---|
| Runs as | a local session you are watching | remote cron routines, unattended |
| Subagents | yes, that is the whole design | **no** - a routine is one flat agent, single model, no `Agent` tool |
| Tier split | per-agent, live (Opus reviews, Sonnet implements) | across *separate routines* handing off through GitHub state |
| Coordination state | orchestrator context + task list | GitHub labels (`plan-ready` / `pr-created` / ...) |
| Watchers | background `Agent`s per push and per PR | the next cron fire *is* the watcher |
| Good for | a backlog you are actively working, high-stakes money paths | steady unattended drip, backlogs nobody is watching |

They compose: the autopilot can open a PR that an interactive session later
takes over, because both drive the same GitHub state. If both are live on
the same repo, treat the autopilot as another concurrent session (§2) and
check for its in-flight labels before claiming an issue.

---

## 1. Roles

The main session is an **orchestrator only**. It does not edit code. It
spawns agents, tracks tasks, evaluates merge gates, and reports. Keeping it
free of file contents is what lets it supervise many PRs at once without
its context filling up with diffs.

| Role | Who | May edit code? |
|------|-----|----------------|
| Orchestrator | main session | no |
| Implementer | one agent per PR | yes, only its PR's worktree |
| Adversarial reviewer | separate agent | no - review only |

**The independence rule**: a reviewer must never review code it wrote, and
never re-bless a change it already approved. This is about roles, not
rounds - the *same* reviewer re-reviewing after the implementer fixes its
findings is correct and cheap, because it still holds the diff. This is the
PR-level form of the local review loop in `CLAUDE.md` §1c; tier selection
and the reuse-vs-respawn call are in `subagent-strategy.md`.

**Context pooling**: pool reviewers by subsystem (Azure providers, backend
API + auth, MCP/purchase path, frontend). An agent warm on a subsystem
reviewing a *different* PR in that subsystem is still fresh on that diff,
so it satisfies independence while skipping the cold-start re-read.
Continue such an agent by message rather than spawning a new one.

---

## 2. Topology

Several sessions work this repo concurrently, each in its own worktree,
and more than one may push to the *same* PR branch.

```
main
 └── <feature-branch> ← PR #N ← possibly several sessions
```

Worktree creation, plan persistence, and post-merge reclaim are in
`worktrees.md`; cross-session locks and sync messages are in
`multi-agent-comms.md`. What the multi-writer-per-branch shape adds on top:

- **Never force-push.** Another session's commits live on that branch. When
  a rebase genuinely requires it, `--force-with-lease` only, per
  `git-workflow.md`.
- **Always check freshness before pushing** (§5, stale worktree).
- **Never bare `git stash`** - the stash stack is shared across worktrees, so
  a bare `pop` can restore another session's work into yours. Use a WIP
  commit, or `git stash push -u -m "<unique-tag>"` and recover by SHA with
  `apply`, never `pop`.
- **Push explicitly**: `git push origin HEAD:<branch>`, never bare
  `git push`.

---

## 3. The merge gate

A PR merges only when **all four** hold. A blanket "merge them" is never
authorization to bypass one.

| Gate | Check | Prevents |
|------|-------|----------|
| CI green | every workflow run `success` for the exact HEAD SHA | merging broken code |
| CR clean | 0 unresolved threads **AND** latest review newer than the HEAD push | the false-clean trap (§5) |
| Adversarial clean | independent agent finds nothing, verified against current HEAD | green-CI-but-still-broken |
| Mergeable | `MERGEABLE` + `CLEAN`; no `--admin`, no `--no-verify` | merging past a pending check |

The mergeable gate is `git-workflow.md` §4 ("never bypass required checks");
this table exists because the other three are easy to skip when several PRs
are in flight at once.

Merge mechanics under multi-PR load: merge **one at a time**, via the REST
endpoint (`gh api PUT .../merge`) rather than `gh pr merge`, with ~30s
spacing plus backoff. Bursting merges trips GitHub's secondary rate limit,
which is distinct from the hourly quota and is handled per
`git-workflow.md` §"Rate-limit handling". Rebase **just in time** - only
immediately before merging that PR. Eager mass-rebasing churns, because each
merge invalidates the next.

---

## 4. The loops

Each runs to a fixed point. "I fixed round 1" is not an exit condition, and
that is the part orchestrating many PRs makes easy to forget.

**CodeRabbit loop** - full rules in `git-workflow.md` §"Post-PR review loop"
(trigger/watcher atomicity, the triage buckets, never `@coderabbitai
resolve`, `full review` after a throttle). Harness-level: exit is *zero
actionables on the latest review*, and the false-clean trap in §5 is what
makes an apparent exit untrustworthy.

**Adversarial loop** - reviewer reports, implementer fixes, reviewer
re-reviews, repeat until a round finds nothing. Findings must be verified
live against current HEAD, not taken on faith. Roles per §1.

**CI watcher** - per `git-workflow.md` §"Post-push CI watcher". The
harness-level rule: arm it *before* or with the push, never after the fact,
and one per workflow run.

Ordering: work PRs and issues in triage-priority order (`priority/p0`
first), not by number or by interest. Rubric in `triage.md`.

---

## 5. Traps

Each of these has produced a wrong "done" in practice.

### False-clean CodeRabbit
Zero unresolved threads can mean CR is **paused or throttled**, or that the
previous round's threads were resolved while the newest commit was never
reviewed. **Always compare the latest review's `submittedAt` against the
HEAD push time.** An older review does not cover the newer commit. CR also
edits its walkthrough comment *in place* (`created_at` unchanged,
`updated_at` bumped), so watchers keyed on comment creation miss reviews
entirely - key on review `submittedAt`. Throttle detection and the
`full review` recovery are in `git-workflow.md` §"Post-PR review loop" §2.

### Stale worktree
A long-lived worktree is often a *pre-rebase copy*: same commit subjects,
different SHAs. Testing there verifies code that is not on the branch, and
the push fails as non-fast-forward at the very end. **Before any work**:

```bash
git fetch origin <branch>
git rev-list --left-right --count origin/<branch>...HEAD   # left=origin-only
```

If behind: branch from origin tip, cherry-pick, and **re-run every gate**,
because the base changed. Never resolve this with `--force`.

Note what does *not* detect staleness: file mtimes look merely "old", and
externally-visible schemas can be byte-identical across versions. Only the
ahead/behind count is conclusive.

### Stale adversarial review
An adversarial-review comment on a PR proves nothing unless it postdates
the current HEAD commit. Compare timestamps; re-review the delta otherwise.

### Worktrees vanish
Worktrees get pruned mid-session. Detect (`fatal: not a git repository`),
recreate at origin tip, re-run gates. Do not assume prior verification
still applies. This is distinct from the plan-file orphan detection in
`worktrees.md`: the plan can be intact while the tree it described is gone.

### Tests that cannot fail
A regression test written *after* a fix, which passes both with and without
it, is not coverage. See §6.

### Generated scripts
A `sed`-built script can silently produce a 0-byte file. `bash -n` any
generated script before arming it as a watcher (script review rules:
`tool-usage.md`).

---

## 6. Verification discipline

The standards are in `CLAUDE.md` §4 (verification before done) and §6
(root-cause bug fixing): a regression guard must fail against the pre-fix
code, green tests are not proof the feature works, trace the real
end-to-end path, re-check live state before asserting status. Procedure for
the load-bearing one: revert the fix, run the new test, confirm it FAILS,
restore, confirm it PASSES, and state both outcomes when reporting.

What running this at multi-PR scale adds:

- **Report exit codes.** Empty lint output with a nonzero exit is a failed
  run, not a clean one. An agent reporting "lint clean" with no exit code is
  reporting nothing.
- **Assert the defect, not a proxy.** If the bug is "two identical
  purchases derive different idempotency tokens", assert on the *tokens*,
  not on a field that happens to feed them.
- **Reproduce CI exactly.** Use the CI-pinned linter version; a newer local
  version can have a different bundled ruleset and report a false clean.
- **Pre-existing debt on lines you did not touch**: report it, do not fix it
  in a CR-fix commit, and never suppress it. Some autofixes are actively
  unsafe - a blanket misspell rewrite once renamed a real DB column
  (`cancelled_by`) and broke integration tests.

---

## 7. When everything is blocked

Blocked means every open PR is waiting on an external signal (throttled
review bot, CI in flight, human decision). Then: pick up unresolved GitHub
issues in priority order (`triage.md`) and drive each through this same
pipeline - implement, adversarial review by a *different* agent, CR loop,
merge, close - until every issue is addressed.

Do not idle-poll. Long-running work notifies on completion; polling just
re-blocks the orchestrator (`subagent-strategy.md`,
§"Background-first execution").

---

## 8. Agent briefing template

A cold agent knows nothing. `subagent-strategy.md` covers which tier to
spawn and when to reuse a warm agent instead; this is what every brief must
*contain*:

1. **Goal** and the exact PR/branch/HEAD SHA.
2. **Setup** - which worktree, or how to create one at origin tip.
3. **What is already done**, so it does not re-derive or revert it.
4. **Focus areas, hardest first** - for money paths: idempotency in *both*
   directions (two identical requests → different tokens = double spend;
   two different requests → same token = a real purchase silently skipped),
   fail-closed gates, fabricated values, silent failures, cross-tenant
   bleed.
5. **Method** - adversarial: try to *refute* each candidate first; report
   only survivors; each finding needs a concrete failure scenario
   (inputs → wrong output, in dollars where applicable).
6. **Explicit permission to find nothing.** "NO CONFIRMED FINDINGS" plus
   what was attacked and why each attack failed is a valuable result.
   Without this, agents invent findings to appear useful.
7. **Output shape** and whether it may modify code.

---

## 9. Known gaps

Honest limitations of this harness, to be closed as they bite:

- **"Adversarial review done" is not machine-detectable.** It is inferred
  from PR comments plus timestamps. A structured marker (a label, or a
  fixed comment header) would make it a real gate instead of a heuristic.
- **Reviewer verdicts are trusted.** A reviewer that reports "no findings"
  because it ran out of steam is indistinguishable from a genuine clean.
  Partial mitigation: require it to list what it attacked.
- **Throttle is the pacing constraint.** No orchestration fixes it; a
  serialised central sweep re-triggers, and parallel pings make it worse.
  Check for an existing sweep before adding a trigger.
- **Subsystem pooling is manual.** There is no registry mapping subsystem →
  warm agent; the orchestrator tracks it by hand.
- **Self-review risk on orchestrator-authored code.** When the orchestrator
  has (historically) written code, its review must be delegated to a fresh
  agent. The fix is structural: the orchestrator does not write code.
