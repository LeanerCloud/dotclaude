# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Core Principles

- **Simplicity First**: Make every change as simple as possible. Minimal impact.
- **No Laziness**: Find root causes. No temporary fixes. Senior developer standards.
- **Minimal Impact**: Changes should only touch what's necessary. Avoid introducing bugs.
- When uncertain between two approaches, pick the simpler one and move forward rather than asking.
- **Don't touch what you weren't asked to touch**: No drive-by refactors, no unsolicited formatting changes, no adding types/comments to untouched code - unless explicitly asked for a thorough review of the changed code.
- **Backward compatibility**: Only for libraries/packages consumed by external code - don't break callers without a migration path. Within the project itself, refactor freely.
- **Flag existing issues**: When reading code before modifying it, flag existing bugs or tech debt to the user rather than silently inheriting them. Maintain a `known-issues.md` in source control to track issues to be addressed later, and consult it to inform further work. Remove resolved issues from the list as they are fixed.

## Preferred Stack

- **Language**: Go for new projects
- **IaC**: Terraform when infrastructure is needed
- **Testing**: TDD wherever possible. Use the most popular test framework/idioms for the current language (e.g., `go test` + `testify` for Go, `pytest` for Python, `jest` for JS/TS)
- **Coverage**: Target 80% test coverage for all new code
- **Dependencies**: Prefer stdlib over third-party deps when reasonable (especially in Go). Pin dependency versions. Don't add dependencies without justification.
- **Task runner**: Projects should have a `Makefile` or `Taskfile` as the single entry point for build/test/lint/deploy commands.

## Workflow Orchestration

### 1. Plan Mode Default

- Enter plan mode for ANY non-trivial task (3+ steps or architectural decisions)
- If something goes sideways, STOP and re-plan immediately - don't keep pushing
- Use plan mode for verification steps, not just building
- Write detailed specs upfront to reduce ambiguity
- **Plan review loop**: After creating a plan, enter a review loop — thoroughly review the plan and fix any issues found. Repeat until no issues are found for 3 consecutive review passes. In each pass, print a summary of issues found before/after fixing them. Once the plan is stable, proceed to implement it in distinct atomic commits, writing tests as you go.

### 2. Subagent Strategy

- Use subagents liberally to keep main context window clean
- Offload research, exploration, and parallel analysis to subagents
- For complex problems, throw more compute at it via subagents
- One task per subagent for focused execution
- If a subagent runs out of context, split the work across multiple smaller subagents and re-run automatically

### 3. Self-Improvement Loop

- After ANY correction from the user: save the lesson to auto-memory (`~/.claude/projects/<project>/memory/`)
- Write rules for yourself that prevent the same mistake
- Ruthlessly iterate on these lessons until mistake rate drops
- Review lessons at session start for relevant project

### 4. Verification Before Done

- Never mark a task complete without proving it works
- Diff behavior between main and your changes when relevant
- Ask yourself: "Would a staff engineer approve this?"
- Run tests, check logs, demonstrate correctness
- Simulate CI/CD pipelines locally when possible (e.g., `act` for GitHub Actions, `gitlab-runner exec`) to catch issues before pushing

### 5. Demand Elegance (Balanced)

- For non-trivial changes: pause and ask "is there a more elegant way?"
- If a fix feels hacky: "Knowing everything I know now, implement the elegant solution"
- Skip this for simple, obvious fixes - don't over-engineer
- Challenge your own work before presenting it

### 6. Autonomous Bug Fixing

- When given a bug report: just fix it. Don't ask for hand-holding
- Point at logs, errors, failing tests - then resolve them
- Zero context switching required from the user
- Go fix failing CI tests without being told how

## Task Management

- Use Claude Code's built-in task system (TaskCreate/TaskList/TaskUpdate)
- Plan first, verify plan, then track progress through built-in tasks
- Explain changes with a high-level summary at each step
- Capture lessons in auto-memory after corrections

## Git Conventions

- Use bullet points in commit message bodies
- **NEVER** mention Anthropic or Claude in commit messages (no Co-Authored-By lines)
- Keep subject line concise, body explains the "why"
- **Small atomic commits**: Each commit should capture one small, distinct piece of functionality
- Commits must be independently revertable without breaking the project
- Within a single file, stage and commit small chunks separately rather than committing the whole file at once
- Create feature branches for non-trivial work
- **Commit hooks**: Projects should have comprehensive pre-commit hooks (linting, formatting, tests). Ensure all hooks pass cleanly on new code before committing - never skip them with `--no-verify`.
- **Pre-commit review**: Before every commit, do a second thorough review of all staged changes. Read the diff carefully, check for bugs, style issues, missing tests, and leftover debug code. Fix all found issues before committing - never commit known problems.
- **Docs with code**: Each commit should include relevant documentation updates (README, inline comments, CHANGELOG) when the code changes warrant it - keep docs, IaC, backend, and frontend all in sync.

## Security

- Never hardcode credentials, secrets, or API keys
- Use environment variables or secret managers for sensitive config
- Never commit `.env` files, credentials, or tokens

## Error Handling

- Return errors explicitly - never swallow errors silently (especially in Go)
- Wrap errors with context so the caller knows where and why it failed
- Log at appropriate levels (debug/info/warn/error) - don't log everything as error
- Use structured logging (JSON) for machine-parseable output when the project already follows this pattern
- Include correlation IDs for request tracing across services
- Never log PII, secrets, or tokens

## Performance

- Consider performance implications of changes (n+1 queries, unnecessary allocations, missing indexes)
- Don't prematurely optimize, but don't introduce obvious bottlenecks either
- **Go concurrency**: Think about race conditions, use `-race` flag in tests, be deliberate about shared state
- **Cyclomatic complexity**: Keep below 10 on all new code. Break up complex functions.

## API Design

- Follow REST conventions consistently (proper HTTP methods, status codes, resource naming)
- Use a consistent error response format across all endpoints
- Version APIs when breaking changes are unavoidable
- Validate all input at system boundaries - don't trust external data
- Design operations to be idempotent where possible (safe to retry/re-run without side effects)
- Scripts, IaC, migrations, and handlers should be safe to run multiple times without side effects

## Rollback Awareness

- Before deploying or applying Terraform changes, consider what the rollback plan is
- For destructive Terraform operations (`destroy`, resource replacement), always confirm with the user first
- Design changes to be reversible where possible

## Go Conventions

- Project layout: `cmd/` for entrypoints, `internal/` for private packages, `pkg/` for public libraries
- Run `golangci-lint` and `go vet` before commits
- Use table-driven tests as the default test pattern
- Favour interfaces for testability and decoupling
- Follow Go naming idioms: exported names in CamelCase, unexported in camelCase

## Naming Conventions

- Be consistent within each project: files, packages, variables, resources
- Follow language idioms (camelCase for Go exports, snake_case for Terraform/Python)
- Descriptive names over abbreviations - code is read more than written

## Docker Conventions

- Use multi-stage builds to keep images small
- Minimal base images (distroless or alpine)
- Never run containers as root
- Maintain a `.dockerignore` - keep build context clean

## Cost Awareness

- Tag all cloud resources (project, environment, owner at minimum)
- Prefer spot/preemptible instances where workloads allow
- Consider cost implications of resource choices (instance types, storage classes, data transfer)
- Right-size resources - don't default to large instances

## Project Knowledge Base

Every project should maintain a living knowledge base at `.project-docs/` in the project root. These docs are kept up to date as work progresses and consulted whenever relevant.

### Structure

```
.project-docs/
  INDEX.md              # Master index linking to all docs below - always kept current
  architecture.md       # System architecture, component relationships, data flow
  development.md        # Project-specific dev standards, build/test/lint commands, local setup
  deployment.md         # Deployment instructions, environments, CI/CD pipeline details
  history.md            # Historical evolution of the codebase derived from git history
  decisions.md          # Key architectural and technical decisions with rationale
  conventions.md        # Project-specific naming, style, and patterns (overrides globals)
  infrastructure.md     # Cloud resources, Terraform layout, networking, IAM
  api.md                # API contracts, endpoints, auth, versioning
  data-model.md         # Database schema, migrations strategy, data flows
```

### Rules

- **On first visit to a project**: Check if `.project-docs/` exists. If not, create it with `INDEX.md` and populate what you can by reading the codebase and git history.
- **During work**: Update relevant docs as you make changes - treat them like code, not afterthoughts.
- **Before starting a task**: Consult relevant docs to understand context, prior decisions, and conventions.
- **INDEX.md**: Always reflects the current set of docs with a one-line summary of each. Update it whenever you add, remove, or significantly change a doc.
- **history.md**: Derive from `git log` - summarize major phases, key refactors, and inflection points. Update when significant milestones are reached.
- **Only create docs that are relevant**: Not every project needs all files. A simple CLI tool doesn't need `api.md` or `infrastructure.md`.
- **Keep docs concise and current**: Stale docs are worse than no docs. Remove or update outdated sections rather than letting them rot.

## Session Handoff

- When ending a session or running low on context, leave a summary of: current state, what's done, what's left, and any blockers - so the next session can pick up seamlessly

## Idempotency

- Scripts, IaC, migrations, and API handlers should be safe to run multiple times without side effects
- Use `CREATE IF NOT EXISTS`, upserts, and idempotency keys where applicable
- Terraform is inherently idempotent - maintain that property in custom scripts and provisioners

## Documentation

- Update READMEs when adding new features, changing setup steps, or altering project structure
- Inline comments only for non-obvious logic - don't state the obvious
- Maintain a CHANGELOG if one already exists in the project

## Terraform Conventions

- Use consistent module structure: `main.tf`, `variables.tf`, `outputs.tf`, `versions.tf`
- Remote state backends (never local state for shared projects)
- Run `tflint` and `terraform validate` before committing
- Use meaningful resource naming conventions with project/environment prefixes
- Pin provider and module versions

## Database & Migrations

- All schema changes via versioned migration files - never manual DDL in production
- Migrations must be backward-compatible: add before remove (e.g., add new column, migrate data, then drop old column)
- Test migrations against a copy of prod data before applying to production
- Every migration must have a corresponding rollback/down migration

## Monitoring & Health Checks

- Expose `/health` and `/ready` endpoints for all services
- Track key metrics: latency, error rates, saturation
- Define meaningful alerts - not just "is it up" but "is it performing correctly"
- Health checks should verify downstream dependencies (DB, cache, external APIs)

## Timeouts & Resilience

- Always set explicit timeouts on HTTP clients, DB connections, and external calls - never use defaults that wait forever
- Use retries with exponential backoff for transient failures
- Circuit breakers for external dependencies to prevent cascade failures
- Graceful degradation: fail partially rather than completely when a dependency is down

## Multi-Environment Awareness

- Maintain dev/staging/prod parity - same Docker image, same IaC, different config
- Environment-specific config via environment variables only, never via code branches or conditionals
- Test infrastructure changes in lower environments before promoting to production

## Dependency Updates

- Keep dependencies current - don't let them drift for months
- Read changelogs for breaking changes before updating
- Batch minor/patch updates together, handle major version bumps individually with thorough testing

## Debugging Approach

- When stuck, add observability first (logs, metrics) rather than guessing
- Always reproduce the bug before attempting a fix
- Write a failing test that captures the bug, then fix it - the test proves the fix and prevents regression
