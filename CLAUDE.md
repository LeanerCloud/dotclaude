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

> **Scale to context**: Some rules below (PR reviews, staging environments, on-call rotations) assume a multi-person team. Apply them proportionally — a solo project doesn't need a formal review process, but the underlying principle (don't merge broken code, test before deploying) always applies.

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
- If neither exists and the project has more than ~5 source files or the architecture isn't clear from the directory listing alone, run graphify to build the graph: `~/devel/graphify/.venv/bin/python -m graphify .`
- After modifying code files in a session, keep the graph current: `~/devel/graphify/.venv/bin/python -c "from graphify.watch import _rebuild_code; from pathlib import Path; _rebuild_code(Path('.'))"`

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
  - Fix every issue found. Re-review after fixes. Do not declare done until this passes cleanly.

### 2. Subagent Strategy

- Use subagents liberally to keep the main context window clean
- Offload research, exploration, and parallel analysis to subagents
- One task per subagent for focused execution
- If a subagent runs out of context, split across multiple smaller subagents and re-run
- **Match model to task complexity via the `Task` tool's `model` parameter** — pay for capability only when it earns it:
  - **Haiku**: file renames, typo fixes, mechanical edits with a clear spec, simple lookups (grep for a symbol, find where X is called), reading a single file to answer a factual question, formatting/style fixes, running a single command and reporting output. Cheap, fast, good enough when the answer is mostly mechanical.
  - **Sonnet** (default for most delegations): focused multi-file changes with clear requirements, writing a new test, implementing a well-specified function, code review of a single diff, migrating between APIs with known mappings.
  - **Opus** (or stay on the current top-level model): architecture decisions, multi-file refactors where the shape is unclear, debugging gnarly bugs that need hypothesis iteration, reading a large unfamiliar codebase and synthesising a mental model, any work where "understanding" is the hard part rather than the mechanical output.
  - **When in doubt, go one tier cheaper and see if the result is good enough** — it's easy to re-spawn on a stronger model if Haiku/Sonnet struggles, and the savings on routine work add up.
  - The main conversation's model is set by the user and doesn't change mid-session; this rule only applies to `Task` tool spawns.

### 2a. Tool Selection

Full rules live in `~/.claude/tool-usage.md` — **read it before any Bash call or script creation**. Headline principles:

- Prefer native tools (`Read`, `Edit`, `Write`, `Glob`, `Grep`, `NotebookEdit`) over Bash for file operations — faster, safer, no approval prompts.
- When using Bash, avoid approval-triggering patterns: compound `cd && ...`, shell expansions on untrusted paths, `sudo`/`rm -rf`/`chmod`/`chown`, piping into `bash`/`sh`, `eval`/`source`.
- **Any multiline shell MUST be a script file in `.claude/scripts/` (persistent) or `.claude/scripts/tmp/` (throw-away) — no exceptions.** Review every script with 3 clean passes before executing. See `tool-usage.md` for the full script review loop and cleanup rules.

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

## Git Conventions

### Commit messages
- Use **conventional commits** format: `type(scope): subject` — e.g. `feat(auth): add OAuth2 login`, `fix(api): handle nil pointer on empty response`
  - Common types: `feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `perf`, `ci`, `build`
  - `scope` is optional but useful for larger repos; omit when the repo is small
  - This enables automated changelog and release notes generation (see infra-ops.md CI/CD)
- Subject line: imperative mood, lowercase, no trailing period, ≤72 chars
- Body (when needed): bullet points explaining the *why*, not the *what*; wrap at 72 chars
- **NEVER** mention Anthropic or Claude in commit messages (no Co-Authored-By lines)
- **Avoid heredocs for `git commit -m`**: patterns like `git commit -m "$(cat <<'EOF' ... EOF)"` are fragile — the `$(...)` command substitution can trigger approval prompts, the heredoc-inside-substitution interacts badly with pre-commit hooks that stash unstaged changes (the stash/restore cycle has been observed to fail repeatedly), and quoting/escaping bugs are easy to introduce. Instead, write the message to `.claude/scripts/tmp/commit-msg.txt` with the `Write` tool, run `git commit -F .claude/scripts/tmp/commit-msg.txt`, and delete the file after a successful commit.
- **Small atomic commits**: one distinct piece of functionality per commit; independently revertable
- Stage and commit small chunks within a file separately rather than the whole file at once
- **⚠️ MANDATORY pre-commit review loop — NO EXCEPTIONS**: Before every commit, you MUST enter a review loop (same discipline as the plan review loop). Do NOT commit after a single pass — iterate until **3 consecutive review passes find zero issues**. Do NOT skip, shortcut, or batch this step. The goal is to land clean commits in the first place, so the history doesn't need fix-up commits.
  - **Each pass**: read the full staged diff (`git diff --cached`) and the relevant unstaged context, and systematically check all four dimensions:
    - **Completeness**: Does the commit deliver what it claims? Nothing missing? All touched files consistent with the commit message? Tests updated for the changed behaviour?
    - **Correctness**: Any logic errors, off-by-ones, wrong assumptions, broken invariants, stale references, type mismatches, leftover debug code, unused imports?
    - **Security**: Any injection vectors, auth bypasses, secrets exposure, missing input validation, OWASP top 10 violations?
    - **Bugs**: Race conditions, null derefs, edge cases, resource leaks, error handling gaps, broken tests, stale mocks?
  - **Each iteration**: print a short summary of issues found before and after fixing them (matches the plan-review-loop format). An iteration with fixes resets the clean-pass counter — you need 3 clean passes *after* the last fix.
  - **For multi-commit work** (a sequence of atomic commits implementing one plan): review each commit's staged diff individually AND think about how it interacts with already-committed work in the sequence.
  - **Delegate when useful**: for staged changes touching multiple concerns (Go + TS + Terraform), launch specialised review agents in parallel (`feature-dev:code-reviewer` works well) and compile findings before committing.
  - **Fix before committing, never after**: if the review finds issues, fix them in the same staged changeset — do not commit and then create a follow-up fix commit. The history should not contain "oops, fixing previous commit" patterns when the issue could have been caught before the commit landed.
- **Post-commit sanity check**: after committing, run a quick sanity scan (`git show HEAD`) to catch anything the pre-commit loop missed — but if this finds issues, treat it as a process failure (the pre-commit loop should have caught them). Fix-forward in a new commit only when strictly necessary (e.g., pre-commit hook caught a legitimate issue that required the commit to land first).
- **Post-push CI watcher (background agent)**: after every `git push` that publishes new commits, immediately launch a background agent (`Agent` tool with `run_in_background: true`) to monitor the GitHub Actions run(s) for the pushed commit(s). The agent's job is to: (1) poll `gh run list --commit <sha>` and `gh run watch` until each workflow finishes; (2) on failure, fetch logs with `gh run view --log-failed`, diagnose the root cause, and **fix it autonomously** with a follow-up commit + push (e.g., lint failures, broken tests, terraform validation, type errors, missing env vars in CI) — not just report back; (3) only escalate to the user if the failure requires a decision (credentials, infra changes, ambiguous design choices) or if its own fix attempt also fails CI. Use a clear background-agent name like `ci-watch-<short-sha>` so it's addressable via `SendMessage`. Do NOT poll the watcher from the foreground — it will notify on completion. This keeps the main session unblocked while CI runs and ensures broken main never sits unaddressed.

### Pull requests
- **Keep PRs small**: aim for ≤400 lines of meaningful change; large PRs get shallow reviews
- **One concern per PR**: don't mix a refactor with a behaviour change, or a bug fix with a new feature — split them; each PR should be independently revertable
- PR title should follow the same conventional commits format as commit messages (enables changelog generation from PR titles as a fallback)
- Include a short description of *why* the change is needed, not just *what* it does
- Create feature branches for non-trivial work; name them `type/short-description` (e.g. `feat/oauth-login`, `fix/nil-pointer-api`)

### Hooks & docs
- **Pre-commit hooks**: projects must have hooks for linting, formatting, and tests. Never skip with `--no-verify`.
- **Docs with code**: each commit includes relevant doc updates (README, CHANGELOG, inline comments) when warranted

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
