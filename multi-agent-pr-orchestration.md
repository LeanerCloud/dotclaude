# Multi-agent PR orchestration

The canonical model for driving issues to merged PRs with **multiple agents
working concurrently**: who plays which role, how they coordinate when they
share no memory, what gates a merge, and which failure modes produce a
confident but wrong "done". Every rule here was written after the failure it
prevents actually happened.

The model is **variant-independent**. It has two execution variants (§1):
the interactive one, which this file describes throughout, and the scheduled
one, whose cron-specific mechanics live in `issue-pr-autopilot.md`.

Everything already owned by another file is cross-referenced, not restated:

| Topic | Owner |
|-------|-------|
| Commit conventions, review-bot loop, CI watchers, merge mechanics, rate limits | `git-workflow.md` |
| Worktree isolation, staleness/disappearance, crash recovery, post-merge reclaim | `worktrees.md` |
| Model-tier selection, reviewer independence, subsystem pooling, agent reuse | `subagent-strategy.md` |
| Peer-session coordination on one repo (locks, sync messages) | `multi-agent-comms.md` |
| Priority ordering of issues and PRs | `triage.md` |
| Verification standards, adversarial verification | `CLAUDE.md` §4, §6 |
| Testing rules, including asserting the defect | `coding-standards.md` |
| Matching the CI-pinned tool version | `tool-usage.md` |
| Cron routines, RemoteTrigger bodies, run budget | `issue-pr-autopilot.md` |

Paths below use placeholders: `<repo>` is the main checkout, `<wt>` a
worktree root (this repo conventionally puts them under a scratch dir such
as `$TMPDIR/claude/`).

---

## 1. Execution variants

Two ways to run the same model. Pick by which constraints you are under, not
by preference.

| | **Interactive** (this file) | **Scheduled** (`issue-pr-autopilot.md`) |
|---|---|---|
| Runs as | a local session you are watching | remote cron routines, unattended |
| Subagents | yes, that is the whole design | **no** - a routine is one flat agent, single model, no `Agent` tool |
| Tier split | per-agent, live (top tier reviews, a tier down implements) | across *separate routines* handing off through GitHub state |
| Coordination state | orchestrator context + task list + `multi-agent-comms.md` locks | GitHub labels only |
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
reasoning only where judgement is the hard part (`subagent-strategy.md`
§"Routine PR-shipping splits across tiers") - and the tier rubric is the
same one.

**Independence and pooling**: a reviewer must never review code it wrote,
and never re-bless a change it already approved; reviewers are pooled by
subsystem so a warm agent stays independent on a *different* PR. Both rules
live in `subagent-strategy.md` §"Reuse agents before spawning new ones",
the owner of agent-reuse strategy. The PR-level consequence is the
adversarial-clean merge gate (§5).

### The handoff contract

When the planner and the implementer **do not share a process**, they can
communicate only through **durable state**: the plan is committed rather than
remembered (a plan branch, or `~/.claude/projects/<project>/plans/` per
`worktrees.md`); the claim is the **first** durable action after it, which is
what shrinks the §3 race window; and the handoff marker is parseable, so the
next actor resolves it mechanically. The scheduled variant's instance (plan
branch + `autopilot-branch:` marker + `plan-ready` label) is in
`issue-pr-autopilot.md` §"Plan handoff".

---

## 3. Coordination and concurrency

Several actors work the same repo at once, each in its own worktree, and
more than one may push to the *same* PR branch. Interactive sessions can
additionally use the filesystem lock bus in `multi-agent-comms.md`; that bus
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
`git-workflow.md`. **Always check freshness before pushing** (`worktrees.md`
§"Staleness and disappearance", and the §7 trap it backs). **Never bare
`git stash`** - the stack is shared across worktrees, so use a WIP commit, or
`git stash push -u -m "<unique-tag>"` and recover by SHA with `apply`, never
`pop`. **Push explicitly**: `git push origin HEAD:<branch>`.

---

## 4. Cost control

Tier selection is owned by `subagent-strategy.md` (§"Delegate to the
cheapest sufficient tier", §"Routine PR-shipping splits across tiers"); do
not re-derive it here or in `issue-pr-autopilot.md`. Three controls are
specific to running many items at once:

- **A cheap preflight gate** before any expensive phase, so a quiet backlog
  costs almost nothing.
- **Top-tier reasoning only on the bounded planning and review phases**; the
  long implement and review-response loops run a tier down. This is the
  whole point of the role split in §2.
- **Per-pass caps** on how many new items enter the pipeline, so the number
  of in-flight PRs (and their review loops) stays bounded. The scheduled
  variant's concrete caps and run budget are in `issue-pr-autopilot.md`.

---

## 5. The merge gate

A PR merges only when **all four** hold. A blanket "merge them" is never
authorization to bypass one.

| Gate | Check | Prevents |
|------|-------|----------|
| CI green | every workflow run `success` for the exact HEAD SHA | merging broken code |
| Review bot clean | 0 unresolved threads **AND** latest review newer than the HEAD push | the false-clean trap (§7) |
| Adversarial clean | an independent agent **returned** either concrete findings that are now fixed, or an explicit "NO CONFIRMED FINDINGS" plus what it attacked, verified against current HEAD, **and posted that verdict on the PR** (`git-workflow.md` §3b) | green-CI-but-still-broken, the idle-is-not-a-verdict trap (§7), and a verdict that dies with the session |
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

## 6. The loops

Each runs to a fixed point. "I fixed round 1" is not an exit condition, and
that is the part orchestrating many PRs makes easy to forget.

**Order the work.** Process PRs and issues in triage-priority order
(`priority/p0` first), not by number or by interest. Rubric in `triage.md`.

**Rebase before reviewing, always.** Findings triaged against a tree that is
not the one under review are wasted at best and misleading at worst.

**Review-bot loop** - full rules in `git-workflow.md` §"Post-PR review loop"
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
*before* or with the trigger, never after (`git-workflow.md`); scheduled
routines cannot spawn anything, so the next fire is the watcher, with the
"disabling the routine orphans every triggered PR" consequence in
`issue-pr-autopilot.md` §"Watcher model".

---

## 7. Traps

Each of these has produced a wrong "done" in practice.

### Absence read as success
The most repeated defect in this model, in many costumes. **Many tools answer
"I could not find it" and "there is nothing" with the same output**, so any
gate that treats an absence as a pass must first establish that it *looked
successfully*. Every other outcome announces itself; this one has to be made
to.

| Instance | The tell |
|---|---|
| Adversarial reviewer goes idle | no verdict at all, and it cuts both ways: a dead agent reads as clean, a clean one reads as dead |
| Review bot shows 0 unresolved threads | it is paused or throttled, or last round's threads were resolved while the newest commit went unreviewed; compare the latest review to HEAD's `committedDate` |
| A formal review with a fresh timestamp | an EMPTY body; freshness needs a substance test beside it |
| A gate keyed only on `.reviews[]` | the verdict came as a plain comment, or an in-place edit of the walkthrough comment (`created_at` unchanged, `updated_at` bumped), so a reviewed commit reads as unreviewed |
| The bot's newest reply | an acknowledgement ("reviewing the current HEAD") carries a newer timestamp than the verdict, and names no SHA and no findings |
| The bot's commit status | `success` while rate-limited (truth only in the description), and `"Review rate limited"` five seconds after a real verdict: wrong in **both** directions, so read the comment stream, never the check |
| `gh run list --commit <short-sha>` | an empty list rather than an error; it needs the full 40-char SHA |
| `statusCheckRollup` reading uniformly `PENDING` | it mixes `CheckRun` (`.conclusion`/`.status`) with `StatusContext` (`.state`), and a `//` chain over absent fields manufactures the default; use `(.conclusion // .state // .status // "PENDING")` |
| `go test` printing "no tests to run", exit 0 | a `//go:build` tag excluded the file; require a count of tests executed, not an exit code |
| A watcher that ends quietly on timeout | it must print "no verdict - do NOT read this as clean" |

Guards:

- **Classify bot output, do not count it.** Sort each response into
  action-performed wrapper (keep waiting), rate-limit notice (wait the window
  out, then re-trigger a **full** review, since an incremental pass silently
  skips the commits the throttled one dropped), or a verdict naming findings or
  their absence. Timestamp- and count-based tests all fail here.
- **Treat an idle agent as *ready to report*.** Request the verdict once;
  replace on a second silent idle. Briefing does not fix this - a reviewer
  respawned with "your final message is the deliverable" at the top of its
  brief idled without reporting anyway - so the guard is orchestrator-side.
  Replacing costs everything that agent knew, so carry its known leads into the
  new brief, and do not replace a non-blocking agent while higher-priority work
  is queued.
- **Require reviewers to bracket the work**: a start line naming the SHA under
  review, a progress line rather than silence if it runs long, and a final
  verdict that when clean says "NO CONFIRMED FINDINGS" and lists what was
  attacked and why each attack failed (§10, items 6-7). That list is what makes
  a clean verdict auditable; without it, "no findings" is indistinguishable
  from "did not look".

Detection mechanics - which API objects carry a verdict, throttle detection,
the `full review` recovery - are owned by `git-workflow.md` §"Post-PR review
loop". Enumerate every channel before concluding a review happened.

### Stale adversarial review
An adversarial-review comment on a PR proves nothing unless it postdates
the current HEAD commit. Compare timestamps; re-review the delta otherwise.

### Stale worktree, or no worktree
A long-lived worktree is often a *pre-rebase copy* - same commit subjects,
different SHAs - so tests there verify code that is not on the branch.
Detection and recovery are in `worktrees.md` §"Staleness and disappearance".
The orchestration-level rule: a worktree that turns out stale or missing
invalidates **every gate already run in it**.

### Stale plan
A plan rots while it waits. If the base advanced, re-validate the plan against
the current tree before implementing, and re-plan or defer rather than build
on it. The longer the gap between planning and implementing, the more this
matters, which is why it bites the scheduled variant hardest.

### Tests that cannot fail
A regression test written *after* a fix, which passes both with and without
it, is not coverage. See §8.

### Generated scripts
A `sed`-built script can silently produce a 0-byte file. `bash -n` any
generated script before arming it as a watcher (script review rules:
`tool-usage.md`).

### A fix can be defeated by its own remediation path
Three shapes, two of them from one script closing an over-broad IAM grant:

- **A scan scoped by a value the fix tells the operator to change.** Its
  search for the legacy grant keyed on the *current* pool ID, so following the
  script's own "migrate to a dedicated pool" advice rebuilt that string for the
  NEW pool and the wildcard grant went unseen, exit 0. Walk a remediation
  end-to-end **with its own recommendation applied**: the interesting failure
  is rarely "the fix doesn't work", it is "the fix doesn't work for people who
  followed the advice".
- **Detection scope wider than removal scope**, which leaves the defect
  *detected and surviving* - worse than not scanning, because the check reports
  it looked. Derive both from one list, and **re-read the state after mutating
  it**.
- **A control the code REFERENCES may not EXIST.** A fix rested on a
  deployment environment's approval gate; those environments did not exist, and
  the platform auto-creates a referenced environment BARE on first use, so the
  YAML was correct and the control vacuous. Verify an external control's live
  configuration rather than its reference in code, prefer controls declared in
  IaC, and state which half of a claim you verified.

### The orchestrator's own status read has a shelf life
In a fast-moving fan-out, a state read goes stale in minutes, and the
orchestrator is structurally the LAST to know: each agent holds fresher
state about its own PR than the orchestrator ever can, because it is the
thing changing it.

Observed: the orchestrator queried a PR head, composed an instruction, and
told the agent that a fix "has not landed" - a push had arrived in the gap.
The agent had to spend a round correcting it. Harmless once; corrosive if
repeated, because an agent that receives confidently wrong state starts
double-checking everything the orchestrator says.

Two habits:
- **Re-verify immediately before ASSERTING a state to someone else**, not
  merely before deciding. Deciding on a 3-minute-old read is usually fine;
  telling an agent its work is missing is not.
- **Phrase instructions conditionally when the agent owns the state**:
  "if the README fix has not landed yet, fold it in" rather than "the README
  fix has not landed". The conditional is correct under both readings and
  costs nothing.

### A shared adaptive quota makes parallel retry self-defeating
The obvious design - each PR's watcher re-triggers its own review on
throttle - is actively wrong when the quota is shared, and catastrophically
wrong when it is also adaptive.

Established in practice: the review bot's limit is **per-developer, per-
organization, not per-PR**, and it tightens at the 95th percentile of recent
volume. So N watchers retrying independently do not merely compete for one
allowance, they *ratchet the ceiling down* for everything queued behind
them. A fan-out that looks like N independent loops is one shared resource
being contended by N clients that cannot see each other.

The correct shape:
- **One trigger in flight globally.** Serialise re-triggers across the whole
  batch, highest-stakes PR first. Every other watcher is **observe-only** -
  it classifies and reports, and never posts.
- When the owner lands a verdict, the next PR takes the budget.
- On throttle, wait the window out rather than retrying sooner; a retry
  before the window both fails and counts.

Generalise beyond review bots: **before giving each agent its own retry
loop, ask whether the resource is per-agent or shared.** If shared, retry
logic belongs at the orchestrator, not in the workers - each worker
observing and reporting, with one of them holding the token. Independent
backoff across N clients on one adaptive limit is a thundering herd with
extra steps.

### Put the guard in the command, not in the memory
A documented trap only helps if it is recalled at the instant the command is
written, and that is exactly when it is not: the short-SHA trap above fired
**three times in one day, across different agents**, despite being written
down, each time because the note was read at session start and the command was
composed an hour later. Make the mistake impossible to express rather than
memorable to avoid - ship the assertion inline in the template:

```bash
[ ${#SHA} -eq 40 ] || { echo "need full 40-char SHA"; exit 1; }
```

**Recurrence across independent agents is evidence that a documentation-based
mitigation has failed**, not evidence that the documentation needs to be
louder. Encode the constraint where the mistake is made: a precondition in the
script, a required argument, a wrapper that rejects the bad shape.

### A push and its PR are not atomic
A branch can be pushed and its PR never opened - the authoring session dies in
the gap - leaving complete, reviewed-quality work on origin that is invisible
to every priority query and every reviewer. This is distinct from the
merged-branch stranding case in `git-workflow.md`: the branch is live, the
work is finished, and nothing points at it.

Recovery: for each branch you pushed, confirm a PR exists. Before
re-implementing anything, check origin for a branch that already contains it -
one p0 security fix was recovered this way, complete with a breaking-change
note, when re-implementation would have thrown it away.

### Correlated agent failure
Every per-agent guard assumes agents fail independently. That broke when **ten
agents died within five minutes** on a shared account-level session limit -
every implementer, every reviewer, and the agent holding the only copy of
several lessons. What survived was already committed or pushed; what was
nearly lost sat *uncommitted in a worktree*, or *only in the orchestrator's
context*. Guards:

- **Checkpoint before you cannot.** "Commit and push" is the unit of progress;
  an agent working a long time without committing carries unrecoverable state.
- **Durable state beats conversational state.** Findings, assignments and gate
  status belong in the task list or a file (§11).
- **On mass failure, survey before restarting anything**: for every worktree,
  record HEAD, uncommitted files, and whether HEAD is on origin. Back
  uncommitted diffs up to patch files (`git diff > <path>.patch`) rather than
  committing them - a WIP commit confuses the agent that resumes there, while a
  pruned worktree loses the work outright.
- **Resume from the patch, never re-implement**, which discards work that
  already exists and reintroduces decisions already made correctly.
- **But assess the recovered work's COMPLETENESS first.** What a dead agent
  leaves is a snapshot of *unfinished* work, and unfinished work does not
  announce itself: one recovered double-spend fix had a well-argued rationale
  comment on its first half, no second half, and no tests, so committing that
  worktree as-is would have shipped a plausible-looking fix that still
  double-spends. **A partial fix is more dangerous than no fix, because it
  looks finished.** The resuming agent's first task is to state what it found,
  what it kept, and what was missing.

---

## 8. Verification discipline

The standards are in `CLAUDE.md` §4 (verification before done) and §6
(root-cause bug fixing): a regression guard must fail against the pre-fix
code, green tests are not proof the feature works, trace the real
end-to-end path, re-check live state before asserting status. Procedure for
the load-bearing one: revert the fix, run the new test, confirm it FAILS,
restore, confirm it PASSES, and state both outcomes when reporting. The
"assert the defect, not a proxy" rule is in `coding-standards.md`
§"Testing Philosophy"; matching the CI-pinned tool version before trusting
a local lint or format result is in `tool-usage.md`.

What running this at multi-PR scale adds:

- **Report exit codes.** Empty lint output with a nonzero exit is a failed
  run, not a clean one. An agent reporting "lint clean" with no exit code is
  reporting nothing.
- **Pre-existing debt on lines you did not touch**: report it, do not fix it
  in a review-fix commit, and never suppress it. Some autofixes are actively
  unsafe - a blanket misspell rewrite once renamed a real DB column
  (`cancelled_by`) and broke integration tests.
- **Prove "pre-existing" instead of asserting it.** Run the linter on the
  branch, again against a reverted baseline, normalise both finding sets and
  diff. Minutes, and it turns the easiest claim in code review to make and the
  hardest to check into a measurement. Same technique settles "is this test
  failure mine?".
- **Applying one change to N parallel sites: verify each site by running it,
  not by reading the diff.** Reading N near-identical blocks is where the eye
  supplies what it expects, so the replication is the part most likely to be
  incomplete and the part invisible to whoever made it.
- **Check the new code is REACHABLE from a production entry point.** A feature
  can be fully implemented, fully tested and **never wired up** - a state that
  passes every gate, because CI is green, the review bot sees clean code, and
  an adversarial reviewer finds no bugs in code that never runs. Ask what would
  have to happen at runtime for the new line to execute, and answer it from a
  real handler, scheduler, CLI command or consumer.
- **Ask which layer the test exercises.** A test can prove the unit beneath the
  defect and nothing else: one fired 50 direct calls at a service function and
  correctly conserved counts, while the defect was that the function runs
  *twice per request* from two layers, so it passed unchanged with the bug
  present and looked like diligence.
- **Verification is self-reported unless the reviewer re-runs it.** Requiring
  the pre-fix output **verbatim** makes fabrication effortful, but it is a
  deterrent, not a check. The check: **the round-2 reviewer re-derives the
  failure itself** - `git checkout <fix-commit>^ -- <changed-file>`, run the
  guard, confirm it fails on the specific assertions claimed, restore, confirm
  clean. Make it standing in the round-2 brief. It also catches a guard that
  fails pre-fix for the **wrong reason** (an unwired mock chain panicking into
  an error that satisfied `require.Error`), so assert the specific observable
  rather than "an error occurred".

---

## 9. When everything is blocked

Blocked means every open PR is waiting on an external signal (throttled
review bot, CI in flight, human decision). Then: pick up unresolved GitHub
issues in priority order (`triage.md`) and drive each through this same
pipeline - implement, adversarial review by a *different* agent, review-bot
loop, merge, close - until every issue is addressed.

Do not idle-poll. Long-running work notifies on completion; polling just
re-blocks the orchestrator (`subagent-strategy.md`
§"Background-first execution").

---

## 10. Agent briefing template

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
   `git-workflow.md` §3b: a reviewer posts its verdict and the evidence
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
