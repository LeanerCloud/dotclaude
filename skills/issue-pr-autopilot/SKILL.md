---
name: issue-pr-autopilot
description: The scheduled cron-routine variant of PR orchestration - routine creation, the GitHub
  label state machine, run budget, and scheduled-context quirks. Invoke when setting up or operating
  the unattended issue-to-PR autopilot.
disable-model-invocation: true
---

# Issue -> PR Autopilot (scheduled, multi-routine, multi-tier)

Host mapping for Claude-only primitives: `skills/HOSTS.md`.

> This is the scheduled variant of the model in the `pr-orchestration` skill. **Read that first**;
> this file covers only what differs when the trigger is a cron routine. Roles, the handoff contract,
> the concurrency model, the merge gate, the loops and the traps are defined there.

A continuous delivery loop driven by GitHub issue labels. **Two** scheduled **remote** Claude Code
agents (created via the `schedule` skill / `RemoteTrigger`) fire on staggered 4-hourly crons, plan and
open PRs for eligible issues, and stamp labels that act as the state machine. The label *is* the dedup
guard and the handoff medium between tiers.

This file is the source of truth for the per-repo config and the two routine prompts
(`plan.prompt.md`, `worker.prompt.md`, same directory). It composes existing machinery:

- Orchestration model (roles, concurrency, merge gate, loops, traps): `pr-orchestration`
- Selection / triage rubric: `work-selection`, `triage-labels`
- Plan -> worktree -> implement -> review -> PR: `AGENTS.md`, `worktrees`, `coding-standards`, `conventions`
- Commit/PR hygiene: `git-commit`, `pr-lifecycle`; post-PR CR loop: `cr-loop`
- Model-tier selection: `subagent-strategy`
- Driving an opened PR to merge-ready: `pr-iterate` (`skills/pr-iterate/SKILL.md`)

## Why multiple routines

A cloud scheduled routine is **one flat agent**: tools `[Bash, Read, Write, Edit, Glob, Grep]` only,
**no subagent tool**, **pinned to a single model** for the whole run, with no shared memory between
fires. The local tier-split (top tier plans, Sonnet implements, as subagents) is impossible inside one
routine. So run **separate routines on separate models** that hand off through **durable GitHub state**
(labels + a plan committed to a branch + a parseable issue comment). The **Opus planner** does the
bounded planning phase; the **Sonnet worker** does implementation, conflict-resolution and CR-advance.
Opus never runs the long implement/CR loops.

The `schedule` API floor is **1 hour** (`*/30` is rejected); claude.ai schedules are capped at about
**15 routine fires per day** account-wide (see "Run-budget").

> **Runtime caveat:** remote agents run in Anthropic's cloud with their **own git checkout and zero
> access to a developer's `~/.claude`**. The design uses **two git sources**: the target repo **and**
> `LeanerCloud/dotclaude`. The cold agent reads the guideline set (`git-commit`, `pr-lifecycle`,
> `cr-loop`, `triage-labels`, `coding-standards`, `conventions`, `worktrees`, `pr-iterate` skills) from
> the dotclaude checkout, plus the target repo's `CLAUDE.md`/`CONTRIBUTING.md`. Both prompts also inline
> the global hard constraints as a backstop. Keep dotclaude and each target repo's conventions current.

## The label state machine

| Issue state | Label(s) | Meaning | Loop action |
|---|---|---|---|
| fresh / unlabelled | (none of the autopilot labels) | no plan yet | **eligible for planning**: the planner picks it, writes a plan branch, adds `plan-ready` |
| plan ready | `plan-ready` | a plan branch + parseable `autopilot-branch:` marker exist; awaiting implementation | the worker implements it, opens a PR, adds `pr-created` |
| PR in flight | `pr-created` (+ `plan-ready`) | a PR exists (open or merged) | skip create; the worker drives it through CR |
| PR merged | `pr-created` + `pr-merged` | delivered | skip; reconcile only |
| stuck | `needs-human` | N consecutive autopilot failures | skip entirely; a human takes over |

- `plan-ready` (`FBCA04`): the planner committed a plan branch and posted the `autopilot-branch:` marker.
- `pr-created` (`1D76DB`): a PR has been opened for this issue.
- `pr-merged` (`0E8A16`): the issue's PR has been merged.
- `needs-human` (`B60205`): gave up after N consecutive failures; do not retry automatically.

Labels are created on first fire if missing. `plan-ready` is never removed (it records that a plan
branch exists); the worker layers `pr-created` on top, so the create-dedup guard (`pr-created`) and the
plan-claim guard (`plan-ready`) are independent and monotonic.

## Routines (2 total: 1 Opus planner + 1 Sonnet worker, staggered 30 min apart)

Both run in environment **Default** (`anthropic_cloud`, `env_01DCb7bHtxWMDQZ8MBr67maL`), with two git
sources (target repo + `LeanerCloud/dotclaude`) and tools `[Bash, Read, Write, Edit, Glob, Grep]`. The
planner fires at the top of each 4-hour window, the worker 30 minutes later, so a fresh plan is usually
ready before the worker acts.

| Routine | Cron (UTC) | Model | Prompt | Routine ID | Role |
|---|---|---|---|---|---|
| `cudly-autopilot-plan` | `0 */4 * * *` | latest available Opus model | `plan.prompt.md` | `trig_01PwHdsKE5bsbrQJRkYJjZAd` | plan top eligible issue(s) |
| `cudly-autopilot-worker` | `30 */4 * * *` | latest available Sonnet model | `worker.prompt.md` | `trig_01GjFaCDmu7moBS6jmBkLa3B` | reconcile + conflict-resolve + CR-advance + implement |

Both are currently **disabled** (pending validation).

> Set the planner to the **current latest Opus model id** and the worker to the **current latest Sonnet
> model id** at creation; refresh both together when a newer generation ships.

> **Server-pinned minute (observed 2026-07-27):** the `RemoteTrigger` API pins the cron **minute**
> server-side (the planner landed at `:44` and `:55` on successive writes; resubmitting `0` does not
> stick), so `:00`/`:30` are **nominal**. This is latency only: correctness comes from labels. At
> enable-time read the actual `next_run_at` / `cron_expression` of both; for strict plan-before-work
> ordering set the worker's nominal minute later than the planner's assigned minute (the server may
> re-pin it too). An inverted order just defers pickup to the next worker fire.

> **The planner runs on Opus, the top tier**; nothing above it exists to escalate to mid-run, so a
> gnarly architecture or money-path plan needs no special routing. To put a human in the loop, re-plan
> out-of-band by hand and let the normal worker fire pick it up via `plan-ready`.

> **Earlier exploration routines** `cudly-autopilot-plan-30` and `cudly-autopilot-worker-45` are no
> longer part of the design. Delete them at https://claude.ai/code/routines (no API delete endpoint).

### RemoteTrigger create-bodies

Generate a fresh lowercase v4 UUID for `events[].data.uuid` per routine. `session_context.message.content`
is the full text of the prompt file, inlined (the cloud agent is cold).

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
          "message": {"role": "user", "content": "<full text of plan.prompt.md>"}
        }}
      ]
    }
  }
}
```

**Worker** (`cudly-autopilot-worker`): same shape with `name: "cudly-autopilot-worker"`,
`cron_expression: "30 */4 * * *"`, `model: "<current latest Sonnet model id>"`, `message.content` = full
text of `worker.prompt.md`, and a fresh `uuid`.

Create both with `enabled: false`, do one `run` of each to confirm the cloud env can clone both sources,
plan / implement / open a PR and edit labels, then `enable`. You **cannot delete** via API.

## Watcher model (scheduled context differs from a local session)

The generalised rule (every trigger needs something durable that acts on the response) is in
`pr-orchestration` §"The loops". Here neither routine can spawn `cr-watch`/`ci-watch` background agents
(no subagent tool; background processes die when the bounded run ends), so the `pr-lifecycle` "spawn
a watcher" steps do not apply literally. The invariant is satisfied **structurally and cross-routine**:
every PR the worker triggers/re-pings stays in the in-flight set (`pr-created`, not `pr-merged`, PR
open) and is re-examined by the next worker fire. **The worker cron is the durable watcher** for both
planner-spawned and human-spawned PRs, so **disabling the worker routine orphans every triggered PR**.
CR rate-limit recovery uses `@coderabbitai full review` (not the incremental form), per `cr-loop`.

## Plan handoff (the durable contract between the two tiers)

Planner and worker never share a process; they are bound by the handoff contract in `pr-orchestration`
§"The handoff contract" and communicate only through GitHub state:

1. **Plan branch**: `auto/<issue#>-<slug>`, branched off the repo base.
2. **Plan file**: committed as the branch's FIRST commit (`plan.md`, or `.autopilot/plan-<issue#>.md`),
   message `chore(autopilot): plan for #<issue>`.
3. **Marker comment** on the issue, an exact parseable line: `autopilot-branch: auto/<issue#>-<slug>`.
4. **`plan-ready` label** on the issue, added LAST as the claim.

### Opus planner, per fire

1. **Preflight (cheap gate).** Resolve repo + login; ensure all four labels exist. Count eligible issues
   (open, `triaged`, not `plan-ready`, not `pr-created`, not `needs-human`, not
   `type/question`/`status/blocked`/`status/needs-info`). If 0, write a one-line summary and stop.
2. **Select.** Rank by `work-selection` (priority -> urgency -> impact -> unblocks-others -> effort).
   Take the top **1-2** (per-repo plan cap), and challenge each against the current code first
   (AGENTS.md §1): an obsolete issue or one with a more fundamental fix gets a comment plus
   `status/needs-info` instead of a plan.
3. **Plan + claim, per issue, in this order** (claim as a tight sequence right after the plan commit, to
   minimise the dup-window): create `auto/<issue#>-<slug>` off the base; write `plan.md`; commit it as
   the FIRST commit; push; comment the `autopilot-branch:` marker; add `plan-ready`. If any step fails,
   do not add `plan-ready` (the issue stays eligible) and log the reason.
4. **Never** implement code, open a PR, or run the CR loop.

### Sonnet worker, per fire (FOUR sub-phases, IN THIS ORDER)

1. **Reconcile (cheap).** For each `pr-created` and not `pr-merged` issue, find its linked PR
   (closing-keyword / `(#N)` / title-mirror; CUDly PRs target a non-default base so auto-close does not
   fire). If MERGED, add `pr-merged`. Never remove `pr-created`.
2. **Conflict-resolve (before CR-advance).** For each `pr-created`, open, `mergeable == CONFLICTING` PR:
   rebase onto the base, push `--force-with-lease` the PR's own branch. STOP + log if a conflict needs a
   semantic human decision. Never address CR findings on a conflicting/stale tree.
3. **CR-advance.** For each `pr-created`, open, NON-conflicting PR: one `pr-iterate` pass (pull CR
   findings, triage, fix minimally + regression test for real bugs, build/lint/tests + pre-commit, push
   the PR's own branch, re-ping `@coderabbitai review` / `full review` on rate-limit recovery). The
   cron is the durable watcher. Never self-merge; never `@coderabbitai resolve`. Active-human guard:
   defer a PR whose latest commit is a human push newer than the last CR review.
4. **Implement (capped).** For each `plan-ready` and not `pr-created` and not `needs-human` issue, up to
   the per-repo implement cap:
   a. Read the `autopilot-branch:` marker; checkout that branch.
   b. **Rebase the auto branch onto the current base** and re-validate `plan.md`; if the plan is stale,
      re-plan inline or defer (add nothing, log staleness) rather than build on a rotten plan.
   c. Implement to repo standards (reuse helpers; no duplication); five-dimension self-review;
      build/lint/tests + pre-commit MUST pass; never `--no-verify`.
   d. **Erase the plan from history**: `git reset --soft <base>`, delete `plan.md`, re-commit ONLY the
      implementation as clean conventional commit(s), so `plan.md` never appears in final history.
   e. Open the PR against the base (`Closes #<issue>` + summary + verification notes); leave the
      runtime's attribution footer in place; mirror the issue's triage rubric onto the PR; add
      `pr-created` to the issue; trigger `@coderabbitai review`. Never self-merge.

> **Correctness is from labels, not the clock.** An Opus plan can exceed 15 minutes, so the worker gates
> on `plan-ready` (and `pr-created`), never on "the previous offset must have finished". Offsets are
> latency tuning; a plan not yet `plan-ready` is picked up by a later worker fire.

## Concurrency model (how the generic model maps onto labels)

Generic model: `pr-orchestration` §"Coordination and concurrency". Instantiation here:

- **Substrate**: GitHub issue labels plus the `autopilot-branch:` comment, the ONLY shared state between
  isolated fires.
- **Disjoint slices**: unlabelled -> plan; `plan-ready` (no `pr-created`) -> implement; `pr-created` +
  open + `CONFLICTING` -> rebase; `pr-created` + open + clean -> CR-advance; merged -> stamp `pr-merged`.
- **Mitigation (a), no simultaneous actors**: cron staggering (planner `:00`, worker `:30` of each
  4-hour window, zero simultaneous fires); each fire must finish within its slot.
- **Mitigation (b), claim-first**: the planner pushes the plan commit, posts the marker and adds
  `plan-ready` immediately on selection; the worker adds `pr-created` at PR-open.
- **Mitigation (c), idempotent gates**: worst case of a lost race is a duplicate `auto/<issue>` branch,
  caught at the `pr-created` gate and cleaned up (close the orphan).
- **Throughput**: to go faster, **prefer raising the per-fire cap or adding target repos** over adding
  routines: each extra routine costs 6+ fires/day against the ~15/day cap. Simultaneous fires also trade
  mitigation (a) for throughput.

## Per-repo config

| Repo | Base branch | Eligibility (plan) | Plan cap/fire | Implement cap/fire | CR-advance cap/fire | Cadence | Status |
|---|---|---|---|---|---|---|---|
| `LeanerCloud/CUDly` | `main` | any `triaged` issue except `type/question`, `status/blocked`, `status/needs-info`, `needs-human` | 2 | 2 | none (all open in-flight PRs) | every 4h (plan `:00` / worker `:30`), 12 runs/day | disabled (pending validation) |
| other `LeanerCloud/*` | repo default unless stated | TBD | TBD | TBD | TBD | TBD | pending |
| `cristim/*` | repo default | TBD | TBD | TBD | TBD | TBD | pending |

## Cost controls

Generic controls (cheap preflight gate, top-tier only on bounded planning/review, per-pass caps):
`pr-orchestration` §"Cost control"; tier selection: `subagent-strategy`. Values here:

- Preflight gates in both routines, so quiet backlogs cost ~nothing.
- Opus runs **only** the bounded planning phase (cap 2 issues/fire); implement and CR-advance run on Sonnet.
- Per-fire plan and implement caps of 2 bound new branches and PRs (and their CR loops) per run.
- The `schedule` API floor is **1 hour**. Staggered offsets x caps is the spend lever; more cadence means
  more routines, not finer crons.

## Run-budget

claude.ai routines are capped at about **15 fires per day, account-wide** across ALL routines. The
two-routine 4-hour design uses 6 planner + 6 worker = **12 fires/day**, leaving about 3/day for manual
`run` calls.

- Do NOT add routines or raise cadence without first checking the remaining budget across all active
  routines in the account.
- Other routines in the same account (a CR re-review drip, a nightly sweep) count against the SAME cap.
  Prefer folding them into an existing phase: the worker's CR-advance already re-triggers CodeRabbit on
  every in-flight PR, subsuming a separate CR drip routine.
- ~15/day is an observed practical cap, not a published limit; treat it conservatively and disable unused
  or exploratory routines promptly.

## Creating and operating the routines

Follow in order; do not skip the validation run.

### Step 1 - Create both routines (disabled)

Both exist (IDs in the routine table), **disabled**. To recreate, use the `schedule` skill
(`RemoteTrigger create`) with the JSON bodies above: environment `env_01DCb7bHtxWMDQZ8MBr67maL`, two git
sources (CUDly + dotclaude), tools `[Bash, Read, Write, Edit, Glob, Grep]`, `enabled: false`, and
`message.content` = the full text of the respective prompt file (the cold agent cannot read it from disk
unless inlined). Planner on the current latest Opus, worker on the current latest Sonnet.

### Step 2 - Validate disabled-first

One manual `run` of the planner, then (after it completes) one of the worker:

```
schedule run trig_01PwHdsKE5bsbrQJRkYJjZAd   # planner
schedule run trig_01GjFaCDmu7moBS6jmBkLa3B   # worker
```

Confirm:
- The cloud env clones both repos.
- The planner reads the prompt from the dotclaude checkout, selects an eligible issue, commits a plan
  branch, posts the `autopilot-branch:` marker and adds `plan-ready`. With no eligible issues, a one-line
  "nothing to plan" summary is expected.
- The worker reconciles in-flight PRs and (if a `plan-ready` issue exists) implements it, opens a PR,
  mirrors labels and triggers `@coderabbitai review`.
- Neither run self-merges or pushes to `main`.

Only after both pass: set `enabled: true` on each.

### Step 3 - Ongoing management

- **Kill switch**: `enabled: false` via `schedule update` or https://claude.ai/code/routines. Prefer
  disabling the planner alone (pauses new work; the worker keeps draining in-flight PRs); disable the
  worker only to stop CR follow-up entirely, because that orphans every triggered PR.
- **Model updates**: when a newer Opus or Sonnet ships, update `model` on both together via
  `schedule update`.
- **No API delete**: remove routines via the UI only.

## Caveats and gotchas (scheduled-specific)

Variant-independent ones (state not clock, rebase before advancing a review, plan staleness, poison-item
guard) are in `pr-orchestration` §§"Coordination and concurrency", "The loops", "Traps". Specific to cron:

- **Correctness is from labels, not the clock**: the worker acts only on durable labels, never on "the
  planner offset must have finished".
- **Conflict-resolve runs as its own phase, before CR-advance**, so every PR reaching CR-advance is
  non-conflicting.
- **Cross-routine atomic-coupling.** The worker routine is the durable watcher for every CR trigger.
  **Disabling the worker routine orphans every triggered PR.**
- **Plan staleness has a wide window** (up to a full cron gap), so the worker rebases the `auto/<issue>`
  branch onto base and re-validates `plan.md` before implementing, re-planning inline or deferring if it
  diverged.
- **Rare double-plan race.** With a single planner, a double plan needs the same planner firing twice
  (manual `run` during a scheduled fire). Worst case: two `auto/<issue>-*` branches. The worker
  implements exactly one (it reads the single `autopilot-branch:` marker; with two markers it takes the
  newest and logs the duplicate), the issue gets `pr-created` once, and the worker closes the orphan
  branch.
- **Poison-item guard**: N=3 consecutive autopilot failures on the same issue (plan or implement) adds
  `needs-human` (adjust N per repo in the config table). Eligibility excludes `needs-human`; a human
  clears it once handled.
- **No subagents / no shared memory** in the cloud env: the reason the design is separate single-model
  routines.

## Known runtime quirks (observed)

- The CCR runtime auto-appends a `Generated by Claude Code` / claude.ai session footer to PR bodies. It
  is left in place (Phase 4i): the runtime adds it deliberately, and stripping it cost an extra
  `gh pr edit` per PR.
- Untracked files under a developer's `~/.claude` (the dotclaude checkout) get wiped by sync. Commit
  anything that must persist.
