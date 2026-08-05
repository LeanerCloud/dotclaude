# Issue -> PR Autopilot (scheduled, multi-routine, multi-tier)

> **This is the scheduled variant of the model documented in
> `multi-agent-pr-orchestration.md`. Read that first**; this file covers only
> what differs when the trigger is a cron routine rather than an interactive
> session. The roles, the handoff contract, the concurrency model, the merge
> gate, the loops, and the traps are defined there and are not repeated here.

A continuous delivery loop driven by GitHub issue labels. **Two** scheduled
**remote** Claude Code agents (created via the `schedule` skill / `RemoteTrigger`)
fire on staggered 4-hourly crons, plan and open PRs for eligible issues, and stamp
labels that act as the state machine. The label *is* the dedup guard and the
handoff medium between tiers.

This file is the source of truth for the per-repo config and the two routine
prompts (`issue-pr-autopilot.plan.prompt.md` and
`issue-pr-autopilot.worker.prompt.md`, same directory). It composes existing
machinery, it does not duplicate it:

- The orchestration model this implements (roles, concurrency, merge gate,
  loops, traps) -> `multi-agent-pr-orchestration.md`
- Selection / triage rubric -> `triage.md` (work selection + label rubric)
- Plan -> worktree -> implement -> review -> PR -> `CLAUDE.md`, `worktrees.md`,
  `coding-standards.md`, `conventions.md`
- Commit/PR hygiene + post-PR CR loop -> `git-workflow.md`
- Model-tier selection -> `subagent-strategy.md`
- Driving an opened PR to merge-ready -> the `pr-iterate` flow (`commands/` / skills)

## Why multiple routines (the load-bearing constraint)

A cloud scheduled routine runs as **one flat agent**: it has tools
`[Bash, Read, Write, Edit, Glob, Grep]` only, **no Agent/Task tool**, so it
**cannot spawn subagents**, and it is **pinned to a single model** for the whole
run. There is no shared memory between fires either. So the local
CLAUDE.md tier-split (Opus plans, Sonnet implements, each as a subagent) is
impossible inside a single routine.

The only way to apply the tier-split in the cloud is to run **separate routines on
separate models** that hand off through **durable GitHub state** (labels + a plan
committed to a branch + a parseable issue comment). The **Opus planner** does the
bounded, judgement-heavy planning phase; the **Sonnet worker** does the
mechanical implementation, conflict-resolution, and CR-advance phases. This is
cost-optimal: Opus runs only the short planning phase, never the long
implement/CR loops.

The `schedule` API floor is **1 hour** (`*/30` is rejected), and claude.ai
schedules are capped at approximately **15 routine fires per day** (account-wide).
Running four routines at 15-minute offsets would consume 96 fires/day -- far over
that cap. The 4-hour cadence with two routines (6 planner + 6 worker = 12 fires/day)
fits comfortably within the budget. See the "Run-budget" section below.

> **Runtime caveat (load-bearing):** remote scheduled agents run in Anthropic's
> cloud, isolated, with their **own git checkout and zero access to a developer's
> `~/.claude`**. The design solves this with **two git sources**: the target repo
> **and** `LeanerCloud/dotclaude` (this repo). The cold agent reads the full
> guideline set (git-workflow, triage, coding-standards, conventions, worktrees,
> pr-iterate) from the `dotclaude` checkout, plus the target repo's own
> `CLAUDE.md`/`CONTRIBUTING.md` for repo-specifics. Both routine prompts also inline
> the global hard constraints as a backstop. Keep `dotclaude` and each target
> repo's committed conventions current.

## The label state machine

| Issue state | Label(s) | Meaning | Loop action |
|---|---|---|---|
| fresh / unlabelled | (none of the autopilot labels) | no plan yet | **eligible for planning** - the planner picks it, writes a plan branch, adds `plan-ready` |
| plan ready | `plan-ready` | a plan branch + parseable `autopilot-branch:` marker exist; awaiting implementation | the worker implements it, opens a PR, swaps to `pr-created` state |
| PR in flight | `pr-created` (+ `plan-ready`) | a PR exists (open or merged) | skip create; the worker drives it through CR |
| PR merged | `pr-created` + `pr-merged` | delivered | skip; reconcile only |
| stuck | `needs-human` | N consecutive autopilot failures | skip entirely; a human takes over |

- `plan-ready` (color `FBCA04`): the Opus planner has committed a plan branch and
  posted the `autopilot-branch:` marker comment for this issue.
- `pr-created` (color `1D76DB`): a PR has been opened for this issue.
- `pr-merged` (color `0E8A16`): the issue's PR has been merged.
- `needs-human` (color `B60205`): the autopilot gave up on this issue after N
  consecutive failures; do not retry automatically.

Labels are created on first fire if missing. `plan-ready` is never removed once
set (it records that a plan branch exists); the worker layers `pr-created` on top
rather than swapping labels, so the create-dedup guard (`pr-created`) and the
plan-claim guard (`plan-ready`) are independent and monotonic.

## Routines (2 total: 1 Opus planner + 1 Sonnet worker, staggered 30 min apart)

Both routines run in environment **Default** (`anthropic_cloud`,
`env_01DCb7bHtxWMDQZ8MBr67maL`), with two git sources (the target repo +
`LeanerCloud/dotclaude`) and tools `[Bash, Read, Write, Edit, Glob, Grep]`. The
planner runs `issue-pr-autopilot.plan.prompt.md`; the worker runs
`issue-pr-autopilot.worker.prompt.md`. The 30-minute offset staggers plan -> work
within the same 4-hour slot: the planner fires at the top of each 4-hour window,
the worker fires 30 minutes later in the same window so a fresh plan is ready before
the worker acts.

| Routine | Cron (UTC) | Model | Prompt | Routine ID | Role |
|---|---|---|---|---|---|
| `cudly-autopilot-plan` | `0 */4 * * *` | latest available Opus model | `issue-pr-autopilot.plan.prompt.md` | `trig_01PwHdsKE5bsbrQJRkYJjZAd` | plan top eligible issue(s) |
| `cudly-autopilot-worker` | `30 */4 * * *` | latest available Sonnet model | `issue-pr-autopilot.worker.prompt.md` | `trig_01GjFaCDmu7moBS6jmBkLa3B` | reconcile + conflict-resolve + CR-advance + implement |

Both are currently **disabled** (pending validation).

> Set the planner model to the **current latest Opus model id** and the worker to
> the **current latest Sonnet model id** at routine-creation time. Refresh both
> together when a newer generation ships so they stay on the same generation.

> **Server-pinned minute (observed 2026-07-27):** the `RemoteTrigger` API pins the
> cron **minute** server-side for load-spreading, so a submitted `0 */4 * * *` is
> rewritten to a stable server-chosen minute (the planner has landed at `:44` and
> `:55` on successive writes, not `:00`; resubmitting `0` does not stick). The `:00`/`:30` values below are
> therefore **nominal**. This is a latency detail only: correctness comes from the
> `plan-ready`/`pr-created` labels, not the clock (see "Correctness is from labels,
> not the clock"), so an inverted plan/worker order within a slot just defers the
> pickup to the next worker fire. At enable-time, read the actual `next_run_at` /
> `cron_expression` of both routines; if you want strict plan-before-work ordering,
> set the worker's nominal minute later than the planner's server-assigned minute
> (accepting the server may re-pin it too).

> **Frontier-judgement escalation is manual.** The planner runs on Opus (the default
> top tier, ~Opus-5-class): it cannot escalate to the Fable peak reserve mid-run,
> because a routine is pinned to a single model for the whole fire and has no
> Agent/Task tool. Opus 5 lands within ~0.5% of Fable 5's peak at roughly half the
> cost, so Opus is the right default here and Fable is not worth a permanent second
> routine. If a specific issue genuinely needs frontier judgement (a gnarly
> architecture call or a high-stakes money-path plan), a **human** re-plans it
> out-of-band on Fable — e.g. do one manual `run` of the planner with its model
> temporarily set to the latest Fable id, or draft the plan branch by hand — then
> let the normal worker fire pick it up via the `plan-ready` label. Do NOT add a
> standing Fable planner routine for this; it would burn run-budget (see
> "Run-budget") for the rare case.

> **Earlier exploration routines:** `cudly-autopilot-plan-30` and
> `cudly-autopilot-worker-45` were created during design exploration and are no
> longer part of the active design. Delete them via the routines UI at
> https://claude.ai/code/routines (no API delete endpoint exists).

### RemoteTrigger create-bodies

Each routine is created with the shape below. Generate a fresh lowercase v4 UUID
for `events[].data.uuid` per routine. Substitute `MODEL` and `PROMPT_FILE_CONTENTS`
per the table above (`session_context.message.content` is the full text of the
prompt file inlined, since the cloud agent is cold). The two git sources let the
cold agent read this repo's guidelines plus the target repo.

**Planner** (`cudly-autopilot-plan`, cron `0 */4 * * *`):

```json
{
  "name": "cudly-autopilot-plan",
  "cron_expression": "0 */4 * * *",
  "enabled": false,
  "job_config": {
    "ccr": {
      "environment_id": "env_01DCb7bHtxWMDQZ8MBr67maL",
      "session_context": {
        "model": "<current latest Opus model id>",
        "sources": [
          {"git_repository": {"url": "https://github.com/LeanerCloud/CUDly"}},
          {"git_repository": {"url": "https://github.com/LeanerCloud/dotclaude"}}
        ],
        "allowed_tools": ["Bash", "Read", "Write", "Edit", "Glob", "Grep"]
      },
      "events": [
        {"data": {
          "uuid": "<fresh-lowercase-v4-uuid>",
          "session_id": "",
          "type": "user",
          "parent_tool_use_id": null,
          "message": {"role": "user", "content": "<full text of issue-pr-autopilot.plan.prompt.md>"}
        }}
      ]
    }
  }
}
```

**Worker** (`cudly-autopilot-worker`, cron `30 */4 * * *`): same shape with
`name: "cudly-autopilot-worker"`, `cron_expression: "30 */4 * * *"`,
`model: "<current latest Sonnet model id>"`, `message.content` = full text of
`issue-pr-autopilot.worker.prompt.md`, and a fresh `uuid`.

Create both with `enabled: false`, do one `run` of each to confirm the cloud env
can clone both sources + plan / implement / open a PR + edit labels, then `enable`.
You **cannot delete** via API; use https://claude.ai/code/routines.

## Watcher model (scheduled context differs from a local session)

The generalised rule - every trigger needs something durable that will act on
the response - is in `multi-agent-pr-orchestration.md` §"The loops". What
differs here: neither routine can spawn `cr-watch`/`ci-watch` background agents
(no Agent/Task tool; background processes die when the bounded run ends), so the
local git-workflow.md "spawn a watcher" steps do not apply literally. The
invariant is satisfied **structurally and cross-routine** instead: every PR the
worker triggers/re-pings stays in the in-flight set (`pr-created`, not
`pr-merged`, PR open) and is therefore guaranteed to be re-examined by the next
worker fire. **The worker cron is the durable watcher** for both planner-spawned
and human-spawned PRs, which is why **disabling the worker routine orphans every
triggered PR**. CR rate-limit recovery uses `@coderabbitai full review` (not the
incremental form), per git-workflow.md.

## Plan handoff (the durable contract between the two tiers)

The planner and the worker never run in the same process, so they are bound by
the handoff contract in `multi-agent-pr-orchestration.md` §"The handoff
contract" (plan committed not remembered, claim as the first durable action,
parseable marker). This is its concrete instance - they communicate only
through GitHub state:

1. **Plan branch**: `auto/<issue#>-<slug>`, branched off the repo base.
2. **Plan file**: the plan committed to that branch as its FIRST commit
   (`plan.md`, or `.autopilot/plan-<issue#>.md`), conventional message
   `chore(autopilot): plan for #<issue>`.
3. **Marker comment** on the issue, an exact parseable line:
   `autopilot-branch: auto/<issue#>-<slug>`.
4. **`plan-ready` label** on the issue, added LAST as the claim.

### Opus planner, per fire

1. **Preflight (cheap gate).** Resolve repo + login; ensure all four labels exist.
   Count eligible issues (open, `triaged`, not `plan-ready`, not `pr-created`, not
   `needs-human`, not `type/question`/`status/blocked`/`status/needs-info`). If 0,
   write a one-line summary and stop.
2. **Select.** Rank eligible issues by `triage.md` (priority -> urgency -> impact
   -> unblocks-others -> effort). Take the top **1-2** (the per-repo plan cap).
3. **Plan + claim, per issue, in this order** (claim as a tight sequence right
   after the plan commit lands, to minimise the dup-window between the two
   staggered planners): create `auto/<issue#>-<slug>` off the base; write the plan
   to `plan.md`; commit it as the branch's FIRST commit; push the branch; comment
   the `autopilot-branch:` marker on the issue; add `plan-ready`. If any step
   fails, do not add `plan-ready` (so the issue stays eligible) and log the reason.
4. **Never** implement code, open a PR, or run the CR loop. Planning only.

### Sonnet worker, per fire (FOUR sub-phases, IN THIS ORDER)

1. **Reconcile (cheap).** For each `pr-created` and not `pr-merged` issue, find its
   linked PR (closing-keyword / `(#N)` / title-mirror, since CUDly PRs target a
   non-default base so auto-close does not fire). If MERGED, add `pr-merged`. Never
   remove `pr-created`.
2. **Conflict-resolve (before CR-advance).** For each `pr-created`, open,
   `mergeable == CONFLICTING` PR: rebase onto the base, push `--force-with-lease`
   the PR's own branch. STOP + log if a conflict needs a semantic human decision.
   Never address CR findings on a conflicting/stale tree.
3. **CR-advance.** For each `pr-created`, open, NON-conflicting PR: one `pr-iterate`
   pass (pull CR findings, triage, fix minimally + regression test for real bugs,
   build/lint/tests + pre-commit, push the PR's own branch, re-ping
   `@coderabbitai review` / `full review` on rate-limit recovery). The
   cron/cross-routine **is** the durable watcher (see atomic-coupling note). Never
   self-merge; never `@coderabbitai resolve`. Active-human guard: defer a PR whose
   latest commit is a human push newer than the last CR review.
4. **Implement (capped).** For each `plan-ready` and not `pr-created` and not
   `needs-human` issue, up to the per-repo implement cap:
   a. Read the `autopilot-branch:` marker from the issue; checkout that branch.
   b. **Rebase the auto branch onto the current base** and re-validate the plan
      against `plan.md`; if the base diverged enough that the plan is stale,
      re-plan inline or defer (re-add nothing, log staleness) rather than building
      on a rotten plan.
   c. Implement to repo standards (reuse helpers; no duplication); five-dimension
      self-review; build/lint/tests + pre-commit MUST pass; never `--no-verify`.
   d. **Erase the plan from history**: `git reset --soft <base>`, delete `plan.md`,
      and re-commit ONLY the implementation as clean conventional commit(s), so
      `plan.md` never appears in the branch's final history regardless of how the
      human merges.
   e. Open the PR against the base (`Closes #<issue>` + summary + verification
      notes); strip any auto-injected Claude footer; mirror the issue's triage
      rubric onto the PR; add `pr-created` to the issue; trigger
      `@coderabbitai review`. Never self-merge.

> **Correctness is from labels, not the clock.** An Opus plan can exceed 15
> minutes, so the worker gates on `plan-ready` (and `pr-created`), never on "the
> previous offset must have finished". The minute offsets are latency tuning only;
> a plan that is not yet `plan-ready` is simply picked up by a later worker fire.

## Concurrency model (how the generic model maps onto labels)

The model - durable state as the coordination substrate, disjoint slices per
phase, claims are not atomic locks, the three required mitigations, and
correctness-from-state-not-the-clock - is in
`multi-agent-pr-orchestration.md` §"Coordination and concurrency". This is how
it instantiates here:

- **The substrate** is GitHub issue labels plus the `autopilot-branch:` issue
  comment: the ONLY shared state between isolated routine fires (no shared
  memory; see "no subagents / no shared memory" below).
- **The disjoint slices** are: unlabelled -> plan; `plan-ready` (no
  `pr-created`) -> implement; `pr-created` + open + `CONFLICTING` -> rebase;
  `pr-created` + open + clean -> CR-advance; merged -> stamp `pr-merged`.
- **Mitigation (a), no simultaneous actors**, is implemented as cron staggering:
  never schedule two routines at the same minute-offset (planner at `:00`,
  worker at `:30` of each 4-hour window, zero simultaneous fires), and each fire
  must finish within its slot.
- **Mitigation (b), claim-first**, is: the planner pushes the plan commit, posts
  the `autopilot-branch:` marker, and adds `plan-ready` immediately on selection;
  the worker adds `pr-created` at PR-open.
- **Mitigation (c), idempotent gates**, means the worst case of a lost race is a
  duplicate `auto/<issue>` branch, caught at the `pr-created` gate and cleaned up
  (close the orphan), per the double-plan caveat below.
- **Throughput vs the 1h floor and run-budget.** With two routines at 4-hour
  cadence, something happens every 30 minutes within each 4-hour window with
  ZERO simultaneous fires. To go faster, **prefer raising the per-fire cap or
  adding target repos** over adding more routines: each extra routine costs 6+
  fires/day against the ~15/day account-wide cap (see "Run-budget" below).
  Simultaneous fires also trade mitigation (a) for throughput.

## Per-repo config

| Repo | Base branch | Eligibility (plan) | Plan cap/fire | Implement cap/fire | CR-advance cap/fire | Cadence | Status |
|---|---|---|---|---|---|---|---|
| `LeanerCloud/CUDly` | `feat/multicloud-web-frontend` | any `triaged` issue except `type/question`, `status/blocked`, `status/needs-info`, `needs-human` | 2 | 2 | none (all open in-flight PRs) | every 4h (plan `:00` / worker `:30`), 12 runs/day | disabled (pending validation) |
| other `LeanerCloud/*` | repo default unless stated | TBD | TBD | TBD | TBD | TBD | pending |
| `cristim/*` | repo default | TBD | TBD | TBD | TBD | TBD | pending |

## Cost controls

The three generic controls (cheap preflight gate, top-tier reasoning only on the
bounded planning/review phases, per-pass caps) are in
`multi-agent-pr-orchestration.md` §"Cost control", and tier selection itself is
owned by `subagent-strategy.md`. Their concrete values here:

- Preflight gates in both routines, so quiet backlogs cost ~nothing.
- Opus runs **only** the bounded planning phase (cap 2 issues/fire); the long
  implement and CR-advance loops run on Sonnet.
- Per-fire plan and implement caps of 2 bound new branches and PRs (and
  downstream CR loops) per run.
- The `schedule` API floor is **1 hour** (`*/30` is rejected). Staggered offsets x
  caps is the spend lever; raising cadence means more routines, not finer crons.

## Run-budget

claude.ai scheduled routines are capped at approximately **~15 fires per day,
account-wide** across ALL routines in the account. The current two-routine 4-hour
design uses 6 planner + 6 worker = **12 fires/day**, leaving a small headroom of
~3 fires/day for one-off manual `run` calls or transient extra loads.

**Constraints that follow from this:**

- Do NOT add more routines or raise the cadence (e.g. to 2-hour, or back to the
  hourly grid) without first checking the remaining budget across all active
  routines in the account.
- If you also run other routines in the same account (e.g. a separate CR re-review
  drip or a nightly sweep), each of those fires counts against the SAME ~15/day
  cap. Prefer folding them into one of the existing routine phases: the worker's
  CR-advance phase already re-triggers CodeRabbit on every in-flight PR, which
  subsumes a separate CR drip routine.
- The ~15/day figure is an observed practical cap, not a published hard limit;
  treat it conservatively. Disable unused or exploratory routines promptly to
  keep the budget stable.

## Creating and operating the routines

Follow these steps in order. Do not skip the prerequisite or the validation run.

### Step 1 - Prerequisite: merge this PR first

The two prompt files (`issue-pr-autopilot.plan.prompt.md` and
`issue-pr-autopilot.worker.prompt.md`) must be on the `main` branch of
`LeanerCloud/dotclaude` **before** you enable the routines. The cloud agent
clones dotclaude's default branch on each fire; if the files are absent the
routine STOPs with a "prompt missing" error by design.

Do not enable either routine until this PR is merged into `main`.

### Step 2 - Create both routines (disabled)

The two routines already exist (created during design exploration):

| Routine | ID | Cron | Model |
|---|---|---|---|
| `cudly-autopilot-plan` | `trig_01PwHdsKE5bsbrQJRkYJjZAd` | `0 */4 * * *` | latest available Opus |
| `cudly-autopilot-worker` | `trig_01GjFaCDmu7moBS6jmBkLa3B` | `30 */4 * * *` | latest available Sonnet |

Both are **disabled**. If you ever need to recreate them from scratch, use the
`schedule` skill (`RemoteTrigger create`) with the JSON bodies documented in the
"RemoteTrigger create-bodies" section above: environment `env_01DCb7bHtxWMDQZ8MBr67maL`,
two git sources (CUDly + dotclaude), tools `[Bash, Read, Write, Edit, Glob, Grep]`,
`enabled: false`, and `message.content` = the full text of the respective prompt
file (the cold agent cannot read it from disk without it being inlined). Set
planner model to the current latest Opus model id and worker to the current
latest Sonnet model id.

### Step 3 - Validate disabled-first

Before enabling, do one manual `run` of the planner, then one manual `run` of
the worker:

```
# planner
schedule run trig_01PwHdsKE5bsbrQJRkYJjZAd

# worker (after planner run completes)
schedule run trig_01GjFaCDmu7moBS6jmBkLa3B
```

Confirm:
- The cloud env clones both repos successfully.
- The planner reads the prompt file from the dotclaude checkout, selects an
  eligible issue, commits a plan branch, posts the `autopilot-branch:` marker,
  and adds `plan-ready`. If no eligible issues exist, a one-line "nothing to
  plan" summary is the expected output.
- The worker reads its prompt, reconciles any in-flight PRs, and (if a
  `plan-ready` issue exists) implements it, opens a PR, mirrors labels, and
  triggers `@coderabbitai review`.
- Neither run self-merges or pushes to `main` or `feat/multicloud-web-frontend`.

Only after both validation runs pass: set `enabled: true` on each routine.

### Step 4 - Run budget

The account-wide cap is approximately **15 routine fires per day** across ALL
routines. This design uses 6 planner + 6 worker = **12 fires/day**, leaving
roughly 3 fires/day headroom for manual `run` calls.

The hourly `cudly-cr-fullreview-drip` routine was **disabled** to stay within
budget: the worker's CR-advance phase (Phase 3) already re-triggers CodeRabbit
on every in-flight PR each fire, which subsumes a separate drip.

Do not add routines or raise the cadence without first auditing the remaining
budget across all active routines in the account.

### Step 5 - Ongoing management

- **Kill switch**: set `enabled: false` via `schedule update` or the routines
  UI at https://claude.ai/code/routines. Prefer disabling the planner alone
  (pauses new work; worker keeps draining in-flight PRs); disable the worker
  only when you intend to stop CR follow-up entirely, because disabling the
  worker orphans every triggered PR.
- **Model updates**: when a newer Opus or Sonnet generation ships, update
  `model` on both routines together via `schedule update` so they stay on the
  same generation.
- **No API delete**: the `RemoteTrigger` API has no delete endpoint. Remove
  routines via the UI at https://claude.ai/code/routines only.

### Step 6 - Cleanup: delete leftover exploration routines

Delete these paused exploration routines via the routines UI (no API delete):

| Name | ID |
|---|---|
| `cudly-autopilot-plan-30` | `trig_01S76xzpHdbje4nTksEkSRNt` |
| `cudly-autopilot-worker-45` | `trig_012PZB6Mx7zLQ9hWmrZFg4c5` |
| `cudly-issue-pr-autopilot` (superseded single-routine) | (check routines UI) |

Navigate to https://claude.ai/code/routines, locate each by name or ID, and
delete.

## Caveats and gotchas (scheduled-specific)

The variant-independent ones - correctness from state rather than the clock,
rebase before advancing a review, plan staleness, and the poison-item guard -
are in `multi-agent-pr-orchestration.md` §§"Coordination and concurrency", "The
loops", "Traps". What is specific to running this on a cron:

- **Correctness is from labels, not the clock.** Restated because it is the
  crux *here*: cron offsets are latency tuning; the worker only ever acts on
  durable labels, never on "the planner offset must have finished".
- **Conflict-resolve runs as its own phase, before CR-advance**, so every PR
  reaching CR-advance is already non-conflicting.
- **Cross-routine atomic-coupling.** The worker routine is the durable watcher for
  every CR trigger (the PR stays in the in-flight set and the next worker fire WILL
  re-examine it). **Disabling the worker routine orphans every triggered PR.**
- **Plan staleness has a wide window here** (up to a full cron gap), so the
  worker rebases the `auto/<issue>` branch onto base and re-validates `plan.md`
  before implementing, re-planning inline or deferring if the plan has diverged.
- **Rare double-plan race.** With a single planner routine, a concurrent double-plan
  can only occur if the same planner fires twice (manual `run` while a scheduled
  fire is in-flight). Worst case is two `auto/<issue>-*` branches. The worker
  implements exactly one (it reads the single `autopilot-branch:` marker; if two
  markers exist it takes the newest and logs the duplicate), the issue gets
  `pr-created` once, and the worker closes the orphan `auto/<issue>` branch as
  cleanup.
- **Poison-item guard**, instantiated: N=3 consecutive autopilot failures on the
  same issue (plan-or-implement) adds `needs-human`. (N=3 for CUDly; adjust per
  repo in the per-repo config table based on tolerance for retry cost.)
  Eligibility excludes `needs-human`. A human clears it once handled.
- **No subagents / no shared memory** in the cloud env. This is the reason the
  whole design is split into separate single-model routines instead of one
  routine that spawns tier-matched subagents.

## Known runtime quirks (observed)

- The CCR runtime auto-appends a `Generated by Claude Code` / claude.ai session
  footer to PR bodies. This violates the no-Claude-mention rule, so the worker
  strips it post-create (implement phase). If it re-appears after edit, disable it
  at the environment level.
- Untracked files under a developer's `~/.claude` (which is the `dotclaude`
  checkout) get wiped by sync. Commit anything that must persist (this file is an
  example).
