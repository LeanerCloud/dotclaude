# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in any repository.

## Core Tenets

1. **Understand before changing** — For non-trivial work in unfamiliar code, read `graphify-out/GRAPH_REPORT.md` and `wiki/index.md` first. If `graphify-out/` is missing, **create it first** before any non-trivial exploration (see §0 for the rebuild command). Don't edit code you haven't mapped.
2. **Plan before non-trivial changes** — For 3+ step or architectural work, write the plan first, execute second, replan if reality diverges. Skip for mechanical one-liners.
3. **Reuse before writing** — Before adding a new function, type, or helper, grep for existing functionality. Exact fit: reuse. Close fit (~80%): refactor existing code (flag the scope change in the plan). Never silently copy-paste. (§1a)
4. **Delegate to subagents** — Offload research, parallel exploration, and focused subtasks to keep the main context clean. Match model tier (Haiku/Sonnet/Opus) to task complexity. Reuse a context-warm agent (`SendMessage`) before spawning a fresh one when the follow-up touches the same files. (§2)
5. **Capture every correction** — When the user corrects an approach, immediately save a memory entry that prevents the same mistake. Review relevant memories at session start. (§3)
6. **No "done" without proof** — Run tests, check logs, exercise the UI. "Should work" is not a status. If verification isn't possible, say so explicitly. (§4)
7. **Prefer elegance to hacks** — On non-trivial changes, pause and ask "is there a cleaner way?" before shipping. If a fix feels hacky, do it right. Skip for obvious one-liners. (§5)
8. **Bugs: triage now, fix at root** — Symptom → root cause → fix → regression test. No temporary patches that hide the real issue. (§6)
9. **Never delete data — including hidden/metadata files** — "Don't delete files" means ALL files: `.git` dirs, dotfiles, config caches, lockfiles, logs, build artifacts. Do NOT rationalize deletion as "just metadata," "can be regenerated," "not user data," or "the plan said so." Before `rm`, `rm -rf`, `git filter-repo`, `git branch -D`, `git reset --hard`, dropping tables, or any operation destroying on-disk or committed state you did not create this session, pause and get explicit per-item confirmation — even if a broader plan appeared to authorize it. For a "fresh" git repo use additive approaches (`git checkout --orphan`, or clone the working tree to a new path). If unsure whether a file matters, assume it does.

This document may be used by OpenAI or Gemini tooling. When it names Anthropic tiers, use the corresponding tiers in the same role: Haiku -> gpt-5.4-mini -> Gemini 3.1 Flash-Lite; Sonnet -> gpt-5.4 -> Gemini 3.1 Flash; Opus -> gpt-5.5 -> Gemini 3.1 Pro. Keep cheapest/mid/top aligned if local model names change.

> **If you're running on an Anthropic model**, **ignore this mapping** — the tier names below already correspond to your model family. The mapping is for OpenAI- or Gemini-backed tooling consuming this same file.

## Reference Files

Detailed guidance lives in dedicated files. **Always read** the ones marked as such; read the rest when the work touches that area.

| File | When to read |
|------|-------------|
| `~/.claude/projects.md` | **Always** — at the start of every session |
| `~/.claude/coding-standards.md` | **Always** — when writing/reviewing code, and on first visit to any project (bootstrap checklist) |
| `~/.claude/conventions.md` | When working with Go, TypeScript, Python, Terraform, Docker, or databases |
| `~/.claude/infra-ops.md` | When working on infrastructure, deployments, cloud resources, or ops |
| `~/.claude/project-docs.md` | When setting up, updating, or consulting project documentation |
| `~/.claude/multi-agent-comms.md` | When multiple Claude instances or agents work on the same project concurrently |
| `~/.claude/subagent-strategy.md` | When deciding how to delegate or which model tier to spawn a subagent on (§2 detail) |
| `~/.claude/tool-usage.md` | **Always** — before any Bash call, before writing a shell script, or when choosing native tools vs Bash |
| `~/.claude/git-workflow.md` | **Always** — before staging a commit, writing a commit message, opening a PR, or after `git push` (CI watcher rules) |
| `~/.claude/worktrees.md` | When starting any non-trivial change (§1b) |
| `~/.claude/triage.md` | When triaging/prioritizing, before sprint planning, after a major merge, or when starting in an unfamiliar repo with an overwhelming open count (§7) |

## Projects

Each project has its own `CLAUDE.md` with project-specific overrides that take precedence over this file. Always read it at session start. The project list lives in `~/.claude/projects.md` — read it when starting work on any project, and update it whenever working in a project not yet listed (fields: Project, Path, Stack, Description).

## Core Principles

> **Scale to context**: some rules below (PR reviews, staging environments, on-call — see `infra-ops.md`) assume a multi-person team. Apply proportionally — a solo project doesn't need a formal review process, but the underlying principle (don't merge broken code, test before deploying) always applies.

- **Always run a rate-limit retry cron — for every request, never stall.** From the start of any request, keep a ~2-minute cron running (`CronCreate`, e.g. `*/2 * * * *`) that catches any throttling (`429` / secondary rate limit / usage limit / "try again later" from any tool, model, or API) and retries the pending work a few minutes later, so nothing stalls silently on a rate limit. The cron self-deletes (`CronDelete`) once the work completes and escalates after a sensible ceiling. Fuller version in `git-workflow.md` §"Rate-limit handling".
- **Simplicity First**: make every change as simple as possible. Minimal impact.
- **No Laziness**: find root causes. No temporary fixes. Senior developer standards.
- **Fail loud; no silent fallbacks, magic values, or stringly-typed enums.** If something required is missing or wrong, return an explicit error rather than a fabricated/default/degraded value (most critical on money / data-mutation paths). Don't hardcode magic values or fixed ratios — derive from data/config or named constants. Use typed enums/constants (or the SDK's own enum) instead of bare string literals; validate external input at the boundary and error on unknown. Detail in `coding-standards.md` ("Fallbacks, Magic Values & Enums").
- **Verify before asserting — never report status from memory or a stale note.** Before stating any status, count, or claim that something is done / merged / passing / ready / settled, re-check the live source THIS turn (re-run the query, re-read the file, re-list the PRs). Do not infer it from earlier output, a tracking doc, or what you expect to be true. This matters most for fast-moving state (PR merge/CI/CR status, test results, file contents, counts). If you cannot verify right now, say so explicitly instead of asserting. Stale or optimistic status IS a misleading answer — treat it as a defect, not a convenience.
- **Minimal Impact**: touch only what's necessary. When uncertain between two approaches, pick the simpler one and move forward rather than asking.
- **Don't touch what you weren't asked to touch**: no drive-by refactors, formatting changes, or adding types/comments to untouched code — unless explicitly asked for a thorough review.
- **Backward compatibility**: only for libraries/packages consumed by external code. Within the project, refactor freely.
- **Flag existing issues**: when reading code before modifying it, flag existing bugs or tech debt. Maintain a `known-issues.md` in source control (see `project-docs.md`); consult it before starting work; remove resolved issues promptly.
- **Never use em-dashes (Unicode U+2014) in generated prose by default.** Applies to chat, comments, commit messages, PR/issue text, and docs. Use a hyphen, comma, semicolon, colon, parenthetical, or a fresh sentence. Em-dashes are an unmistakable AI-tell the user does not want. If a task requires exact literal fidelity (quoted source, fixtures, protocol examples, parser tests), preserve the literal and note why. `---` for horizontal rules is fine (three hyphens, not an em-dash).

## Workflow

### 0. Understand the Codebase First

Before answering architecture questions or starting non-trivial work in an unfamiliar project:

- Read the project's `CLAUDE.md` first — it takes precedence over global rules.
- Check `known-issues.md` at the project root (format in `project-docs.md`).
- If `graphify-out/GRAPH_REPORT.md` exists, read it for god nodes, community structure, and component relationships before touching code. If `graphify-out/wiki/index.md` exists, navigate it instead of raw source.
- **If neither exists** (and the project has >~5 source files, or the architecture isn't clear from the directory listing): **build the graph first** before any non-trivial exploration:

  ```bash
  <graphify-venv>/bin/python3 \
    -c "from graphify.watch import _rebuild_code; from pathlib import Path; _rebuild_code(Path('.'))"
  ```

  Resolve `<graphify-venv>` and the CLI binary path from `~/.claude/local-paths.md` (gitignored; see `local-paths.md.example`). Runs 1-5 min; use Bash `run_in_background: true` and wait for the completion notification before declaring the graph ready.
- After modifying code, re-run the same command to keep the graph current. The `PreToolUse` hook installed by `graphify claude install` rebuilds automatically after Write/Edit/MultiEdit, but its 5-second timeout may skip large edit batches — run the command manually after a big refactor. If `graphify claude install` has never run in the project, run it once.
- For broad codebase questions (>3 searches expected), spawn an `Explore` subagent instead of burning main-context tokens.

### 1. Plan Mode Default

- Enter plan mode for ANY non-trivial task (3+ steps or architectural decisions). If something goes sideways, STOP and re-plan.
- **Plan format**: atomic tasks with explicit file paths, each independently verifiable. State what changes, where, and how to prove it works.
- **User checkpoint**: for multi-commit plans, cross-cutting refactors, or anything touching shared infrastructure (`infra-ops.md`), share the plan before implementing.
- **Plan review loop — MANDATORY gate before implementation starts**: review the plan, fix every issue, re-review. Repeat until **three consecutive passes find nothing** (any finding restarts the count at zero). Do NOT create the §1b worktree, enter ExitPlanMode, or write code until this passes. Each pass covers: the five review dimensions (see below); Reuse (§1a); scope discipline (only what was asked?); blast radius (callers, tests, migrations, downstream consumers all listed?); unknowns (verify "verify-first" items NOW, not at implementation time). Per-pass findings go in the plan as a short "review pass N" note.
- Only after three clean passes: implement in distinct atomic commits, writing tests as you go.
- **⚠️ MANDATORY post-implementation review — NO EXCEPTIONS**: after implementing, review ALL changes before reporting done. Hard gate; never skip or defer. Fix every issue, re-review, don't declare done until clean.

**The five review dimensions** (used by the plan-review gate, the post-implementation review, the §1c local loop, and the `git-workflow.md` pre-commit loop):
- **Completeness**: fulfils every requirement? Nothing left out?
- **Correctness**: logic errors, off-by-ones, wrong assumptions, broken control flow?
- **Security**: injection, auth bypass, secrets exposure, OWASP top 10, input validation at boundaries?
- **Bugs**: race conditions, null derefs, edge cases, error-handling gaps, resource leaks?
- **Duplication**: re-invents anything already in the project? If yes, reuse/refactor per §1a.

### 1a. Reuse Before Writing — Avoid Duplication

Before writing any new function, type, helper, or module, search the codebase for existing functionality that does the job or ~80% of it. Duplication is far easier to prevent than to clean up.

- **During planning** (required step): grep/Glob for keywords from the task — the behaviour, the data type, the verb, related domain nouns. Read the top 3-5 hits. Ask: "does something already solve this, or 80% of this?"
- **Check neighbours first**: same package/module, then `utils`/`common`/`shared`/`lib`, then sibling packages.
- **Use graphify when available**: search the wiki and god-node list — the graph surfaces helpers grep misses because names don't overlap. When missing, create it first (§0).
- **If similar code exists, decide explicitly**: exact fit -> reuse (import, don't copy); close fit (~80%) -> propose refactoring the existing code (flag the refactor and blast radius in the plan, get approval before expanding scope); superficially similar but semantically different -> document in the plan *why* you're not reusing it.
- **Never silently copy-paste.** If something "feels familiar," stop and search.
- **Cross-language duplication** (e.g. validation mirrored frontend/backend) is acceptable only when unavoidable; comment both sides referencing the other.
- **Scope discipline**: a reuse refactor is the minimum change that lets existing code serve the new case. If it balloons, land it as a separate refactor commit first, then build on top.

### 1b. Worktree Isolation Per Change

Non-trivial work happens in a dedicated git worktree branched off the current branch — never commit in-progress work directly on the branch you started from. Full rules in `~/.claude/worktrees.md`. Headline rules:

- **Precondition**: plan has passed the §1 three-pass review gate before the worktree exists.
- **Plan persistence**: authoritative plan lives at `~/.claude/projects/<project>/plans/<slug>.md` (YAML header + embedded workflow + tasks) so a crash mid-implementation is recoverable; symlinked into the worktree as `plan.md` and added to `.git/info/exclude`.
- **Merge gate**: all plan items implemented + §1 post-implementation review clean + **three consecutive verification passes find no gaps** (any finding restarts the count at zero).
- **Rebase, don't merge**, by default; clean up the worktree and delete the plan file after.
- **Skip only for trivially mechanical edits** — typo, pure rename, comment tweak. When in doubt, create the worktree.

### 1c. Local Review Loop — Opus Reviews Every Sonnet Change, Sonnet Fixes, Repeat

Every code change Sonnet produces during the implementation phase is reviewed locally by Opus before it counts as done. Local analog of the post-PR CodeRabbit loop (`git-workflow.md`): catch issues in the worktree before the diff is pushed. Does NOT replace the §1 post-implementation review or the §1b merge gate; it runs inside the implementation phase, upstream of both.

The loop:
1. **Sonnet implements** an atomic task (or one logically complete chunk) per the approved plan.
2. **Opus reviews the diff locally** across the five review dimensions plus Reuse (§1a) and scope discipline. Review on the main session when it is Opus, or spawn a dedicated Opus reviewer subagent to keep the implementer's context clean. Emit a concrete findings list (`file:line` + what's wrong + suggested fix), or an explicit "no actionable findings".
3. **Sonnet addresses** every finding. Mechanical, decided fixes stay on Sonnet; a finding needing a design call escalates that item to Opus (§2 carve-out), then the decided fix goes back to Sonnet.
4. **Opus re-reviews** the updated diff.
5. Repeat 3-4 until a review pass returns no actionable findings. "Until everything is addressed" means a clean pass, not "the obvious ones are fixed".

Rules: reviewer and implementer are distinct roles, ideally distinct agents (review the diff as if a stranger wrote it); implementer on Sonnet, reviewer on Opus (set via `model`); log per-round findings in the plan file for auditability; review per task as it lands, don't batch; skip only for the same trivially mechanical edits §1b lets you skip the worktree for. Across rounds, keep the SAME implementer and SAME reviewer agents alive and continue them via `SendMessage` (§2 agent reuse): the diff and files stay loaded in each agent's context, so round N+1 costs only the delta, not a full re-read. Reviewer/implementer separation is between the two roles, not between rounds of the same role.

### 2. Subagent Strategy

Full rubric, PR-shipping tier split, and rationale in `~/.claude/subagent-strategy.md`. Headline rules:

- Use subagents liberally to keep the main context clean; one focused task per subagent. **Brief them fully** — they start cold: goal, relevant context (what you tried/ruled out), expected output format, length cap.
- **Reuse a live agent before spawning a new one.** Before any `Agent` spawn, check whether an agent from earlier in the session already holds the relevant context: same files, same diff, same investigation thread. If so, continue it via `SendMessage` instead of starting fresh; the files it read stay loaded, so nothing is re-read and the briefing shrinks to just the delta. Prime cases: re-review rounds on an updated diff (§1c), an implementer fixing findings in files it just wrote, follow-up questions to a research/Explore agent. Do NOT reuse when independence is the point (adversarial verification per §4, fresh-eyes review where reviewer must not trust the implementer), when the agent needs a different model tier, or when its context is bloated or polluted by failed attempts. Detail in `subagent-strategy.md` ("Reuse agents before spawning new ones").
- **In `Workflow` scripts, batch same-file work into one agent.** `agent()` calls always start cold, so structure the script so each file/module is owned by a single agent doing all its steps (read, fix, verify), rather than per-step agents each re-reading the same files.
- **When NOT to use subagents**: tight debugging loops where each iteration informs the next, work needing multiple rounds of your own judgement, interactive refinement with the user.
- **Background-first: don't block the main chat.** Default to running independent or long-running work in the background so the main session stays responsive instead of waiting on it: spawn subagents with `run_in_background: true` (builds, full test suites, CI/deploy/CR watchers, migrations, broad sweeps, research fan-outs, anything that takes more than ~30s or fans out), and use Bash `run_in_background: true` for long shell commands. Keep moving on other independent work or hand control back to the user while it runs, and collect results when the completion notification arrives - **never poll** (`TaskOutput`/status loops just re-block you). Run in the **foreground only** when the very next step truly needs that result, or for the 'When NOT to use subagents' carve-outs (tight debugging loops, interactive refinement). Fire independent calls together: multiple queries -> one message with parallel `Agent` calls. Lock patterns for concurrent agents in `multi-agent-comms.md`.
- **Delegate to the cheapest sufficient tier — actively, not just when in doubt.** The main session is usually the most expensive option; reserving it for work that needs it is the biggest cost lever.
- **Set the `model` parameter on EVERY `Agent` call — never rely on inheritance** (inheriting Opus is a 5-10x premium on work that rarely needs it).
- **PR-shipping splits across tiers**: planning phase + iteration loops (incl. the §1c review loop, CR responses, fix-push, rebases) -> **Opus**; implementation phase (writing the diff, tests/lint/build, opening the PR, label-mirror, `gh`/`git` mechanics) -> **Sonnet**. Mechanical single steps within an iteration loop stay Haiku/Sonnet. Compact tier guide:

  | Tier | Use for |
  |------|---------|
  | Haiku | renames, typo/format fixes, mechanical edits with a clear spec, simple lookups, single-command runs, tightly-specified function/test, small single-file review, documented API migration, rubric classification, short summaries |
  | Sonnet | PR implementation phase; focused multi-file changes with a decided shape; functions with 1-2 design choices; multi-file/non-trivial-logic review; refactors with a clear target |
  | Opus | PR planning + iteration loops; architecture decisions; unclear-shape refactors; gnarly hypothesis-driven debugging; reading a large unfamiliar codebase from scratch; any work where understanding/weighing options is the hard part |

  When in doubt, go one tier cheaper and re-spawn stronger if it struggles — *except* planning/iteration loops, which default to Opus.
- **Every `gh pr create` MUST mirror the closing issue's triage labels onto the PR** (`priority/*`, `severity/*`, `urgency/*`, `impact/*`, `effort/*`, `type/*`, plus `triaged` only if the issue carries it). Part of the open-PR step, not a follow-up. Detail in `subagent-strategy.md`.

### 2a. Tool Selection

Full rules in `~/.claude/tool-usage.md` — **read before any Bash call or script creation**. Headline:

- Prefer native tools (`Read`, `Edit`, `Write`, `Glob`, `Grep`, `NotebookEdit`) over Bash for file ops — faster, safer, no approval prompts.
- When using Bash, avoid approval-triggering patterns: composed commands (`&&`, `||`, `;`), compound `cd && ...`, shell expansions on untrusted paths, `sudo`/`rm -rf`/`chmod`/`chown`, piping into `bash`/`sh`, `eval`/`source`.
- **Any multiline shell MUST be a script file in `.claude/scripts/` (persistent) or `/tmp/claude/` (throw-away)** — review with 3 clean passes before executing. Full script review loop and cleanup rules in `tool-usage.md`.

### 3. Self-Improvement Loop

- After ANY correction: save the lesson to auto-memory (`~/.claude/projects/<project>/memory/`) as a rule that prevents the same mistake.
- **Memory entry structure**: lead with the rule or fact, then a **Why:** line (reason or past incident) and a **How to apply:** line (when it triggers). Knowing *why* lets you judge edge cases.
- **Before creating an entry, search existing memories** — prefer updating over duplicating. **Remove stale entries promptly.** Review lessons at session start for the relevant project.
- **Apply per-project memory at write time and review gates, not only after CR.** Project `feedback_*.md` entries (`~/.claude/projects/<project-slug>/memory/`) encode agreed patterns from prior CR rounds. Read them in four contexts, highest-leverage first: while writing new code; during the §1 pre-commit and post-impl 3-pass review gates; and before pushing CR-fix commits. Write-side schema + quality bar live in `git-workflow.md`'s "Per-project feedback memory" section.
- **Workflow improvements**: when you notice a rule in `CLAUDE.md` or any linked file could be improved (missing guidance, ambiguous wording, outdated advice, a gap that just caused a problem), **proactively propose the change** and wait for approval before editing. Applies equally to project-level `CLAUDE.md` files.

### 4. Verification Before Done

- Never mark a task complete without proving it works. Ask: "Would a staff engineer approve this?" Run tests, check logs, demonstrate correctness.
- **Green tests are NOT proof the feature works.** A passing unit/CI suite routinely coexists with a still-broken feature — especially when a test exercises a helper in isolation instead of the real user-facing path. (Lesson: four successive "fixes" for the same account-filter bug each merged with green tests + clean CR while the bug survived in production, because the tests filtered the column the fix touched rather than the column the data actually used.) After implementing ANY change, **verify the actual end-to-end scenario**: trace the real request / params / data the user triggers through every layer (FE call -> handler -> store/query -> response -> render) and confirm the observed behavior matches the Expected. "Tests pass" / "CI green" / "CR clean" is necessary, not sufficient.
- **The regression test must replicate the REAL failing scenario** — same inputs and data shape that reproduced the bug (e.g. the actual NULL/empty/edge-value rows, the exact params the client sends), not a narrower unit that can stay green while the bug lives. Confirm it FAILS on the pre-fix code and PASSES after. If the existing tests would have passed with the bug present, they don't count as verification.
- **Adversarially verify high-stakes or previously-"fixed" changes.** When a change touches a recurring/critical path, or claims to resolve something prior attempts did not, run an INDEPENDENT verification (a fresh reviewer/agent that does not trust the implementer) that traces each scenario against the *committed* code and actively probes edge cases: NULL/empty fields, alternate enum/provider values, cross-tenant data, the branch with no test. Treat every "this finally fixes it" with default skepticism until the real scenario is demonstrated.
- **Per-change-type**:
  - **UI/frontend**: start the dev server, use the feature in a browser, check golden path + edge cases, watch for regressions. Type checks and unit tests verify code, not feature. *If the project deploys on push* (preview/staging-on-push, Vercel/Netlify): after local verification, push, wait for the deploy, then re-verify in the deployed browser. Local pass ≠ deployed pass.
  - **Backend/API**: hit the endpoint with `curl` or a test; verify response shape, status codes, error paths.
  - **Libraries/shared code**: run the test suite AND exercise at least one consumer.
  - **Infrastructure/ops**: staging-first rules in `infra-ops.md`.
  - **CI/CD changes**: simulate locally with `act` before pushing.
- **When verification isn't possible** (no dev env, external dep, sandbox limit): say so explicitly. Don't claim success from type checks alone. Language-specific testing conventions in `conventions.md`.

### 5. Demand Elegance (Balanced)

- For non-trivial changes: pause and ask "is there a more elegant way?"
- **Signs a fix is hacky** (if any apply, look for an alternative): special-case branches for the one broken caller; an apologetic comment ("hack:", "temporary", "TODO: revisit"); a `try`/`except` swallowing a symptom instead of fixing the cause; a new flag/config knob added to route around the problem; duplicated logic with slight differences (§1a); a hardcoded placeholder (`0`, `""`, `false`, `nil`) with a "TODO"-style comment — represent absent data explicitly (pointer `*T`, sentinel, or omit the field) so consumers can distinguish "missing" from "actually zero".
- If a fix feels hacky, implement the elegant solution. Skip for simple obvious fixes — a three-line conditional doesn't need a new abstraction. Broader quality/style expectations in `coding-standards.md`.

### 6. Autonomous Bug Fixing

- Given a bug report: just fix it. Point at logs, errors, failing tests, then resolve them. Fix failing CI tests without being told how.
- **Root-cause process**: reproduce -> isolate -> identify the faulty assumption -> fix the assumption, not the symptom. A fix that only makes the test pass is often a patch hiding the real issue.
- **Add a regression test that replicates the real failing scenario** (same inputs / data shape the user actually hit — not a unit that can stay green while the bug lives), and confirm it fails pre-fix and passes post-fix (§4). A test that would have passed *with the bug present* has not verified the fix. If a test genuinely can't be written (environmental, flaky race), document why in the commit message AND verify the scenario manually end-to-end instead.
- **A fix is not "done" on green CI alone — demonstrate the actual scenario now works** (§4). This matters most for bugs a prior "fix" already claimed to resolve: re-confirm against the committed code, end-to-end, before declaring it fixed.
- **Escalate only for decisions, not investigations.** CI failures after a push: see `git-workflow.md` (post-push CI watcher).

### 7. Backlog Triage + Work Selection

Full playbook (label rubric, three-pass approach for huge backlogs, parallel-agent farming, stale-sweep, anti-patterns, mechanics) in `~/.claude/triage.md`. Run a full triage pass when ANY is true:

- The user says "triage", "prioritize the backlog", "go over open issues", or asks for a backlog/issue overview by name.
- The user asks "what should I work on next?" AND the open count is non-trivial (>10 items) or labels don't already give a clear ordering. (At ≤10 already-labelled items, just sort.)
- A backlog scan at session start (or after a major merge) shows >30 untriaged items / >5 open PRs not touched in 7 days (`updated:<…`) / a P0 without recent activity — *offer* a pass; don't run it uninvited.

**Always-on per-item rule** (regardless of whether you're running a pass): **whenever you read, create, or update an issue or PR, apply the triage rubric inline if it lacks the `triaged` marker.** Don't leave untriaged items in your wake.

- **Creating** (`gh issue/pr create`): pass `--label` with the full rubric on the same call — `priority/p[0-3]`, `severity/*`, `urgency/*`, `impact/*`, `effort/*`, `type/*`, plus `triaged`.
- **Updating** (editing body/title, commenting, any `gh issue/pr edit`): if no `triaged` label, fold a triage pass into the same edit — apply the labels, or apply `status/needs-info` + post a specific clarifying question.
- **Reading** as part of a larger task: if you'd be the next human-attention checkpoint, apply the rubric; skip if genuinely incidental (mention it as a hygiene note).
- **`type/question` items skip the priority rubric**: apply `type/question` + `status/needs-info`, post the question, mark `triaged`, leave open.

Why per-item: untriaged items accumulate silently between passes; labelling is cheapest while the context is fresh.

Key rules from the reference file: triage tags 5 dimensions plus a derived priority (importance is encoded as `severity/*`); conform to the project's existing scheme (`gh label list` first). Parallelize for ~20+ items (chunks of 10-15, one `Agent` each, cap ~6/wave). Pick work in priority order, not interest order — PRs usually outrank issues; for issues sort by priority -> urgency -> impact -> unblocks-others -> effort, surface top 3-5 with "why now". While working an issue, file follow-ups as separate triaged issues — don't expand scope. Proactively close gaps (clarifying-question comments, edit issues to capture discovered context — never overwrite the asker's words). End every pass with counts + a focus list.

## Task Management

- Use Claude Code's built-in task system (TaskCreate/TaskList/TaskUpdate). Plan first, verify the plan, then track progress through tasks. Explain changes with a high-level summary at each step. Capture lessons in auto-memory after corrections.

## Git Workflow

Full rules in `~/.claude/git-workflow.md` — **read before every commit, PR, or push**. Headline:

- **Conventional commits**: `type(scope): subject`, imperative, ≤72 chars. Never mention Anthropic/Claude. Never use heredoc-based `git commit -m` — `Write` a fresh uniquely-named file under `/tmp/claude/`, commit with `git commit -F`, delete it after.
- **Small atomic commits**: one concern each, independently revertable.
- **⚠️ Mandatory pre-commit review loop — 3 clean passes**: read `git diff --cached` and check the five review dimensions (§1). Fix in the same changeset, never via follow-up commits. **Run this review on Opus as comprehensively as possible — CodeRabbit's lens is the floor, not the ceiling** (architecture, type design, silent failures, test coverage, security, comment accuracy, performance, convention fit; fan out the `pr-review-toolkit:*` agents for substantial diffs). Goal: land clean for CR AND humans on the first pass. Catching a finding locally costs one pass; catching it after review costs a push + review wait + fix commit + another round. Shipping it well the first time is much faster.
- **Post-push CI watcher**: after every `git push`, enumerate all workflow runs for the pushed commit and launch one background `Agent` per run (`run_in_background: true`) named `ci-watch-<short-sha>-<workflow-slug>`. Each fetches failed logs and **fixes failures autonomously**. Coordinate via the `git-push` lock before pushing fixes. Escalate only for decisions.
- **PRs**: ≤400 lines, one concern, conventional-commit title, feature branch `type/short-description`. Never `--no-verify`.
- **Post-PR review loop**: after `gh pr create`, trigger CodeRabbit (`@coderabbitai review`) **and spawn `cr-watch-<pr-#>` as ONE atomic action — posting the trigger without arming the watcher is a defect identical in shape to `git push` without a CI watcher; safe ordering is spawn-watcher-then-trigger so a posted trigger always implies a live watcher** (re-request CR after every push to the PR branch and wait ~10 min before triaging so CR has time to post, then 60-120s polling, soft-handle 429s — but recover from any CR rate-limit with `@coderabbitai full review`, never the incremental `@coderabbitai review`, since a throttled pass silently skips the in-flight commits and yields a false-clean), triage suggestions (actionable / dismiss-with-justification / batch-nitpick), push fixes per the pre-commit loop, then re-ping `@coderabbitai review` and loop until a clean review (**never** `@coderabbitai resolve` or hand-resolving threads to silence the bot), then `merge-watch-<pr-#>` awaits **human merge** (no self-merge by default), then deploy + verify (Chrome MCP for UI, `curl` for API, `terraform plan` for IaC), then post a recommendation-to-close on the originating issue and **file new issues for out-of-scope follow-ups**. **Before declaring PR work done for a session (or any time you spot unaddressed CR comments), run the reconciliation sweep**: every open PR you authored must have a live `cr-watch`, a clean terminal CR state, or be closed — re-arm any that slipped. Full lifecycle in `git-workflow.md`.

## Session Handoff

When ending a session or running low on context, leave a summary so the next session can continue without re-reading the conversation:

```
## Session Handoff — [date]

**Done**: completed work with file paths or commit refs
**In progress**: what's partially done and where it was left off
**Blocked**: anything waiting on the user, an external dep, or a decision
**Gotchas**: anything surprising that will affect next steps
**Next steps**: concrete first action for the next session
```
