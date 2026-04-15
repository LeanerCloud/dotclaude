# Project Knowledge Base

Every project should maintain a living knowledge base at `.project-docs/` in the project root. These docs are kept in sync with the code and consulted whenever relevant.

## Structure

```
.project-docs/
  INDEX.md              # Master index — always current; one-line summary per doc
  architecture.md       # System architecture, component relationships, data flow diagrams
  development.md        # Local setup, build/test/lint commands, project-specific dev standards
  deployment.md         # Deployment instructions, environments, CI/CD pipeline, rollback steps
  history.md            # Evolution of the codebase: major phases, key refactors, inflection points
  decisions.md          # Key architectural and technical decisions with rationale (or use ADRs below)
  conventions.md        # Project-specific naming, style, and patterns (overrides globals)
  infrastructure.md     # Cloud resources, Terraform layout, networking, IAM, cost notes
  api.md                # API contracts, endpoints, auth, versioning, example requests/responses
  data-model.md         # Database schema, migration strategy, data flows, index rationale
  runbooks/             # Operational runbooks for common tasks and incidents (see below)

known-issues.md         # project root — NOT inside .project-docs/ — known bugs and tech debt
```

Only create files that are relevant — a simple CLI tool doesn't need `api.md` or `infrastructure.md`. An incomplete but accurate doc is better than a comprehensive stale one.

## graphify Knowledge Graph

graphify builds a persistent knowledge graph from the codebase — community detection, cross-file relationships, and architecture overview — that survives across sessions and reduces the need to re-read raw files.

- **On first visit to a project**: check if `graphify-out/GRAPH_REPORT.md` exists
  - If yes → read it before starting work; it shows god nodes, community clusters, and component relationships
  - If `graphify-out/wiki/index.md` exists → navigate the wiki instead of reading raw source files
  - If neither exists and the project is non-trivial → run `~/devel/graphify/.venv/bin/python -m graphify .` to build the graph
- **During work**: after modifying code files, keep the graph current: `~/devel/graphify/.venv/bin/python -c "from graphify.watch import _rebuild_code; from pathlib import Path; _rebuild_code(Path('.'))"`
- **Install into a project**: `~/devel/graphify/.venv/bin/python -m graphify claude install` — writes the graphify section to the project's `CLAUDE.md` and installs git hooks for auto-rebuild

## Rules

- **On first visit**: Check if `.project-docs/` exists. If not, create `INDEX.md` and populate what you can by reading the codebase and `git log`. Also check for `known-issues.md` in the project root — create it if missing. Don't create placeholder files — only create docs you can actually fill in.
- **During work**: Update relevant docs as you make changes. Treat them like code: they're part of the commit, not an afterthought.
- **Before starting a task**: Consult `known-issues.md` and relevant `.project-docs/` sections to understand prior decisions and outstanding issues before proposing changes.
- **INDEX.md**: Always reflects the current set of docs with a one-line summary of each. Update whenever you add, remove, or significantly change a doc.
- **history.md**: Derive from `git log --oneline` and `git log --stat`. Summarize major phases and inflection points. Update when significant milestones are reached.
- **Stale docs are worse than no docs**: Remove or update outdated sections rather than letting them rot. If a section is no longer accurate and you don't have time to fix it, delete it and note the gap in `INDEX.md`.
- **When to use what**:
  - `known-issues.md` — bugs and tech debt that aren't being fixed right now
  - Inline code comment — non-obvious logic in a specific spot
  - `.project-docs/` — cross-cutting context, decisions, and operational knowledge
  - README — getting-started and high-level overview for new contributors

---

## known-issues.md

Maintain a `known-issues.md` in the project root to track bugs and tech debt that are acknowledged but not yet fixed. Format each entry as:

```markdown
## [ISSUE-ID or short slug] Short title

**Severity**: low | medium | high  
**Discovered**: YYYY-MM-DD  
**Area**: package or component name

Description of the issue and why it hasn't been fixed yet.

**Workaround** (if any): ...
**Fix plan** (if known): ...
```

- Consult `known-issues.md` before starting work — a known issue in the area may affect your approach
- Remove entries when the issue is resolved; reference the fixing commit or PR
- Don't let it grow unbounded — triage regularly; escalate or schedule high-severity items

---

## Architecture Decision Records (ADRs)

For significant decisions (technology choice, major design pattern, data model approach), write a short ADR in `.project-docs/decisions/`:

```markdown
# NNN. Short Title

**Date**: YYYY-MM-DD  
**Status**: Accepted | Superseded by ADR-NNN  
**Stakeholders**: names or roles of people involved in the decision  

## Context
What situation, constraint, or requirement forced this decision?

## Options Considered
- **Option A**: description — pros / cons
- **Option B**: description — pros / cons

## Decision
What was chosen and why.

## Consequences
What does this make easier? What does it make harder? What are the ongoing obligations?
```

**Rules:**
- ADRs are immutable once accepted — don't edit them to reflect hindsight. Write a new ADR that supersedes the old one instead.
- Link to the relevant ADR from code or docs where the decision manifests: `// See .project-docs/decisions/003-use-postgres.md`
- Number sequentially: `001-`, `002-`, etc.

---

## Runbooks

Operational runbooks live in `.project-docs/runbooks/`. Each runbook covers a specific recurring task or incident type.

### When to write a runbook
- Any operation you've done manually more than once
- Any incident post-mortem action item
- Anything that would be hard to figure out under pressure at 2am

### Runbook format

```markdown
# Runbook: [Title]

**Last verified**: YYYY-MM-DD  
**Owner**: team or person responsible  
**Alert / trigger**: what situation this runbook addresses  

## Symptoms
What does the problem look like? What alerts fire? What do users report?

## Impact
Who is affected and how severely?

## Diagnosis Steps
1. Check X: `command or query`
2. Look at Y dashboard: [link]
3. ...

## Resolution Steps
1. Do A: `command`
2. Verify with: `command`
3. ...

## Escalation
If unresolved after N minutes, escalate to: [person/team/runbook]

## Post-incident
- File a known issue or bug if root cause isn't fully fixed
- Update this runbook if steps were wrong or missing
```

Common runbooks to write early (adapt to what's relevant for the project):
- Failed deployment / how to roll back
- Secret rotation procedure
- Any manual operation you've already done once and will need to do again

---

## development.md Template

At minimum, `development.md` should cover:

```markdown
## Prerequisites
List tools and versions required (Go 1.22+, Node 20+, Docker, etc.)

## Setup
Step-by-step local setup from a clean clone to a running service.

## Common Commands
| Command          | Description                        |
|------------------|------------------------------------|
| make build       | Build the project                  |
| make test        | Run the full test suite            |
| make test-short  | Run unit tests only (fast)         |
| make lint        | Run all linters                    |
| make dev         | Start local dev environment        |
| make migrate     | Run pending database migrations    |

## Environment Variables
| Variable       | Required | Description / Example        |
|----------------|----------|------------------------------|
| DATABASE_URL   | yes      | postgres://localhost/myapp   |
| PORT           | no       | Default: 8080                |

## Running Locally
Any non-obvious steps, service dependencies, seed data, etc.

## Running a Subset of Tests
- Single package: `go test ./internal/users/...`
- Single test: `go test -run TestUserService_Create ./internal/users/`
- With coverage: `go test -coverprofile=coverage.out ./... && go tool cover -html=coverage.out`

## Troubleshooting
| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| `connection refused` on startup | DB not running | `make db-start` |
| Tests fail with `port in use` | Previous test run didn't clean up | `make kill-test-servers` |
```
