# CLAUDE.md

Always-on guidance for any repository. Everything that isn't needed on every turn lives in a
**skill** (`skills/<name>/SKILL.md`) that loads when its trigger fires. This file is the core plus
the routing.

The same skills drive Claude Code, Codex CLI and Gemini CLI, whose invocation syntax differs
(`/name`, `$name`, implicit activation). This file therefore always says **"invoke the `<name>`
skill"**. Discovery paths and the portability contract are in [`skills/README.md`](skills/README.md).

## Core Tenets

1. **Understand before changing** — For non-trivial work in unfamiliar code, read
   `graphify-out/GRAPH_REPORT.md` and `wiki/index.md` first. If `graphify-out/` is missing, **create
   it first** before any non-trivial exploration (§0). Don't edit code you haven't mapped.
2. **Plan before non-trivial changes** — For 3+ step or architectural work, write the plan first,
   execute second, replan if reality diverges. Skip for mechanical one-liners.
3. **Reuse before writing** — Before adding a new function, type, or helper, grep for existing
   functionality. Exact fit: reuse. Close fit (~80%): refactor existing code (flag the scope change
   in the plan). Never silently copy-paste. (§1a)
4. **Delegate to subagents** — Offload research, parallel exploration, and focused subtasks to keep
   the main context clean. Match model tier (Haiku/Sonnet/Opus/Fable) to task complexity. Reuse a
   context-warm agent (`SendMessage`) before spawning a fresh one when the follow-up touches the same
   files. (§2)
5. **Capture every correction** — When the user corrects an approach, immediately save a memory entry
   that prevents the same mistake. Review relevant memories at session start. (§3)
6. **No "done" without proof** — Run tests, check logs, exercise the UI. "Should work" is not a
   status. If verification isn't possible, say so explicitly. (§4)
7. **Prefer elegance to hacks** — On non-trivial changes, pause and ask "is there a cleaner way?"
   before shipping. Cleaner means simpler, not more elaborate. If a fix feels hacky, do it right.
   Skip for obvious one-liners. (§5)
8. **Bugs: triage now, fix at root** — Symptom → root cause → fix → regression test. No temporary
   patches that hide the real issue. (§6)
9. **Never delete data — including hidden/metadata files** — "Don't delete files" means ALL files:
   `.git` dirs, dotfiles, config caches, lockfiles, logs, build artifacts. Do NOT rationalize
   deletion as "just metadata," "can be regenerated," "not user data," or "the plan said so." Before
   `rm`, `rm -rf`, `git filter-repo`, `git branch -D`, `git reset --hard`, dropping tables, or any
   operation destroying on-disk or committed state you did not create this session, pause and get
   explicit per-item confirmation — even if a broader plan appeared to authorize it. For a "fresh"
   git repo use additive approaches (`git checkout --orphan`, or clone the working tree to a new
   path). If unsure whether a file matters, assume it does.

This document may be used by OpenAI or Gemini tooling. When it names Anthropic tiers, use the
corresponding tiers in the same role: Haiku -> gpt-5.4-mini -> Gemini 3.1 Flash-Lite; Sonnet ->
gpt-5.4 -> Gemini 3.1 Flash; Opus (the default top tier — planning, review, iteration, debugging,
non-trivial implementation, see §1c and §2) -> gpt-5.5 -> Gemini 3.1 Pro; Fable (peak reserve, ~2x
Opus cost, only when the last-0.5% of max-effort intelligence decides it) -> the top tier at max
effort (gpt-5.5 / Gemini 3.1 Pro). Keep cheapest/mid/top aligned if local model names change.

> **If you're running on an Anthropic model**, **ignore this mapping** — the tier names below already
> correspond to your model family. The mapping is for OpenAI- or Gemini-backed tooling consuming this
> same file.

## Skills

| Skill | Invoke when |
|-------|-------------|
| `coding-standards` | writing or reviewing code; first visit to any project |
| `conventions` | working with Go, TypeScript, Python, Shell, Docker, Terraform, or databases |
| `tool-usage` | **before any Bash call**, before writing a shell script, choosing native tools vs Bash |
| `git-commit` | **before staging a commit** or writing a commit message |
| `ci-watch` | immediately after any `git push` |
| `pr-lifecycle` | opening a PR, or driving one to merge |
| `cr-loop` | a CodeRabbit review is pending or has arrived |
| `pr-iterate` | driving one or many existing PRs to merge-ready |
| `rate-limit-retry` | any 429 / usage limit / "try again later" |
| `review-staged-diff` | reviewing a staged changeset before it lands |
| `review-and-implement` | a plan is written and ready to be hardened, then built |
| `worktrees` | starting any non-trivial change |
| `subagent-strategy` | deciding how to delegate, or which tier to spawn on |
| `multi-agent-comms` | several agents or sessions share one project |
| `pr-orchestration` | orchestrating several PRs/agents at once |
| `issue-pr-autopilot` | setting up or operating the scheduled issue→PR autopilot |
| `triage-labels` | reading, creating or updating any untriaged issue or PR |
| `triage-pass` | "triage", "prioritize the backlog", "go over open issues" |
| `work-selection` | "what should I work on next?" |
| `infra-ops` | infrastructure, deployments, cloud resources, ops |
| `project-docs` | setting up, updating, or consulting project documentation |

Read `~/.claude/projects.md` at the start of every session, and update it whenever working in a
project not yet listed (fields: Project, Path, Stack, Description). Per-machine paths and tool
locations live in `~/.claude/local-paths.md` (gitignored; see `local-paths.md.example`).

## Projects

Each project has its own `CLAUDE.md` with project-specific overrides that take precedence over this
file. Always read it at session start.

## Core Principles

> **Scale to context**: some rules below (PR reviews, staging environments, on-call) assume a
> multi-person team. Apply proportionally — a solo project doesn't need a formal review process, but
> the underlying principle (don't merge broken code, test before deploying) always applies.

- **Always run a rate-limit retry cron — for every request, never stall.** From the start of any
  request, keep a ~2-minute cron running (`CronCreate`, e.g. `*/2 * * * *`) that catches any
  throttling and retries the pending work a few minutes later, so nothing stalls silently. It
  self-deletes (`CronDelete`) once the work completes and escalates after a sensible ceiling. Invoke
  the `rate-limit-retry` skill.
- **Simplicity First (YAGNI)**: make every change as simple as possible. Build only what a current
  caller needs; no parameters, flags, hooks, or abstraction layers for a future that hasn't arrived.
  Sophistication is a cost, not a virtue.
- **No Laziness**: find root causes. No temporary fixes. Senior developer standards.
- **Fail loud; no silent fallbacks, magic values, or stringly-typed enums.** If something required is
  missing or wrong, return an explicit error rather than a fabricated/default/degraded value (most
  critical on money / data-mutation paths). Don't hardcode magic values or fixed ratios — derive from
  data/config or named constants. Use typed enums/constants instead of bare string literals; validate
  external input at the boundary and error on unknown.
- **Verify before asserting — never report status from memory or a stale note.** Before stating any
  status, count, or claim that something is done / merged / passing / ready / settled, re-check the
  live source THIS turn (re-run the query, re-read the file, re-list the PRs). Do not infer it from
  earlier output, a tracking doc, or what you expect to be true. This matters most for fast-moving
  state (PR merge/CI/CR status, test results, file contents, counts). If you cannot verify right now,
  say so explicitly. Stale or optimistic status IS a misleading answer — treat it as a defect, not a
  convenience.
- **Minimal Impact**: touch only what's necessary. When uncertain between two approaches, pick the
  simpler one and move forward rather than asking.
- **Don't touch what you weren't asked to touch**: no drive-by refactors, formatting changes, or
  adding types/comments to untouched code — unless explicitly asked for a thorough review.
- **Comment sparingly**: default to no comment; add one only where the *why* isn't deducible from the
  code, and keep it to 1-2 lines. Rationale belongs in the PR description, not the source.
- **Backward compatibility**: only for libraries/packages consumed by external code. Within the
  project, refactor freely.
- **Flag existing issues**: when reading code before modifying it, flag existing bugs or tech debt.
  Maintain a `known-issues.md` in source control; consult it before starting work; remove resolved
  issues promptly.
- **Never use em-dashes (Unicode U+2014) in generated prose by default.** Applies to chat, comments,
  commit messages, PR/issue text, and docs. Use a hyphen, comma, semicolon, colon, parenthetical, or
  a fresh sentence. Em-dashes are an unmistakable AI-tell the user does not want. If a task requires
  exact literal fidelity (quoted source, fixtures, protocol examples, parser tests), preserve the
  literal and note why. `---` for horizontal rules is fine (three hyphens, not an em-dash).

## Workflow

### 0. Understand the Codebase First

Before answering architecture questions or starting non-trivial work in an unfamiliar project:

- Read the project's `CLAUDE.md` first — it takes precedence over global rules. Check
  `known-issues.md` at the project root (format: invoke `project-docs`).
- If `graphify-out/GRAPH_REPORT.md` exists, read it for god nodes, community structure, and component
  relationships before touching code. If `graphify-out/wiki/index.md` exists, navigate it instead of
  raw source.
- **If neither exists** (and the project has >~5 source files, or the architecture isn't clear from
  the directory listing): **build the graph first**, before any non-trivial exploration:

  ```bash
  <graphify-venv>/bin/python3 \
    -c "from graphify.watch import _rebuild_code; from pathlib import Path; _rebuild_code(Path('.'))"
  ```

  Resolve `<graphify-venv>` from `~/.claude/local-paths.md`. Runs 1-5 min; use Bash
  `run_in_background: true` and wait for the completion notification before declaring it ready.
- Re-run the same command after modifying code. The `PreToolUse` hook installed by
  `graphify claude install` rebuilds automatically after Write/Edit, but its 5-second timeout may skip
  large edit batches — run it manually after a big refactor. If `graphify claude install` has never
  run in the project, run it once.
- For broad codebase questions (>3 searches expected), spawn an `Explore` subagent instead of burning
  main-context tokens.

### 1. Plan Mode Default

- Enter plan mode for ANY non-trivial task (3+ steps or architectural decisions). If something goes
  sideways, STOP and re-plan.
- **Plan format**: atomic tasks with explicit file paths, each independently verifiable. State what
  changes, where, and how to prove it works.
- **Plan the smallest thing that satisfies the request.** Name the caller for every parameter, option,
  and abstraction the plan introduces — if that caller is hypothetical, cut the item. Don't plan
  extensibility nobody asked for, and don't plan a helper you'd write exactly one call to.
- **User checkpoint**: for multi-commit plans, cross-cutting refactors, or anything touching shared
  infrastructure, share the plan before implementing.
- **Plan review loop — MANDATORY gate before implementation starts**: review the plan, fix every
  issue, re-review. Repeat until **three consecutive passes find nothing** (any finding restarts the
  count at zero). Do NOT create the §1b worktree, enter ExitPlanMode, or write code until this passes.
  Each pass covers: the six review dimensions (below); Reuse (§1a); scope discipline (only what was
  asked?); blast radius (callers, tests, migrations, downstream consumers all listed?); unknowns
  (verify "verify-first" items NOW, not at implementation time). Per-pass findings go in the plan as a
  short "review pass N" note. The `review-and-implement` skill drives this loop.
- Only after three clean passes: implement in distinct atomic commits, writing tests as you go.
- **⚠️ MANDATORY post-implementation review — NO EXCEPTIONS**: after implementing, review ALL changes
  before reporting done. Hard gate; never skip or defer. Fix every issue, re-review, don't declare
  done until clean.

**The six review dimensions** (used by the plan-review gate, the post-implementation review, the §1c
local loop, and the pre-commit loop in `git-commit`):

- **Completeness**: fulfils every requirement? Nothing left out?
- **Correctness**: logic errors, off-by-ones, wrong assumptions, broken control flow?
- **Security**: injection, auth bypass, secrets exposure, OWASP top 10, input validation at
  boundaries?
- **Bugs**: race conditions, null derefs, edge cases, error-handling gaps, resource leaks?
- **Duplication**: re-invents anything already in the project? If yes, reuse/refactor per §1a.
- **Over-engineering**: is every parameter set by a real caller, every abstraction used by more than
  one consumer, every guard protecting a reachable state, every comment earning its line? Prune what
  fails. Review this dimension **adversarially**: the author's local justification for a piece of
  machinery almost always holds up, so ask instead what the calling system actually does and what
  would break if the machinery were deleted. Correct, well-tested code guarding an unreachable state
  still comes out.

### 1a. Reuse Before Writing — Avoid Duplication

Before writing any new function, type, helper, or module, search for existing functionality that does
the job or ~80% of it. Duplication is far easier to prevent than to clean up.

- **During planning** (required step): grep/Glob for keywords from the task — the behaviour, the data
  type, the verb, related domain nouns. Read the top 3-5 hits. Ask: "does something already solve
  this, or 80% of this?"
- **Check neighbours first**: same package/module, then `utils`/`common`/`shared`/`lib`, then sibling
  packages. Use graphify when available — the graph surfaces helpers grep misses because names don't
  overlap. When missing, create it first (§0).
- **If similar code exists, decide explicitly**: exact fit -> reuse (import, don't copy); close fit
  (~80%) -> propose refactoring the existing code (flag the refactor and blast radius in the plan, get
  approval before expanding scope); superficially similar but semantically different -> document in
  the plan *why* you're not reusing it.
- **Never silently copy-paste.** If something "feels familiar," stop and search.
- **Cross-language duplication** (e.g. validation mirrored frontend/backend) is acceptable only when
  unavoidable; comment both sides referencing the other.
- **Scope discipline**: a reuse refactor is the minimum change that lets existing code serve the new
  case. If it balloons, land it as a separate refactor commit first, then build on top.

### 1b. Worktree Isolation Per Change

Non-trivial work happens in a dedicated git worktree branched off the current branch — never commit
in-progress work directly on the branch you started from. **Invoke the `worktrees` skill** for the
full protocol. Headlines: the plan must have passed the §1 three-pass gate before the worktree
exists; the authoritative plan lives at `~/.claude/projects/<project>/plans/<slug>.md` so a crash
mid-implementation is recoverable; the merge gate is all plan items implemented + a clean §1
post-implementation review + **three consecutive verification passes finding no gaps**; rebase rather
than merge by default. Skip only for trivially mechanical edits (typo, pure rename, comment tweak) —
when in doubt, create the worktree.

### 1c. Local Review Loop — Opus Reviews Every Implementation Change

Every change the implementer produces is reviewed locally by Opus before it counts as done. This runs
inside the implementation phase, upstream of the §1 post-implementation review and the §1b merge
gate; it does not replace either.

1. **The implementer** (Sonnet for simpler changes, Opus for non-trivial code, per §2) implements one
   atomic task per the approved plan. Write the plan's task, not a generalised version of it. If the
   task seems to need machinery the plan didn't call for, that is a signal to re-plan rather than to
   improvise it.
2. **Opus reviews the diff locally** across the six review dimensions plus Reuse (§1a) and scope
   discipline, as a dedicated reviewer subagent (set `model`) so the implementer's context stays
   clean. Escalate to Fable only for the hardest money-path / architecture calls. Emit a concrete
   findings list (`file:line` + what's wrong + suggested fix), or an explicit "no actionable
   findings".
3. **The implementer addresses** every finding. Mechanical fixes stay with the implementer; a finding
   needing a design call escalates that item to Opus, then the decided fix goes back down.
4. **Opus re-reviews.** Repeat 3-4 until a pass returns no actionable findings — a clean pass, not
   "the obvious ones are fixed".

Reviewer and implementer are distinct roles, ideally distinct agents (review the diff as if a stranger
wrote it). Log per-round findings in the plan file. Review per task as it lands, don't batch. Across
rounds keep the SAME implementer and SAME reviewer alive and continue them via `SendMessage` (§2), so
round N+1 costs only the delta.

### 2. Subagent Strategy

**Invoke the `subagent-strategy` skill** for the full rubric; `pr-orchestration` when several
PRs/agents run at once. Headlines:

- Use subagents liberally to keep the main context clean; one focused task per subagent. **Brief them
  fully** — they start cold: goal, relevant context, expected output format, length cap.
- **Reuse a live agent before spawning a new one** (`SendMessage`) when it already holds the relevant
  files, diff, or investigation thread. Do NOT reuse when independence is the point (adversarial
  verification, fresh-eyes review), when a different tier is needed, or when its context is polluted.
- **In `Workflow` scripts, batch same-file work into one agent** — `agent()` calls always start cold.
- **When NOT to use subagents**: tight debugging loops where each iteration informs the next, work
  needing multiple rounds of your own judgement, interactive refinement with the user.
- **Background-first: don't block the main chat.** Default `run_in_background: true` for anything
  over ~30s or that fans out; keep moving and collect results on notification — **never poll**. Fire
  independent calls together in one message. **Reap background agents at their terminal condition**:
  a watcher that emits an `idle_notification` is "waiting for more," not "done" — `TaskStop` any still
  parked once its PR reaches a terminal state. Run in the **foreground only** when the very next step
  truly needs that result, or for the carve-outs above.
- **Delegate to the cheapest sufficient tier — actively, not just when in doubt.** The main session is
  usually the most expensive option.
- **Set the `model` parameter on EVERY `Agent` call — never rely on inheritance.**

| Tier | Use for |
|------|---------|
| Haiku | renames, typo/format fixes, mechanical edits with a clear spec, simple lookups, single-command runs, tightly-specified function/test, small single-file review, documented API migration, rubric classification, short summaries |
| Sonnet | PR implementation of simpler, decided-shape changes; focused multi-file changes with a decided shape; functions with 1-2 design choices; refactors with a clear target |
| Opus | **the default top tier.** PR planning; all review loops (§1c local review, §1 pre-commit/post-impl, adversarial money-path review); architecture/design decisions; iteration loops (CR responses, fix-push, rebases); gnarly hypothesis-driven debugging; non-trivial implementation; reading a large unfamiliar codebase from scratch; any work where understanding/weighing options is the hard part |
| Fable | **peak reserve (~2x Opus cost).** Only when the last ~0.5% of max-effort intelligence decides the outcome — the hardest money-path adversarial reviews, the gnarliest architecture calls. |

Mechanical single steps stay Haiku/Sonnet. When in doubt, go one tier cheaper and re-spawn stronger
if it struggles — *except* planning, review, iteration, debugging, and non-trivial implementation,
which default to Opus.

**Every `gh pr create` MUST mirror the closing issue's triage labels onto the PR** (`priority/*`,
`severity/*`, `urgency/*`, `impact/*`, `effort/*`, `type/*`, plus `triaged` only if the issue carries
it). Part of the open-PR step, not a follow-up.

### 2a. Tool Selection

**Invoke the `tool-usage` skill before any Bash call or script creation.** Headlines: prefer native
tools (`Read`, `Edit`, `Write`, `Glob`, `Grep`, `NotebookEdit`) over Bash for file ops; avoid
approval-triggering Bash patterns (composed commands, compound `cd &&`, `sudo`/`rm -rf`/`chmod`,
piping into `bash`, `eval`); **any multiline shell MUST be a script file** in `.claude/scripts/`
(persistent) or `/tmp/claude/` (throw-away), reviewed with 3 clean passes before executing.

### 3. Self-Improvement Loop

- After ANY correction: save the lesson to auto-memory (`~/.claude/projects/<project>/memory/`) as a
  rule that prevents the same mistake.
- **Memory entry structure**: lead with the rule or fact, then a **Why:** line (reason or past
  incident) and a **How to apply:** line (when it triggers). Knowing *why* lets you judge edge cases.
- **Before creating an entry, search existing memories** — prefer updating over duplicating. **Remove
  stale entries promptly.** Review lessons at session start for the relevant project.
- **Apply per-project memory at write time and review gates, not only after CR** — invoke the
  `git-commit` skill for when to read and write the `feedback_*.md` garden.
- **Workflow improvements — self-update via PR**: when you notice a gap in the `~/.claude` guidance
  (missing, ambiguous, contradictory, outdated, or something that just caused friction), **capture it
  as a pull request against `LeanerCloud/dotclaude`**. First open a GitHub issue describing the gap,
  then branch off `origin/main` (`chore/<slug>` or `docs/<slug>`), make the minimal focused edit,
  commit, push, and `gh pr create` with `Closes #<n>` in the body. Batch several gaps noticed in the
  same session into one issue + PR pair. **Guardrails**: the PR is the approval gate — NEVER push
  straight to `main` or self-merge; one coherent concern per PR; raise a PR only for genuine, reusable
  gaps; restore whatever branch was originally checked out afterwards; surface the PR link to the
  user. For a project-level `CLAUDE.md`, open the PR against that project's own repo.

### 4. Verification Before Done

- Never mark a task complete without proving it works. Ask: "Would a staff engineer approve this?"
- **Green tests are NOT proof the feature works.** A passing suite routinely coexists with a still-
  broken feature, especially when a test exercises a helper in isolation instead of the real
  user-facing path. After implementing ANY change, **verify the actual end-to-end scenario**: trace
  the real request / params / data the user triggers through every layer (FE call -> handler ->
  store/query -> response -> render) and confirm the observed behavior matches the Expected. "Tests
  pass" / "CI green" / "CR clean" is necessary, not sufficient.
- **The regression test must replicate the REAL failing scenario** — same inputs and data shape that
  reproduced the bug, not a narrower unit that can stay green while the bug lives. Confirm it FAILS on
  the pre-fix code and PASSES after. If the existing tests would have passed with the bug present,
  they don't count as verification.
- **Adversarially verify high-stakes or previously-"fixed" changes** with an INDEPENDENT reviewer that
  does not trust the implementer, tracing each scenario against the *committed* code and probing edge
  cases: NULL/empty fields, alternate enum/provider values, cross-tenant data, the branch with no
  test. Treat every "this finally fixes it" with default skepticism.
- **Per-change-type**: **UI/frontend** — start the dev server and use the feature in a browser, golden
  path + edge cases; if the project deploys on push, re-verify in the deployed browser afterwards
  (local pass ≠ deployed pass). **Backend/API** — hit the endpoint with `curl` or a test; verify
  response shape, status codes, error paths. **Libraries/shared code** — run the suite AND exercise at
  least one consumer. **Infrastructure/ops** — staging-first (invoke `infra-ops`). **CI/CD** —
  simulate locally with `act` before pushing.
- **When verification isn't possible** (no dev env, external dep, sandbox limit): say so explicitly.
  Don't claim success from type checks alone.

### 5. Demand Elegance (Balanced)

- For non-trivial changes: pause and ask "is there a simpler way?" **Elegance means fewer moving
  parts, not more sophisticated ones**: fewer lines, fewer concepts, fewer names. If the "more
  elegant" version is longer or adds a concept, it isn't more elegant, it's over-engineering. Read
  this as a prompt to *remove* machinery, never as an invitation to add it.
- **Signs a fix is hacky** (if any apply, look for an alternative): special-case branches for the one
  broken caller; an apologetic comment ("hack:", "temporary", "TODO: revisit"); a `try`/`except`
  swallowing a symptom instead of fixing the cause; a new flag/config knob added to route around the
  problem; duplicated logic with slight differences (§1a); a hardcoded placeholder (`0`, `""`,
  `false`, `nil`) with a "TODO"-style comment — represent absent data explicitly so consumers can
  distinguish "missing" from "actually zero".
- If a fix feels hacky, implement the elegant solution. Skip for simple obvious fixes — a three-line
  conditional doesn't need a new abstraction.

### 6. Autonomous Bug Fixing

- Given a bug report: just fix it. Point at logs, errors, failing tests, then resolve them. Fix
  failing CI tests without being told how.
- **Root-cause process**: reproduce -> isolate -> identify the faulty assumption -> fix the
  assumption, not the symptom. A fix that only makes the test pass is often a patch hiding the real
  issue.
- **Add a regression test that replicates the real failing scenario** and confirm it fails pre-fix and
  passes post-fix (§4). If a test genuinely can't be written (environmental, flaky race), document why
  in the commit message AND verify the scenario manually end-to-end instead.
- **A fix is not "done" on green CI alone — demonstrate the actual scenario now works** (§4). This
  matters most for bugs a prior "fix" already claimed to resolve.
- **Escalate only for decisions, not investigations.** For CI failures after a push, invoke `ci-watch`.

### 7. Backlog Triage + Work Selection

Invoke `triage-pass` when the user says "triage" / "prioritize the backlog" / "go over open issues";
or on "what should I work on next?" **when** the open count is non-trivial (>10 items) or labels don't
already give a clear ordering — at ≤10 already-labelled items, invoke `work-selection` and just sort.
A session-start scan showing >30 untriaged items, >5 open PRs untouched in 7 days, or a P0 without
recent activity is grounds to *offer* a pass — don't run it uninvited.

**Always-on per-item rule** (regardless of any pass): **whenever you read, create, or update an issue
or PR, apply the triage rubric inline if it lacks the `triaged` marker** — invoke `triage-labels`.
Don't leave untriaged items in your wake.

## Task Management

Use the built-in task system (TaskCreate/TaskList/TaskUpdate). Plan first, verify the plan, then track
progress through tasks. Explain changes with a high-level summary at each step. Capture lessons in
auto-memory after corrections.

## Git Workflow

- **Repo first — check at TASK START, not commit time**: if you're working in a PROJECT dir that isn't
  a git repo, `git init` immediately, before the first non-trivial edit. Multi-phase work in an
  unversioned tree loses its per-step history irreversibly, and creating a repo is safe and additive
  (the opposite of the never-destroy-`.git` rule, tenet 9). **Exceptions (do NOT init)**: the home dir
  itself, system temp / scratchpad, `~/Downloads`/`~/Desktop` and similar scratch locations.
- **Before staging a commit, invoke `git-commit`** — conventional commits, atomic commits, and the
  mandatory pre-commit review loop that runs to 3 clean passes. Never mention Anthropic/Claude in
  commit messages. Never use heredoc-based `git commit -m`.
- **After every `git push`, invoke `ci-watch`** — one background watcher per workflow run, fixing
  failures autonomously.
- **When opening a PR, invoke `pr-lifecycle`**; when a CodeRabbit review is in flight, invoke
  `cr-loop`. PRs are ≤400 lines, one concern, conventional-commit title, branch `type/short-description`.
  Never `--no-verify`; never `gh pr merge --admin` to merge past pending or failing checks.
- **Before declaring PR work done for a session**, run the reconciliation sweep in `pr-lifecycle`:
  every open PR you authored must have a live `cr-watch`, a clean terminal CR state, or be closed.

## Session Handoff

When ending a session or running low on context, leave a summary so the next session can continue
without re-reading the conversation:

```
## Session Handoff — [date]

**Done**: completed work with file paths or commit refs
**In progress**: what's partially done and where it was left off
**Blocked**: anything waiting on the user, an external dep, or a decision
**Gotchas**: anything surprising that will affect next steps
**Next steps**: concrete first action for the next session
```
