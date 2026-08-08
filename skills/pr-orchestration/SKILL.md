---
name: pr-orchestration
description: Driving issues to merged PRs with many agents at once - execution variants, the
  orchestrator/implementer/reviewer split, the handoff contract, concurrency, cost control, the
  four-gate merge check and the agent briefing template. Invoke when orchestrating several PRs.
---

# Multi-agent PR orchestration

The canonical model for driving issues to merged PRs with **multiple agents
working concurrently**: who plays which role, how they coordinate when they
share no memory, what gates a merge, and which failure modes produce a
confident but wrong "done". Every rule here was written after the failure it
prevents actually happened.

The model is **variant-independent**. It has two execution variants (§1):
the interactive one, which this file describes throughout, and the scheduled
one, whose cron-specific mechanics live in the `issue-pr-autopilot` skill.

Everything already owned by another file is cross-referenced, not restated:

| Topic | Owner |
|-------|-------|
| Commit conventions | the `git-commit` skill |
| Review-bot loop | the `cr-loop` skill |
| CI watchers | the `ci-watch` skill |
| Merge mechanics | the `pr-lifecycle` skill |
| Rate limits | the `rate-limit-retry` skill |
| Worktree isolation, staleness/disappearance, crash recovery, post-merge reclaim | the `worktrees` skill |
| Model-tier selection, reviewer independence, subsystem pooling, agent reuse | the `subagent-strategy` skill |
| Peer-session coordination on one repo (locks, sync messages) | the `multi-agent-comms` skill |
| Priority ordering of issues and PRs | the `work-selection` skill |
| Verification standards, adversarial verification | `CLAUDE.md` §4, §6 |
| Testing rules, including asserting the defect | the `coding-standards` skill |
| Matching the CI-pinned tool version | the `tool-usage` skill |
| Cron routines, RemoteTrigger bodies, run budget | the `issue-pr-autopilot` skill |

Paths below use placeholders: `<repo>` is the main checkout, `<wt>` a
worktree root (this repo conventionally puts them under a scratch dir such
as `$TMPDIR/claude/`).

---

## 1. Execution variants

Two ways to run the same model. Pick by which constraints you are under, not
by preference.

| | **Interactive** (this file) | **Scheduled** (the `issue-pr-autopilot` skill) |
|---|---|---|
| Runs as | a local session you are watching | remote cron routines, unattended |
| Subagents | yes, that is the whole design | **no** - a routine is one flat agent, single model, no `Agent` tool |
| Tier split | per-agent, live (top tier reviews, a tier down implements) | across *separate routines* handing off through GitHub state |
| Coordination state | orchestrator context + task list + the `multi-agent-comms` skill locks | GitHub labels only |
| Watchers | background `Agent`s per push and per PR | the next cron fire *is* the watcher |
| Good for | a backlog you are actively working, high-stakes money paths | steady unattended drip, backlogs nobody is watching |

They compose: the scheduled variant can open a PR an interactive session
later takes over, because both drive the same GitHub state. When both are
live on one repo, treat the autopilot as another concurrent actor (§3) and
check its in-flight labels before claiming an issue.

---

## 2. Roles

The main session is an **orchestrator only**. It does not edit code. It
spawns agents, tracks tasks, evaluates merge gates, and reports. Keeping it
free of file contents is what lets it supervise many PRs at once without
its context filling up with diffs.

| Role | Interactive | Scheduled | May edit code? |
|------|-------------|-----------|----------------|
| Orchestrator | main session | *(none - the cron is the scheduler)* | no |
| Planner | an agent, or the orchestrator itself | the planner routine | plan only |
| Implementer | one agent per PR | the worker routine | yes, only its PR's worktree |
| Adversarial reviewer | separate agent | *(gap - see §11)* | no - review only |

**Terminology**: the scheduled variant's "planner" and "worker" are the
planner and implementer roles above, not a different model. Read
planner = the judgement-heavy phase, worker = the mechanical phase. The
split exists for the same reason in both variants - to put top-tier
reasoning only where judgement is the hard part (the `subagent-strategy` skill
§"Routine PR-shipping splits across tiers") - and the tier rubric is the
same one.

**Independence and pooling**: a reviewer must never review code it wrote,
and never re-bless a change it already approved; reviewers are pooled by
subsystem so a warm agent stays independent on a *different* PR. Both rules
live in the `subagent-strategy` skill §"Reuse agents before spawning new ones",
the owner of agent-reuse strategy. The PR-level consequence is the
adversarial-clean merge gate (§5).

### The handoff contract

When the planner and the implementer **do not share a process**, they can
communicate only through **durable state**: the plan is committed rather than
remembered (a plan branch, or `~/.claude/projects/<project>/plans/` per
the `worktrees` skill); the claim is the **first** durable action after it, which is
what shrinks the §3 race window; and the handoff marker is parseable, so the
next actor resolves it mechanically. The scheduled variant's instance (plan
branch + `autopilot-branch:` marker + `plan-ready` label) is in
the `issue-pr-autopilot` skill §"Plan handoff".

---

## 3. Coordination and concurrency

Several actors work the same repo at once, each in its own worktree, and
more than one may push to the *same* PR branch. Interactive sessions can
additionally use the filesystem lock bus in the `multi-agent-comms` skill; that bus
does not exist for scheduled routines, so the rules below assume only what
both variants have.

- **Durable shared state is the coordination substrate.** Issue/PR labels
  plus a parseable marker comment are the only state isolated actors share.
  Each phase acts on a **disjoint slice** of items so phases never fight
  over the same one: unclaimed -> plan; planned-not-implemented ->
  implement; PR open + conflicting -> rebase; PR open + clean -> advance
  review; merged -> stamp done.
- **Claims are NOT atomic locks (the load-bearing caveat).** GitHub has no
  compare-and-swap on labels, so there is a TOCTOU window: two actors
  running AT THE SAME TIME can both read an item as unclaimed before either
  writes the claim, and both act on it (double plan / double PR). This is
  the one real concurrency hazard.
- **Mitigations (all three required):**
  (a) **Ensure no two actors work the same slice simultaneously** - stagger
  scheduled fires so none overlap; in an interactive session, never hand two
  agents the same item.
  (b) **Claim as the FIRST durable action**, per §2's handoff contract, so
  the TOCTOU window is only that tight sequence.
  (c) **Every downstream gate is idempotent** so a lost race degrades
  gracefully: worst case is a duplicate branch, caught at the next claim
  gate and cleaned up rather than shipped twice.
- **Correctness comes from state, not from the clock.** Never gate on "the
  previous step must have finished by now" - an item that is not yet claimed
  is simply picked up later. Timing (cron offsets, agent spawn order) is
  latency tuning only; if timing is load-bearing for correctness, the design
  is wrong.
- **Poison-item guard.** After **N consecutive failures on the same item**
  (3 is a reasonable default), stop retrying it automatically and mark it
  for a human. Without this, one bad item burns a slot on every pass
  forever. Eligibility for new work must exclude items so marked.

### Multi-writer branch hazards

Several actors may push to one PR branch. **Never force-push** - another
session's commits live there; `--force-with-lease` only, per
the `cr-loop` skill. **Always check freshness before pushing** (the `worktrees` skill
§"Staleness and disappearance", and the §7 trap it backs). **Never bare
`git stash`** - the stack is shared across worktrees, so use a WIP commit, or
`git stash push -u -m "<unique-tag>"` and recover by SHA with `apply`, never
`pop`. **Push explicitly**: `git push origin HEAD:<branch>`.

---

## 4. Cost control

Tier selection is owned by the `subagent-strategy` skill (§"Delegate to the
cheapest sufficient tier", §"Routine PR-shipping splits across tiers"); do
not re-derive it here or in the `issue-pr-autopilot` skill. Three controls are
specific to running many items at once:

- **A cheap preflight gate** before any expensive phase, so a quiet backlog
  costs almost nothing.
- **Top-tier reasoning only on the bounded planning and review phases**; the
  long implement and review-response loops run a tier down. This is the
  whole point of the role split in §2.
- **Per-pass caps** on how many new items enter the pipeline, so the number
  of in-flight PRs (and their review loops) stays bounded. The scheduled
  variant's concrete caps and run budget are in the `issue-pr-autopilot` skill.

---

## 5. The merge gate

A PR merges only when **all four** hold. A blanket "merge them" is never
authorization to bypass one.

| Gate | Check | Prevents |
|------|-------|----------|
| CI green | every workflow run `success` for the exact HEAD SHA | merging broken code |
| Review bot clean | 0 unresolved threads **AND** latest review newer than the HEAD push | the false-clean trap (§7) |
| Adversarial clean | an independent agent **returned** a verdict against current HEAD - findings now fixed, or "NO CONFIRMED FINDINGS" plus what it attacked - **and posted it on the PR** (the `cr-loop` skill §3b) | green-CI-but-still-broken, §7, and a verdict that dies with the session |
| Mergeable | `MERGEABLE` + `CLEAN`; no `--admin`, no `--no-verify` | merging past a pending check |

The mergeable gate is the `pr-lifecycle` skill §4 ("never bypass required checks");
this table exists because the other three are easy to skip when several PRs
are in flight at once.

Merge mechanics under multi-PR load: merge **one at a time**, via the REST
endpoint (`gh api PUT .../merge`) rather than `gh pr merge`, with ~30s
spacing plus backoff. Bursting merges trips GitHub's secondary rate limit,
which is distinct from the hourly quota and is handled per
the `rate-limit-retry` skill. Rebase **just in time** - only
immediately before merging that PR. Eager mass-rebasing churns, because each
merge invalidates the next.

---

## 6. The loops

Each runs to a fixed point. "I fixed round 1" is not an exit condition, and
that is the part orchestrating many PRs makes easy to forget.

**Order the work.** Process PRs and issues in triage-priority order
(`priority/p0` first), not by number or by interest. Rubric in the `triage-labels` skill.

**Rebase before reviewing, always.** Findings triaged against a tree that is
not the one under review are wasted at best and misleading at worst.

**Review-bot loop** - full rules in the `cr-loop` skill
(trigger/watcher atomicity, triage buckets, never `@coderabbitai resolve`,
`full review` after a throttle). Orchestration-level: exit is *zero
actionables on the latest review*, and §7 is what makes an apparent exit
untrustworthy.

**Adversarial loop** - reviewer reports, implementer fixes, reviewer
re-reviews, until a round finds nothing, every finding verified live against
current HEAD. Roles and independence per §2.

**The watcher model.** Every trigger of asynchronous work needs something
durable that will act on the response; a trigger with no watcher is the
defect. Interactive sessions arm background agents per push and per PR
*before* or with the trigger, never after (the `pr-lifecycle` skill); scheduled
routines cannot spawn anything, so the next fire is the watcher, with the
"disabling the routine orphans every triggered PR" consequence in
the `issue-pr-autopilot` skill §"Watcher model".

---

## 9. When everything is blocked

Blocked means every open PR is waiting on an external signal (throttled
review bot, CI in flight, human decision). Then: pick up unresolved GitHub
issues in priority order (the `triage-labels` skill) and drive each through this same
pipeline - implement, adversarial review by a *different* agent, review-bot
loop, merge, close - until every issue is addressed.

Do not idle-poll. Long-running work notifies on completion; polling just
re-blocks the orchestrator (the `subagent-strategy` skill
§"Background-first execution").

---

## 10. Agent briefing template

A cold agent knows nothing. the `subagent-strategy` skill covers which tier to
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
6. **That its final message IS the deliverable, and explicit permission to
   find nothing.** "NO CONFIRMED FINDINGS" plus what was attacked and why
   each attack failed is a valuable result; going idle without returning one
   is not a result at all (§7). Without this, agents either invent findings
   to appear useful or stop without reporting.
7. **The start/progress/stop protocol**, stated as three numbered steps so
   it is hard to skip: send a one-line message on **starting** that names the
   SHA under review; a brief **progress** line rather than silence if the
   work runs long; and a **final** message carrying the verdict. Tell it
   plainly never to end a turn holding an unsent verdict, and that a partial
   report naming what it did not reach beats silence. A clean review is the
   only outcome that does not announce itself (§7), so the brief has to make
   it announce itself.
8. **Output shape** and whether it may modify code.
9. **What it must post on the PR before reporting done**, per
   the `cr-loop` skill §3b: a reviewer posts its verdict and the evidence
   under it, an implementer posts what its fix commit changed and how it
   was verified. A result that exists only in the reply to the
   orchestrator is gone the moment the session ends, and the next reader
   of that PR has no way to tell the review happened at all.

---

## 11. Known gaps

Honest limitations of this model, to be closed as they bite:

- **"Adversarial review done" is not machine-detectable.** It is inferred
  from PR comments plus timestamps. A structured marker (a label, or a
  fixed comment header) would make it a real gate instead of a heuristic.
- **Reviewer verdicts are trusted.** A reviewer that reports "no findings"
  because it ran out of steam is indistinguishable from a genuine clean.
  Partial mitigation: require it to list what it attacked.
- **The scheduled variant has no adversarial reviewer at all** (§2): a
  routine cannot spawn one, and a single flat agent reviewing its own work
  violates the independence rule. Scheduled PRs therefore reach the merge
  gate with three of the four checks satisfied, and depend on an interactive
  session or a human for the fourth.
- **Throttle is the pacing constraint.** No orchestration fixes it; a
  serialised central sweep re-triggers, and parallel pings make it worse.
  Check for an existing sweep before adding a trigger.
- **Subsystem pooling is manual.** There is no registry mapping subsystem →
  warm agent; the orchestrator tracks it by hand.
- **Self-review risk on orchestrator-authored code.** When the orchestrator
  has (historically) written code, its review must be delegated to a fresh
  agent. The fix is structural: the orchestrator does not write code.
