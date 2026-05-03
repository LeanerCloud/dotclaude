# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in any repository.

## Core Tenets

1. **Understand before changing** — For non-trivial work in unfamiliar code, read `graphify-out/GRAPH_REPORT.md` and `wiki/index.md` first. If `graphify-out/` is missing, **create it first** before any non-trivial exploration — run the graphify rebuild command (see the graphify section below; short form: `python3 -c "from graphify.watch import _rebuild_code; from pathlib import Path; _rebuild_code(Path('.'))"` via the graphify venv). Don't edit code you haven't mapped.
2. **Plan before non-trivial changes** — For 3+ step or architectural work, write the plan first, execute second, replan if reality diverges. Skip for mechanical one-liners.
3. **Reuse before writing** — Before adding a new function, type, or helper, grep the codebase for existing functionality. Exact fit: reuse. Close fit (~80%): refactor existing code (flag the scope change in the plan). Never silently copy-paste.
4. **Delegate to subagents** — Offload research, parallel exploration, and focused subtasks to keep the main context clean. Match model tier (Haiku/Sonnet/Opus) to task complexity.
5. **Capture every correction** — When the user corrects an approach, immediately save a memory entry that prevents the same mistake. Review relevant memories at session start.
6. **No "done" without proof** — Run tests, check logs, exercise the UI. "Should work" is not a status. If verification isn't possible, say so explicitly.
7. **Prefer elegance to hacks** — On non-trivial changes, pause and ask "is there a cleaner way?" before shipping. If a fix feels hacky, do it right. Skip for obvious one-liners.
8. **Bugs: triage now, fix at root** — Don't defer. Symptom → root cause → fix → regression test. No temporary patches that hide the real issue.
9. **Never delete data — including hidden/metadata files** — When the user says "don't delete files," they mean ALL files: `.git` directories, hidden dotfiles, config caches, lockfiles, logs, build artifacts — everything. Do NOT rationalize deletion as "just metadata," "can be regenerated," "not user data," or "the plan said so." Version control history, IDE state, caches, and sync metadata are user data. Before `rm`, `rm -rf`, `git filter-repo`, `git branch -D`, `git reset --hard`, dropping tables, or any operation that destroys on-disk or committed state you did not create in this session, pause and get explicit per-item confirmation — even if a broader plan appeared to authorize it. If you need a "fresh" git repo, use additive approaches: `git checkout --orphan` or clone the working tree to a new path. If unsure whether a given file matters to the user, assume it does.

This document may be used by OpenAI or Gemini tooling as well. When it names Anthropic model tiers, use the corresponding OpenAI or Gemini tiers in the same role:

- Haiku -> gpt-5.4-mini -> Gemini 3.1 Flash-Lite
- Sonnet -> gpt-5.4 -> Gemini 3.1 Flash
- Opus -> gpt-5.5 -> Gemini 3.1 Pro
- Keep the cheapest/mid/top-tier mapping aligned if the local model names change.

> **If you're running on an Anthropic model** (Claude Opus / Sonnet / Haiku), **ignore this mapping** — the tier names below already correspond to your model family directly. The mapping above is for OpenAI- or Gemini-backed tooling that consumes this same file.

## Reference Files

Detailed guidance lives in dedicated files. **Always read** the ones marked as such; read the rest when the work touches that area.

| File | When to read |
|------|-------------|
| `~/.claude/projects.md` | **Always** — at the start of every session |
| `~/.claude/coding-standards.md` | **Always** — when writing or reviewing code, and on first visit to any project (bootstrap checklist) |
| `~/.claude/conventions.md` | When working with Go, TypeScript, Python, Terraform, Docker, or databases |
| `~/.claude/infra-ops.md` | When working on infrastructure, deployments, cloud resources, or ops |
| `~/.claude/project-docs.md` | When setting up, updating, or consulting project documentation |
| `~/.claude/multi-agent-comms.md` | When multiple Claude instances or agents work on the same project concurrently |
| `~/.claude/tool-usage.md` | **Always** — before any Bash call, before writing a shell script, or when choosing between a native tool (Read/Edit/Glob/Grep/NotebookEdit) and Bash |
| `~/.claude/git-workflow.md` | **Always** — before staging a commit, writing a commit message, opening a PR, or after `git push` (for the CI watcher rules) |
| `~/.claude/worktrees.md` | When starting any non-trivial change — the worktree-creation step, plan-file YAML header, PID/ownership lifecycle, crash recovery, and merge gate live here (referenced from §1b) |
| `~/.claude/triage.md` | When the user asks to triage / prioritize / "what should I work on next", before sprint planning, after a major merge invalidates open items, or when starting in an unfamiliar repo and the open count is overwhelming |

## Projects

Each project has its own `CLAUDE.md` with project-specific overrides. Always read the project `CLAUDE.md` at session start — it takes precedence over this file for anything it addresses.

The project list lives in `~/.claude/projects.md`. Read it when starting work on any project, and update it whenever working in a project that isn't listed yet. Each entry should include:

| Field | Purpose |
|-------|---------|
| Project | Short name |
| Path | Absolute path |
| Stack | Primary language(s) and key frameworks |
| Description | One sentence on what it does and who uses it |

## Core Principles

> **Scale to context**: Some rules below (PR reviews, staging environments, on-call rotations — see `infra-ops.md`) assume a multi-person team. Apply them proportionally — a solo project doesn't need a formal review process, but the underlying principle (don't merge broken code, test before deploying) always applies.

- **Simplicity First**: Make every change as simple as possible. Minimal impact.
- **No Laziness**: Find root causes. No temporary fixes. Senior developer standards.
- **Minimal Impact**: Changes should only touch what's necessary. Avoid introducing bugs.
- When uncertain between two approaches, pick the simpler one and move forward rather than asking.
- **Don't touch what you weren't asked to touch**: No drive-by refactors, no unsolicited formatting changes, no adding types/comments to untouched code — unless explicitly asked for a thorough review.
- **Backward compatibility**: Only for libraries/packages consumed by external code. Within the project itself, refactor freely.
- **Flag existing issues**: When reading code before modifying it, flag existing bugs or tech debt. Maintain a `known-issues.md` in source control (see `~/.claude/project-docs.md` for guidance); consult it before starting work; remove resolved issues promptly.

## Workflow

### 0. Understand the Codebase First

Before answering architecture questions or starting non-trivial work in an unfamiliar project:

- Read the project's `CLAUDE.md` first — it takes precedence over global rules for anything it addresses
- Check `known-issues.md` at the project root (format in `~/.claude/project-docs.md`) to avoid tripping over documented bugs or tech debt
- Check if `graphify-out/GRAPH_REPORT.md` exists — if so, read it for god nodes, community structure, and component relationships before touching any code
- If `graphify-out/wiki/index.md` exists, navigate it instead of reading raw source files
- **If neither exists** (and the project has more than ~5 source files, or the architecture isn't clear from the directory listing alone): **build the graph first** before any non-trivial exploration. The graphify CLI binary registers hooks and installs the skill — the actual graph build is driven by the graphify venv's Python entry point:

  ```bash
  <graphify-venv>/bin/python3 \
    -c "from graphify.watch import _rebuild_code; from pathlib import Path; _rebuild_code(Path('.'))"
  ```

  Resolve `<graphify-venv>` and the CLI binary path from `~/.claude/local-paths.md` (gitignored — see `local-paths.md.example` for the expected shape).

  Runs 1–5 minutes depending on repo size. Use Bash `run_in_background: true` so you can start reading other things while it finishes, then wait for the completion notification before declaring the graph ready.

- After modifying code files in a session, re-run the same command to keep the graph current. The `PreToolUse` hook installed by `graphify claude install` rebuilds automatically after Write/Edit/MultiEdit, but the hook has a 5-second timeout — on large edit batches it may be skipped, so run the command manually after a big refactor.
- If `graphify claude install` has never been run in the current project, run it once to register the PreToolUse hook + add a project-level graphify section to `CLAUDE.md`.
- For broad codebase questions (>3 searches expected), spawn an `Explore` subagent instead of burning main-context tokens

### 1. Plan Mode Default

- Enter plan mode for ANY non-trivial task (3+ steps or architectural decisions)
- If something goes sideways, STOP and re-plan — don't keep pushing
- Write detailed specs upfront to reduce ambiguity
- **Plan format**: atomic tasks with explicit file paths, each independently verifiable. State what changes, where, and how to prove the change works.
- **User checkpoint**: for multi-commit plans, cross-cutting refactors, or anything touching shared infrastructure (see `~/.claude/infra-ops.md`), share the plan with the user before starting implementation.
- **Plan review loop — MANDATORY gate before implementation starts**: After drafting a plan, review it thoroughly and fix every issue found. Re-review after fixes. Repeat until **three consecutive passes find nothing** (partial credit does not exist — any finding restarts the count at zero). Do NOT create the §1b worktree, do NOT enter ExitPlanMode, and do NOT write any code until this gate passes. Each pass must cover:
  - The five dimensions of the §1 post-implementation review — Completeness, Correctness, Security, Bugs, Duplication
  - **Reuse (§1a)**: does the plan already search for and reference existing code this change can extend rather than duplicate?
  - **Scope discipline**: are we touching only what was asked, or has the plan quietly grown drive-by refactors?
  - **Blast radius**: which callers, tests, migrations, and downstream consumers does the plan's changeset affect? Are they all listed?
  - **Unknowns**: what does the plan say "verify first" about? Verify those NOW, not at implementation time — late SDK-shape surprises force plan revisions after worktree creation, which is exactly what the gate is meant to prevent.
  - Per-pass findings go in the plan itself as a short "review pass N" note so the loop is auditable, not a silent internal monologue.
- Only after three clean passes: implement in distinct atomic commits, writing tests as you go.
- **⚠️ MANDATORY post-implementation review — NO EXCEPTIONS**: After finishing a plan's implementation, you MUST perform a thorough review of ALL changes made before reporting the task as done. This review is a hard gate — never skip it, never defer it. Check every dimension:
  - **Completeness**: Does it fulfil every requirement in the plan? Nothing left out?
  - **Correctness**: Logic errors, off-by-ones, wrong assumptions, broken control flow?
  - **Security**: Injection, auth bypass, secrets exposure, OWASP top 10, input validation at boundaries?
  - **Bugs**: Race conditions, null derefs, edge cases, error handling gaps, resource leaks?
  - **Duplication**: Does the implementation re-invent anything that already exists in the project? If yes, switch to reuse or refactor the existing code per step 1a.
  - Fix every issue found. Re-review after fixes. Do not declare done until this passes cleanly.

### 1a. Reuse Before Writing — Avoid Duplication

Before writing any new function, type, helper, or module, actively search the codebase for existing functionality that already does the job or something close to it. Duplication is far easier to prevent than to clean up, and divergent copies drift over time into subtly different bugs.

- **During planning**: Grep/Glob for keywords from the task — the behaviour, the data type, the verb in the task description, related domain nouns. Read the top 3–5 hits. Ask: "does something already solve this, or 80% of this?" Treat this as a required step of plan creation, not an optional one.
- **Check neighbours first**: look in the same package/module, then the project's `utils`/`common`/`shared`/`lib` directories, then sibling packages. Most duplication lands within a single project, often within a few files of where you're about to add the new code.
- **Use graphify when available**: if `graphify-out/` exists, search the wiki and god-node list — the graph often surfaces existing helpers and utilities that grep misses because names don't overlap. When missing, create it first (see the workflow above) rather than falling back to raw grep for cross-cutting exploration.
- **If similar code exists, decide explicitly**:
  - **Exact fit**: reuse it. Import it, don't copy-paste it.
  - **Close fit (covers ~80% of the need)**: propose refactoring the existing code to cover the new use case — add a parameter, extract a smaller helper, generalise a type, introduce an options struct. Flag the refactor in the plan, explain the blast radius (callers touched, tests affected), and get user approval before expanding scope beyond the original task.
  - **Superficially similar but semantically different**: document in the plan *why* you're not reusing it, so the reader (and future you) knows the duplication is deliberate and not an oversight.
- **Never silently copy-paste**: if you catch yourself writing something that "feels familiar" from earlier in the session or from another file you read, stop and search — you are almost certainly duplicating something that already exists.
- **Cross-language duplication** (e.g. validation logic mirrored between frontend and backend) is acceptable only when unavoidable. Document both sides with a comment referencing the other location so they stay in sync.
- **Scope discipline**: proposing a refactor to avoid duplication is not a licence to restructure the whole file. The refactor should be the minimum change that lets the existing code serve the new use case. If it balloons, split it into a separate refactor commit that lands first, then add the new use case on top.

### 1b. Worktree Isolation Per Change

Non-trivial work happens in a dedicated git worktree branched off the current branch — never commit in-progress work directly on the branch you started from. The base branch stays clean until the change is fully implemented and verified, so a broken or abandoned attempt never pollutes it.

Full rules in `~/.claude/worktrees.md` — the worktree-creation command, the plan-file YAML header (worktree/base_branch/feature_branch/status/pid/host/pid_updated), the embedded workflow checklist that travels with each plan, the PID/ownership write-as-lock protocol, crash-recovery and orphan-detection rules, the 3-clean-pass merge gate, and the rebase + cleanup steps. Headline rules:

- **Precondition**: plan has passed the §1 three-pass review gate before the worktree exists.
- **Plan persistence**: authoritative plan lives at `~/.claude/projects/<project>/plans/<slug>.md` (with YAML header + embedded workflow + tasks) so a crash mid-implementation is recoverable; symlinked into the worktree as `plan.md` and added to `.git/info/exclude`.
- **Merge gate**: all plan items implemented + §1 post-implementation review clean + **three consecutive verification passes find no gaps** (any finding restarts the count at zero, no partial credit).
- **Rebase, don't merge**, by default; clean up the worktree and delete the plan file after.
- **Skip only for trivially mechanical edits** — a typo fix, a pure rename, a comment tweak. When in doubt, create the worktree.

### 2. Subagent Strategy

- Use subagents liberally to keep the main context window clean
- Offload research, exploration, and parallel analysis to subagents
- One task per subagent for focused execution
- If a subagent runs out of context, split across multiple smaller subagents and re-run
- **Subagent briefing checklist** — the agent starts cold with none of your context. Include: the goal, relevant context (what you already tried or ruled out), expected output format, and a length cap. Terse prompts produce shallow work.
- **When NOT to use subagents**: tight debugging loops where each iteration informs the next, work requiring multiple rounds of your own judgement, interactive refinement with the user.
- **Parallel vs background**: multiple independent queries → send them in one message as parallel `Agent` tool calls. Long-running watchers (CI, builds) → `run_in_background: true`, then read output when notified.
- **Multi-agent coordination**: see `~/.claude/multi-agent-comms.md` for lock patterns (e.g., `git-push` lock before pushing shared-branch fixes).
- **Default: delegate to the cheapest sufficient tier — actively, not just when in doubt.** Before doing a piece of work in the main session (or spawning a subagent at the same tier as the main session), ask: *can a cheaper Claude, OpenAI, or Gemini subagent handle this per the rubric below?* If yes, spawn that subagent using the `Agent` tool's `model` parameter. The main session's tier is typically the most expensive option available, so reserving it for work that genuinely needs it is the single biggest cost lever. Treat the rubric as a positive obligation to delegate down, not just a tie-breaker. This applies recursively — when a subagent itself needs to spawn further subagents, it should also default to the cheapest sufficient tier.
- **Match model to task complexity via the `Agent` tool's `model` parameter** — pay for capability only when it earns it:
  - **Haiku / gpt-5.4-mini / Gemini 3.1 Flash-Lite** (default for most delegations): file renames, typo fixes, mechanical edits with a clear spec, simple lookups (grep for a symbol, find where X is called), reading a single file to answer a factual question, formatting/style fixes, running a single command (or routine `gh`/`git` operations) and reporting output, implementing a tightly-specified function, writing a new test from a tight spec, code review of a small single-file diff, mechanical API/SDK migration where the mapping is documented, classifying or labelling items against a clear rubric (e.g. backlog triage chunks), summarising a single file or short diff. Cheap, fast, good enough when the answer is mostly mechanical or rubric-driven.
  - **Sonnet / gpt-5.4 / Gemini 3.1 Flash**: focused multi-file changes where coordination across files needs judgement, implementing a function whose spec is mostly clear but has 1–2 design choices, code review of a multi-file diff or a diff with non-trivial logic, refactors with a clear target shape, agentic loops where each step requires non-trivial reasoning (not just rubric-driven labelling). Use when there's some judgement involved but not deep design or hypothesis iteration.
  - **Opus / gpt-5.5 / Gemini 3.1 Pro** (or stay on the current top-level model): architecture decisions, multi-file refactors where the shape is unclear, debugging gnarly bugs that need hypothesis iteration, reading a large unfamiliar codebase from scratch (without `graphify-out/` available) to synthesise a mental model, any work where "understanding" is the hard part rather than the mechanical output.
  - **When in doubt, go one tier cheaper and see if the result is good enough** — this applies across all model families. It's easy to re-spawn on a stronger model if the cheaper one struggles, and the savings on routine work add up.
  - The main conversation's model is set by the user and doesn't change mid-session; this rule only applies to `Agent` tool spawns.

### 2a. Tool Selection

Full rules live in `~/.claude/tool-usage.md` — **read it before any Bash call or script creation**. Headline principles:

- Prefer native tools (`Read`, `Edit`, `Write`, `Glob`, `Grep`, `NotebookEdit`) over Bash for file operations — faster, safer, no approval prompts.
- When using Bash, avoid approval-triggering patterns: composed commands (`&&`, `||`, `;`), compound `cd && ...`, shell expansions on untrusted paths, `sudo`/`rm -rf`/`chmod`/`chown`, piping into `bash`/`sh`, `eval`/`source`.
- **Any multiline shell MUST be a script file in `.claude/scripts/` (persistent) or `/tmp/claude/` (throw-away) — no exceptions.** Review every script with 3 clean passes before executing. See `tool-usage.md` for the full script review loop and cleanup rules.

### 3. Self-Improvement Loop

- After ANY correction from the user: save the lesson to auto-memory (`~/.claude/projects/<project>/memory/`)
- Write rules that prevent the same mistake
- **Memory entry structure**: lead with the rule or fact, then a **Why:** line (the reason or past incident) and a **How to apply:** line (when the rule triggers). Knowing *why* lets you judge edge cases instead of following the rule blindly.
- **Before creating a new entry, search existing memories** — prefer updating an existing one over creating a duplicate.
- **Remove stale entries promptly** — if a memory is wrong or the underlying code has changed, delete it rather than letting it rot.
- Review lessons at session start for relevant project
- **Workflow improvements**: whenever you notice that a rule or process described in `~/.claude/CLAUDE.md` or any linked file (`coding-standards.md`, `conventions.md`, `infra-ops.md`, `project-docs.md`, `multi-agent-comms.md`, or project-level `CLAUDE.md`) could be improved — missing guidance, ambiguous wording, outdated advice, a gap that just caused a problem — **proactively propose the change** to the user. Describe what you'd add/change and why, and wait for approval before editing the file. Do not silently skip improvements; the system gets better only if gaps are surfaced. This applies equally to project-level `CLAUDE.md` files for project-specific workflow gaps.

### 4. Verification Before Done

- Never mark a task complete without proving it works
- Ask: "Would a staff engineer approve this?"
- Run tests, check logs, demonstrate correctness
- **Per-change-type verification**:
  - **UI/frontend**: start the dev server, use the feature in a browser, check the golden path and edge cases, monitor for regressions elsewhere. Type checks and unit tests verify correctness of code, not correctness of feature.
    - **If the project deploys on push** (preview environments, staging-on-push, Vercel/Netlify/etc.): after local verification, push, wait for the deployment to finish (poll CI or the platform), then re-verify in the deployed browser before declaring done. Local pass ≠ deployed pass — build differences, env vars, and bundling can hide bugs that only surface in the deployed artifact.
  - **Backend/API**: hit the endpoint with `curl` or a test, verify response shape, status codes, and error paths.
  - **Libraries/shared code**: run the test suite AND exercise at least one consumer to catch interface regressions.
  - **Infrastructure/ops**: see `~/.claude/infra-ops.md` for staging-first rules and rollback expectations.
  - **CI/CD changes**: simulate locally with `act` (for GitHub Actions) before pushing.
- **When verification isn't possible** (no dev env, external dependency, sandbox limitation): say so explicitly. Don't claim success based on type checks alone.
- For language-specific testing conventions, see `~/.claude/conventions.md`.

### 5. Demand Elegance (Balanced)

- For non-trivial changes: pause and ask "is there a more elegant way?"
- **Signs a fix is hacky** (if any apply, look for an elegant alternative):
  - Special-case branches for the one caller that's broken
  - A comment apologizing for the approach ("hack:", "temporary", "TODO: revisit")
  - A `try`/`except` (or equivalent) swallowing a symptom rather than fixing the cause
  - A new flag or config knob added just to route around the problem
  - Duplicated logic with slight differences between copies (see §1a)
- If a fix feels hacky: implement the elegant solution
- Skip for simple, obvious fixes — don't over-engineer. A three-line conditional doesn't need a new abstraction.
- See `~/.claude/coding-standards.md` for broader quality and style expectations.

### 6. Autonomous Bug Fixing

- When given a bug report: just fix it. Don't ask for hand-holding.
- Point at logs, errors, failing tests — then resolve them.
- Fix failing CI tests without being told how.
- **Root-cause process**: reproduce → isolate → identify the faulty assumption → fix the assumption, not the symptom. A bug fix that only makes the test pass is often a patch that hides the real issue.
- **Add a regression test** that would have caught the bug. If a test can't reasonably be written (environmental bug, flaky race condition), document why in the commit message.
- **Escalate only for decisions, not investigations** — if you need a product call or a risky tradeoff, ask. If you just haven't finished investigating yet, keep investigating.
- For CI failures after a push, see `~/.claude/git-workflow.md` (post-push CI watcher rules).

### 7. Backlog triage + work selection

Run a full triage pass per `~/.claude/triage.md` when ANY of these is true:

- The user says "triage", "let's triage", "prioritize the backlog", "go over open issues", or asks for a backlog/issue overview by name.
- The user asks "what should I work on next?" AND the open count is non-trivial (>10 items) or the existing labels don't already give a clear ordering. (At ≤10 already-labelled items, skip the full pass and just sort.)
- A backlog scan at session start (or after a major merge) shows >30 untriaged items / >5 open PRs not touched in the last 7 days (`updated:<…` not `created:<…`) / a P0 issue without recent activity — in which case offer a triage pass to the user; don't run it uninvited.

For a casual "what should I do next" with a small, well-labelled backlog, **skip the full triage** — just sort the existing labels and surface the top 3 with one-line "why now" rationales. The full process is for backlogs that aren't already legible.

**Always-on per-item rule** — applies regardless of whether you're running a backlog pass:

> **Whenever you read, create, or update an issue or PR**, apply the triage rubric inline if the item lacks the `triaged` marker. Don't leave untriaged items in your wake.

Concretely:

- **Creating a new issue or PR** (`gh issue create`, `gh pr create`): pass `--label` with the full rubric on the same call — `priority/p[0-3]`, `severity/*`, `urgency/*`, `impact/*`, `effort/*`, `type/*`, plus the positive `triaged` marker. Don't ship a `gh issue create` without labels and rely on a follow-up sweep to clean up. The rubric reasoning lives in `~/.claude/triage.md`'s "Default label set" + "Priority rubric" sections; consult them when the call isn't obvious.
- **Updating an existing issue or PR** (editing body/title, posting a comment, applying any `gh issue edit`/`gh pr edit`): if the item has no `triaged` label, fold a triage pass into the same edit. Either apply the labels yourself if you can decide them, or apply `status/needs-info` + post a specific clarifying-question comment. Either way, the item exits your interaction triaged.
- **Reading an issue or PR** (e.g. as part of a larger task — investigating a bug, looking up a related ticket, surveying which PRs reference a topic): if you'd be the next human-attention checkpoint that item gets, apply the rubric. Skip if the item is genuinely incidental to your task and you have no signal to apply the rubric on; in that case, mention it to the user as a hygiene note.
- **Exception — `type/question` items** still skip the priority rubric per `~/.claude/triage.md` §"Picking the next thing to work on": apply `type/question` + `status/needs-info`, post the clarifying question, mark `triaged`, and leave open.

Why per-item, not just per-pass: untriaged items accumulate silently between dedicated triage passes. The cheap moment to label them correctly is when they're already in your context — re-deriving severity/impact/effort during a scheduled sweep is more expensive than capturing it now while the situation is fresh.

Headline rules from the reference file:

- **Triage tags 5 dimensions plus a derived priority** — not just severity. The user-facing framing is "importance × urgency × impact"; in the taxonomy, importance is encoded as `severity/*` (no separate `importance/*` label). Apply `priority/p[0-3]`, `severity/*`, `urgency/*`, `impact/*`, `effort/*`, `type/*`, plus a `triaged` positive marker. Conform to the project's existing label scheme (run `gh label list` first); only propose new labels if none exists.
- **Parallelize for mid-to-large backlogs.** For ~20+ untriaged items (or when the user says "in parallel"), partition the open list into chunks of ~10–15 items and spawn one `Agent` per chunk in a single message — the main session aggregates their reports into a calibrated focus list. Cap concurrency at ~6 agents per wave; for backlogs that produce more chunks, run sequential waves. Don't fan out for trivially small backlogs (<15 items). Parallelization composes with the three-pass approach for 100+ item backlogs (parallelize within each pass).
- **Picking work is in priority order, not interest order.** Open PRs (yours-with-CI-red, yours-awaiting-fix, mergeable-yours, teammates-you-review) almost always outrank issues — scan the PR queue first. For issues, sort by 5 tiers in this order: priority band (P0→P3) → urgency → impact → unblocks-others (downstream-dependency items beat standalone ones) → effort (cheaper fixes win at all-else-equal). Surface the top 3–5 with one-line "why now" rationales. If you think the user might prefer a lower-ranked item for non-obvious reasons (strategic bet, customer commitment), name the tradeoff explicitly — don't silently substitute.
- **While working an issue, capture follow-ups as new triaged issues — don't expand scope.** When implementing or investigating issue X you'll often spot side-quests (a related-but-separate bug, a TODO/FIXME in nearby code, a refactor opportunity, an infra-hygiene gap surfaced during repro). File each as a separate GitHub issue and **triage it at creation** with the full label set from `triage.md` §"Default label set"; never silently expand the current PR's scope, drop the observation, or leave a TODO in code as the only record. The context is cold and labelling is cheapest at the moment of discovery — untriaged follow-ups push that cost into the future. Detailed playbook in `triage.md` §"Capturing follow-ups discovered while working an issue". This is a generalisation of `git-workflow.md` §7 — that rule fires at PR-completion time, this one fires the moment you spot the side-quest.
- **`type/question` items skip the priority rubric** during *triage* — apply `type/question` + `status/needs-info`, post the clarifying question, leave open. They re-enter triage as `type/bug` or `type/feat` once the user confirms what they actually need. During *work selection*, surface questions without `status/needs-info` (they're awaiting a response) so they get answered or closed; don't pre-filter them out.
- **Proactively close gaps and add learnings** during triage — don't just label-and-move-on. (1) When you can't decide severity / impact / effort because the issue lacks key info, post a *specific* clarifying-question comment and apply `status/needs-info` (don't block the rest of the triage waiting for an answer). (2) When you discover context the asker didn't include (root cause hypothesis, repro steps, related PRs, environment matrix), **edit the issue** to capture it: fix a misleading title via `gh issue edit --title`, or append a clearly-marked *"Triage notes (added by @\<handle\> on YYYY-MM-DD)"* section to the body — never overwrite the asker's words. Comments are not enough — title and body are what the next reader sees first.
- **End every triage pass** with counts and a focus list. The exact counts depend on pass type: a *regular* triage pass reports open-before / newly-triaged / stale-candidates-flagged / closed-as-duplicate / closed-as-wontdo / closed-as-completed / open-after (it does NOT close stale items — that's the dedicated sweep's job); a *stale-sweep* pass reports stale-candidate-count-before / closed-as-stale-or-wontdo / reactivated / stale-candidate-count-after. Surface hygiene flags worth raising (e.g. "5 P1s are 6+ months old — recalibrate").

The detailed playbook (label rubric, three-pass approach for huge backlogs, parallel-agent farming pattern, stale-sweep procedure, anti-patterns, mechanics, proactive flagging at session start) lives in the reference file.

## Task Management

- Use Claude Code's built-in task system (TaskCreate/TaskList/TaskUpdate)
- Plan first, verify plan, then track progress through tasks
- Explain changes with a high-level summary at each step
- Capture lessons in auto-memory after corrections

## Git Workflow

Full rules live in `~/.claude/git-workflow.md` — **read it before every commit, PR, or push**. Headline principles:

- **Conventional commits**: `type(scope): subject`, imperative mood, ≤72 chars. Never mention Anthropic/Claude. Never use heredoc-based `git commit -m` — `Write` a fresh uniquely-named file under `/tmp/claude/`, commit with `git commit -F`, and delete the file after.
- **Small atomic commits**: one concern per commit, independently revertable.
- **⚠️ Mandatory pre-commit review loop — 3 clean passes**: read `git diff --cached` and check Completeness, Correctness, Security, Bugs, and Duplication. Fix in the same changeset, never via follow-up commits. See `git-workflow.md` for the full dimensions and delegation guidance.
- **Post-push CI watcher**: after every `git push`, enumerate all workflow runs for the pushed commit and launch **one background `Agent` per run** (`run_in_background: true`) named `ci-watch-<short-sha>-<workflow-slug>`. Each watcher monitors its assigned run, fetches failed logs, and **fixes CI failures autonomously** with follow-up commits. Coordinate via the multi-agent comms `git-push` lock before pushing fixes. Only escalate when a decision is required.
- **PRs**: ≤400 lines, one concern, conventional commits title, feature branch named `type/short-description`. Never skip pre-commit hooks with `--no-verify`.
- **Post-PR review loop (background agents)**: after `gh pr create`, trigger CodeRabbit (`@coderabbitai review`), spawn `cr-watch-<pr-#>` to wait for the bot review (60–120s polling, soft-handle 429 rate limits), triage suggestions into actionable / dismiss-with-justification / batch-nitpick, push fix commits per the pre-commit review loop, then `merge-watch-<pr-#>` waits for **human merge** (no self-merge by default), then run deploy + verification (Chrome MCP for UI, `curl` for API, `terraform plan` for IaC), then post a recommendation-to-close comment on the originating issue and **file new GitHub issues for any out-of-scope follow-ups surfaced during the loop**. Full lifecycle in `git-workflow.md`.

## Session Handoff

When ending a session or running low on context, leave a summary so the next session can pick up without re-reading the conversation. Minimum format:

```
## Session Handoff — [date]

**Done**: bullet list of completed work with file paths or commit refs
**In progress**: what's partially done and where it was left off
**Blocked**: anything waiting on the user, an external dep, or a decision
**Gotchas**: anything surprising discovered that will affect next steps
**Next steps**: concrete first action for the next session
```
