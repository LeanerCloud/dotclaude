# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in any repository.

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

- Check if `graphify-out/GRAPH_REPORT.md` exists — if so, read it for god nodes, community structure, and component relationships before touching any code
- If `graphify-out/wiki/index.md` exists, navigate it instead of reading raw source files
- If neither exists and the project has more than ~5 source files or the architecture isn't clear from the directory listing alone, run `graphify .` to build the graph
- After modifying code files in a session, re-run `graphify .` to keep the graph current

### 1. Plan Mode Default

- Enter plan mode for ANY non-trivial task (3+ steps or architectural decisions)
- If something goes sideways, STOP and re-plan — don't keep pushing
- Write detailed specs upfront to reduce ambiguity
- **Plan review loop**: After creating a plan, review it thoroughly and fix issues found. Repeat until 3 consecutive passes find nothing. Then implement in distinct atomic commits, writing tests as you go.
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
- **Use graphify when available**: if `graphify-out/` exists, search the wiki and god-node list — the graph often surfaces existing helpers and utilities that grep misses because names don't overlap.
- **If similar code exists, decide explicitly**:
  - **Exact fit**: reuse it. Import it, don't copy-paste it.
  - **Close fit (covers ~80% of the need)**: propose refactoring the existing code to cover the new use case — add a parameter, extract a smaller helper, generalise a type, introduce an options struct. Flag the refactor in the plan, explain the blast radius (callers touched, tests affected), and get user approval before expanding scope beyond the original task.
  - **Superficially similar but semantically different**: document in the plan *why* you're not reusing it, so the reader (and future you) knows the duplication is deliberate and not an oversight.
- **Never silently copy-paste**: if you catch yourself writing something that "feels familiar" from earlier in the session or from another file you read, stop and search — you are almost certainly duplicating something that already exists.
- **Cross-language duplication** (e.g. validation logic mirrored between frontend and backend) is acceptable only when unavoidable. Document both sides with a comment referencing the other location so they stay in sync.
- **Scope discipline**: proposing a refactor to avoid duplication is not a licence to restructure the whole file. The refactor should be the minimum change that lets the existing code serve the new use case. If it balloons, split it into a separate refactor commit that lands first, then add the new use case on top.

### 2. Subagent Strategy

- Use subagents liberally to keep the main context window clean
- Offload research, exploration, and parallel analysis to subagents
- One task per subagent for focused execution
- If a subagent runs out of context, split across multiple smaller subagents and re-run
- **Match model to task complexity via the `Agent` tool's `model` parameter** — pay for capability only when it earns it:
  - **Haiku**: file renames, typo fixes, mechanical edits with a clear spec, simple lookups (grep for a symbol, find where X is called), reading a single file to answer a factual question, formatting/style fixes, running a single command and reporting output. Cheap, fast, good enough when the answer is mostly mechanical.
  - **Sonnet** (default for most delegations): focused multi-file changes with clear requirements, writing a new test, implementing a well-specified function, code review of a single diff, migrating between APIs with known mappings.
  - **Opus** (or stay on the current top-level model): architecture decisions, multi-file refactors where the shape is unclear, debugging gnarly bugs that need hypothesis iteration, reading a large unfamiliar codebase and synthesising a mental model, any work where "understanding" is the hard part rather than the mechanical output.
  - **When in doubt, go one tier cheaper and see if the result is good enough** — it's easy to re-spawn on a stronger model if Haiku/Sonnet struggles, and the savings on routine work add up.
  - The main conversation's model is set by the user and doesn't change mid-session; this rule only applies to `Task` tool spawns.

### 2a. Tool Selection

Full rules live in `~/.claude/tool-usage.md` — **read it before any Bash call or script creation**. Headline principles:

- Prefer native tools (`Read`, `Edit`, `Write`, `Glob`, `Grep`, `NotebookEdit`) over Bash for file operations — faster, safer, no approval prompts.
- When using Bash, avoid approval-triggering patterns: composed commands (`&&`, `||`, `;`), compound `cd && ...`, shell expansions on untrusted paths, `sudo`/`rm -rf`/`chmod`/`chown`, piping into `bash`/`sh`, `eval`/`source`.
- **Any multiline shell MUST be a script file in `.claude/scripts/` (persistent) or `/tmp/claude/` (throw-away) — no exceptions.** Review every script with 3 clean passes before executing. See `tool-usage.md` for the full script review loop and cleanup rules.

### 3. Self-Improvement Loop

- After ANY correction from the user: save the lesson to auto-memory (`~/.claude/projects/<project>/memory/`)
- Write rules that prevent the same mistake
- Review lessons at session start for relevant project
- **Workflow improvements**: whenever you notice that a rule or process described in `~/.claude/CLAUDE.md` or any linked file (`coding-standards.md`, `conventions.md`, `infra-ops.md`, `project-docs.md`, `multi-agent-comms.md`, or project-level `CLAUDE.md`) could be improved — missing guidance, ambiguous wording, outdated advice, a gap that just caused a problem — **proactively propose the change** to the user. Describe what you'd add/change and why, and wait for approval before editing the file. Do not silently skip improvements; the system gets better only if gaps are surfaced. This applies equally to project-level `CLAUDE.md` files for project-specific workflow gaps.

### 4. Verification Before Done

- Never mark a task complete without proving it works
- Ask: "Would a staff engineer approve this?"
- Run tests, check logs, demonstrate correctness
- Simulate CI/CD locally when possible (`act` for GitHub Actions)

### 5. Demand Elegance (Balanced)

- For non-trivial changes: pause and ask "is there a more elegant way?"
- If a fix feels hacky: implement the elegant solution
- Skip for simple, obvious fixes — don't over-engineer

### 6. Autonomous Bug Fixing

- When given a bug report: just fix it. Don't ask for hand-holding.
- Point at logs, errors, failing tests — then resolve them.
- Fix failing CI tests without being told how.

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
