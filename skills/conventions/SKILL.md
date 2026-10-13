---
name: conventions
description: Language- and tool-specific conventions for Go, TypeScript, Python, Shell, Docker,
  Terraform and databases - layout, naming, testing, error handling. Invoke when writing or
  reviewing code in any of those.
---

# Conventions

Language- and technology-specific conventions; read the relevant sections. Project `CLAUDE.md` overrides.

## Go

### Project Layout
- `cmd/` entrypoints (one subdirectory per binary); `internal/` private packages; `pkg/` public libraries (sparingly)
- Go modules: pin versions, always commit `go.sum`, `go mod tidy` before committing
- `testdata/` fixtures and golden files (ignored by normal builds)

### Code Style
- `golangci-lint run` (all checks; includes `govet`) before every commit via pre-commit hook; format with `gofumpt`
- **Pin the formatter to the repo's CI Go version; a version-skewed `gofmt`/`gofumpt` is worse than none.** gofmt output changes between Go minors, so a hook running the developer's *ambient* `gofmt` (default for `dnephin/pre-commit-golang` `go-fmt` and any `language: system` hook) diverges from CI and, with `--all-files`, reformats CI-clean files repo-wide. Make local == CI via `go.mod`:
  - Replace the ambient hook with a local script forcing the `go.mod` `toolchain`. `GOTOOLCHAIN=auto` is NOT enough (it only upgrades, never downgrades a newer dev Go): `TOOLCHAIN=$(awk '/^toolchain /{print $2}' go.mod); GOROOT="$(GOTOOLCHAIN="$TOOLCHAIN" go env GOROOT)"; "$GOROOT/bin/gofmt" -l $(git ls-files '*.go' | grep -v /vendor/)` (fail if non-empty).
  - In CI use `actions/setup-go` with `go-version-file: go.mod`, not a hardcoded `go-version`.
  - Add `default_install_hook_types: [pre-commit, pre-push]` to `.pre-commit-config.yaml`.
  - If your tool version doesn't match CI's, treat the local format/lint result as unreliable; never commit an auto-fixer's whole-repo reformat to "fix CI". Rebase onto the cleaned base and let CI confirm.
- Naming: exported `CamelCase`, unexported `camelCase`, consistent acronyms (`userID`, `HTTPServer`); short consistent receiver names, never `self`/`this`; `ErrFoo` sentinels, `FooError` types
- Interfaces: named by behaviour (`Reader`, `Storer`), small (1-3 methods), defined at the point of use

### Logging
- `log/slog` (no `logrus`/`zap` in new projects) with key-value attributes: `slog.Info("request complete", "duration_ms", ms, "status", code)`
- Package-level logger or via context; avoid global `slog.Default()` in library code

### Testing
- Table-driven by default; `testify/assert` and `testify/require` (`require` for preconditions)
- `t.Parallel()` in unit tests unless shared mutable state; always `go test -race ./...`
- `foo_test.go` same package for white-box; `package foo_test` for black-box tests of public APIs
- `httptest` for handlers, `gomock` or `testify/mock` for interfaces; real implementations over mocks where fast enough
- `goleak`: `defer goleak.VerifyNone(t)` in tests that start goroutines

### Concurrency
- Document goroutine ownership (who starts, cancels, waits); never launch a goroutine without a way to know when it finishes
- `context.Context` first argument for cancellation/deadlines; handle it in select: `select { case <-ctx.Done(): return ctx.Err() case result := <-ch: ... }`
- Channels for coordination, mutexes for shared state; `sync.WaitGroup` + `errgroup` for fan-out
- Recover panics only at top-level boundaries (HTTP middleware, goroutine entry points), always log panic and stack; never as flow control

### Error Handling
- Wrap with `fmt.Errorf("operation: %w", err)`; inspect with `errors.Is` / `errors.As`; don't wrap errors the caller wraps immediately
- Return early on error; avoid deep nesting

### Other
- `go generate`: commit generated files and document the generator command
- Avoid `init()`; `context.WithTimeout` / `WithDeadline` on all external calls
- `go work` for multi-module repos instead of `replace` directives
- `embed.FS` for static files (templates, migrations, fixtures)

---

## TypeScript / Node.js

### Project Layout
- `src/` source; `src/commands/` or `src/routes/` entrypoints; `src/lib/` or `src/services/` business logic; `src/types.ts` shared types
- `dist/` compiled output (never commit); `tests/` or co-located `*.test.ts`

### Code Style
- `"strict": true`; no `any` without justification; new projects also `"noUncheckedIndexedAccess": true` and `"exactOptionalPropertyTypes": true`
- ESLint with `typescript-eslint` (type-aware) + Prettier
- `const` by default, `let` when reassigning, never `var`; `async/await` over raw Promises
- Named exports over default; avoid barrel files in large codebases (circular deps, slow compilation)

### Types
- Explicit types for all function parameters and return values
- `type` for unions/intersections/aliases; `interface` for extensible object shapes
- `readonly` and `as const` where applicable
- Avoid assertions (`as Foo`); fix the type, or comment why unavoidable

### Testing
- `vitest` (preferred) or `jest`; co-locate `foo.test.ts` next to `foo.ts`
- `vi.mock` / `jest.mock` sparingly; prefer dependency injection
- Test compiled output behaviour, not TypeScript types

### Runtime
- Always handle Promise rejections (unhandled rejections crash Node in production)
- Config via `process.env`, validated at startup with a schema (`zod`, `envalid`), fail fast
- `NODE_ENV=production` in production
- ESM (`"type": "module"`) for new projects; don't mix CJS and ESM

---

## Python

### Project Layout
- `src/<package>/` (no flat-module projects); `tests/` mirroring src, files prefixed `test_`
- `pyproject.toml` is the single source for dependencies, build and tool config (no `setup.py`/`setup.cfg`)
- `testdata/` or `tests/fixtures/`

### Code Style
- `ruff check` + `ruff format` (replaces flake8, isort, pyupgrade, pydocstyle); `mypy --strict` in CI
- Type annotations on all public functions and class attributes; internal helpers too for mypy coverage

### Types
- `X | None` over `Optional[X]` (3.10+); `from __future__ import annotations` for forward references
- `TypedDict` for dicts across API boundaries; dataclasses or Pydantic for richer behaviour
- `Protocol` over ABCs when inheritance isn't needed

### Testing
- `pytest` only (no `unittest` in new code); scoped fixtures (`function`, `module`, `session`); `@pytest.mark.parametrize` for tables
- `pytest-cov` with a CI threshold; mock with `unittest.mock.patch` or `pytest-mock`, preferring dependency injection

### Packaging
- Exact pins in `pyproject.toml` for applications, ranges for libraries; `uv` or `pip-tools` lock file, committed
- Dev/test deps in `[project.optional-dependencies]`, separate from runtime

---

## Shell / Bash

- Start with `#!/usr/bin/env bash`, then `set -euo pipefail` (`-e` exit on error, `-u` unset variables are errors, `-o pipefail` propagate pipe failures)
- `shellcheck` in pre-commit and CI
- Quote all expansions: `"$var"`, `"${var}"`, `"$@"`
- `[[ ]]` over `[ ]`; `$(command)` over backticks
- Named variables over positional: `local input="$1"`; `local` in functions; `readonly` for constants
- Usage to stderr and exit 1 on bad input: `echo "Usage: $0 <arg>" >&2; exit 1`; log to stderr, data to stdout
- `mktemp` with `trap 'rm -f "$tmpfile"' EXIT`
- Don't parse `ls`; use globs or `find`
- Keep scripts short, logic in named functions; for complex workflows (multi-step, many flags, cross-platform) use Go or Python

---

## Naming Conventions

- Consistent within each project; follow language idioms: Go exported `CamelCase`, unexported `camelCase`; JS/TS variables/functions `camelCase`, types/classes/components `PascalCase`; Terraform and Python `snake_case`; JS/TS filenames `kebab-case`
- Descriptive over abbreviated (`userRepository`, not `ur`)
- Booleans: `is`/`has`/`can` prefix
- Name functions for what they do, not that they may fail (`LoadConfig`, not `TryLoadConfig`)
- Avoid stuttering (`user.UserID` -> `user.ID`)
- Constants: `ALL_CAPS` where idiomatic (Python, shell); `CamelCase` in Go; `SCREAMING_SNAKE_CASE` for TS module-level
- **Test files**: `foo_test.go`, `foo.test.ts`, `test_foo.py`
- **Migration files**: timestamp prefix, `20260408120000_add_users_table.sql` (guarantees order, avoids conflicts)
- **Environment variables**: app-name prefix (`MYAPP_DATABASE_URL`, `MYAPP_PORT`)

---

## Docker

- Multi-stage builds: full SDK image to build, minimal runtime image to run
- Minimal bases: `distroless` for Go, `node:XX-alpine`, `python:XX-slim`; pin versions by digest or specific tag, never `latest`
- Never run as root; add a non-root user
- `HEALTHCHECK` for long-running services; `.dockerignore` (`vendor/`, `node_modules/`, `.git/`, secrets, local config)
- Resource limits (`--memory`, `--cpus`) in production
- `COPY --chown` instead of a separate `RUN chown`; `COPY` over `ADD` unless you need tar extraction or URL fetch
- Order layers least to most frequently changed
- `ARG` for non-sensitive build-time values, `ENV` for runtime config; never bake secrets into either
- Sensitive build-time values (tokens, SSH keys): `RUN --mount=type=secret` with `docker buildx build --secret id=mysecret,src=...` (never stored in a layer)
- Multi-arch: `docker buildx build --platform linux/amd64,linux/arm64`
- Scan images before deploying (`trivy image`, `docker scout`)

---

## Terraform

### Structure
- Module layout `main.tf`, `variables.tf`, `outputs.tf`, `versions.tf`; `versions.tf` pins provider and Terraform versions (no unconstrained ranges)
- Remote state with locking always (never local state for shared projects)
- One root config per environment (`envs/prod/`, `envs/staging/`), not `terraform.workspace` branching inside modules

### Workflow
- **Always run `terraform plan` and review it with the user before applying**; never apply without a reviewed plan
- `tflint` and `terraform validate` before committing
- Run `apply`, `destroy` and other long commands in the background with output to a file
- Destructive operations (`destroy`, forced replacement): confirm with the user first and consider rollback
- `moved` blocks for renames/moves; avoid `terraform state mv` unless unavoidable, and document it

### Style
- Meaningful names with project/environment prefixes (`prod-api-sg`, not `sg1`)
- Tag all resources: `project`, `environment`, `owner` at minimum; `cost-center` for billing
- `locals {}` against repetition; extract deeply nested `for_each` into a module; small focused modules
- `terraform-docs` for module docs
- `precondition` / `postcondition` blocks (1.2+) and `check` blocks for ongoing health assertions (1.5+)

---

## Database & Migrations

- All schema changes via versioned migration files (`flyway`, `golang-migrate`, `alembic`, `prisma migrate`); never manual DDL in production; timestamp-prefixed names
- Backward-compatible across a rolling deploy, add before remove:
  - Deploy 1: add the column (nullable or defaulted); new code writes both old and new
  - Deploy 2: migrate existing data; make non-nullable if needed
  - Deploy 3: remove the old column once no code reads it
- **Never drop a column or table in the same deploy as the code change that stops using it.**
- Every migration has a tested rollback/down migration; test migrations on a copy of production data first
- **Indexes**: all foreign keys, frequently filtered and sort columns; `EXPLAIN ANALYZE` for slow queries; partial indexes for sparse conditions (`WHERE deleted_at IS NULL`)
- **Bulk migrations**: `ANALYZE` afterwards so the PostgreSQL planner has fresh statistics
- Connection pooling (`pgbouncer` or application-level); never unbounded connections
- Short transactions for multi-step mutations; avoid `SELECT *`; use `RETURNING` instead of a follow-up `SELECT`
- Soft deletes (`deleted_at`) when audit trail matters; hard deletes when storage and compliance allow
- **Read replicas**: route read-heavy queries (reports, analytics, search) there; never writes or reads that must see the latest write; document which queries go where
