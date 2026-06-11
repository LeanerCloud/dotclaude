# Subagent Strategy — Detailed Rubric

Detail extracted from `CLAUDE.md` §2. The headline triggers stay in `CLAUDE.md`; the rationale, the model rubric, and the PR-shipping tier split live here. Read this when deciding how to delegate work, which model tier to spawn a subagent on, or whether to continue an existing agent instead of spawning a new one.

## Delegate to the cheapest sufficient tier — actively

Before doing a piece of work in the main session (or spawning a subagent at the parent's tier), ask: *can a cheaper Claude, OpenAI, or Gemini subagent handle this per the rubric below?* If yes, spawn it with the `Agent` tool's `model` parameter. The main session's tier is typically the most expensive option available, so reserving it for work that genuinely needs it is the single biggest cost lever. Treat the rubric as a positive obligation to delegate down, not just a tie-breaker. This applies recursively: a subagent that needs to spawn further subagents also defaults to the cheapest sufficient tier.

**Set the `model` parameter on EVERY `Agent` call — never rely on inheritance.** Omitting it makes the subagent inherit the parent's model, which when the parent is Opus silently makes every subagent Opus too: a 5-10x cost premium for work that almost always doesn't need it. Even when you want the parent's tier, pass it explicitly so the choice is visible at the call site.

## Routine PR-shipping splits across tiers

The standard pattern (plan + 3-pass review + worktree + implement + test + push + open-PR + ping-CR + arm-CI-watcher) is NOT a single-tier workload:

- **Planning phase -> Opus.** Drafting the §1b plan file, the three-pass plan review gate, the §1 post-implementation review, the §1a reuse analysis, and the §5 elegance check all require holding multiple competing constraints in working memory at once: invariants from the issue body, lessons from prior commits, cross-cutting test impact, edge cases the acceptance criteria didn't enumerate. Sonnet plan reviews miss enough subtleties on real PR work that the rework cost exceeds the up-front Opus delta.
- **Iteration loops -> Opus.** Responding to CodeRabbit pass-N findings, fix-push cycles after a failed CI run, worktree-recovery after a watchdog stall, the §1c local review loop, and conflict-resolution rebases all share one shape: react to feedback that didn't fit the original plan without breaking what already worked. Sonnet stalls here; the triage surface grows each round and a Sonnet-tier triage either dismisses real findings as "out of scope" without filing a follow-up or produces fixes that don't exercise the contract under review.
- **Implementation phase -> Sonnet.** Writing the diff per the plan's task breakdown, running tests/lint/build, opening the PR, mirroring labels, routine `gh`/`git` mechanics. With a clean plan in hand and design questions answered upstream, Sonnet ships diffs cleanly across multi-file changes.
- **Carve-out boundary.** A single mechanical step within an iteration loop (apply a one-line CR-suggested diff verbatim, push) is still Haiku/Sonnet-able. Escalate to Opus when the loop step requires judgement about *what* to do, not just executing a decided fix. (Same rule of thumb as §1b's "skip the worktree only for trivially mechanical edits".)
- **Scope.** This split applies to PR-shipping. Other workflows have their own rubrics: backlog triage uses `triage.md`; routine watchers (`ci-watch-*`, `cr-watch-*`, `merge-watch-*`) use polling-on-Haiku, escalate-on-Opus per `git-workflow.md`.

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

- **Independence is the point.** Adversarial verification (CLAUDE.md §4), fresh-eyes review, refute-style judging: a verifier that shares the implementer's context inherits its blind spots. Reviewer and implementer stay distinct agents; but the same reviewer SHOULD persist across rounds of its own loop.
- **Wrong tier.** An agent's model is fixed at spawn. If the follow-up needs Opus judgement and the warm agent is Haiku/Sonnet (or the follow-up is mechanical and the warm agent is Opus, where each continued turn re-reads its whole accumulated context at Opus prices), a fresh right-tier spawn is cheaper than a wrong-tier continuation.
- **Polluted or bloated context.** The agent went down failed paths, accumulated huge tool output, or is near its context limit. A fresh agent with a tight briefing beats a confused warm one.
- **Unrelated task.** Overlap in time is not overlap in context; don't funnel misc work through one long-lived agent.

**Tie-breaker**: when the follow-up reads the same >2-3 files the agent already loaded, reuse usually wins; when the briefing is two sentences and the files are small, a cold spawn at a cheaper tier may still be cheaper. Decide by which context is larger: the files to re-read, or the delta message.

**`Workflow` scripts have no SendMessage**: each `agent()` call is a cold start. Get the same economy structurally:

- **Partition by file/module, not by step.** One agent owns each file or cluster and performs ALL steps on it (read, fix, test, verify) in a single `agent()` call, instead of a per-step pipeline where stage 2's agent re-reads everything stage 1's agent just read.
- Multi-stage pipelines are still right when stages genuinely need different perspectives (find -> adversarially verify) or different tiers; accept the re-read there, it buys independence.
- When stages must stay separate but stage 2 only needs stage 1's *conclusions*, pass them in the prompt (file paths, line numbers, findings) so stage 2 reads only the cited spans, not the whole surface again.

## Model rubric — match tier to task complexity

- **Haiku / gpt-5.4-mini / Gemini 3.1 Flash-Lite** (default for most delegations): file renames, typo fixes, mechanical edits with a clear spec, simple lookups (grep for a symbol, find where X is called), reading one file to answer a factual question, formatting/style fixes, running a single command (or routine `gh`/`git` op) and reporting output, implementing a tightly-specified function, writing a test from a tight spec, code review of a small single-file diff, mechanical API/SDK migration with a documented mapping, classifying/labelling against a clear rubric (e.g. triage chunks), summarising one file or short diff. Cheap, fast, good enough when the answer is mostly mechanical or rubric-driven.
- **Sonnet / gpt-5.4 / Gemini 3.1 Flash**: the implementation phase of PR-shipping; focused multi-file changes where coordination needs judgement but the target shape is decided; implementing a function whose spec is mostly clear but has 1-2 design choices; code review of a multi-file or non-trivial-logic diff; refactors with a clear target shape. Use when design questions are answered upstream and the work is "execute the plan", not "decide the plan" or "react to a reviewer".
- **Opus / gpt-5.5 / Gemini 3.1 Pro** (or stay on the top-level model): the planning phase and the iteration loops of PR-shipping (per the split above); architecture decisions; multi-file refactors where the shape is unclear; debugging gnarly bugs needing hypothesis iteration; reading a large unfamiliar codebase from scratch (no `graphify-out/`) to synthesise a mental model; any work where "understanding" or "weighing options" is the hard part.

**When in doubt, go one tier cheaper and see if it's good enough** — for implementation, research, and mechanical work; re-spawn stronger if it struggles. *Exception*: planning phases and iteration loops default to Opus and step down only when the specific step is clearly mechanical. The cost of a stalled iteration agent that needs main-session takeover exceeds the up-front Opus delta. The main conversation's model is user-set and fixed mid-session; this rule only governs `Agent` spawns.

## Label-mirroring on PR creation

Every `gh pr create` MUST be followed by mirroring the closing issue's triage labels onto the new PR: `priority/*`, `severity/*`, `urgency/*`, `impact/*`, `effort/*`, `type/*`, plus `triaged` (only if the issue carries it — never invent it). PRs without triage labels are invisible to the same priority queries that surface the issues, so an unlabeled PR is effectively unreviewable in priority order. Treat label-mirroring as part of the `open-PR` step. For PRs closing multiple issues, take the highest `priority/*` and `severity/*` across the set and union the rest. When delegating PR shipping to a subagent, include this step in the prompt explicitly.
