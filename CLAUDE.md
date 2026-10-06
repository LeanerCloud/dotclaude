# CLAUDE.md

Always-on guidance for any repository. Everything that isn't needed on every turn lives in a
**skill** (`skills/<name>/SKILL.md`) that loads when its trigger fires. This file is the core plus
the routing.

The same skills drive Claude Code, Codex CLI and Gemini CLI, whose invocation syntax differs
(`/name`, `$name`, implicit activation). This file therefore always says **"invoke the `<name>`
skill"**. Discovery paths and the portability contract are in [`skills/README.md`](skills/README.md).

## Core Tenets

1. **Understand before changing**: map the code you will touch before editing it. (§0)
2. **Plan before non-trivial changes**: plan first, execute second, replan if reality diverges. (§1)
3. **Reuse before writing**: search for existing code first; never silently copy-paste. (§1a)
4. **Delegate to subagents**: keep the main context clean; match tier to task; reuse warm agents. (§2)
5. **Capture every correction** as a memory entry that prevents the same mistake. (§3)
6. **No "done" without proof**: "should work" is not a status. (§4)
7. **Prefer elegance to hacks**: cleaner means simpler, not more elaborate. (§5)
8. **Bugs: triage now, fix at root**, with a regression test; no temporary patches. (§6)
9. **Never delete data — including hidden/metadata files** — "Don't delete files" means ALL files:
   `.git` dirs, dotfiles, config caches, lockfiles, logs, build artifacts. Do NOT rationalize
   deletion as "just metadata," "can be regenerated," "not user data," or "the plan said so." Before
   `rm`, `rm -rf`, `git filter-repo`, `git branch -D`, `git reset --hard`, dropping tables, or any
   operation destroying on-disk or committed state you did not create this session, pause and get
   explicit per-item confirmation — even if a broader plan appeared to authorize it. For a "fresh"
   git repo use additive approaches (`git checkout --orphan`, or clone the working tree to a new
   path). If unsure whether a file matters, assume it does.

Non-Anthropic hosts map Sonnet 5.5 low / medium / high to their cheapest/mid/top tier in the same
role (the `subagent-strategy` skill §"Model rubric" names them).

An exact reviewer model pinned by a project-specific rule or skill section overrides this generic
tier mapping. Do not satisfy an exact pin with a cross-provider substitute or floating model alias.
Project-specific review gates live in each project's `CLAUDE.md`.

## Skills

### Written here

| Skill | Invoke when |
|-------|-------------|
| `coding-standards` | writing or reviewing code; first visit to any project; before launching a user-facing app |
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
| `triage-labels` | in a repo that uses the rubric: creating an issue or PR, or updating an untriaged one you own or were asked to work on |
| `triage-pass` | "triage", "prioritize the backlog", "go over open issues" |
| `work-selection` | "what should I work on next?" |
| `infra-ops` | infrastructure, deployments, cloud resources, ops; multi-repo integration builds |
| `project-docs` | setting up, updating, or consulting project documentation |
| `cristi-voice` | writing or reviewing site/marketing copy, LinkedIn posts, or any prose published under Cristian's or LeanerCloud's name |
| `playwright-verify` | after any web-app change, before declaring it done; when setting up a new web project's local run/verification harness |

### Imported

Curated from pstack, cursor-team-kit, superpowers, gstack, `anthropics/skills`, `terraform-skill`
and `claude-code-owasp`, pinned as submodules under `~/.claude/upstreams/`. What was taken, what was
rejected and why: [`skills/UPSTREAM.md`](skills/UPSTREAM.md). These are **Claude Code only** - Codex
truncates its skill list near 8000 characters, so `setup-agent-symlinks.sh` exports only the table
above. Where an imported skill contradicts this file, **this file wins**.

| Skill | Invoke when |
|-------|-------------|
| `brainstorming` | the requirement is still vague - before any plan exists |
| `office-hours` | deciding whether the thing is worth building at all |
| `how` / `why` / `teach` | how a subsystem runs / why it was built that way / explaining either to a person |
| `architect` | settling types, signatures and module shape before code |
| `arena` | one attempt would lock in the wrong shape - run N in parallel, graft the best |
| `systematic-debugging` | any bug, test failure or unexpected behaviour, **before** proposing a fix |
| `tdd` | a bug with a cheap local test target, or an explicit ask for a failing test first |
| `blast-radius` | what a change could break beyond the diff - proven by running code |
| `interrogate` | adversarial multi-lens review of a diff (§4's independent reviewer) |
| `thermo-nuclear-code-quality-review` | the harshest maintainability pass on abstraction quality and file sprawl |
| `owasp-security` / `cso` | security review of code / threat-modelling a system |
| `verify-this` | one claim needs fresh local evidence, baseline vs treatment |
| `verification-before-completion` | about to say done, fixed, or passing |
| `create-verification-skill` / `maintain-verification-skill` | a repo has no scripted way to drive its real app / that script has drifted |
| `figure-it-out` / `show-me-your-work` | a large migration or unattended run / the decision trail it must leave |
| `technical-writing` / `unslop` / `deslop` | writing docs, RFCs or PR text / cutting AI tells from prose / from a diff |
| `typescript-best-practices` / `terraform-skill` | depth under `conventions` for TS / Terraform |
| `mcp-builder` | authoring an MCP server |
| `reflect` / `writing-skills` | turning a long task's lessons into skill edits / writing the skill |
| `health` / `retro` | code-quality dashboard / weekly engineering retrospective |

The pstack and cursor-team-kit skills were written for Cursor. Resolve every Cursor primitive they
name (`Task` tool, `~/.cursor/rules/pstack-models.mdc`, Cursor model slugs, `.cursor/skills/`)
through [`upstreams/HOST-MAPPING.md`](upstreams/HOST-MAPPING.md); the model roster their multi-model
fan-outs read is [`pstack-models.md`](pstack-models.md).

### Chains for common tasks

Skills compose. The routing above answers "which one"; this answers "in what order".

- **Non-trivial change** - `brainstorming` (if the ask is vague) → `how`/`why` (§0, map before
  changing) → `architect` (settle the shape) → §1 plan + `review-and-implement` → `worktrees` →
  `tdd` where a cheap test target exists → `blast-radius` → `review-staged-diff` → `deslop` →
  `git-commit` → `ci-watch` → `pr-lifecycle` → `cr-loop`.
- **Bug report** - `systematic-debugging` (root cause first, §6) → `tdd` (regression test that fails
  pre-fix) → `blast-radius` → `verification-before-completion` → `git-commit`.
- **Understanding unfamiliar code** - Compass (§0) → `how` → `why` → `teach` if a person needs it.
- **Review before it lands** - `review-staged-diff` always;
  `thermo-nuclear-code-quality-review` when the concern is maintainability; `interrogate` on money,
  auth or tenant-isolation paths; `owasp-security` when the diff touches input, auth or secrets.
- **Proving it works (§4)** - `verify-this` for a single claim, `playwright-verify` for a web-app
  change, `create-verification-skill` once per repo so later sessions inherit the harness,
  `verification-before-completion` as the last gate before saying done.
- **Wide design space** - `arena` instead of one attempt, then `interrogate` the winner.
- **Large migration or unattended run** - `figure-it-out` for the playbook, `show-me-your-work` for
  the decision trail, `pr-orchestration` for the fan-out.
- **After a long task** - `reflect` to route the transcript's lessons into concrete skill edits,
  `writing-skills` to write them, plus the §3 memory entry.

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

- **Simplicity First (YAGNI)**: build only what a current caller needs. (`coding-standards`
  §"Simplicity & Scope")
- **No Laziness**: find root causes. No temporary fixes. Senior developer standards.
- **Fail loud; no silent fallbacks, magic values, or stringly-typed enums.** (`coding-standards`
  §"Fallbacks, Magic Values & Enums")
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
- **Shared checkouts**: before editing, building, installing or pushing in a repo another session may
  be using, check for other sessions and coordinate with them; run shared build, install and push
  steps under a per-repo lock. Invoke the `multi-agent-comms` skill.
- **Comment sparingly**: default to no comment; prune generated ones. (`coding-standards` §"Comments")
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
- **Anti-slop writing: direct, no filler.** Applies to all generated prose (chat, marketing/UI copy,
  comments, commit/PR/issue text, docs), same category as the em-dash rule:
  - *No Victorian/pseudo-profound framing.* Never call mundane logic, variables, or architecture
    "load-bearing", "foundational", "a tapestry", "epistemic", "an intricate dance"; don't "delve".
    State the technical reality plainly rather than romanticizing it with architectural metaphors.
  - *No paraprosdokian / melodramatic antithesis.* Forbid setup-then-reversal copy ("Ten features.
    Zero headaches.", "Everything about billing changed. Your invoice didn't."). Say what the thing
    does and integrates with, without the dramatic reversal.
  - *No unsolicited refactoring.* Do exactly what's asked; a 1-line fix returns the 1-line fix. Don't
    rewrite surrounding code, add modular layers, or introduce dependencies unprompted (reinforces
    YAGNI and "don't touch what you weren't asked to touch").
  - *No sycophantic apology or epistemic hedging.* When corrected, acknowledge in one clause
    ("Corrected.") and give the fix; drop "You're entirely right, thank you for your sharp eye", "I
    want to push back slightly", "to be candid rather than merely encouraging".
  - *No fluff openings.* Lead with the code, command, or answer, not "Here is the solution" or
    "Let's examine the fascinating tension between...".

## Workflow

### 0. Understand the Codebase First

Before answering architecture questions or starting non-trivial work in an unfamiliar project:

- Read the project's `CLAUDE.md` first — it takes precedence over global rules. Check
  `known-issues.md` at the project root (format: invoke `project-docs`).
- **Build the Compass graph first** when the project has >~5 source files or the architecture isn't
  clear from the directory listing, and query it (`compass explain`, `path`, `affected`) instead of
  grep-and-read loops; the binary's location is in `~/.claude/local-paths.md`. Where it can't resolve
  something, fall back to `rg` plus reading the source. Commands and limits: `coding-standards`
  §"Code graph (Compass)".
- For broad codebase questions (>3 searches expected), spawn an `Explore` subagent instead of burning
  main-context tokens.

### 1. Plan Mode Default

- Enter plan mode for architectural decisions and multi-commit work; a few obvious steps don't need a
  formal plan. If something goes sideways, STOP and re-plan.
- **Plan format**: atomic tasks with explicit file paths, each independently verifiable. State what
  changes, where, and how to prove it works. For any item whose necessity isn't self-evident, also
  state **what breaks without it**: a task that can't answer that is a task to cut, and the answer
  becomes the deletion probe the reviewer runs later.
- **Plan the smallest thing that satisfies the request.** Name the caller for every parameter, option,
  and abstraction the plan introduces — if that caller is hypothetical, cut the item. Don't plan
  extensibility nobody asked for, and don't plan a helper you'd write exactly one call to.
- **User checkpoint**: for multi-commit plans, cross-cutting refactors, or anything touching shared
  infrastructure, share the plan before implementing.
- **Plan review loop — MANDATORY gate before implementation starts**: run **2 adversarial review
  passes** over the plan, act on what they find, then go with it. Two passes, whatever the stakes:
  not three, and not "re-review until a pass finds nothing", which on a long plan never converges.
  Keep the SAME reviewer across both passes (`SendMessage`, §2) instead of respawning a fresh one,
  and close the loop by talking with it until you and it agree the plan is good. Do NOT create the
  §1b worktree, enter ExitPlanMode, or write code before those two passes are done.
  Each pass covers: the six review dimensions (below); Reuse (§1a); scope discipline (only what was
  asked?); blast radius (callers, tests, migrations, downstream consumers all listed?); unknowns
  (verify "verify-first" items NOW, not at implementation time). Per-pass findings go in the plan as a
  short "review pass N" note. The `review-and-implement` skill drives this loop.
- Only after those two passes: implement in distinct atomic commits, writing tests as you go.
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
  still comes out. Where the answer is genuinely arguable, **don't argue it, run a deletion probe**
  (invoke the `coding-standards` skill, "Deletion probes"): delete the candidate, run the
  verification, and let the result decide. A "nothing broke" that turns out to be a coverage gap
  rather than dead code is the most valuable finding this dimension produces.

### 1a. Reuse Before Writing — Avoid Duplication

Before writing any new function, type, helper, or module, search for existing functionality that does
the job or ~80% of it. Duplication is far easier to prevent than to clean up.

- **During planning** (required step): grep/Glob for keywords from the task — the behaviour, the data
  type, the verb, related domain nouns. Read the top 3-5 hits. Ask: "does something already solve
  this, or 80% of this?"
- **Check neighbours first**: same package/module, then `utils`/`common`/`shared`/`lib`, then sibling
  packages. Use Compass (`compass query`, `compass explain`) when a graph exists, since it surfaces
  callers and related helpers that grep misses. When missing, build it first (§0).
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

Multi-commit or long-running work, and any work in a checkout another session may be using, happens in
a dedicated git worktree under `~/.claude/worktrees/`, branched off the current branch; never commit
in-progress work directly on the branch you started from. Never create a persistent worktree under
`/tmp`, `$TMPDIR`, or another directory cleared at reboot. **Invoke the `worktrees` skill** for the
full protocol. Headlines: the plan must have passed the §1 review before the worktree exists; the
authoritative plan lives at `~/.claude/projects/<project>/plans/<slug>.md` so a crash
mid-implementation is recoverable; the merge gate is all plan items implemented + a clean §1
post-implementation review + 2 verification passes acted on (per §1); rebase rather than merge by
default. A linked worktree does not isolate submodules either, so `git submodule update`
inside one moves the main clone's submodule HEADs for every worktree on it. A small single-commit
change can stay on a feature branch in the main checkout when no other session is using it.

### 1c. Local Review Loop — Sonnet 5.5 High Reviews Every Implementation Change

Every change the implementer produces is reviewed locally by Sonnet 5.5 high before it counts as done. This runs
inside the implementation phase, upstream of the §1 post-implementation review and the §1b merge
gate; it does not replace either.

1. **The implementer** (Sonnet 5.5 medium; all coding runs at medium, per §2) implements one
   atomic task per the approved plan. Write the plan's task, not a generalised version of it. If the
   task seems to need machinery the plan didn't call for, that is a signal to re-plan rather than to
   improvise it. Before handing the diff to review, **probe your own additions** (invoke the
   `coding-standards` skill, "Deletion probes"): delete each piece whose necessity you couldn't state
   in one sentence and see whether anything actually fails. Cheaper to find here than in review, and
   what survives arrives with evidence attached.
2. **Sonnet 5.5 high reviews the diff locally** across the six review dimensions plus Reuse (§1a) and scope
   discipline, as a dedicated reviewer subagent (set `model`) so the implementer's context stays
   clean. Emit a concrete findings list (`file:line` + what's wrong + suggested fix), or an explicit
   "no actionable findings".
3. **The implementer addresses** every finding. Mechanical fixes stay with the implementer; a finding
   needing a design call escalates that item to Sonnet 5.5 high, then the decided fix goes back down.
4. **Sonnet 5.5 high reviews a second time**, then you go with it: 2 passes per §1, closing on agreement with
   the reviewer rather than further rounds.

Reviewer and implementer are distinct roles, ideally distinct agents (review the diff as if a stranger
wrote it). Log per-round findings in the plan file. Review per task as it lands, don't batch. Keep the
SAME implementer and SAME reviewer alive across both passes via `SendMessage` (§2), so the second
pass costs only the delta.

### 2. Subagent Strategy

**Invoke the `subagent-strategy` skill** for the full rubric; `pr-orchestration` when several
PRs/agents run at once. Headlines:

- Use subagents liberally to keep the main context clean; one focused task per subagent. **Brief them
  fully** — they start cold: goal, relevant context, expected output format, length cap.
- **Reuse a live agent before spawning a new one** (`SendMessage`) when it already holds the relevant
  files, diff, or investigation thread and its context is still small (roughly under 100K tokens); a
  new task gets a fresh agent, even in the same repo. Do NOT reuse when independence is the point (adversarial
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
- **Token budget: every call re-sends the agent's whole context.** One issue per fresh implementer,
  at most 3-4 working agents unless the user asks for more, CI waits in a background shell (never an
  agent poll loop), and tests/lint print only failures. Details: `pr-orchestration` §4.
- **Delegate to the cheapest sufficient tier — actively, not just when in doubt.** The main session is
  usually the most expensive option.
- **Set the `model` parameter on EVERY `Agent` call — never rely on inheritance.**
  Both Sonnet tiers are `model: sonnet`; the effort is the difference. State the effort (medium or
  high) in the brief or use an agent definition that pins it, since `Agent` has no effort parameter.

| Tier | Use for |
|------|---------|
| Sonnet 5.5 low | renames, typo/format fixes, mechanical edits with a clear spec, simple lookups, single-command runs, tightly-specified function/test, small single-file review, documented API migration, rubric classification, short summaries |
| Sonnet 5.5 medium | **all coding.** PR implementation of any size or difficulty, including non-trivial code; writing tests; applying a decided fix from a review, CodeRabbit or CI failure; rebases and conflict resolution; refactors; routine `gh`/`git` mechanics. Design questions are settled upstream by a high-tier planner or reviewer, then the decided fix goes down to medium. |
| Sonnet 5.5 high | **planning, adversarial review and verification only.** PR planning and plan review; architecture/design decisions; diagnosis and hypothesis-driven debugging (deciding what is wrong); reading a large unfamiliar codebase from scratch; all review loops (§1c local review, §1 pre-commit/post-impl, §1d cross-agent PR review, adversarial money-path review); triaging review findings (deciding which are real and what the fix should be); independent verification and reproducing evidence (§4). Nothing escalates above it. It does not write the code. |

A sharper test for a task that could plausibly be either tier: does it write or change code, or does
it decide, review or verify? Writing or changing code is Sonnet 5.5 medium, even when the code is hard;
deciding the design, judging whether an implementation is honest, or checking evidence is Sonnet 5.5 high.
If a medium implementer hits a design question it cannot settle from the plan, it stops and sends that
question to the high-tier planner or reviewer; the decided answer goes back to it.

Mechanical single steps stay Sonnet 5.5 low/medium. When in doubt about a coding task, stay at medium and
send the hard question up rather than escalating the whole task.

### 2a. Tool Selection

**Invoke the `tool-usage` skill before any Bash call or script creation.** Headlines: prefer native
tools (`Read`, `Edit`, `Write`, `Glob`, `Grep`, `NotebookEdit`) over Bash for file ops; avoid
approval-triggering Bash patterns (composed commands, compound `cd &&`, `sudo`/`rm -rf`/`chmod`,
piping into `bash`, `eval`); **any multiline shell MUST be a script file** in `.claude/scripts/`
(persistent) or `/tmp/claude/` (throw-away), reviewed before executing (2 passes per §1, including
for scripts that delete, push, or touch credentials).

### 3. Self-Improvement Loop

- After ANY correction: save the lesson to auto-memory (`~/.claude/projects/<project>/memory/`) as a
  rule that prevents the same mistake.
- **Memory entry structure**: lead with the rule or fact, then a **Why:** line (reason or past
  incident) and a **How to apply:** line (when it triggers). Knowing *why* lets you judge edge cases.
- **Before creating an entry, search existing memories** — prefer updating over duplicating. **Remove
  stale entries promptly.** Review lessons at session start for the relevant project.
- **Apply per-project memory at write time and review gates, not only after CR** — invoke the
  `git-commit` skill for when to read and write the `feedback_*.md` garden.
- **Workflow improvements — propose, then PR**: when you notice a gap in the `~/.claude` guidance
  (missing, ambiguous, contradictory, outdated, or something that just caused friction), **tell the
  user and propose the change**; once they agree, capture it as a pull request against
  `LeanerCloud/dotclaude`. First open a GitHub issue describing the gap,
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
- **Per-change-type**: **UI/frontend** — invoke the `playwright-verify` skill: run the prod-parity
  local stack and drive the feature with Playwright, golden path + edge cases; if the project deploys
  on push, re-verify in the deployed browser afterwards (local pass ≠ deployed pass). **Backend/API**
  — hit the endpoint with `curl` or a test; verify response shape, status codes, error paths.
  **Libraries/shared code** — run the suite AND exercise at least one consumer.
  **Infrastructure/ops** — staging-first (invoke `infra-ops`). **CI/CD** — simulate locally with
  `act` before pushing.
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
- **Regression test and end-to-end proof per §4.** If a test genuinely can't be written
  (environmental, flaky race), say why in the commit message and verify the scenario manually.
- **Escalate only for decisions, not investigations.** For CI failures after a push, invoke `ci-watch`.

### 7. Backlog Triage + Work Selection

Invoke `triage-pass` when the user says "triage" / "prioritize the backlog" / "go over open issues";
or on "what should I work on next?" **when** the open count is non-trivial (>10 items) or labels don't
already give a clear ordering — at ≤10 already-labelled items, invoke `work-selection` and just sort.
A session-start scan showing >30 untriaged items, >5 open PRs untouched in 7 days, or a P0 without
recent activity is grounds to *offer* a pass — don't run it uninvited.

**Per-item rule** (regardless of any pass): **when you create an issue or PR, or update one you own or
were asked to work on, in a repo that uses the triage rubric, apply it inline if the item lacks the
`triaged` marker** (invoke
`triage-labels`). Don't label other people's items as a side effect of reading them; mention them to
the user instead.

## Task Management

Track multi-step work in the built-in task system (TaskCreate/TaskList/TaskUpdate), with a high-level
summary at each step.

## Git Workflow

- **Repo first — check at TASK START, not commit time**: if you're working in a PROJECT dir that isn't
  a git repo, offer to `git init` it before the first non-trivial edit (init without asking only when
  the user asked you to create the project there). Multi-phase work in an
  unversioned tree loses its per-step history irreversibly, and creating a repo is safe and additive
  (the opposite of the never-destroy-`.git` rule, tenet 9). **Exceptions (do NOT init)**: the home dir
  itself, system temp / scratchpad, `~/Downloads`/`~/Desktop` and similar scratch locations.
- **Before staging a commit, invoke `git-commit`** — conventional commits, atomic commits, and the
  mandatory pre-commit review loop (2 passes per §1). Never use heredoc-based `git commit -m`.
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
