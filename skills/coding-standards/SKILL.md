---
name: coding-standards
description: >-
  Stack preferences, testing, error handling, security checklist, API design, YAGNI and comment
  rules, first-visit bootstrap. Use when writing or reviewing code, on first visit to a project,
  before launching a user-facing app.
---

# Coding Standards

Invoke when writing or reviewing code, and on first visit to any project. Project `CLAUDE.md` overrides.

## First Visit: Project Bootstrap Checklist

Check for and create missing foundational files before the actual task, one atomic commit each.

### Linting & Formatting
- **Go**: `golangci-lint` (`.golangci.yml`), all checks enabled; disable only with inline justification.
- **TypeScript/Node**: ESLint + Prettier; `eslint-plugin-security` for server-side; `typescript-eslint` for type-aware rules.
- **Python**: `ruff` + `mypy`.
- **Terraform**: `tflint` + `terraform validate` in CI.

### Pre-commit Hooks
Set up [`pre-commit`](https://pre-commit.com/) (Husky for Node) if absent. Baseline:
- Trailing-whitespace / EOF-newline fixers
- Secret scan of staged content (`detect-secrets` or `gitleaks`); `gitleaks` separately for full history
- Language formatter (gofumpt, prettier, ruff format)
- Go: `golangci-lint run`, `go mod tidy`
- Type checker (`tsc --noEmit`, `mypy`)
- Tests touching changed files

Hooks must pass on all existing code before being introduced (fix violations first). Never `--no-verify` (see `git-commit`).

### CI Pipeline
At minimum in `.github/workflows/` (or equivalent): lint + format (same config as pre-commit); full tests with coverage threshold; security scan (`govulncheck`, `npm audit --audit-level=high`, `trivy`); build verification; branch protection (`infra-ops` CI/CD section).

### Other Baseline Files
- `.editorconfig`; `.gitignore` (no build artifacts, secrets, IDE files)
- `.env.example`: every required variable with placeholders, **no real secrets**, kept in sync
- `Makefile` or `Taskfile` with `lint`, `test`, `build` targets
- `known-issues.md` (see `project-docs`)

### Code graph (Compass)

Build a Compass graph before non-trivial work when the project has >~5 source files or the architecture is unclear. It is a local Rust binary (no model credentials); location in `~/.claude/local-paths.md`.

- Repo you own: `compass init . --yes` once (writes `.compass/config.toml`, `compass-out/`; gitignore `compass-out/`), then `compass update .` or `compass watch`.
- Repo you don't own or another session uses: build out of tree, e.g. `compass extract <path> --code-only --out <dir>`, and query from `<dir>`.
- **Query instead of grep-and-read**: `compass explain <symbol>`, `compass path <from> <to>`, `compass affected <symbol> --depth 3`, `compass query "<terms>"`; overview via `compass export html` (or `wiki` / `obsidian`).
- **Limits, then fall back to `rg` plus reading source**: `query` matches identifiers and words, not meaning; C macros and `#include "file.c"` code are not indexed; dynamic dispatch, reflection and generated code resolve partially.

---

## Simplicity & Scope (YAGNI)

The simplest thing that works wins: fewer lines, fewer concepts, fewer names.

**The test is "does a current caller need this, and can this state be reached given the callers that exist?", not "is this correct?".** Correct, well-tested code guarding an unreachable state is still waste. Find out what the calling system actually does before building for the general case.

- **YAGNI.** No parameters, flags, hooks or abstraction layers for a future that hasn't arrived. One consumer: write it for that consumer.
- **DRY, but rule-of-three.** Two similar pieces are fine; abstract at the third. Premature abstraction is harder to undo than duplication. (Complements `AGENTS.md` §1a.)
- **Don't validate the impossible.** Validate at trust boundaries (external input, config, API payloads); trust your own callers inside the module. Don't double up (type constraint plus runtime assert).
- **Prefer removing the hazard to defending against it.** If a value needs validating because it reaches a shell, check whether it needs to reach a shell at all.
- **Elegance means fewer moving parts** (`AGENTS.md` §5).

**Self-check before opening a PR:**
- Does every parameter have a caller that sets it?
- Does every abstraction have more than one implementation or consumer?
- Does every validation guard a state that can occur?
- Could a competent colleague have written this in half the lines? If yes, cut.

Cut speculation, not behaviour: a parameter a real caller sets, a timeout the platform default gets wrong, auth config a private dependency needs are all required however many lines they add.

## Verifying a Refactor

A cleanup that changes behaviour is a bug in a tidy diff. Cutting and proving are one task.

- **Subtractive only.** Delete and inline; never add an abstraction to "simplify".
- **Compare generated output, not just tests.** Where code emits an artifact (IaC plans/synth, migrations, codegen, bundles, snapshots), byte-compare every emitted file before and after and require identical. A renamed IaC logical id silently destroys live infrastructure and no unit test sees it.
- **Carry a protected list.** Before starting, name the decisions that must survive (one-line reason each); hard-won constraints look like cruft.
- **Never delete a security assertion on your own judgement.** Tests pinning authorization, resource scoping or privilege boundaries go only with the owner's explicit sign-off.
- **"Nothing here is worth removing" is a complete answer.**
- **Report what you left**: candidates considered and rejected, and why.

If you can't prove an edit is behaviour-preserving, don't make it.

### Deletion probes - test necessity, don't argue it

For each candidate machinery: **delete it, run the full verification, read the result.**

- **Something fails** -> it earns its place. Restore it and record *what* failed (PR description or protected list).
- **Nothing fails** -> say which: (1) genuinely unnecessary, delete it; or (2) **the verification is too weak to notice**, so restore the code and report a coverage gap.

**"Nothing broke" is not automatic permission to delete.** A guard on a real invariant that breaks no test is "untested", not "unnecessary"; those findings are worth more than the deletions.

Restore between probes and probe one thing at a time (two deletions that cancel out read as a clean run).

## Preferred Stack

- **Language**: Go for new backend/CLI; TypeScript/Node for frontend, lightweight CLIs or strong ecosystem fit; match the existing language in existing projects.
- **IaC**: Terraform.
- **Testing**: TDD where possible. Go: `go test` + `testify`; TypeScript: `vitest` (or `jest`); Python: `pytest`.
- **Coverage**: 80% for new code; 100% for critical paths (auth, payments, data mutations). Enforce in CI (`go test -coverprofile` + `go tool cover`, `jest --coverage --coverageThreshold`, `pytest --cov --cov-fail-under=80`).
- **Dependencies**: prefer stdlib; pin versions; justify each new one (maintenance, license, attack surface).
- **Task runner**: a `Makefile` or `Taskfile` is the single entry point for build/test/lint/deploy.

## Testing Philosophy

- Pyramid: many fast unit tests, fewer integration, few end-to-end (critical user journeys only).
- Test behaviour through public APIs, not implementation. Tests are fast, isolated, deterministic, readable.
- Use discriminating inputs: a test whose input a wrong implementation also passes (e.g. a value valid in either operand slot) is not a guard. Use spellings the real toolchain accepts, measured before writing the test, and confirm with a mutation probe that the test fails.
- Unit: pure logic, algorithms, transformations, edge cases. Integration: DB queries, HTTP handlers, external clients, queue consumers.
- Prefer real implementations (in-memory DB, temp files, `httptest`); mock only at true boundaries (external APIs, email, payments).
- Every bug fix starts with a failing test that captures the bug.
- **Assert the defect, not a proxy**: if the bug is "two identical purchases derive different idempotency tokens", assert on the tokens, not a field that feeds them.
- Test names read as sentences: `TestUserService_CreateUser_ReturnsErrorWhenEmailTaken`.
- Don't test the framework or language.

## Error Handling

- Return errors explicitly; never swallow. Wrap with context (`fmt.Errorf("loading config: %w", err)`).
- Recoverable errors return; programming errors panic (Go); expected domain errors are typed/sentinel (when callers branch on them; plain otherwise).
- Levels: `debug` internal state, `info` normal events, `warn` recoverable unexpected, `error` failures needing attention.
- Structured logging when the project already uses it; correlation/request IDs across services.
- Never log PII, secrets or tokens.
- Fail fast at startup on missing required config.

## Fallbacks, Magic Values & Enums

- **No silent fallbacks - fail loud.** If something required is missing, wrong or unavailable (config value, upstream response, looked-up record, price/amount), return an explicit error rather than a fabricated, default or degraded value. Most critical on irreversible or money-affecting paths. Keep a fallback only with deliberate sign-off, and log it loudly.
- **No hardcoded magic values or fixed ratios.** Derive from source data, config or named constants; don't bake in financial assumptions (discount %, terms), unit ratios (memory-per-vCPU) or environment specifics (region/account/endpoint). On unrecognized or absent input, error.
- **Typed enums/constants over bare strings** for status, type, payment option, term, provider. Parse external input into the typed value at the boundary and error on unknown. For outbound SDK/API fields use the SDK's enum constants verbatim.
- Fix such instances proactively when you read or modify code, each with a regression test on the error path.

## Security

- Never hardcode credentials/API keys; use env vars or a secret manager. Never commit `.env`, credentials or tokens; gitignore proactively.
- Validate and sanitize all input at system boundaries; allowlists over denylists.
- Parameterize all DB queries; sanitize HTML output (XSS).
- Security headers on web services: `Content-Security-Policy`, `X-Frame-Options`, `X-Content-Type-Options`, `Strict-Transport-Security`.
- **CORS**: explicit; no `*` in production; allowlist origins.
- **JWT**: always verify signature, `exp`, `nbf`, `aud`, `iss`; reject tokens missing a required claim.
- **File uploads**: size limits, MIME from content (not extension), malware scan on sensitive platforms, store outside web root.
- **Supply chain**: lock files (`go.sum`, `package-lock.json`, `poetry.lock`); verify checksums; prefer well-maintained packages with few transitive deps. Scan for CVEs (`govulncheck`, `npm audit`, Dependabot, Snyk, Trivy).
- Least privilege for service accounts, IAM roles, DB users; short-lived credentials; rotate secrets.
- Rate-limit and throttle public endpoints.

### Pre-Launch Security Checklist

Gate before shipping any user-facing app. Tick every box or record why it doesn't apply. Numbers are for reference only.

**Secrets & keys**
- [ ] (1) Hide API keys: no secrets in client code, repos or bundles; load from env/secret manager.
- [ ] (2) Detect and rotate leaked secrets: scan full history (`gitleaks`/`trufflehog`) and revoke or rotate anything ever committed (see `infra-ops`). **Rotation is the fix, not history rewriting**: a committed secret is compromised whether or not the commit still exists. Rewriting history or removing committed data is destructive and needs explicit per-item confirmation (tenet 9).
- [ ] (3) Client uses the public/anon key only (Supabase anon, Firebase web config); service-role/admin keys never ship to browser or mobile.

**Auth, sessions & abuse**
- [ ] (6) Enforce auth server-side: every mutation and protected read re-verifies authn + authz on the server.
- [ ] (10) Hash passwords with argon2id (or bcrypt/scrypt) + per-user salt; never plaintext or fast hashes (MD5/SHA-*). Prefer an IdP/OAuth over rolling your own.
- [ ] (9) Secure session cookies: `HttpOnly`, `Secure`, `SameSite=Lax`/`Strict` by default, scoped domain/path, sane expiry; rotate the session id on login and privilege change. `SameSite=None` only where a cross-site flow needs it, with `Secure` plus explicit CSRF or `Origin` checking (cookie attributes narrow CSRF exposure, they don't replace CSRF protection).
- [ ] (11) Throttle auth endpoints (login, signup, reset, OTP) against brute force and credential stuffing: rate limits, progressive delays, challenges. Prefer these to lockouts (account lockout lets an attacker DoS any known user; endpoint lockout denies everyone). Where a lockout is warranted, make it temporary and risk-based, with an unabusable recovery path, and alert on it.
- [ ] (12) Bot protection on public forms/signup (CAPTCHA/Turnstile or WAF).

**Data access & authorization**
- [ ] (4) Row-level security (RLS) at the database; scope every query to the authenticated principal.
- [ ] (7) Verify object-level ownership on every read/write; deny by default (IDOR / horizontal escalation).
- [ ] (8) Block field tampering: bind writable fields via an explicit allowlist (DTO/schema); never spread untrusted request bodies into an ORM model (`is_admin`, `role`, `price`).
- [ ] (5) Encrypt sensitive data: TLS in transit, encryption at rest, app-layer encryption (KMS) for high-sensitivity fields (tokens, PII, financial).

**Input & output**
- [ ] (14) Validate all input at boundaries; allowlist over denylist.
- [ ] (13) Parameterize all DB queries.
- [ ] (15) Escape/sanitize user content rendered in HTML (XSS).
- [ ] (16) Restrict file uploads: size limits, MIME from content, outside web root, scanned.
- [ ] (17) Trim API responses: serialize through an explicit response DTO; never dump full DB rows or internal fields (password hashes, tokens, other users' data).

**Transport, headers & supply chain**
- [ ] (19) Force HTTPS: redirect HTTP, TLS everywhere, HSTS.
- [ ] (18) Verify the *effective* header values, not just presence: reviewed CSP (no blanket `unsafe-inline`/`*`), framing policy (`X-Frame-Options: DENY`/`SAMEORIGIN` or `frame-ancestors`), `X-Content-Type-Options: nosniff`, HSTS served over HTTPS. Read them off a real response (`X-Frame-Options: ALLOWALL` passes a presence check and protects nothing).
- [ ] (20) Scan dependencies for CVEs (`govulncheck`/`npm audit`/Trivy/Dependabot).

## Performance

- Profile first (`go tool pprof`, `py-spy`, Chrome DevTools); optimize the measured bottleneck; validate with benchmarks (`go test -bench`, `pytest-benchmark`).
- Watch for n+1 queries, hot-path allocations, missing indexes, unbounded collections.
- Cache at the right layer and document the invalidation strategy.
- Paginate all list endpoints; pool DB and HTTP connections, tuned to workload.
- **Go concurrency**: `-race` in tests always; channels for coordination, mutexes for state; document goroutine lifecycle (who creates, cancels, waits).
- **Cyclomatic complexity** below 10 on new code; split into named helpers.

## API Design

- REST conventions: proper methods, meaningful status codes, resource-oriented URLs.
- Consistent error format: `{"error": {"code": "...", "message": "...", "request_id": "..."}}`.
- Version on breaking changes (`/v1/`, `/v2/`).
- Validate at the boundary; `400` with a descriptive message, not `500`.
- Idempotent operations where possible; `PATCH` for partial updates, `PUT` for full replacement.
- Paginate lists with a consistent cursor or offset+limit; total count where cheap.
- `429` with `Retry-After` when rate-limiting; `ETag` / `Last-Modified` for cacheable resources.
- **Datetimes**: ISO 8601 / RFC 3339 with timezone (`2026-04-08T14:30:00Z`); store in UTC.
- **Webhooks**: HMAC signatures; document retry behaviour and expected response codes; idempotent receivers; event type and timestamp in every payload.
- Document with OpenAPI, in sync with implementation (generate from code where possible).
- Short-lived tokens (JWT, OAuth2); never credentials in query strings.

## Idempotency

- Scripts, IaC, migrations and API handlers are safe to run repeatedly.
- `CREATE IF NOT EXISTS`, upserts (`INSERT ... ON CONFLICT`), idempotency keys (also to deduplicate async retries).
- Deletes are safe to repeat: succeed if already deleted.

## Comments

Rationale belongs in the PR description; source carries only what a future editor needs in order not to break something.

- **Default to no comment.** Naming and structure carry meaning.
- **Edit generated comments**: prune to what the code cannot say; rewriting them down often surfaces real design issues.
- **Comment only where the WHY isn't deducible**: a deliberate choice a reader would "fix", a non-visible constraint, a workaround for an external bug (link the issue or CVE), a deviation from the obvious approach.
- **Terse: 1-2 lines.** More belongs in the PR description or commit message.
- **Two hard per-comment rules**, checked one comment at a time: **no comment exceeds 2 lines**, and **no comment describes what the code does** (delete, don't shorten).
- **Budget: under ~15% comment-to-code on added lines**, measured as below. Over budget is a trigger, not a veto: delete until under, or name why the file is a genuine exception.
- **Delete on sight**: restatements of the next line; multi-paragraph rationale; considered-and-rejected narrative; references to review rounds, past bugs or `"as of <date>"`; JSDoc/docstrings restating signature types; long test preambles.

**Measure the same way every time** and state the number in the PR description or pre-commit self-check:

```bash
git diff <base>...HEAD -- . ':(exclude)**/dist/*' ':(exclude)**/*.lock' | awk '
  /^\+/ && !/^\+\+\+/ { t++; l=substr($0,2); sub(/^[ \t]+/,"",l)
    if (l ~ /^(\/\/|\/\*|\*|#)/) c++; if (l=="") b++ }
  END { printf "added=%d comment=%d code=%d ratio=%.1f%%\n", t, c, t-c-b, c*100/(t-b) }'
```

The number lies if you: count whole files instead of `+` lines; include generated/vendored output (exclude it); or measure a stacked PR against `main` instead of its parent branch.

**Legitimate exceptions, named not assumed**: a constants/config file (the values are the content; only the why of each is unrecoverable), and a small file where a few necessary notes price in high. Say which applies.

**Fix stale comments adjacent to your diff** (the block above the function you edited, the note beside the line you moved, comments inside the hunk): if one restates the code, runs past 2 lines or has rotted, fix or delete it, and **delete commented-out code on sight**. Adjacent to the diff, not the whole file; this is a narrow carve-out from "don't touch what you weren't asked to touch". Put the cleanup in its own commit.

```go
// Bad: restates the code and narrates the decision
// We use a 30 second timeout here. This value was chosen after considering
// 10s (too aggressive) and 60s (too slow). Raised from 10s in review round 2.
ctx, cancel := context.WithTimeout(ctx, 30*time.Second)

// Good: one line, only because the number is otherwise unexplainable
// Upstream's p99 is 25s; anything tighter flakes.
ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
```

## Documentation

- Update READMEs when adding features, changing setup or structure.
- Inline comments: see **Comments**.
- Maintain a CHANGELOG if one exists; add one for projects with releases or external users.
- Keep API docs (OpenAPI, docstrings) in sync; stale docs are harmful.
- ADRs for significant decisions (`project-docs`).
