# Conventions

Language- and technology-specific conventions. Read the relevant sections when working with that technology. Project-specific overrides live in the project's `CLAUDE.md`.

## Go

### Project Layout
- `cmd/` — entrypoints (one subdirectory per binary)
- `internal/` — private packages not importable by external code
- `pkg/` — public libraries intended for external use (use sparingly; most code belongs in `internal/`)
- `vendor/` or Go modules — pin versions; always commit `go.sum`; run `go mod tidy` before committing
- `testdata/` — test fixtures and golden files; Go toolchain ignores this directory during normal builds

### Code Style
- Run `golangci-lint run` (all checks enabled) before every commit via pre-commit hook (`govet` is included by default — no need to run `go vet` separately)
- Format with `gofumpt` (stricter than `gofmt`)
- Follow Go naming idioms: exported names in `CamelCase`, unexported in `camelCase`, acronyms consistent (`userID` not `userId`, `HTTPServer` not `HttpServer`)
- Receiver names: short, consistent, never `self` or `this`
- Error variables: `ErrFoo` for sentinel errors, `FooError` for types
- Interfaces: name by behaviour (`Reader`, `Storer`), keep them small (1–3 methods); define at the point of use, not the point of implementation

### Logging
- Use `log/slog` (stdlib since Go 1.21) for structured logging — no need for `logrus` or `zap` in new projects
- Default to `slog.Info`, `slog.Error`, etc. with key-value attributes: `slog.Info("request complete", "duration_ms", ms, "status", code)`
- Create a package-level logger or pass via context; avoid the global `slog.Default()` in library code

### Testing
- Table-driven tests as the default pattern
- Use `testify/assert` and `testify/require`; `require` for preconditions that make further assertions meaningless if they fail
- Use `t.Parallel()` in unit tests unless there's shared mutable state
- Use `-race` flag in all test runs: `go test -race ./...`
- Test file naming: `foo_test.go` in the same package for white-box tests; use `package foo_test` (external test package) for black-box tests of public APIs
- Use `httptest` for HTTP handlers, `gomock` or `testify/mock` for interface mocks
- Prefer real implementations over mocks where fast enough (in-memory DB, temp files)
- Use `goleak` to detect goroutine leaks: `defer goleak.VerifyNone(t)` in tests that start goroutines

### Concurrency
- Document goroutine ownership: who starts it, who cancels it, who waits for it
- Always use `context.Context` for cancellation and deadline propagation; pass as first argument
- Prefer channels for coordination between goroutines; mutexes for protecting shared state
- Use `sync.WaitGroup` + `errgroup` for fan-out patterns
- Never launch a goroutine without a way to know when it finishes
- Handle context cancellation in select: `select { case <-ctx.Done(): return ctx.Err() case result := <-ch: ... }`
- Panics: only recover at top-level boundaries (HTTP middleware, goroutine entry points) to prevent process crash; always log the panic and stack trace; never use recover for normal flow control

### Error Handling
- Wrap with `fmt.Errorf("operation: %w", err)`; use `errors.Is` / `errors.As` to inspect
- Don't wrap errors that will be wrapped again immediately by the caller
- Return early on error — avoid deep nesting

### Other
- Use `go generate` for code generation; commit generated files and document the generator command
- Avoid `init()` functions; prefer explicit initialization
- Use `context.WithTimeout` / `context.WithDeadline` on all external calls
- Use `go work` (workspaces) for multi-module repos to enable local cross-module development without `replace` directives
- Use `embed.FS` for embedding static files (templates, migrations, fixtures) into the binary — no runtime file path dependencies

---

## TypeScript / Node.js

### Project Layout
- `src/` — all source code
- `src/commands/` or `src/routes/` — entrypoints / request handlers
- `src/lib/` or `src/services/` — business logic
- `src/types.ts` or `src/types/` — shared type definitions
- `dist/` — compiled output (never commit)
- `tests/` or co-located `*.test.ts` files

### Code Style
- Strict TypeScript: `"strict": true` in `tsconfig.json`; no `any` without explicit justification. For new projects also enable `"noUncheckedIndexedAccess": true` and `"exactOptionalPropertyTypes": true` — they catch real bugs and are increasingly standard
- ESLint with `typescript-eslint` for type-aware linting; Prettier for formatting
- Use `const` by default; `let` only when reassignment is needed; never `var`
- Prefer `async/await` over raw Promises; avoid callback-style APIs in new code
- Use named exports over default exports for better refactoring and IDE support
- Avoid barrel files (`index.ts` re-exporting everything) in large codebases — they cause circular deps and slow down TS compilation

### Types
- Define explicit types/interfaces for all function parameters and return values
- Use `type` for unions/intersections and aliases; `interface` for object shapes that may be extended
- Use `readonly` for properties that shouldn't be mutated; use `as const` for literal types
- Avoid type assertions (`as Foo`) — fix the type instead; if unavoidable, add a comment explaining why

### Testing
- Use `vitest` (preferred for modern projects) or `jest`
- Co-locate test files as `foo.test.ts` next to `foo.ts`
- Use `vi.mock` / `jest.mock` sparingly — prefer dependency injection so tests don't need module mocking
- Test the compiled output behaviour, not TypeScript types

### Runtime
- Always handle Promise rejections — unhandled rejections crash Node.js in production
- Use `process.env` for config; validate and fail fast at startup with a schema (e.g. `zod`, `envalid`)
- Set `NODE_ENV=production` in production — many libraries have dev-only overhead when it's unset
- Use ESM (`"type": "module"` in `package.json`) for new projects; be consistent, don't mix CJS and ESM

---

## Python

### Project Layout
- `src/<package>/` — all source code under a proper package; avoid flat-module projects
- `tests/` — test files mirroring the src layout; prefix test files with `test_`
- `pyproject.toml` — single source of truth for dependencies, build config, tool config; no `setup.py` or `setup.cfg`
- `testdata/` or `tests/fixtures/` — test fixtures and sample files

### Code Style
- Use `ruff` for linting and formatting (`ruff check` + `ruff format`) — replaces flake8, isort, pyupgrade, pydocstyle
- Use `mypy` with strict settings (`--strict`) for type checking; run in CI
- Type annotations required on all public functions and class attributes; internal helpers should have them too for mypy coverage

### Types
- Prefer `X | None` over `Optional[X]` (Python 3.10+); use `from __future__ import annotations` for forward references
- Use `TypedDict` for structured dicts passed across API boundaries; prefer dataclasses or Pydantic models for richer behaviour
- Use `Protocol` for duck-typed interfaces instead of ABCs when inheritance is not needed

### Testing
- Use `pytest`; no `unittest` in new code
- Use fixtures for shared setup/teardown; scope them appropriately (`function`, `module`, `session`)
- Use `@pytest.mark.parametrize` for table-driven tests
- Use `pytest-cov` for coverage; enforce threshold in CI
- Mock with `unittest.mock.patch` or `pytest-mock`; prefer dependency injection so mocking is rare

### Packaging
- Pin all dependencies with exact versions in `pyproject.toml` (application) or version ranges (library)
- Use `uv` or `pip-tools` for lock file management; commit the lock file
- Separate `[project.optional-dependencies]` for dev/test deps; don't mix them with runtime deps

---

## Shell / Bash

- Always start scripts with `#!/usr/bin/env bash` (not `/bin/bash`) for portability
- Always include `set -euo pipefail` immediately after the shebang:
  - `-e` — exit on error
  - `-u` — treat unset variables as errors
  - `-o pipefail` — propagate pipe failures, not just the last command's exit code
- Lint all shell scripts with `shellcheck`; add it to the pre-commit hook and CI
- Quote all variable expansions: `"$var"`, `"${var}"`, `"$@"` — unquoted variables are a common source of bugs
- Use `[[ ]]` over `[ ]` for conditionals — more predictable, supports `&&`/`||`, no word splitting
- Use `$(command)` over backticks — nestable and readable
- Prefer named variables over positional `$1`/`$2` — assign at the top of the function: `local input="$1"`
- Declare local variables with `local` inside functions to avoid polluting global scope
- Use `readonly` for constants: `readonly CONFIG_FILE="/etc/app/config.yaml"`
- Print usage to stderr and exit 1 on bad input: `echo "Usage: $0 <arg>" >&2; exit 1`
- Log to stderr, output data to stdout — keeps the script composable in pipelines
- Use `mktemp` for temporary files; clean up with a `trap`: `trap 'rm -f "$tmpfile"' EXIT`
- Avoid parsing `ls` output — use globs or `find` instead
- Keep scripts short; extract logic into functions with descriptive names
- For complex workflows (multi-step, many flags, cross-platform), prefer a proper language (Go, Python) over shell

---

## Naming Conventions

- Be consistent within each project: files, packages, variables, resources
- Follow language idioms: Go exported identifiers use `CamelCase`; Go unexported use `camelCase`; JS/TS variables/functions use `camelCase`, types/classes/components use `PascalCase`; Terraform and Python use `snake_case`; filenames in JS/TS projects use `kebab-case`
- Descriptive names over abbreviations — code is read more than written (`userRepository` not `ur`)
- Boolean variables and functions: use `is`/`has`/`can` prefix (`isValid`, `hasPermission`)
- Functions that return errors: name for what they do, not that they might fail (`LoadConfig`, not `TryLoadConfig`)
- Avoid stuttering: `user.UserID` → `user.ID`; package name is part of the identifier
- Constants: `ALL_CAPS` only in languages where it's idiomatic (Python, shell); `CamelCase` in Go; `SCREAMING_SNAKE_CASE` in TS for module-level constants
- **Test files**: `foo_test.go` (Go), `foo.test.ts` (TS), `test_foo.py` (Python)
- **Migration files**: prefix with a timestamp — `20260408120000_add_users_table.sql`; this guarantees execution order and avoids conflicts
- **Environment variables**: prefix with the app name to avoid collisions — `MYAPP_DATABASE_URL`, `MYAPP_PORT`

---

## Docker

- Use multi-stage builds to keep final images small — build in a full SDK image, copy artifacts to a minimal runtime image
- Minimal base images: `distroless` for Go binaries, `node:XX-alpine` for Node, `python:XX-slim` for Python
- Never run containers as root — add a non-root user in the Dockerfile
- Pin base image versions using a digest or specific tag, not `latest`
- Add `HEALTHCHECK` instructions for long-running services
- Maintain a `.dockerignore` — exclude `vendor/`, `node_modules/`, `.git/`, secrets, local config
- Set resource limits (`--memory`, `--cpus`) in production deployments
- Use `COPY --chown` to set ownership in a single layer rather than a separate `RUN chown`
- Order layers from least to most frequently changed to maximise cache reuse
- Use `COPY` instead of `ADD` unless you specifically need `ADD`'s tar extraction or URL fetching
- Use `ARG` for non-sensitive build-time variables (image tags, build numbers); use `ENV` for runtime config — never bake secrets into either
- For sensitive build-time values (tokens, SSH keys): use `RUN --mount=type=secret` with `docker buildx build --secret id=mysecret,src=...` — the secret is never stored in any layer
- Use `--platform` flag for multi-arch builds: `docker buildx build --platform linux/amd64,linux/arm64`
- Scan images for vulnerabilities before deploying (`trivy image`, `docker scout`)

---

## Terraform

### Structure
- Consistent module layout: `main.tf`, `variables.tf`, `outputs.tf`, `versions.tf`
- `versions.tf` pins provider and Terraform versions — never use unconstrained version ranges
- Remote state backends always (never local state for shared projects); use state locking
- One root config per environment (`envs/prod/`, `envs/staging/`) rather than using `terraform.workspace` for branching logic inside modules

### Workflow
- **Always run `terraform plan` and review the output with the user before applying** — never run `terraform apply` without a reviewed plan
- Run `tflint` and `terraform validate` before committing
- For `terraform apply`, `terraform destroy`, and other long-running commands, run in the background with output to a file for full review
- For destructive operations (`destroy`, forced resource replacement), always confirm with the user first and consider the rollback plan
- Use `moved` blocks when renaming or moving resources — never manipulate state manually with `terraform state mv` unless unavoidable, and document it when you do

### Style
- Use meaningful resource names with project/environment prefixes (`prod-api-sg`, not `sg1`)
- Tag all resources: `project`, `environment`, `owner` at minimum; add `cost-center` for billing attribution
- Use `locals {}` to avoid repetition; avoid deeply nested `for_each` logic — extract to a module
- Keep modules small and focused; avoid "god modules" that do too much
- Use `terraform-docs` to generate module documentation from variable/output descriptions
- Add `precondition` / `postcondition` blocks to validate assumptions and invariants (Terraform 1.2+)
- Add `check` blocks for ongoing infrastructure health assertions — not just apply-time validation (Terraform 1.5+)

---

## Database & Migrations

- All schema changes via versioned migration files — never manual DDL in production (`flyway`, `golang-migrate`, `alembic`, `prisma migrate`)
- **Migration file naming**: prefix with a timestamp — `20260408120000_add_users_table.sql` — guarantees order and prevents conflicts
- Migrations must be backward-compatible across a rolling deploy: add before remove
  - Phase 1 deploy: add new column (nullable or with default), deploy new code that writes to both old and new
  - Phase 2 deploy: migrate existing data, make column non-nullable if needed
  - Phase 3 deploy: remove old column after all code no longer reads it
- **Never drop a column or table in the same deploy as the code change that stops using it** — always a separate follow-up deploy
- Every migration must have a corresponding rollback/down migration; test the down migration
- Test migrations against a copy of production data before applying to production
- **Index strategy**: add indexes for all foreign keys, frequently filtered columns, and sort columns; use `EXPLAIN ANALYZE` for slow queries; consider partial indexes for sparse conditions (`WHERE deleted_at IS NULL`)
- **Bulk migrations**: run `ANALYZE` after large data migrations so the PostgreSQL query planner has fresh statistics
- Connection pooling: use `pgbouncer` or application-level pooling; never open unbounded connections
- Use transactions for multi-step mutations; keep transactions short to avoid lock contention
- Avoid `SELECT *` — select only needed columns
- Use `RETURNING` clauses instead of a separate `SELECT` after `INSERT`/`UPDATE`
- Soft deletes (`deleted_at`) when audit trail matters; hard deletes when storage and compliance allow
- **Read replicas**: route read-heavy queries (reports, analytics, search) to replicas; never use a replica for writes or reads that must see the latest write; document which queries go where
