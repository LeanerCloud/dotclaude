# Coding Standards

Read this file when writing or reviewing code, and on first visit to any project (bootstrap checklist below). Project-specific overrides live in the project's `CLAUDE.md`.

## First Visit: Project Bootstrap Checklist

When working in a project for the first time, check for and create any missing foundational files before starting the actual task. Commit each as a separate atomic commit.

### Linting & Formatting
- **Go**: `golangci-lint` config (`.golangci.yml`) with all checks enabled; disable specific checks only with inline justification. Run via pre-commit hook on every commit.
- **TypeScript/Node**: ESLint (`eslint.config.js` or `.eslintrc`) + Prettier (`.prettierrc`); add `eslint-plugin-security` for server-side projects; use `typescript-eslint` for type-aware rules
- **Python**: `ruff` (replaces flake8+isort+pyupgrade) + `mypy` for type checking
- **Terraform**: `tflint` + `terraform validate` in CI

### Pre-commit Hooks
Set up [`pre-commit`](https://pre-commit.com/) (or Husky for Node projects) if not already present. Baseline hook set:
- Trailing whitespace / end-of-file newline fixers
- No committed secrets: use `detect-secrets` or `gitleaks` in the pre-commit hook to scan staged content; use `gitleaks` separately to audit full git history
- Language formatter (gofumpt, prettier, ruff format)
- **Go**: `golangci-lint run` with all checks enabled; `go mod tidy` to keep module files clean
- Language type checker where applicable (`tsc --noEmit`, `mypy`)
- Run affected tests (at minimum, tests touching changed files)

Hooks must pass cleanly on all existing code before being introduced — fix violations first, then add the hook. Never skip hooks with `--no-verify` (see `git-workflow.md`).

### CI Pipeline
Ensure `.github/workflows/` (or equivalent) has at minimum:
- Lint + format check (same `golangci-lint` config as pre-commit)
- Full test suite with coverage threshold enforcement (see coverage target in Preferred Stack below)
- Security scan (`govulncheck`, `npm audit --audit-level=high`, `trivy`)
- Build verification
- Branch protection (see `~/.claude/infra-ops.md` CI/CD section for the full policy)

### Other Baseline Files
- `.editorconfig` — consistent whitespace/encoding across editors
- `.gitignore` — appropriate for the language/framework; never commit build artifacts, secrets, or IDE files
- `.env.example` — commit this with all required variable names and placeholder/example values but **no real secrets**; it serves as living documentation of what config the project needs; keep it in sync with the actual required variables
- `Makefile` or `Taskfile` with `make lint`, `make test`, `make build` targets
- `known-issues.md` — track known bugs and tech debt; see `~/.claude/project-docs.md`

---

## Preferred Stack

- **Language**: Go for new backend/CLI projects; TypeScript/Node for frontend, lightweight CLIs, or when the ecosystem fit is strong; match the existing language for additions to existing projects
- **IaC**: Terraform when infrastructure is needed
- **Testing**: TDD wherever possible. Use the most popular framework/idioms for the language:
  - Go: `go test` + `testify`
  - TypeScript: `vitest` (preferred) or `jest`
  - Python: `pytest`
- **Coverage**: Target 80% for all new code; 100% for critical paths (auth, payments, data mutations). Enforce in CI by failing the build when coverage drops below threshold (`go test -coverprofile` + `go tool cover`, `jest --coverage --coverageThreshold`, `pytest --cov --cov-fail-under=80`)
- **Dependencies**: Prefer stdlib over third-party when reasonable. Pin all versions. Don't add a dependency without justification — evaluate maintenance status, license, and attack surface.
- **Task runner**: Every project needs a `Makefile` or `Taskfile` as the single entry point for build/test/lint/deploy. No undocumented one-off commands.

## Testing Philosophy

- **Test pyramid**: favour many fast unit tests, fewer integration tests, and a small number of end-to-end tests
- **Test behaviour, not implementation**: test through public APIs and observable outcomes; avoid coupling tests to internal details
- **Good tests are**: fast, isolated, deterministic, and readable as documentation
- **When to use unit tests**: pure logic, algorithms, data transformations, edge cases
- **When to use integration tests**: database queries, HTTP handlers, external service clients, message queue consumers
- **When to use end-to-end tests**: critical user journeys only — they're slow and brittle
- Prefer real implementations over mocks where fast enough (in-memory DB, temp files, `httptest` servers)
- Use mocks/stubs only at true boundaries (external APIs, email services, payment processors)
- Every bug fix should start with a failing test that captures the bug; the fix makes it pass
- Test names should read as sentences: `TestUserService_CreateUser_ReturnsErrorWhenEmailTaken`
- Don't test the framework or language — test your code

## Error Handling

- Return errors explicitly — never swallow silently
- Wrap errors with context so the caller knows where and why it failed (`fmt.Errorf("loading config: %w", err)` in Go)
- Distinguish: recoverable errors (return), programming errors (panic in Go), expected domain errors (typed/sentinel errors)
- Use typed/sentinel errors for errors callers need to branch on; plain strings for errors that just propagate up
- Log at appropriate levels:
  - `debug`: internal state useful during development
  - `info`: normal operational events
  - `warn`: recoverable unexpected conditions
  - `error`: failures requiring attention
- Use structured logging (key-value / JSON) when the project already follows this pattern
- Include correlation/request IDs for tracing across services
- Never log PII, secrets, or tokens — scrub before logging
- Fail fast at startup for missing required config; don't discover it at runtime

## Security

- Never hardcode credentials, secrets, or API keys — use environment variables or a secret manager
- Never commit `.env` files, credentials, or tokens; add to `.gitignore` proactively
- Validate and sanitize all input at system boundaries — never trust external data
- Parameterize all database queries — never interpolate user input into SQL
- Sanitize output rendered in HTML to prevent XSS
- Use allowlists over denylists for input validation where possible
- Set security headers on web services: `Content-Security-Policy`, `X-Frame-Options`, `X-Content-Type-Options`, `Strict-Transport-Security`
- **CORS**: configure explicitly — never use wildcard `*` in production; allowlist specific origins
- **JWT validation**: always verify signature, expiry (`exp`), not-before (`nbf`), audience (`aud`), and issuer (`iss`); reject tokens missing any required claim
- **File uploads**: enforce size limits, validate MIME type from content (not filename extension), scan for malware on sensitive platforms, store outside the web root
- **Supply chain**: use lock files (`go.sum`, `package-lock.json`, `poetry.lock`); verify checksums; prefer well-maintained packages with few transitive deps
- Scan dependencies for known CVEs (`govulncheck`, `npm audit`, Dependabot, Snyk, Trivy)
- Apply principle of least privilege to service accounts, IAM roles, and database users
- Use short-lived credentials; rotate secrets regularly
- Rate-limit and throttle public-facing endpoints

## Performance

- Don't prematurely optimize — profile first (`go tool pprof` for CPU/memory, `py-spy`, Chrome DevTools), optimize the measured bottleneck
- Use benchmarks (`go test -bench`, `pytest-benchmark`) to validate that an optimization actually helps
- Watch for: n+1 queries, unnecessary allocations in hot paths, missing indexes, unbounded collection returns
- Cache at the right layer (in-memory, CDN, DB query cache) — always document the invalidation strategy
- Paginate all list endpoints; never return unbounded result sets
- Use connection pooling for databases and HTTP clients; tune pool sizes based on workload
- **Go concurrency**: Use `-race` in tests always. Prefer channels for coordination, mutexes for protecting state. Document goroutine lifecycle explicitly (who creates, who cancels, who waits).
- **Cyclomatic complexity**: Keep below 10 on all new code. Break complex functions into named helpers with clear responsibilities.

## API Design

- Follow REST conventions: proper HTTP methods, meaningful status codes, resource-oriented URLs
- Use a consistent error response format across all endpoints: `{"error": {"code": "...", "message": "...", "request_id": "..."}}`
- Version APIs on breaking changes (`/v1/`, `/v2/`) — never silently break callers
- Validate input at the boundary; return `400` with a descriptive message, not a `500`
- Design operations to be idempotent where possible
- Use `PATCH` for partial updates, `PUT` for full replacement
- Paginate list responses with a consistent cursor or offset+limit strategy; include total count where cheap
- Return `429 Too Many Requests` with `Retry-After` header when rate-limiting
- Use `ETag` / `Last-Modified` for cacheable resources
- **Datetimes**: always use ISO 8601 / RFC 3339 format (`2026-04-08T14:30:00Z`); always include timezone; store in UTC
- **Webhooks**: use HMAC signatures for payload verification; document retry behaviour and expected response codes; be idempotent (callers will retry); include an event type and timestamp in every payload
- Document with OpenAPI/Swagger; keep specs in sync with implementation (generate from code where possible)
- Authenticate with short-lived tokens (JWT, OAuth2); never pass credentials in query strings

## Idempotency

- Scripts, IaC, migrations, and API handlers must be safe to run multiple times without side effects
- Use `CREATE IF NOT EXISTS`, upserts (`INSERT ... ON CONFLICT`), and idempotency keys
- For async operations (queues, webhooks), use idempotency keys to deduplicate retries
- Make deletes safe to repeat — return success if already deleted

## Documentation

- Update READMEs when adding features, changing setup steps, or altering project structure
- Inline comments only for non-obvious logic — document the *why*, not the *what*
- Maintain a CHANGELOG if one already exists; add one for projects with releases or external users (skip for small internal tools — scale to context)
- Keep API docs (OpenAPI, docstrings) in sync with implementation — stale docs are harmful
- Use ADRs for significant decisions — see `~/.claude/project-docs.md` for the ADR template and format
