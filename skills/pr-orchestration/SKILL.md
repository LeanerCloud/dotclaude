---
name: pr-orchestration
description: >-
  Drive many issues to merged PRs with many agents: roles, coordination, four-gate merge check,
  false-"done" traps. Use when orchestrating several PRs, or before calling multi-agent work done.
---

# Multi-agent PR orchestration

Host mapping (Claude Code: `Agent`, `SendMessage`, `TaskStop`, background Bash; other hosts: `skills/HOSTS.md`).

How to drive issues to merged PRs with **multiple agents working concurrently**: roles, coordination
between actors that share no memory, what gates a merge, and which failure modes produce a confident
but wrong "done". Each rule was written after the failure it prevents.

Two execution variants (§1): interactive (this file) and scheduled (cron mechanics in the
`issue-pr-autopilot` skill, user-invoked via `/issue-pr-autopilot`; agents read
`~/.claude/skills/issue-pr-autopilot/SKILL.md` directly).

Owned elsewhere, cross-referenced not restated:

| Topic | Owner |
|-------|-------|
| Commit conventions | the `git-commit` skill |
| Review-bot loop | the `cr-loop` skill |
| CI watchers | the `ci-watch` skill |
| Merge mechanics | the `pr-lifecycle` skill (project gates: the project's `CLAUDE.md`) |
| Rate limits | the `rate-limit-retry` skill |
| Worktree isolation, staleness/disappearance, crash recovery, post-merge reclaim | the `worktrees` skill |
| Model tiers, reviewer independence, subsystem pooling, agent reuse | the `subagent-strategy` skill |
| Peer-session coordination (messaging, per-repo locks) | the `multi-agent-comms` skill |
| Priority ordering of issues and PRs | the `work-selection` skill |
| Verification standards, adversarial verification | `AGENTS.md` §4, §6 |
| Testing rules, asserting the defect | the `coding-standards` skill |
| Matching the CI-pinned tool version | the `tool-usage` skill |
| Cron routines, RemoteTrigger bodies, run budget | `skills/issue-pr-autopilot/SKILL.md` |

Paths use placeholders: `<repo>` main checkout, `<repo-name>` its directory name, `<wt>` a durable
worktree at `$HOME/.claude/worktrees/<repo-name>-issue-<issue-number>-<slug>` for issue-linked work.
Choose new directory names with the `worktrees` skill, including its no-issue fallback; preserve
existing worktree paths. Never put a worktree that carries
implementation or plan state under `/tmp`, `$TMPDIR`, or another reboot-cleared directory.

---

## 1. Execution variants

Pick by constraint, not preference.

| | **Interactive** (this file) | **Scheduled** (`skills/issue-pr-autopilot/SKILL.md`) |
|---|---|---|
| Runs as | a local session you watch | remote cron routines, unattended |
| Subagents | yes | **no** - one flat agent, single model, no subagent tool |
| Tier split | per-agent, live | across *separate routines* handing off through GitHub state |
| Coordination state | orchestrator context + task list + `multi-agent-comms` locks | GitHub labels only |
| Watchers | background shell watchers per push (CI), subagents per PR (review) | the next cron fire *is* the watcher |
| Good for | an actively worked backlog, high-stakes money paths | steady unattended drip |

They compose: both drive the same GitHub state, so a scheduled PR can be taken over interactively.
When both are live on one repo, treat the autopilot as another concurrent actor (§3) and check its
in-flight labels before claiming an issue. Before planning a claimed issue, challenge it against the
current code (AGENTS.md §1 "Challenge the task first").

---

## 2. Roles

The main session is an **orchestrator only**: it does not edit code. It spawns agents, tracks tasks,
evaluates merge gates and reports; staying free of file contents is what lets it supervise many PRs.

| Role | Interactive | Scheduled | May edit code? |
|------|-------------|-----------|----------------|
| Orchestrator | main session | *(none - the cron schedules)* | no |
| Planner | an agent, or the orchestrator | the planner routine | plan only |
| Implementer | one agent per PR | the worker routine | yes, only its PR's worktree |
| Adversarial reviewer | separate agent | *(gap - §11)* | no |

The scheduled "planner" and "worker" are these roles: planner = judgement phase, worker = mechanical
phase (`subagent-strategy` skill §"Routine PR-shipping splits across tiers"; same tier rubric).

**Independence and pooling**: a reviewer never reviews code it wrote and never re-blesses a change it
already approved; reviewers are pooled by subsystem so a warm agent stays independent on a *different*
PR (`subagent-strategy` skill §"Reuse agents before spawning new ones"). PR-level consequence: the
adversarial-clean gate (§5).

### The handoff contract

Planner and implementer that do not share a process communicate only through **durable state**: the
plan is committed, not remembered (a plan branch, or `~/.claude/projects/<project>/plans/` per the
`worktrees` skill); the claim is the **first** durable action after it (this shrinks the §3 race
window); the handoff marker is parseable. Scheduled instance (plan branch + `autopilot-branch:` marker
+ `plan-ready` label): `skills/issue-pr-autopilot/SKILL.md` §"Plan handoff".

---

## 3. Coordination and concurrency

Several actors work one repo at once, each in its own worktree, and more than one may push to the
*same* PR branch. Interactive sessions also have session messaging and `multi-agent-comms` locks;
scheduled routines do not, so the rules assume only what both have.

- **Durable shared state is the substrate.** Labels plus a parseable marker comment are the only state
  isolated actors share. Each phase acts on a **disjoint slice**: unclaimed -> plan; planned-not-
  implemented -> implement; PR open + conflicting -> rebase; PR open + clean -> advance review; merged
  -> stamp done.
- **Claims are NOT atomic locks.** GitHub has no compare-and-swap on labels: two actors running at the
  same time can both read an item as unclaimed and both act (double plan / double PR).
- **Mitigations (all three required):** (a) no two actors work the same slice simultaneously - stagger
  scheduled fires; interactively never hand two agents the same item. (b) Claim as the FIRST durable
  action (§2). (c) Every downstream gate is idempotent, so a lost race costs at worst a duplicate
  branch, caught at the next claim gate and cleaned up.
- **Correctness comes from state, not the clock.** Never gate on "the previous step must have finished
  by now"; timing is latency tuning only. If timing is needed for correctness, the design is wrong.
- **Poison-item guard.** After **N consecutive failures on the same item** (3 by default) stop retrying
  and mark it for a human; eligibility for new work excludes marked items.

### Multi-writer branch hazards

**Never force-push** (other sessions' commits live there); `--force-with-lease` only, per the
`cr-loop` skill. **Check freshness before pushing** (`worktrees` skill §"Staleness and
disappearance", and the §7 trap). **Never bare `git stash`** - the stack is shared across worktrees;
use a WIP commit, or `git stash push -u -m "<unique-tag>"` and recover by SHA with `apply`, never
`pop`. **Push explicitly**: `git push origin HEAD:<branch>`.

---

## 4. Cost control

Tier selection is owned by `subagent-strategy` (§"Delegate to the cheapest sufficient tier", §"Routine
PR-shipping splits across tiers"); do not re-derive it here or in the autopilot skill.

Every tool call re-sends the agent's whole context, so an agent costs about context size times call
count. A 2026-09-28 burndown that gave each worker 7-10 issues across about 35 agents re-read over 2.7
billion cached tokens (workers grew to about 350K, each later issue costing several times the first).

- **One issue per fresh implementer.** Never batch. Reap it once its deliverable lands (PR
  open, CI terminal). Later rounds on that PR may resume it; a new issue gets a new agent. Standing
  rosters are for reviewers, whose per-round delta is small. Every agent starts at a fixed floor of
  about 75K tokens (system prompt, CLAUDE.md files, memory index, schemas) re-sent on every call, so
  split a task expected to need more than about 80 tool calls into separate fresh agents (migration,
  then rebase, then tests) and keep the always-loaded surface small. Measured: single-PR agents
  started at 75K; a 146-call rework peaked at 238K; 26-50-call reviews peaked at 120-150K.
- **Cap concurrency at 3 to 4 working agents** unless the user asks for more. Background shell
  watchers do not count.
- **Wait in a shell, not an agent**: long waits are one background shell watcher (`ci-watch` skill),
  never a polling loop inside an agent.
- **Keep the orchestrator lean**: ask agents for short verdicts, do not read diffs or logs yourself,
  reap idle agents at their terminal state.
- **A cheap preflight gate** before any expensive phase, so a quiet backlog costs almost nothing.
- **Top-tier reasoning only on bounded planning and review**; implement and review-response loops run
  a tier down (the point of §2's split).
- **Per-pass caps** on new items entering the pipeline. The scheduled variant's caps and run budget
  are in `skills/issue-pr-autopilot/SKILL.md`.

---

## 5. The merge gate

A PR merges only when **all four** hold. The `pr-lifecycle` skill's project gates add to these. A
blanket "merge them" never authorizes bypassing one.

| Gate | Check | Prevents |
|------|-------|----------|
| CI green | every workflow run `success` for the exact HEAD SHA | merging broken code |
| Review bot clean | 0 unresolved threads **AND** latest review newer than the HEAD push | the false-clean trap (§7) |
| Adversarial clean | an independent agent **returned** a verdict against current HEAD - findings now fixed, or "NO CONFIRMED FINDINGS" plus what it attacked - **and the invoking session posted the verdict verbatim on the PR** (`cr-loop` skill §3b) | green-CI-but-still-broken, §7, and a verdict that dies with the session |
| Mergeable | `MERGEABLE` + `CLEAN`; no `--admin`, no `--no-verify` | merging past a pending check |

The mergeable gate is `pr-lifecycle` §4 ("never bypass required checks"); the table exists because
the other three are easy to skip under multi-PR load.

Under multi-PR load merge **one at a time** via the REST endpoint (`gh api PUT .../merge`) rather than
`gh pr merge`, ~30s apart plus backoff; bursts trip GitHub's secondary rate limit (handled per
`rate-limit-retry`). Rebase **just in time**, immediately before merging that PR; eager mass-rebasing
churns because each merge invalidates the next.

The merge driver reclaims right after each merge: `~/.claude/scripts/reclaim-merged-worktrees.sh --apply
--branch <b> --repo <repo>` (`worktrees` skill). It does not wait for the authoring session, which is
usually gone.

---

## 6. The loops

Each runs to a fixed point; "I fixed round 1" is not an exit.

**Order the work** by triage priority (`priority/p0` first), not number or interest (`triage-labels`).

**Rebase before reviewing, always.** Findings triaged against a tree other than the one under review
are wasted or misleading.

**Review-bot loop**: full rules in `cr-loop` (trigger/watcher atomicity, triage buckets, never
`@coderabbitai resolve`, `full review` after a throttle). Orchestration-level: exit is *zero
actionables on the latest review*, and §7 is what makes an apparent exit untrustworthy.

**Adversarial loop**: reviewer reports, implementer fixes, reviewer re-reviews; 2 passes with the same
reviewer per `AGENTS.md` §1 (CR rounds are capped separately, `cr-loop` §3); every finding verified
live against current HEAD. Roles and independence per §2.

**The watcher model.** Every trigger of asynchronous work needs something durable that will act on the
response; a trigger with no watcher is the defect. Interactive sessions arm watchers per push and per
PR *before* or with the trigger (`pr-lifecycle` skill); scheduled routines cannot spawn anything, so
the next fire is the watcher, with the "disabling the routine orphans every triggered PR" consequence
in `skills/issue-pr-autopilot/SKILL.md` §"Watcher model".

---

## 7. Traps

Each has produced a wrong "done".

### Absence read as success
The most repeated defect. **Many tools answer "I could not find it" and "there is nothing" with the
same output**, so a gate that treats absence as a pass must first establish that it *looked
successfully*.

**Compare SHAs, not timestamps, wherever you can.** Where a bot names the SHA it reviewed, compare it
to HEAD directly: a timestamp is an inference about coverage, a SHA is an identity. The table is the
fallback for actors that do not name one (§11).

| Instance | The tell |
|---|---|
| Adversarial reviewer goes idle | no verdict at all; a dead agent reads as clean, a clean one reads as dead |
| Review bot shows 0 unresolved threads | it is paused or throttled, or last round's threads were resolved while the newest commit went unreviewed; compare the latest review to HEAD's `committedDate` |
| A formal review with a fresh timestamp | an EMPTY body; freshness needs a substance test beside it |
| A gate keyed only on `.reviews[]` | the verdict came as a plain comment or an in-place edit of the walkthrough comment (`created_at` unchanged, `updated_at` bumped), so a reviewed commit reads as unreviewed |
| The bot's newest reply | an acknowledgement ("reviewing the current HEAD") has a newer timestamp than the verdict and names no SHA or findings |
| The bot's commit status | `success` while rate-limited (truth only in the description), and `"Review rate limited"` five seconds after a real verdict: wrong in **both** directions, so read the comment stream, never the check |
| `gh run list --commit <short-sha>` | an empty list, not an error; it needs the full 40-char SHA |
| `statusCheckRollup` reading uniformly `PENDING` | it mixes `CheckRun` (`.conclusion`/`.status`) with `StatusContext` (`.state`); a `//` chain over absent fields manufactures the default; use `(.conclusion // .state // .status // "PENDING")` |
| `go test` printing "no tests to run", exit 0 | a `//go:build` tag excluded the file; require a count of tests executed, not an exit code |
| A watcher that ends quietly on timeout | it must print "no verdict - do NOT read this as clean" |

Guards:

- **Classify bot output, do not count it**: action-performed wrapper (keep waiting), rate-limit notice
  (wait the window out, then re-trigger a **full** review; an incremental pass silently skips the
  commits the throttled one dropped), or a verdict naming findings or their absence.
- **Treat an idle agent as *ready to report***, never "done forever" (same root cause as the
  watcher-teardown leak in `pr-lifecycle`, but yielding a wrong verdict). Request the verdict once;
  replace on a second silent idle. Briefing does not fix this (a reviewer told "your final message is
  the deliverable" idled anyway), so the guard is orchestrator-side. Replacing costs everything the
  agent knew, so carry its known leads into the new brief, and do not replace a non-blocking agent
  while higher-priority work is queued.
- **Require reviewers to bracket the work** with a start line, a progress line and a final verdict
  (§10 items 6-7). The attacked-and-failed list makes a clean verdict auditable.

Detection mechanics (which API objects carry a verdict, throttle detection, `full review` recovery)
are owned by `cr-loop`. Enumerate every channel before concluding a review happened.

### Stale adversarial review
A review comment proves nothing unless it postdates the current HEAD commit. Compare timestamps;
re-review the delta otherwise.

### Stale worktree, or no worktree
A long-lived worktree is often a *pre-rebase copy* (same subjects, different SHAs), so tests there
verify code not on the branch. Detection and recovery: `worktrees` skill §"Staleness and
disappearance". A stale or missing worktree invalidates **every gate already run in it**.

### Stale plan
If the base advanced, re-validate the plan against the current tree before implementing; re-plan or
defer. It bites the scheduled variant hardest.

### Tests that cannot fail
A regression test written *after* a fix that passes with and without it is not coverage (§8).

### Generated scripts
A `sed`-built script can silently produce a 0-byte file. `bash -n` any generated script before arming
it as a watcher (review rules: `tool-usage` skill).

### A fix can be defeated by its own remediation path
- Walk a remediation end-to-end **with its own recommendation applied**; a scan keyed on a value the fix tells the operator to change goes blind afterwards.
- Detection scope wider than removal scope leaves the defect detected and surviving. Derive both from one list and re-read the state after mutating it.
- A control the code references may not exist (a referenced deploy environment is auto-created BARE). Verify an external control's live configuration, prefer controls declared in IaC, and state which half of a claim you verified.

### The orchestrator's own status read has a shelf life
In a fast fan-out a state read goes stale in minutes, and the orchestrator is the LAST to know: each
agent holds fresher state about its own PR. Wrong assertions corrode trust (agents start
double-checking everything). Two habits:
- **Re-verify immediately before ASSERTING a state to someone else**, not merely before deciding.
- **Phrase instructions conditionally when the agent owns the state**: "if the README fix has not
  landed yet, fold it in", not "the README fix has not landed".

### A shared adaptive quota makes parallel retry self-defeating
Before giving each agent its own retry loop, ask whether the resource is per-agent or shared; if
shared, retry logic belongs at the orchestrator. The review bot's limit is **per-developer, per-
organization, not per-PR**, and tightens at the 95th percentile of recent volume, so N watchers
retrying independently *ratchet the ceiling down* for everything queued behind them. The shape:
- **One trigger in flight globally.** Serialise re-triggers across the batch, highest-stakes PR first.
  Every other watcher is **observe-only**: it classifies and reports, never posts.
- When the owner lands a verdict, the next PR takes the budget.
- On throttle, wait the window out; a retry before the window both fails and counts.

### Put the guard in the command, not in the memory
A documented trap is not recalled when the command is written: the short-SHA trap fired three times in
one day across different agents. Make the mistake impossible to express; ship the assertion in the
template:

```bash
[ ${#SHA} -eq 40 ] || { echo "need full 40-char SHA"; exit 1; }
```

Recurrence across independent agents means a documentation-based mitigation has failed; encode the
constraint where the mistake is made (a script precondition, a required argument, a wrapper).

### A push and its PR are not atomic
A branch can be pushed and its PR never opened (the session dies in the gap), leaving finished work on
origin that no priority query or reviewer sees. For each branch you pushed, confirm a PR exists.
Before re-implementing anything, check origin for a branch that already contains it (one p0 security
fix was recovered this way, with its breaking-change note).

### Correlated agent failure
Per-agent guards assume independent failure. That broke when **ten agents died within five minutes**
on a shared account-level session limit; what survived was committed or pushed, and what was nearly
lost sat *uncommitted in a worktree* or *only in the orchestrator's context*. Guards:

- **Checkpoint before you cannot**: "commit and push" is the unit of progress.
- **Durable state beats conversational state**: findings, assignments and gate status belong in the
  task list or a file (§11).
- **On mass failure, survey before restarting anything**: per worktree, record HEAD, uncommitted files,
  and whether HEAD is on origin. Back uncommitted diffs up to patch files (`git diff > <path>.patch`),
  not WIP commits (those confuse the resuming agent; a pruned worktree loses the work outright).
- **Resume from the patch, never re-implement.**
- **Assess the recovered work's COMPLETENESS first.** Unfinished work does not announce itself: one
  recovered double-spend fix had a rationale comment on its first half, no second half and no tests.
  **A partial fix is more dangerous than no fix, because it looks finished.** The resuming agent's
  first task is to state what it found, what it kept, and what was missing.

---

## 8. Verification discipline

Standards: `AGENTS.md` §4 and §6 (a regression guard must fail against the pre-fix code, green tests
are not proof, trace the real end-to-end path, re-check live state before asserting status). The
procedure: revert the fix, run the new test, confirm it FAILS, restore, confirm it PASSES, state both
outcomes. "Assert the defect, not a proxy" is in `coding-standards` §"Testing Philosophy"; matching the
CI-pinned tool version is in `tool-usage`.

What multi-PR scale adds:

- **Report exit codes.** Empty lint output with a nonzero exit is a failed run; "lint clean" with no
  exit code reports nothing.
- **Pre-existing debt on lines you did not touch**: report it, do not fix it in a review-fix commit,
  never suppress it. Some autofixes are unsafe (a blanket misspell rewrite renamed a real DB column,
  `cancelled_by`, and broke integration tests).
- **Prove "pre-existing"**: run the linter on the branch and on a reverted baseline, normalise both
  finding sets and diff. The same technique settles "is this test failure mine?".
- **When adding a requirement, ask what the most permissive value satisfying it is.** If that is as
  bad as omission, the requirement achieves nothing (a non-empty check is not a restriction; a shape
  regex admits every value of that shape).
- **Applying one change to N parallel sites: verify each site by running it, not by reading the
  diff.** The eye supplies what it expects across near-identical blocks.
- **Check the new code is REACHABLE from a production entry point.** A feature can be implemented,
  tested and **never wired up** and pass every gate. Answer from a real handler, scheduler, CLI
  command or consumer what makes the new line execute.
- **Ask which layer the test exercises.** One test fired 50 direct calls at a service function and
  conserved counts, while the defect was that the function runs *twice per request* from two layers.
- **Verification is self-reported unless the reviewer re-runs it.** Requiring pre-fix output
  **verbatim** deters fabrication but is not a check. The check: **the round-2 reviewer re-derives the
  failure itself** (`git checkout <fix-commit>^ -- <changed-file>`, run the guard, confirm it fails on
  the specific assertions claimed, restore, confirm clean). Make it standing in the round-2 brief. It
  also catches a guard that fails pre-fix for the **wrong reason** (an unwired mock chain panicking
  into an error that satisfied `require.Error`), so assert the specific observable.

---

## 9. When everything is blocked

Blocked = every open PR waits on an external signal (throttled bot, CI in flight, human decision).
Then pick up unresolved issues in priority order (`triage-labels`) and drive each through the same
pipeline (implement, adversarial review by a *different* agent, review-bot loop, merge, close) until
all are addressed. Do not idle-poll; long-running work notifies on completion (`subagent-strategy`
skill §"Background-first execution").

---

## 10. Agent briefing template

A cold agent knows nothing. `subagent-strategy` covers tier and warm-agent reuse; every brief must
*contain*:

1. **Goal** and the exact PR/branch/HEAD SHA.
2. **Setup**: which worktree, or how to create one at origin tip.
3. **What is already done**, so it does not re-derive or revert it.
4. **Focus areas, hardest first**. For money paths: idempotency in *both* directions (two identical
   requests giving different tokens = double spend; two different requests giving the same token = a
   real purchase silently skipped), fail-closed gates, fabricated values, silent failures,
   cross-tenant bleed.
5. **Method**: adversarial. Try to *refute* each candidate first; report only survivors; each finding
   needs a concrete failure scenario (inputs -> wrong output, in dollars where applicable).
6. **That its final message IS the deliverable, and explicit permission to find nothing.** "NO
   CONFIRMED FINDINGS" plus what was attacked and why each attack failed is a valuable result; going
   idle without one is not a result (§7). Otherwise agents invent findings or stop without reporting.
7. **The start/progress/stop protocol** as three numbered steps: a one-line message on **starting**
   naming the SHA under review; a brief **progress** line rather than silence on long work; a **final**
   message carrying the verdict. Tell it never to end a turn holding an unsent verdict; a partial
   report naming what it did not reach beats silence. A clean review is the only outcome that does not
   announce itself (§7), so the brief must make it announce itself.
8. **Output shape** and whether it may modify code.
9. **What must be recorded on the PR before reporting done** (`cr-loop` skill §3b): a read-only
   reviewer returns verdict and evidence and the invoking session posts them verbatim; an implementer
   posts what its fix commit changed and how it was verified. A result that exists only in the reply to
   the orchestrator is gone when the session ends.

---

## 11. Known gaps

- **"Adversarial review done" is not machine-detectable**; it is inferred from comments plus
  timestamps. A structured marker (a label or fixed comment header) would make it a real gate.
- **Reviewer verdicts are trusted.** A "no findings" from an agent that ran out of steam is
  indistinguishable from a genuine clean. Partial mitigation: require the list of what it attacked.
- **The scheduled variant has no adversarial reviewer** (§2): a routine cannot spawn one, and a flat
  agent reviewing its own work violates independence. Scheduled PRs reach the merge gate with three of
  four checks and depend on an interactive session or a human for the fourth.
- **Throttle is the pacing constraint.** No orchestration fixes it; a serialised central sweep
  re-triggers, and parallel pings make it worse. Check for an existing sweep before adding a trigger.
- **Subsystem pooling is manual**: no registry maps subsystem to warm agent.
- **Self-review risk on orchestrator-authored code**: its review must go to a fresh agent. The
  structural fix is that the orchestrator does not write code.
