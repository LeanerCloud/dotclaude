---
name: project-docs
description: The `.project-docs/` knowledge-base layout, ADR and runbook templates, and the
  `known-issues.md` convention. Invoke when setting up, updating, or consulting project
  documentation.
---

# Project Knowledge Base

Every project keeps a living knowledge base at `.project-docs/` in the project root, in sync with the code and consulted whenever relevant.

## Structure

```
.project-docs/
  INDEX.md              # Master index, always current; one-line summary per doc
  architecture.md       # Components, relationships, data flow
  development.md        # Local setup, build/test/lint commands, dev standards
  deployment.md         # Deploy instructions, environments, CI/CD, rollback
  history.md            # Major phases, key refactors, inflection points
  decisions.md          # Key decisions with rationale (or ADRs below)
  conventions.md        # Project-specific naming, style, patterns (overrides globals)
  infrastructure.md     # Cloud resources, Terraform layout, networking, IAM, cost
  api.md                # Contracts, endpoints, auth, versioning, examples
  data-model.md         # Schema, migration strategy, data flows, index rationale
  runbooks/             # Operational runbooks (below)

known-issues.md         # project root, NOT inside .project-docs/: known bugs and tech debt
```

Create only relevant files (a simple CLI needs no `api.md`). Incomplete but accurate beats comprehensive but stale.

## Compass Knowledge Graph

Compass builds a local, cross-session graph of the codebase (symbols, calls, dependencies, communities). Usage and limits: `AGENTS.md` §0.

- **First visit**: if `compass-out/` is missing and the project is non-trivial, `compass init . --yes` (or out of tree with `compass extract <path> --code-only --out <dir>` when the checkout isn't yours).
- **Orientation**: `compass export html` (or `wiki`); targeted questions with `compass explain <symbol>` and `compass affected <symbol>`.
- **During work**: `compass update .` after code changes (or keep `compass watch` running).
- `compass install --project` writes Compass guidance into the project; review generated files before committing.

## Rules

- **First visit**: if `.project-docs/` is missing, create `INDEX.md` and populate what you can from the codebase and `git log`; create `known-issues.md` in the root if missing. No placeholder files; only docs you can fill in.
- **During work**: update relevant docs in the same commit as the change.
- **Before a task**: consult `known-issues.md` and relevant `.project-docs/` sections.
- **INDEX.md**: current set of docs, one line each; update on add, remove or significant change.
- **history.md**: derive from `git log --oneline` and `git log --stat`; update at milestones.
- **Stale docs are worse than no docs**: update or delete outdated sections (note the gap in `INDEX.md`).
- **Where things go**: `known-issues.md` for acknowledged unfixed bugs/debt; inline comment for non-obvious logic at one spot; `.project-docs/` for cross-cutting context, decisions, operations; README for getting-started and overview.

---

## known-issues.md

Project root file tracking acknowledged, unfixed bugs and tech debt:

```markdown
## [ISSUE-ID or short slug] Short title

**Severity**: low | medium | high  
**Discovered**: YYYY-MM-DD  
**Area**: package or component name

Description and why it hasn't been fixed yet.

**Workaround** (if any): ...
**Fix plan** (if known): ...
```

- Consult it before starting work (a known issue in the area may change your approach).
- Remove entries when resolved, referencing the fixing commit or PR.
- Triage regularly; escalate or schedule high-severity items.

---

## Architecture Decision Records (ADRs)

For significant decisions (technology choice, major design pattern, data model), write a short ADR in `.project-docs/decisions/`:

```markdown
# NNN. Short Title

**Date**: YYYY-MM-DD  
**Status**: Accepted | Superseded by ADR-NNN  
**Stakeholders**: names or roles of people involved in the decision  

## Context
What situation, constraint, or requirement forced this decision?

## Options Considered
- **Option A**: description - pros / cons
- **Option B**: description - pros / cons

## Decision
What was chosen and why.

## Consequences
What does this make easier? Harder? What ongoing obligations?
```

- ADRs are immutable once accepted: write a new ADR that supersedes the old one instead of editing for hindsight.
- Link from code or docs where the decision manifests: `// See .project-docs/decisions/003-use-postgres.md`
- Number sequentially: `001-`, `002-`.

---

## Runbooks

In `.project-docs/runbooks/`, one per recurring task or incident type. Write one for: any operation done manually more than once, any post-mortem action item, anything hard to figure out under pressure at 2am. Common early ones: failed deployment / rollback, secret rotation.

```markdown
# Runbook: [Title]

**Last verified**: YYYY-MM-DD  
**Owner**: team or person responsible  
**Alert / trigger**: what situation this runbook addresses  

## Symptoms
What the problem looks like; alerts that fire; what users report.

## Impact
Who is affected and how severely.

## Diagnosis Steps
1. Check X: `command or query`
2. Look at Y dashboard: [link]

## Resolution Steps
1. Do A: `command`
2. Verify with: `command`

## Escalation
If unresolved after N minutes, escalate to: [person/team/runbook]

## Post-incident
- File a known issue or bug if root cause isn't fully fixed
- Update this runbook if steps were wrong or missing
```

---

## development.md Template

At minimum:

```markdown
## Prerequisites
Tools and versions (Go 1.22+, Node 20+, Docker, etc.)

## Setup
Clean clone to running service.

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
Non-obvious steps, service dependencies, seed data.

## Running a Subset of Tests
- Single package: `go test ./internal/users/...`
- Single test: `go test -run TestUserService_Create ./internal/users/`
- With coverage: `go test -coverprofile=coverage.out ./... && go tool cover -html=coverage.out`

## Troubleshooting
| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| `connection refused` on startup | DB not running | `make db-start` |
```
