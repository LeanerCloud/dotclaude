---
name: subagent-strategy
description: The delegation rubric - model tiers, the PR-shipping tier split, background-first
  execution, agent reuse over re-spawning, and reviewer independence. Invoke when deciding how to
  delegate work or which tier to spawn a subagent on.
---

# Subagent Strategy - Detailed Rubric

Detail extracted from `AGENTS.md` §2 (headline triggers stay there). Invoke when deciding how to delegate, which tier to spawn on, or whether to continue an existing agent.

## Delegate to the cheapest sufficient tier - actively

Before doing work in the main session (or spawning at the parent's tier), ask whether a cheaper subagent can do it per the rubric below; if so spawn it at that tier (Claude Code: `Agent` tool `model` parameter, `SendMessage` to continue an agent, `run_in_background`, parallel calls in a single message; other hosts: skills/HOSTS.md). The main session is usually the most expensive option, so delegating down is the biggest cost lever, and it applies recursively to subagents that spawn subagents.

**Set the model explicitly on EVERY subagent spawn - never rely on inheritance.** An omitted model inherits the parent's, which silently makes every subagent top tier. Pass it even when you want the parent's tier.

## Routine PR-shipping splits across tiers

Plan + review + worktree + implement + test + push + open-PR + ping-CR + arm-CI-watcher is NOT a single-tier workload:

- **Planning + review + verification -> Sonnet 5.5 high (the top tier).** Includes independent verification (reproducing evidence, mutation checks in a scratch copy), the §1b plan file, the plan review gate, §1 post-implementation review, §1c local review, §1a reuse analysis, §5 elegance check, and adversarial money-path reviews. These hold many competing constraints at once; medium-tier plan reviews miss enough that rework costs more than the delta. Nothing escalates above Sonnet 5.5 high, including the hardest money-path reviews and architecture calls.
- **Iteration loops -> triage at Sonnet 5.5 high, the fix at Sonnet 5.5 medium.** For CodeRabbit pass-N findings, fix-push cycles after failed CI, worktree recovery after a watchdog stall, and conflict-resolution rebases. Deciding which findings are real, what the fix is, and whether a dismissal is justified is review (medium triage dismisses real findings as "out of scope" without filing a follow-up); applying the decided fix, pushing and rebasing is coding.
- **Implementation -> Sonnet 5.5 medium, always.** The diff per the plan's tasks (any size or difficulty), tests, running tests/lint/build, opening the PR, mirroring labels, routine `gh`/`git`. Design questions are answered upstream on Sonnet 5.5 high; an implementer that hits one it cannot settle from the plan stops, asks the planner or reviewer (continuing that subagent), and continues with the decided answer. Sonnet 5.5 high does not write the code.
- **Carve-out boundary.** A single mechanical loop step (apply a one-line CR-suggested diff verbatim, push) can run at Sonnet 5.5 low or medium. A step needing judgement about *what* to do goes to Sonnet 5.5 high; the decided fix returns to medium.
- **Scope.** This split is for PR-shipping. Backlog triage uses `triage-labels`; CI watchers are background shell commands (`ci-watch`); the other watchers (`cr-watch-*`, `merge-watch-*`) poll at Sonnet 5.5 low and escalate to Sonnet 5.5 high per `git-commit`.

## Background-first execution (don't block the main chat)

Default to background for anything that does not gate the immediate next step.

- **Background by default.** Long-running or independent subagent work (builds, test/lint suites, CI/deploy/CR/merge watchers, rebases, migrations, sweeps, research fan-outs) runs in the background; the host notifies on completion; **never poll** (status loops re-block the session). Long shell commands run as background shells the same way.
- **Parallelize independent work** in one dispatch (parallel subagent spawns).
- **Foreground only when** the next action consumes the result and you cannot proceed, it is a tight debugging loop, or interactive refinement. If unsure, background it.
- **Wait in a shell, not a model loop.** A polling agent pays its whole context per poll. Arm one background shell command that loops internally (`gh run watch <id> --exit-status`, `gh pr checks <n> --watch`) and end the turn; its exit wakes you. Concurrency caps and one-issue-per-agent: `pr-orchestration` §"Cost control".
- **Keep tool output quiet.** Tests, lint, builds print only failures and summaries (`2>&1 | tail -n 30`, `grep -E 'FAIL|panic|error'`); read only needed file ranges; use `git diff --stat` or path-limited diffs, `head`/`tail` on logs. Every agent starts with a floor of about 75K tokens re-sent each call, so split tasks expected to need more than ~80 tool calls into fresh agents (`pr-orchestration` §"Cost control").
- **Hand control back while work runs**: summarize what is running and what you will do when it lands.
- **Keep verification honest.** Backgrounding must not skip post-implementation review or end-to-end verification (`AGENTS.md` §4); check each result before reporting done.

## Reuse agents before spawning new ones (context economy)

A fresh spawn starts cold. When an earlier agent holds the context, continue it by name or ID so the follow-up costs only the delta (agent-level analog of §1a).

**Check before any spawn**: does a running or finished agent hold the relevant files, diff or investigation thread, AND is its context still small (roughly under 100K tokens)? Signals:
- The follow-up touches the **same files or module** it just read or edited.
- It is the **next round of the same loop** (§1c re-review of an updated diff, CR-fix push to the same branch, watcher follow-up on the same PR/run).
- It is a **follow-up question** to a research/Explore/triage agent about material it surveyed.

Send just the delta ("review the updated diff; findings 1 and 3 fixed in <files>"); after a rebase give a reviewer the `git range-diff`.

**When NOT to reuse** (spawn fresh):
- **Independence is the point** (adversarial verification, AGENTS.md §4, fresh-eyes review, refute-style judging). **A reviewer must never review code it wrote, and never re-bless a change it already approved.** That is about roles, not rounds: the same reviewer re-reviewing after the implementer fixes its findings is correct and cheap, so the reviewer SHOULD persist across rounds of its own loop while reviewer and implementer stay distinct.
  - **Subsystem pooling** gives independence plus warm context when several reviews run at once: pool reviewers by subsystem (e.g. one per cloud provider, API/auth, frontend). An agent warm on a subsystem reviewing a *different* change there is still fresh on that diff; continue it. PR-level consequences: `pr-orchestration`.
- **Wrong tier.** Model is fixed at spawn. If the follow-up needs Sonnet 5.5 high judgement and the warm agent is low/medium, or the follow-up is coding/mechanical and the warm agent is top tier (each turn re-reads its context at top-tier prices), a fresh right-tier spawn is cheaper.
- **Polluted or bloated context** (failed paths, huge tool output, past ~100K tokens). Carried context is re-sent every call: a warm 350K agent on a ~300-call issue re-reads ~120M tokens, a fresh one ramping from ~50K ~45M.
- **New task.** A different issue or PR gets a fresh agent, even in the same repo.

**Tie-breaker**: reuse wins when the follow-up reads the same >2-3 files already loaded; a cold cheaper-tier spawn may win when the briefing is two sentences and the files are small. Compare the files to re-read against the delta message.

**Keep the ramp-up, drop the bloat.** A finishing agent writes a short repo notes file (`~/.claude/projects/<project>/repo-notes.md`: layout, build/test commands, environment gotchas; updated, not appended) and the next fresh agent reads it first.

### Standing rosters: keep agents across a series of PRs, not just across one follow-up

An agent that worked several changes in one subsystem accumulates a model of **how that subsystem fails**, which no briefing transfers.

**Stand up a small named roster of reviewers at the start of a multi-PR session** (typically one per active subsystem) and route by name. Independence still binds: pooling is per subsystem, so a reviewer that reviewed PR A reviews PR B in the same subsystem and never reviews what it wrote. Implementers do not join the roster: each issue gets a fresh one (its context grows with every edit; `pr-orchestration` §"Cost control"). Hand a reviewer off once each call costs several times its first.

Benefits:
- **Implementers apply prior corrections proactively** (e.g. test a wildcard-carrying scope with `len(x) == 0`, not an `IsUnrestricted(x)` helper). Since each issue gets a fresh implementer, carry such corrections into every implementer brief.
- **Reviewers find defects in the *fixes***, not just the original bugs (a fix that closed the less-reachable half of a bug; a guard causing false refusals).
- **The agent that found a bug is the cheapest verifier of its fix elsewhere.**

**Brief the environment delta, not just the task delta.** Every continuation carries what changed around the agent: base branch moved, CI contract changed, another agent now holds a file it is about to edit, an earlier instruction **retracted**. Omissions cause collisions that look like agent error.

**Handoff contract.** A winding-down agent's report must let a successor act without re-deriving:
- **Pin the baseline in both directions**: record the passing-by-coincidence row of a mock/production divergence too, and why it passes, or the successor tightens until the failing row passes and swaps one wrong answer for another.
- **Name the axis actually verified** ("attribution is safe *across packages*; within-file scope unchecked").
- **State what is not covered**, including anything inspected and deliberately left alone.

**An agent flagging its own context depth and handing over is a good outcome.** Say so in the briefing; prefer a clean handoff now, especially before open-ended fan-outs.

**Workflow scripts cannot continue an agent**; each `agent()` call is cold:
- **Partition by file/module, not by step**: one agent owns a file or cluster and does all steps (read, fix, test, verify) in one call.
- Multi-stage pipelines are right when stages need different perspectives (find -> adversarially verify) or tiers; accept the re-read.
- When stage 2 needs only stage 1's *conclusions*, pass them in the prompt (paths, line numbers, findings).

## Model rubric - match tier to task complexity

- **Sonnet 5.5 low / gpt-5.4-mini / Gemini 3.1 Flash-Lite** (default for mechanical delegations): renames, typo fixes, mechanical edits with a clear spec, simple lookups, reading one file for a factual answer, formatting fixes, running a single command (or routine `gh`/`git` op) and reporting output, a tightly-specified function or test, review of a small single-file diff, mechanical API/SDK migration with a documented mapping, classifying/labelling against a clear rubric (triage chunks), summarising one file or short diff.
- **Sonnet 5.5 medium / gpt-5.4 / Gemini 3.1 Flash** (all coding): PR-shipping implementation at any size or difficulty (complex multi-file features, intricate logic), tests, applying a decided fix from a review/CodeRabbit/CI failure, rebases and conflict resolution, refactors, functions with design choices once the plan settles them. For "execute the plan", not "decide the plan" or "judge a reviewer's finding".
- **Sonnet 5.5 high / gpt-5.5 / Gemini 3.1 Pro** (planning, adversarial review and verification only): PR-shipping planning; all review loops (§1c, §1 pre-commit/post-impl, §1d cross-agent PR review, money-path adversarial reviews); architecture/design decisions; triaging review findings in iteration loops (deciding which are real and what the fix is); diagnosing gnarly bugs needing hypothesis iteration; independent verification and reproducing evidence; reading a large unfamiliar codebase from scratch (no Compass graph yet); and peak-critical cases. Nothing escalates above it, and it does not write the code.

**When in doubt about a coding task, stay at Sonnet 5.5 medium** and send the hard design question up to a Sonnet 5.5 high planner or reviewer, then take the decided answer back down; don't escalate the whole task. *Exception*: planning, review and verification default to Sonnet 5.5 high; step down only for a clearly mechanical step, and never step up (high is the ceiling). The main conversation's model is user-set and fixed mid-session; this governs subagent spawns only.

An exact reviewer model pinned by a project-specific rule or skill section overrides this rubric. Project-specific review gates live in each project's `CLAUDE.md`.

## Label-mirroring on PR creation

Every `gh pr create` MUST be followed by mirroring the closing issue's triage labels onto the PR: `priority/*`, `severity/*`, `urgency/*`, `impact/*`, `effort/*`, `type/*`, plus `triaged` (only if the issue carries it; never invent it). Unlabeled PRs are invisible to priority queries. It is part of the open-PR step. For PRs closing multiple issues take the highest `priority/*` and `severity/*` and union the rest. When delegating PR shipping, include this step in the subagent's prompt.
