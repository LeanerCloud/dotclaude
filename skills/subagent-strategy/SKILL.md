---
name: subagent-strategy
description: The delegation rubric - stable task roles, background-first execution, agent reuse over
  re-spawning, and reviewer independence. Invoke when deciding how to delegate work or which model
  ID to use for a subagent.
---

# Subagent Strategy — Detailed Rubric

Detail extracted from `CLAUDE.md` §2. The headline triggers stay in `CLAUDE.md`; the rationale, the model rubric, and the PR-shipping tier split live here. Invoke this skill when deciding how to delegate work, which model tier to spawn a subagent on, or whether to continue an existing agent instead of spawning a new one.

## Select the stable task role explicitly

Choose a role from `CLAUDE.md` before selecting a host-advertised model ID. Planning, design
decisions, adversarial review, and independent verification use the planning/review role.
Implementation uses the implementation role after the shape is settled. Tightly specified lookups,
renames, formatting, and single-command work use mechanical assistance. These roles apply equally
to Codex, Anthropic, and Gemini; provider mappings live in `CLAUDE.md`.

**Set the `model` parameter on EVERY `Agent` call - never rely on inheritance.** Use the explicit
host-advertised ID that fulfills the selected role. Future model families, versions, reasoning modes,
and flavors retain the role semantics; a name alone does not establish capability. Do not silently
substitute a weaker model or bypass an exact pin when unavailable. Report the limitation instead.

## Routine PR-shipping role split

The standard pattern (plan + review + worktree + implement + test + push + open-PR + ping-CR + arm-CI-watcher) is not a single-role workload:

- **Planning/review role.** Drafting and reviewing plans, resolving architecture or debugging choices, local review loops, adversarial review, and independent verification.
- **Implementation role.** Applying a settled task, tests, routine workflow commands, and PR mechanics. Return unexpected material ambiguity to the planning role.
- **Carve-out boundary.** A single mechanical step within an iteration loop remains mechanical assistance. Move to the planning/review role when the step requires judgment about what to do.
- **Scope.** This split applies to PR-shipping. Other workflows still use their own skills, while their model choices resolve through the same roles.

## Background-first execution (don't block the main chat)

The main session is the user's interactive channel; blocking it on work that could run detached wastes their time. Default to background for anything that does not gate the immediate next step.

- **Background by default.** Subagent work that is long-running or independent - builds, full test/lint suites, CI/deploy/CR/merge watchers, rebases, migrations, codebase-wide sweeps, research fan-outs - is spawned with `run_in_background: true`. The harness notifies you on completion; **never poll** (`TaskOutput` / status loops just re-block the main session). Long shell commands (builds, test suites, `terraform plan`, large downloads) use Bash `run_in_background: true` the same way.
- **Parallelize independent work.** When several tasks do not depend on each other, dispatch them in a single message (parallel `Agent` calls / one batch) rather than serially.
- **Foreground only when** the very next action consumes the result and you cannot proceed without it, it is a tight debugging loop where each step informs the next, or it is interactive refinement with the user. When unsure whether the result gates the next step, background it and move other work forward.
- **Hand control back while work runs.** After dispatching background work, return to the user or pick up the next independent task instead of idling - summarize what is running and what you will do when it lands.
- **Keep verification honest.** Backgrounding must not skip the post-implementation review or end-to-end verification (CLAUDE.md section 4). Collect and check each background result before reporting it done: a launched agent is not a completed one.

## Reuse agents before spawning new ones (context economy)

Every fresh `Agent` spawn starts cold: it re-reads the project docs, re-greps, and re-loads every file it needs before doing anything useful. When an agent from earlier in the session already holds that context, continuing it via `SendMessage` (by agent ID or name) makes the follow-up cost only the delta. This is the agent-level analog of §1a "reuse before writing": check what already exists before creating something new.

**The check, before any spawn**: does a running or recently finished agent already have the relevant files, diff, or investigation thread in context? Signals that it does:

- The follow-up touches the **same files or module** the agent just read or edited (fix findings in code it wrote, extend a change it made, answer another question about the area it explored).
- It is the **next round of the same loop**: §1c re-review of an updated diff, a CR-fix push to the same branch, a watcher follow-up on the same PR/run.
- It is a **follow-up question** to a research/Explore/triage agent about material it already surveyed.

In all of these, send the agent the new instruction with just the delta ("review the updated diff; previous findings 1 and 3 were fixed in <files>") instead of a full cold briefing.

**When NOT to reuse** (spawn fresh instead):

- **Independence is a role and context boundary.** Start the reviewer without the author's transcript.
  For Codex, use `fork_turns="none"` when supported; for Anthropic, use a separate session or
  subagent with a self-contained brief and no author transcript. Keep planner, implementer, and
  reviewer separate. Reuse that independent reviewer across plan revisions, fix loops, and final
  verification when its context remains current. Reusing it does not
  mean trusting prior approval: re-read the full relevant final artifacts, obtain local evidence, and
  challenge assumptions, root cause, requirements, simpler alternatives, and failure cases at every
  gate.
  - **Subsystem pooling** preserves independence while saving context: a reviewer warm on a subsystem
    may continue reviewing the same stream through a changed SHA after re-reading the final artifacts.
    Spawn fresh for authorship or role conflicts, material anchoring or missed findings, polluted or
    stale context, unrelated work, or an explicit independent gate. See `pr-orchestration` for pooling.
- **Wrong role.** An agent's model is fixed at spawn. If a follow-up needs planning/review judgment
  and the warm agent is implementation or mechanical assistance, use a fresh model ID for the right
  role rather than continuing with the wrong role.
- **Polluted or bloated context.** The agent went down failed paths, accumulated huge tool output, or is near its context limit. A fresh agent with a tight briefing beats a confused warm one.
- **Unrelated task.** Overlap in time is not overlap in context; don't funnel misc work through one long-lived agent.

**Tie-breaker**: when the follow-up reads the same >2-3 files the agent already loaded, reuse usually
wins within the same role; when independence or a different role is required, spawn fresh.

**`Workflow` scripts have no SendMessage**: each `agent()` call is a cold start. Get the same economy structurally:

- **Partition by file/module, not by step.** One agent owns each file or cluster and performs ALL steps on it (read, fix, test, verify) in a single `agent()` call, instead of a per-step pipeline where stage 2's agent re-reads everything stage 1's agent just read.
- Multi-stage pipelines are still right when stages genuinely need different perspectives or roles;
  accept the re-read there because it buys independence.
- When stages must stay separate but stage 2 only needs stage 1's *conclusions*, pass them in the prompt (file paths, line numbers, findings) so stage 2 reads only the cited spans, not the whole surface again.

## Role rubric - match work to the task

- **Planning/review role**: plans, architecture and debugging choices, all review loops, adversarial
  review, and independent verification. Resolve ambiguous design choices here before handoff.
- **Implementation role**: settled-shape code or documentation changes, tests, and routine PR work.
- **Mechanical assistance**: clear lookups, renames, formatting, single-command runs, and short
  summaries.

An exact reviewer model pinned by a project-specific rule or skill section overrides this rubric.
Do not replace that pin with a role-equivalent model. See the `pr-lifecycle` skill's CUDly final-HEAD
gate for its exact reviewer and separate verification role.

## Label-mirroring on PR creation

Every `gh pr create` MUST be followed by mirroring the closing issue's triage labels onto the new PR: `priority/*`, `severity/*`, `urgency/*`, `impact/*`, `effort/*`, `type/*`, plus `triaged` (only if the issue carries it — never invent it). PRs without triage labels are invisible to the same priority queries that surface the issues, so an unlabeled PR is effectively unreviewable in priority order. Treat label-mirroring as part of the `open-PR` step. For PRs closing multiple issues, take the highest `priority/*` and `severity/*` across the set and union the rest. When delegating PR shipping to a subagent, include this step in the prompt explicitly.
