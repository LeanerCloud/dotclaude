# Multi-agent PR orchestration

The canonical model for driving issues to merged PRs with **multiple agents
working concurrently**: who plays which role, how they coordinate when they
share no memory, what gates a merge, and which failure modes produce a
confident but wrong "done". Every rule here was written after the failure it
prevents actually happened, so treat §7 as incident reports rather than
theory.

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

Whenever the planner and the implementer **do not share a process** - a
scheduled routine handing off to the next fire, or an interactive
orchestrator whose planning agent has already returned - they can only
communicate through **durable state**. Three rules make that reliable:

1. **The plan is committed, not remembered.** Write it where the next actor
   will find it (a plan branch, or `~/.claude/projects/<project>/plans/`
   per `worktrees.md`), not in an agent's context.
2. **The claim is the first durable action** after the plan lands, posted as
   a tight sequence with it. This is what shrinks the race window in §3.
3. **The handoff marker is parseable**, so the next actor resolves it
   mechanically rather than by re-deriving what to do.

The scheduled variant's concrete instance of this contract (plan branch +
`autopilot-branch:` marker comment + `plan-ready` label) is in
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

```
main
 └── <feature-branch> ← PR #N ← possibly several actors
```

- **Never force-push.** Another session's commits live on that branch. When
  a rebase genuinely requires it, `--force-with-lease` only, per
  `git-workflow.md`.
- **Always check freshness before pushing** - see `worktrees.md`
  §"Staleness and disappearance", and the §7 trap it backs.
- **Never bare `git stash`** - the stash stack is shared across worktrees,
  so a bare `pop` can restore another session's work into yours. Use a WIP
  commit, or `git stash push -u -m "<unique-tag>"` and recover by SHA with
  `apply`, never `pop`.
- **Push explicitly**: `git push origin HEAD:<branch>`, never bare
  `git push`.

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
| Adversarial clean | an independent agent **returned** either concrete findings that are now fixed, or an explicit "NO CONFIRMED FINDINGS" plus what it attacked, verified against current HEAD | green-CI-but-still-broken, and the idle-is-not-a-verdict trap (§7) |
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

**Rebase before reviewing, always.** Never address review findings on a
conflicting or stale tree: resolve conflicts first, then advance the review.
Findings triaged against a tree that is not the one under review are wasted
at best and misleading at worst.

**Review-bot loop** - full rules in `git-workflow.md` §"Post-PR review loop"
(trigger/watcher atomicity, the triage buckets, never `@coderabbitai
resolve`, `full review` after a throttle). Orchestration-level: exit is
*zero actionables on the latest review*, and §7's false-clean trap is what
makes an apparent exit untrustworthy.

**Adversarial loop** - reviewer reports, implementer fixes, reviewer
re-reviews, repeat until a round finds nothing. Findings must be verified
live against current HEAD, not taken on faith. Roles and independence per
§2.

**The watcher model.** Every trigger that kicks off asynchronous work needs
something durable that will act on the response; a trigger with no watcher
is the defect. What plays that role differs by variant: interactive sessions
spawn background agents per push and per PR (`git-workflow.md`
§"Post-push CI watcher" and §"Post-PR review loop"), and must arm them
*before* or with the trigger, never after the fact. Scheduled routines
cannot spawn anything, so the next fire is the watcher - those mechanics,
and the "disabling the routine orphans every triggered PR" consequence, are
in `issue-pr-autopilot.md` §"Watcher model".

---

## 7. Traps

Each of these has produced a wrong "done" in practice.

### An idle agent is not a completed review
Two adversarial reviewers once emitted an `idle_notification` with reason
"available" and returned **no findings at all**. Reading that as "done,
nothing found" would have recorded a PR as having passed adversarial review
when the review never reported. This is the same shape as the false-clean
below: **an absence of output read as an absence of problems.**

Same root cause as the watcher-teardown leak in `git-workflow.md`
§"Post-PR review loop" - an idling agent is saying "finished this turn,
available for more," never "done forever" - but the consequence here is a
wrong verdict rather than a leaked background agent. Guards:

- When an agent goes idle without a verdict, the orchestrator **explicitly
  requests the verdict** rather than inferring one. Treat idle as *ready to
  report*, not as failure.
- **Replace threshold**: request once; if a *second* idle follows with no
  content, replace the agent. Asking a third time is the
  retry-the-same-failing-action trap.
- **Replacing costs everything that agent knew.** A replaced reviewer's
  findings are gone. One left only a scratch filename hinting at what it had
  been investigating, and the replacement had to be told to re-derive that
  specific lead. Carry known leads forward explicitly in the new brief.
- **Do not replace a non-blocking agent while higher-priority work is
  queued.** A discovery/sweep agent that blocks nothing competes for capacity
  with queued p0 work; reallocate rather than replace.
- A PR counts as adversarially reviewed only once a reviewer has **returned**
  either concrete findings or an explicit "NO CONFIRMED FINDINGS" plus what
  it attacked (§5, adversarial gate; §10, item 6).

### A clean review must be reported, not merely finish
The hardest state to detect is a reviewer that found nothing, because
"nothing found" and "nothing done" produce identical silence. Every other
outcome announces itself; this one has to be made to.

Require reviewers to bracket the work explicitly:

- **Start**: one line on beginning, naming the PR and the **SHA** being
  reviewed. This alone separates "running" from "never started", which no
  amount of waiting otherwise distinguishes, and pins what the eventual
  verdict covers.
- **Progress**: a brief line if the review runs long, rather than silence.
- **Stop**: a final message stating the outcome. When that outcome is
  nothing, it must say so *explicitly* - "NO CONFIRMED FINDINGS" - and list
  **what was attacked and why each attack failed**.

The attacked-and-failed list is what makes a clean verdict auditable. Without
it, "no findings" is indistinguishable from "did not look", and the
orchestrator has no basis to decide whether the gate was really satisfied.
With it, a clean review is often *more* informative than a noisy one: the
strongest verdict in one batch listed the delimiter-injection, scope-
divergence, gate-bypass and region-collapse attacks it had mounted and
refuted, which told the orchestrator far more about the code's safety than
a list of nitpicks would have.

State plainly in the brief that **finding nothing is a complete and
acceptable result**, and that inventing findings to appear useful is worse
than finding none. Without that permission, agents pad reports with style
nitpicks to look diligent, which buries any real finding and trains the
orchestrator to skim.

Two failure modes this closes, both observed:

- A reviewer finishing silently and its clean result being read as a dead
  agent - so the PR gets re-reviewed from scratch, wasting the work.
- A dead agent being read as a clean result - so a PR is recorded as having
  passed adversarial review that never reported at all. That direction is
  the dangerous one.

**A diagnosis that looked right and was wrong**, recorded because it cost a
working agent: the first explanation for the silence was that the briefs
lacked a "your final message is the deliverable" clause, and the proposed fix
was to templatise that clause. A reviewer was then respawned *with that clause
stated at the top of its brief* - and idled without reporting anyway. Across
five reviewers: one delivered unprompted, two delivered after one request, two
never delivered no matter how they were asked, including one that ignored a
deliberately minimal "just give me four lines" request. The fix is
orchestrator-side, not briefing-side. The clause is still worth having; it is
simply not what was causing the silence.

### False-clean review bot
Zero unresolved threads can mean the bot is **paused or throttled**, or that
the previous round's threads were resolved while the newest commit was never
reviewed. **Always compare the latest review against the HEAD commit time.**
An older review does not cover the newer commit. Throttle detection and the
`full review` recovery are in `git-workflow.md` §"Post-PR review loop" §2.

### The bot delivers verdicts through more than one channel
This one cuts **both ways**, and the second direction is easy to miss.

CodeRabbit emits outcomes as at least three different objects: a formal
review (`.reviews[]`, has `submittedAt`), a plain issue comment
(`.comments[]`, has `createdAt`), and an *in-place edit* of its existing
walkthrough comment (`created_at` unchanged, `updated_at` bumped). A gate
that reads only one of these is wrong in a way that depends on which one it
missed:

- **False clean** - keying on comment creation misses reviews delivered by
  in-place edit, so an unreviewed commit looks reviewed.
- **False stale** - keying only on `.reviews[]` misses a verdict delivered as
  a comment, so a commit that *was* reviewed clean looks unreviewed.

The false-stale direction cost a real error: a PR whose HEAD had been
reviewed clean 3 minutes after it was pushed was re-pinged two hours later
with a public comment asserting "that review does not cover the current
HEAD" - which was false, wasted a review cycle on an already-throttled bot,
and put an incorrect claim on the PR. The verdict was there; it was a
comment, not a review object.

**A bodyless review object is not a verdict either.** The bot can emit a
formal review with a fresh `submittedAt` and an EMPTY body. A gate keyed on
"newest review is newer than HEAD" passes on it while nothing was said. So
the freshness test must be paired with a substance test: a verdict has a
non-empty body that names findings or their absence.

Observed on one PR simultaneously: a stale formal review two commits old, an
acknowledgement comment with a newer timestamp, an in-place edit of the
walkthrough comment (`created_at` unchanged, `updated_at` bumped), and a
commit status that is unreliable in both directions. **Four decoys, one
verdict, and the verdict was the only one absent.** Enumerate all of them
before concluding a review happened.

**Acknowledgements are not verdicts.** "Understood - reviewing the current
HEAD", "Acknowledged", "I'll review the unresolved threads" are all replies
to a trigger, not outcomes, and they carry a *newer* timestamp than the
verdict you are looking for. Timestamp alone therefore cannot distinguish
"reviewed" from "about to review" - and treating an ack as a verdict is a
false clean.

The check that works: take the newest CR object of **either** kind, and
classify it by **content** - a verdict names the reviewed SHA and states
findings or their absence; an ack does neither. Then require that verdict's
timestamp to postdate HEAD's `committedDate`. Where the bot names the SHA it
reviewed, compare SHAs directly and skip the timestamp reasoning entirely
(§11: SHA-bound attestation would remove this whole class).

### Stale adversarial review
An adversarial-review comment on a PR proves nothing unless it postdates
the current HEAD commit. Compare timestamps; re-review the delta otherwise.

### Stale worktree, or no worktree
A long-lived worktree is often a *pre-rebase copy* - same commit subjects,
different SHAs - so tests there verify code that is not on the branch, and
the push fails as non-fast-forward at the very end. Worktrees also get
pruned mid-session. Detection and recovery for both are in `worktrees.md`
§"Staleness and disappearance". The orchestration-level rule: a worktree
that turns out stale or missing invalidates **every gate already run in
it**, so re-run them rather than carrying the earlier verdict forward.

### Stale plan
A plan can rot while it waits: if the base advanced, re-validate the plan
against the current tree before implementing, and re-plan or defer rather
than building on a rotten plan. The longer the gap between planning and
implementing, the more this matters - which is why it bites the scheduled
variant hardest.

### Tests that cannot fail
A regression test written *after* a fix, which passes both with and without
it, is not coverage. See §8.

### Generated scripts
A `sed`-built script can silently produce a 0-byte file. `bash -n` any
generated script before arming it as a watcher (script review rules:
`tool-usage.md`).

### The remediation path can defeat the remediation
A cleanup routine that scans for the thing it is cleaning up must not scope
its scan by a value the fix itself tells the operator to change.

Observed: a script closing an over-broad grant also scanned for and removed
the legacy grant left by earlier runs - but scoped that scan to the
*current* pool ID. The same script recommends migrating to a dedicated pool,
and doing so rebuilt the search string for the NEW pool, so the original
wildcard grant went unseen and the script exited 0 reporting success. **The
vulnerability survived via the upgrade path the fix itself recommended.**

Generalise: when a fix has a "now do X" recommendation, walk the remediation
end-to-end WITH X applied, not only in place. The interesting failure is
rarely "the fix doesn't work" - it is "the fix doesn't work for people who
followed the advice".

### Detection scope and removal scope must match
Scanning broadly while removing narrowly leaves the defect *detected and
surviving*, which is worse than not scanning: the check reports it looked.
Observed in the same script - detection walked the whole IAM policy, removal
was hardcoded to one role, so an equivalent-power grant under a sibling role
(`serviceAccountTokenCreator` rather than `workloadIdentityUser`, both
yielding impersonation) matched the check and survived the removal, with
nothing re-reading the policy afterwards. Guards: derive both from one list,
and **re-read the state after mutating it** rather than assuming the write
did what the read implied.

### A control the code REFERENCES may not EXIST
Code that names a protection mechanism reads as protected. Whether the
mechanism is configured is a fact about the live system, not about the
source, and the two are checked in completely different places.

Observed: a security fix narrowed a credential to jobs bound to a deployment
environment, and the PR claimed the approval gate as one of its two
controls. Querying the live API showed the repo's environments **all had
`protection_rules: []`**, and the specific environments those jobs named
**did not exist at all** - and the platform auto-creates a referenced
environment BARE on first use, so the credentialed jobs would have run with
no approval whatsoever. The YAML was correct; the control was vacuous. Two
junk environments already present in the repo, minted by an earlier empty
interpolation, proved the auto-create path had fired before.

So: when a fix rests on an external control - a deployment environment, a
branch protection rule, a trust policy, an IAM condition, a required check -
**verify the control's live configuration, not its reference in code.** And
prefer controls declared in IaC over ones that can be conjured empty by
being mentioned.

Corollary for reporting: state which half of a claim you verified. Here the
injection half was closed and provable; the gate half was asserted and
false. A PR that claims both, with only one true, closes an issue that is
still half open.

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

### Classify bot output, do not count it
The strongest watcher seen in practice does not ask "did the bot respond?"
It sorts each response into one of three buckets and acts differently on
each:

- **action-performed wrapper** ("Review finished", "Review triggered") ->
  keep waiting, log as acknowledgement
- **rate-limit notice** -> wait out the window, then re-trigger with a FULL
  review (an incremental pass silently skips the commits the throttled pass
  dropped)
- **formal review OR a comment carrying findings** (an "Actionable comments
  posted" / walkthrough / review-details block) -> report as a verdict

And on timeout it prints "no verdict - do NOT read this as clean" rather
than exiting quietly, because a watcher that ends silently is itself an
absence-read-as-success. Adopt this shape rather than any timestamp- or
count-based test.

### Put the guard in the command, not in the memory
A documented trap only helps if it is recalled at the instant the command is
written, and that is exactly when it is not. The short-SHA trap above
(`gh run list --commit <short-sha>` returning empty) fired **three times in
one day, across different agents**, despite being written down - each time
because the note was read at session start and the command was composed an
hour later.

The durable fix is to make the mistake impossible to express rather than
memorable to avoid. Ship the assertion inline in the template:

```bash
[ ${#SHA} -eq 40 ] || { echo "need full 40-char SHA"; exit 1; }
```

Generalise it: when the same trap recurs across agents, stop strengthening
the documentation and start encoding the constraint where the mistake is
made - a precondition in the script, a required argument, a wrapper that
rejects the bad shape. **Recurrence across independent agents is evidence
that a documentation-based mitigation has failed**, not evidence that the
documentation needs to be louder.

### Empty result vs negative result
The session's most repeated defect, in three costumes: a throttled bot showing
zero unresolved threads, an agent going idle with no findings, and
`gh run list --commit <SHORT-SHA>` returning an **empty list** rather than an
error because it needs the full 40-character SHA - read as "CI never
triggered" when five runs existed.

The general rule: **many tools answer "I could not find it" and "there is
nothing" with the same output.** Any gate that treats an absence as a pass
must first establish that it *looked successfully*. When a query returns
nothing, verify the query was well-formed before believing the answer.

A fourth instance, and the most embarrassing, because the gate itself was
broken rather than the data: GitHub's `statusCheckRollup` returns **two
different shapes**. A `CheckRun` carries `.conclusion` and `.status`; a
`StatusContext` (what a bot publishing a commit status produces) carries
`.state` and neither of the others. A rollup query written as
`.conclusion // .status // "PENDING"` therefore reports every
`StatusContext` as `PENDING` forever - including one whose `state` is
`SUCCESS`. That misreading was repeated across six PRs, produced a public
comment on one of them asserting something false, and delayed a merge that
had been ready for hours.

A fifth, in the test runner itself: a test file carrying a build tag such as
`//go:build integration` is **skipped silently** when the tag is absent.
`go test ./pkg/...` prints "no tests to run" and **exits 0** - the same exit
code as a passing run. An implementer reporting "integration test passes,
exit 0" may have run nothing at all, and a reviewer re-verifying without the
tag gets the same empty green. Whenever a guard lives behind a build tag,
the exact command including the tag belongs in the PR body, and the reviewer
must confirm the test actually *ran* (a count of tests executed, not just an
exit code).

The lesson generalises past this one API: **a `//` fallback chain over
fields that may not exist on the object you were handed will silently
manufacture the default.** When a status query returns a suspiciously
uniform answer across many subjects, dump one raw object and look at what
fields it actually has before trusting the derived value. Handle both
shapes:
`(.conclusion // .state // .status // "PENDING")`.

### A bot's green check is not evidence a review happened
Related but distinct, and it cuts the other way. A review bot may publish a
commit status that reads `state: success` **while it is rate-limited and has
reviewed nothing** - the truth living only in the status *description*
("Review rate limited"). A gate keyed on the check being green therefore
passes a PR that was never reviewed, and unlike the traps above this one
fails **open**.

So a green bot check is necessary, not sufficient. Pair it with an actual
verdict that names the reviewed SHA (see the channels trap above). This is
also a strong argument against adding such a status to required checks: the
checkbox goes green either way, so branch protection would enforce nothing
while appearing to enforce everything.

**And the mirror holds too, which is the part that costs quota.** On one
commit the status read `state: success` with
`description: "Review rate limited"` - and its timestamp was **five seconds
after** the same bot posted a genuine, full-range verdict. So on a single
commit the check was simultaneously wrong in both directions: `success` did
not mean a review had run, and "rate limited" did not mean one had not.

The practical rule: **"rate limited" on the check is not grounds to
re-trigger a review.** Check the comment stream for a verdict first. Firing
a `full review` off the check alone re-reviews already-reviewed code and
burns a shared, adaptive quota that every other PR in the batch is queued
behind - so the cost of misreading it lands on work that has nothing to do
with this PR.

The general form, worth carrying to any bot-published status: **a status
field and the work it purports to describe are two different facts.** Treat
the status as a hint about where to look, never as the answer.

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
Agents are assumed to fail independently, so the guards are all
per-agent: replace this one, continue. That assumption broke when **ten agents
died within five minutes** on a shared account-level session limit - every
implementer, every reviewer, and the documentation agent holding the only copy
of several lessons, including the ones about this class of problem.

What made it survivable was that most work was already committed or pushed.
What was nearly lost was work sitting *uncommitted in a worktree* and lessons
sitting *only in the orchestrator's context*. Guards:

- **Checkpoint before you cannot.** Treat "commit and push" as the unit of
  progress. An agent that has been working for a while without committing is
  carrying unrecoverable state.
- **Durable state beats conversational state.** Findings, assignments and
  gate status belong in the task list or a file, not only in the
  orchestrator's context (§11).
- **On mass failure, survey before restarting anything**: for every worktree,
  record HEAD, uncommitted files, and whether HEAD is on origin. Back up
  uncommitted diffs to patch files (`git diff > <path>.patch`) rather than
  committing them - a WIP commit confuses the agent that resumes there, while
  a pruned worktree loses the work outright.
- **Resume from the patch, never re-implement.** Re-implementing discards
  work that already exists and reintroduces every decision the dead agent had
  already made correctly.
- **But assess the recovered work's COMPLETENESS before trusting it.** An
  agent dies mid-task, so what it leaves is a *snapshot of unfinished work* -
  and unfinished work does not announce itself. A recovered fix for a
  double-spend bug turned out to be half done: the first half was implemented
  with a well-argued rationale comment, the second half was missing, and
  there were no tests at all. Committing that worktree as-is would have
  shipped a plausible-looking fix that still double-spends. **A partial fix
  is more dangerous than no fix, because it looks finished** - it has a
  commit-worthy diff and confident comments, and the missing half is
  invisible unless someone checks the original issue's full fix direction.
  The resuming agent's first task is therefore to state what it found, what
  it kept, and what was still missing - not to pick up typing where the
  previous one stopped.

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
- **A launched agent is not a completed one**, and an idle one is not a
  finished one. Collect and check each result before reporting anything
  done (§7).
- **Pre-existing debt on lines you did not touch**: report it, do not fix it
  in a review-fix commit, and never suppress it. Some autofixes are actively
  unsafe - a blanket misspell rewrite once renamed a real DB column
  (`cancelled_by`) and broke integration tests.
- **Prove "pre-existing" instead of asserting it.** A linter exiting nonzero
  on a branch is not evidence the failures are yours, and "they were already
  there" is the easiest claim in code review to make and the hardest to
  check. The cheap proof: run the linter on the branch, run it again against
  a reverted baseline, normalise both finding sets, and diff. One implementer
  reported "684 before, 684 after, zero new" - which converts an assertion
  into a measurement and takes minutes. Same technique settles "is this test
  failure mine?" (run the suite on the base commit) and "is this flaky or
  broken?".
- **"Make it required" is not a fix when the VALUE can be permissive.**
  Turning an optional security parameter into a mandatory one closes the
  omission case and leaves the substitution case wide open. Observed: a flag
  taking a raw CEL expression, where making it mandatory would still have
  admitted `--subject-condition true` - satisfying every non-empty check
  while matching every identity, i.e. the mandatory version reproducing the
  exact bug it was added to close. The fix was to remove the raw-expression
  flag entirely and construct the condition from a pinned, validated value.
  Ask: *what is the most permissive value that satisfies my new
  requirement?* If that value is as bad as omission, requiring it achieves
  nothing.

  This is one family with the policy-variable trap. Any parameter
  interpolated into an expression or policy language admits two escapes:
  **variable expansion** (AWS IAM `${aws:...}` inside a Condition value, so
  the value becomes a tautology) and **expression break-out** (a quote
  closing the literal, e.g. `x' || true || '` rendering
  `google.subject == 'x' || true || ''`). Check for both, per language -
  they are not the same defect and rejecting one does not reject the other.
  Worth knowing that GCP IAM principal identifiers and CEL conditions have
  **no** policy-variable expansion, so `$` there is literal - the languages
  differ, and assuming they behave alike in either direction is wrong.
- **Applying one change to N parallel sites: verify each site by running it,
  not by reading the diff.** Reading N near-identical blocks is precisely
  where the eye supplies what it expects. Observed: an author tightening a
  validation pattern applied it to the *subject* parameter and left the
  *audience* parameter on the old pattern - and neither inspection nor its
  own diff review caught it. A `terraform plan` against each value did, in
  seconds. The author's summary of the episode is the rule: *exercising the
  gate found what reading it did not.* This is the same lesson as probing a
  predicate against a real database, one level down: wherever a change is
  replicated, the replication itself is the thing most likely to be
  incomplete, and it is invisible to the person who made it.
- **Prove nothing happened before validation, don't argue it.** To show a
  script cannot create anything until its guards pass, stub the external
  command so that **any** invocation exits nonzero and loudly, then run every
  rejection path and confirm the stub was never reached. That converts "the
  validation runs first" from a claim about control flow into an observation.
  Cheaper and stronger than reading the script, and it catches an early
  invocation hiding in an unexpected branch.
- **A fail-loud fix must enumerate every legitimate input first.** Replacing
  a silent default with an explicit error is usually right - but it converts
  "wrong behaviour" into "refused outright" for any input the new mapping
  does not know about, and a refusal on a previously-working path is a
  regression wearing the costume of correctness. Observed: a fix mapping
  machine families onto commitment types would have started REFUSING legacy
  `custom-<vCPUs>-<MB>` machine types, which have no family segment at all -
  the fail-loud branch firing on input that used to work. Before shipping a
  fail-loud change, list the inputs reaching it in production and confirm
  each one still resolves. The narrower the new mapping, the more important
  this is.
- **Check the new code is REACHABLE from a production entry point.** Trace
  from a real entry point (handler, scheduler, CLI command, message consumer)
  to the new component and confirm something actually constructs and calls
  it. A feature can be fully implemented, fully tested, and **never wired
  up** - and that state passes every gate, because CI is green, the review
  bot sees clean code, and an adversarial reviewer finds no bugs in code that
  never runs. Observed: a PR whose headline feature was an org-wide
  multi-subscription collector, where every production construction site
  pinned a single subscription, so the new client was constructed only by its
  own tests. The feature shipped dark and the tests proved only that it
  *would* work if anything called it. Ask "what would have to happen at
  runtime for this line to execute?" and answer it concretely.
- **Ask which layer the test exercises.** The sharpest instance of "tests
  that cannot fail" seen so far: a PR's own test fired 50 direct calls at a
  service function and asserted 50. It genuinely proved the accumulator
  conserved counts - and was irrelevant, because the defect was that the
  function is called *twice per HTTP request* from two different layers. The
  test never crossed the boundary where the bug lived, so it passed unchanged
  with the bug present, and it *looked like diligence*. Diagnostic question:
  **does this test exercise the layer where the defect can occur, or only the
  unit beneath it?**
- **Choose the test double by what the defect is.** When the defect lives in
  semantics the dependency enforces - SQL predicate meaning, NULL handling,
  constraint behaviour - a mock asserts the string you sent, not the rows the
  database returns, so it cannot prove the fix. Use the real dependency
  (e.g. a container-backed database). A mock is right when the defect is in
  *your* logic, wrong when it is in what the dependency does with it.
- **Verification is self-reported unless the reviewer re-runs it.** The
  orchestrator cannot confirm that an implementer actually reverted the fix
  and watched the guard fail, nor that quoted exit codes are real. Requiring
  the pre-fix failure output **verbatim** rather than summarised makes
  fabrication effortful and makes a vague report visible as vague - but it is
  a deterrent, not a check.

  **A test can fail pre-fix for the WRONG REASON, which defeats this whole
  rule.** Observed: a guard asserting a scopeless row must buy nothing
  passed its pre-fix check - but only because the test left the provider
  chain unwired, so the fan-out panicked on an unexpected mock call, the
  runner recovered the panic into an error, and `require.Error` was
  satisfied by that incidental failure rather than by the defect. The author
  caught it only by re-running the pre-fix repro AFTER adding the test.
  Guards: assert the **specific** observable (a count of purchases that
  reached the provider, the exact derived token) rather than "an error
  occurred"; and wire the happy path fully enough that the code under test
  could actually SUCCEED, so the only way to fail is the defect. A test whose
  dependencies are too incomplete to run is indistinguishable from one that
  caught something.

  The actual check is cheap and has been done in practice: **the round-2
  reviewer re-derives the pre-fix failure itself.** Concretely -
  `git checkout <fix-commit>^ -- <changed-file>`, run the guard, confirm it
  fails on the specific assertions claimed, restore, confirm clean. One
  reviewer did exactly this and reproduced the failure on the same three
  assertions plus a row-count mismatch, which converted the implementer's
  claim from asserted to verified. Make it a standing part of the round-2
  brief; it costs the reviewer minutes and it is the only thing that closes
  the loop.

  The same reviewer went further and **empirically probed** the changed
  behaviour rather than only reading it - seeding a throwaway matrix of rows
  against a real database and recording what actually survived a retention
  sweep, cell by cell. That caught a sibling defect one status over that
  reading alone had missed. Where a defect lives in what a dependency *does*,
  reading the code proves less than exercising it.

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
