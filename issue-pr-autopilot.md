# Issue -> PR Autopilot (scheduled, multi-routine, multi-tier)

A continuous delivery loop driven by GitHub issue labels. **Multiple** scheduled
**remote** Claude Code agents (created via the `schedule` skill / `RemoteTrigger`)
fire on staggered crons, plan and open PRs for eligible issues, and stamp labels
that act as the state machine. The label *is* the dedup guard and the handoff
medium between tiers.

This file is the source of truth for the per-repo config and the two routine
prompts (`issue-pr-autopilot.plan.prompt.md` and
`issue-pr-autopilot.worker.prompt.md`, same directory). It composes existing
machinery, it does not duplicate it:

- Selection / triage rubric -> `triage.md` (work selection + label rubric)
- Plan -> worktree -> implement -> review -> PR -> `CLAUDE.md`, `worktrees.md`,
  `coding-standards.md`, `conventions.md`
- Commit/PR hygiene + post-PR CR loop -> `git-workflow.md`
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

The `schedule` API floor is **1 hour** (`*/30` is rejected), but multiple routines
can run at different minute-offsets. Staggering two planners and two workers within
the hour roughly doubles throughput under the floor.

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

## Routines (4 total: 2 Opus planners + 2 Sonnet workers, staggered)

All four routines run in environment **Default** (`anthropic_cloud`,
`env_01DCb7bHtxWMDQZ8MBr67maL`), with two git sources (the target repo +
`LeanerCloud/dotclaude`) and tools `[Bash, Read, Write, Edit, Glob, Grep]`. The
planners run `issue-pr-autopilot.plan.prompt.md`; the workers run
`issue-pr-autopilot.worker.prompt.md`. Offsets stagger plan -> work -> plan -> work
across the hour so a plan from `:00` can be implemented at `:15`, and a plan from
`:30` at `:45`.

| Routine | Cron (UTC) | Model | Prompt | Role |
|---|---|---|---|---|
| `cudly-autopilot-plan-a` | `0 * * * *` | latest available Opus model | `issue-pr-autopilot.plan.prompt.md` | plan top eligible issue(s) |
| `cudly-autopilot-work-a` | `15 * * * *` | latest available Sonnet model | `issue-pr-autopilot.worker.prompt.md` | reconcile + conflict-resolve + CR-advance + implement |
| `cudly-autopilot-plan-b` | `30 * * * *` | latest available Opus model | `issue-pr-autopilot.plan.prompt.md` | plan top eligible issue(s) |
| `cudly-autopilot-work-b` | `45 * * * *` | latest available Sonnet model | `issue-pr-autopilot.worker.prompt.md` | reconcile + conflict-resolve + CR-advance + implement |

> Set the planner model to the **current latest Opus model id** and the worker to
> the **current latest Sonnet model id** at routine-creation time. Refresh both
> together when a newer generation ships so they stay on the same generation.

### RemoteTrigger create-bodies

Each routine is created with the shape below. Generate a fresh lowercase v4 UUID
for `events[].data.uuid` per routine. Substitute `CRON`, `MODEL`, and
`PROMPT_FILE_CONTENTS` per the table above (`session_context.message.content` is
the full text of the prompt file inlined, since the cloud agent is cold). The two
git sources let the cold agent read this repo's guidelines plus the target repo.

```json
{
  "name": "cudly-autopilot-plan-a",
  "cron_expression": "0 * * * *",
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

- `cudly-autopilot-plan-b`: identical except `name`, `cron_expression: "30 * * * *"`,
  and a fresh `uuid`. Same (latest available Opus) model and plan prompt.
- `cudly-autopilot-work-a`: same shape with `name: "cudly-autopilot-work-a"`,
  `cron_expression: "15 * * * *"`, `model: "<current latest Sonnet model id>"`,
  `message.content` = full text of `issue-pr-autopilot.worker.prompt.md`, and a
  fresh `uuid`.
- `cudly-autopilot-work-b`: same as work-a except `name` and
  `cron_expression: "45 * * * *"`, and a fresh `uuid`.

Create all four with `enabled: false`, do one `run` of each to confirm the cloud
env can clone both sources + plan / implement / open a PR + edit labels, then
`enable`. You **cannot delete** via API; use https://claude.ai/code/routines.

## Watcher model (scheduled context differs from a local session)

Neither routine can spawn `cr-watch`/`ci-watch` background agents (no Agent/Task
tool; background processes die when the bounded run ends). The local
git-workflow.md "spawn a watcher" steps do not apply literally. The
atomic-coupling invariant (never request a CR review without a watcher that acts
on the response) is satisfied **structurally and cross-routine**: every PR the
worker triggers/re-pings stays in the in-flight set (`pr-created`, not
`pr-merged`, PR open) and is therefore guaranteed to be re-examined by the next
worker fire. **The worker cron is the durable watcher** for both planner-spawned
and human-spawned PRs. CR rate-limit recovery uses `@coderabbitai full review`
(not the incremental form), per git-workflow.md.

## Plan handoff (the durable contract between the two tiers)

The planner and the worker never run in the same process. They communicate only
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

## Concurrency model (labels are the lock - with caveats)

- **Coordination substrate.** GitHub issue labels plus the `autopilot-branch:`
  issue comment are the ONLY shared state between isolated routine fires (no shared
  memory; see "no subagents / no shared memory" above). Each phase acts on a
  **disjoint slice** of issues/PRs so phases never fight over the same item:
  unlabelled -> plan; `plan-ready` (no `pr-created`) -> implement;
  `pr-created` + open + `CONFLICTING` -> rebase; `pr-created` + open + clean ->
  CR-advance; merged -> stamp `pr-merged`.
- **Labels are NOT atomic locks (the load-bearing caveat).** GitHub has no
  compare-and-swap on labels, so there is a TOCTOU window: two routines firing AT
  THE SAME TIME can both read an item as unclaimed before either writes the claim
  label, and both act on it (double plan / double PR). This is the one real
  concurrency hazard, and the root of the "rare double-plan race" caveat above.
- **Mitigations (all three required):**
  (a) **Stagger offsets so no two routines fire simultaneously** and each fire
  finishes within its slot - never schedule two routines at the same minute-offset
  (the `:00/:15/:30/:45` grid above has zero simultaneous fires).
  (b) **Claim as the FIRST durable action**: the planner pushes the plan commit,
  posts the `autopilot-branch:` marker, and adds `plan-ready` immediately on
  selection; the worker adds `pr-created` at PR-open. This shrinks the TOCTOU
  window to that tight sequence.
  (c) **Every downstream gate is idempotent** so a lost race degrades gracefully:
  worst case is a duplicate `auto/<issue>` branch, caught at the `pr-created` gate
  and cleaned up (close the orphan), per the double-plan caveat above.
- **Throughput vs the 1h floor.** 15-minute activity = the FOUR staggered routines
  on the `:00/:15/:30/:45` grid alternating Opus (plan) / Sonnet (worker); that
  gives something happening every 15 minutes with ZERO simultaneous fires. To go
  faster, **prefer raising the per-fire cap or adding target repos** over stacking
  multiple routines at the same offset: simultaneous fires trade the correctness
  margin above for throughput. Literal duplication of a routine at the same minute
  buys nothing - a failed fire is retried by the next cycle anyway - and only adds
  race risk.

## Per-repo config

| Repo | Base branch | Eligibility (plan) | Plan cap/fire | Implement cap/fire | CR-advance cap/fire | Cadence | Status |
|---|---|---|---|---|---|---|---|
| `LeanerCloud/CUDly` | `feat/multicloud-web-frontend` | any `triaged` issue except `type/question`, `status/blocked`, `status/needs-info`, `needs-human` | 2 | 2 | none (all open in-flight PRs) | planners `0,30 * * * *`; workers `15,45 * * * *` (UTC) | disabled (pending validation) |
| other `LeanerCloud/*` | repo default unless stated | TBD | TBD | TBD | TBD | TBD | pending |
| `cristim/*` | repo default | TBD | TBD | TBD | TBD | TBD | pending |

## Cost controls

- Cheap preflight gate in both routines so quiet backlogs cost ~nothing.
- Opus runs **only** the bounded planning phase (cap 2 issues/fire); the long
  implement and CR-advance loops run on Sonnet. This is the whole point of the
  split: top-tier reasoning only where judgement is the hard part.
- Per-fire plan and implement caps bound new branches and PRs (and downstream CR
  loops) per run.
- The `schedule` API floor is **1 hour** (`*/30` is rejected). Staggered offsets x
  caps is the spend lever; raising cadence means more routines, not finer crons.

## Operating the routines

- Manage via the `schedule` skill (`RemoteTrigger`: list/get/create/update/run).
  You **cannot delete** via API; use https://claude.ai/code/routines.
- **Always test first**: create all four disabled, do one `run` of each, confirm
  the cloud env can clone both sources + plan / implement + open a PR + edit
  labels, then `enable`.
- **Kill switch**: update `{enabled:false}` on a routine, or toggle in the routines
  UI. **Disabling the worker routines orphans every triggered PR** (no other
  process is the CR watcher), so the worker routines are the load-bearing half:
  disable planners alone to pause new work while still draining in-flight PRs;
  disable workers only when you intend to stop CR follow-up entirely.

## Caveats and gotchas (load-bearing)

- **Correctness is from labels, not the clock.** Restated because it is the
  crux: offsets are latency tuning; the worker only ever acts on durable labels.
- **Conflict-resolve before CR-advance.** Never address findings on a
  conflicting/stale tree; CR-advance skips `CONFLICTING` PRs, which
  conflict-resolve has already rebased in the same fire.
- **Cross-routine atomic-coupling.** The worker routine is the durable watcher for
  every CR trigger (the PR stays in the in-flight set and the next worker fire WILL
  re-examine it). **Disabling the worker routine orphans every triggered PR.**
- **Plan staleness.** A `plan-ready` branch can rot if the base advances; the
  worker rebases the `auto/<issue>` branch onto base and re-validates `plan.md`
  before implementing, re-planning inline or deferring if the plan has diverged.
- **Rare double-plan race.** If both staggered planners grab the same issue before
  either claim (`plan-ready`) lands, worst case is two `auto/<issue>-*` branches.
  The worker implements exactly one (it reads the single `autopilot-branch:`
  marker; if two markers exist it takes the newest and logs the duplicate), the
  issue gets `pr-created` once, and the worker closes the orphan `auto/<issue>`
  branch as cleanup.
- **Poison-item guard.** After **N consecutive autopilot failures** on the same
  issue (plan-or-implement), add `needs-human` so a bad item does not retry every
  hour forever. Eligibility excludes `needs-human`. A human clears it once handled.
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
