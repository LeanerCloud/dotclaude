---
name: orchestration-traps
description: The failure modes that produce a confident but wrong "done" - absence read as success,
  stale status reads, tests that cannot fail, correlated agent failure - plus the verification
  discipline that catches them. Invoke before declaring multi-agent work complete.
---

# Orchestration traps and verification discipline

These are §7-§8 of the orchestration model; the roles, coordination and merge gate are the
`pr-orchestration` skill. Every rule here was written after the failure it prevents actually
happened.

## 7. Traps

Each of these has produced a wrong "done" in practice.

### Absence read as success
The most repeated defect in this model, in many costumes. **Many tools answer
"I could not find it" and "there is nothing" with the same output**, so any
gate that treats an absence as a pass must first establish that it *looked
successfully*. Every other outcome announces itself; this one has to be made
to.

**Compare SHAs, not timestamps, wherever you can.** Where a bot names the SHA
it reviewed, compare it to HEAD directly and skip the timestamp reasoning
entirely: a timestamp is an inference about coverage, a SHA is an identity.
Everything below is the fallback for actors that do not name one (§11:
SHA-bound attestation would remove this whole class).

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
- **Treat an idle agent as *ready to report*.** An idling agent is saying
  "finished this turn, available for more", never "done forever" - the same
  root cause as the watcher-teardown leak in the `pr-lifecycle` skill, but yielding a wrong verdict rather than a leaked background agent.
  Request the verdict once; replace on a second silent idle. Briefing does not
  fix this - a reviewer respawned with "your final message is the deliverable"
  at the top of its brief idled without reporting anyway - so the guard is
  orchestrator-side. Replacing costs everything that agent knew, so carry its
  known leads into the new brief, and do not replace a non-blocking agent while
  higher-priority work is queued.
- **Require reviewers to bracket the work** with a start line, a progress line
  and a final verdict, per §10 items 6-7, which is where the clause that goes
  into the brief lives. The attacked-and-failed list is what makes a clean
  verdict auditable; without it, "no findings" is indistinguishable from "did
  not look".

Detection mechanics - which API objects carry a verdict, throttle detection,
the `full review` recovery - are owned by the `cr-loop` skill. Enumerate every channel before concluding a review happened.

### Stale adversarial review
An adversarial-review comment on a PR proves nothing unless it postdates
the current HEAD commit. Compare timestamps; re-review the delta otherwise.

### Stale worktree, or no worktree
A long-lived worktree is often a *pre-rebase copy* - same commit subjects,
different SHAs - so tests there verify code that is not on the branch.
Detection and recovery are in the `worktrees` skill §"Staleness and disappearance".
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
the `tool-usage` skill).

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
thing changing it. Asserting a stale read is harmless once and corrosive if
repeated: an agent that receives confidently wrong state starts double-checking
everything the orchestrator says. Two habits:
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
wrong when it is also adaptive. **Before giving each agent its own retry loop,
ask whether the resource is per-agent or shared**; if shared, retry logic
belongs at the orchestrator, not in the workers.

Established in practice: the review bot's limit is **per-developer, per-
organization, not per-PR**, and it tightens at the 95th percentile of recent
volume. So N watchers retrying independently do not merely compete for one
allowance, they *ratchet the ceiling down* for everything queued behind them.
Independent backoff across N clients on one adaptive limit is a thundering
herd with extra steps.

The correct shape:
- **One trigger in flight globally.** Serialise re-triggers across the whole
  batch, highest-stakes PR first. Every other watcher is **observe-only** -
  it classifies and reports, and never posts.
- When the owner lands a verdict, the next PR takes the budget.
- On throttle, wait the window out rather than retrying sooner; a retry
  before the window both fails and counts.

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
merged-branch stranding case in the `pr-lifecycle` skill: the branch is live, the
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
"assert the defect, not a proxy" rule is in the `coding-standards` skill
§"Testing Philosophy"; matching the CI-pinned tool version before trusting
a local lint or format result is in the `tool-usage` skill.

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
- **When adding a requirement, ask what the most permissive value that
  satisfies it is.** If that value is as bad as omission, requiring it achieves
  nothing: a non-empty check is not a restriction, and a shape regex admits
  every value of that shape.
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
