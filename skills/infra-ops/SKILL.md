---
name: infra-ops
description: >-
  Ops rules: rollback, secrets, monitoring, timeouts, CI/CD, Terraform, staging-first deploys,
  multi-repo integration builds. Use for infra, deploy, cloud and ops work, or when assembling and
  testing unmerged work across repos.
---

# Infrastructure & Operations

Invoke for infrastructure, deployments, cloud resources and operational concerns. Project `CLAUDE.md` overrides.

## Rollback Awareness

- Before every deploy or Terraform apply, have an explicit rollback plan with exact revert steps.
- **Feature flags are the fastest rollback** (no deploy); prefer them for risky changes. Also design for reversibility: backward-compatible schema migrations, blue/green or canary (rollback becomes a traffic switch; prefer for stateless services).
- **Database rollbacks are much harder than code rollbacks**: a code rollback doesn't undo migrations. Verify the down migration is tested and safe before running the up migration in production, and that the old code can run against the new schema before switching traffic back.
- Keep the previous image/artifact version available until the new one is proven stable.
- Terraform:
  - Destructive operations (`destroy`, forced replacement) need user confirmation first; present the plan summary clearly.
  - **Never truncate plan output**: capture it fully to a file (e.g. `/tmp/tf-plan.txt`) and `Read` it; never pipe through `head`, `tail` or any length limit (truncation hides deletions and replacement cascades). As Claude, use Bash output directly or redirect to a file; avoid `2>&1 | tee` (approval prompts, see `tool-usage`).
  - **Always plan before apply**: read the full plan and explicitly count destroyed/replaced resources.
  - **Never `-auto-approve` in production** (manual approval gate); only in dev/CI with no production state.
  - **Avoid `-target`**: it leaves state inconsistent with config; break-glass only.
  - **State**: remote backend with locking; no concurrent applies; never edit `.tfstate` (use `terraform state mv/rm/import`); back up before destructive operations: `terraform state pull > tf-state-backup-$(date +%Y%m%d).json`.
  - **Commit `.terraform.lock.hcl`** (like `package-lock.json`).
  - **CI**: `terraform validate` and `terraform fmt -check` (fail on diff); `tfsec` or `checkov` before apply (IAM wildcards, public S3, unencrypted storage).
  - Mark secrets/tokens `sensitive = true` in outputs.
  - **Drift detection**: weekly `terraform plan` against production state in CI; alert on any diff.

## Secrets Management

- Use a secret manager (AWS Secrets Manager, GCP Secret Manager, Vault); never committed env files, unencrypted S3/GCS, or config files. Inject at runtime via env vars or SDK; never bake into images.
- **Rotation**: on a schedule (API keys quarterly, DB passwords monthly) or immediately on suspected compromise; automate where supported.
- **Emergency rotation**: document and test before needed; rotate without outage via blue/green rotation (create new, update consumers, delete old).
- **Scan git history** (`gitleaks`, `truffleHog`, `git-secrets`); a secret ever committed is compromised: rotate it.
- Least privilege per service, separate credentials per environment; audit access logs and alert on unexpected patterns.

## Cost Awareness

- Tag all resources: `project`, `environment`, `owner`; `cost-center` for billing.
- Spot/preemptible for batch, CI runners, dev; right-size (start small, scale with data); auto-scale and scale non-prod down outside business hours.
- Budget alerts and cost anomaly detection (AWS Cost Anomaly Detection, GCP Budget Alerts) early.
- Clean up orphaned resources (unattached EBS volumes, unused Elastic IPs, old snapshots), automated where possible; lifecycle policies to move cold data to cheaper tiers and expire the rest.
- Mind egress between regions/AZs/internet; co-locate services.
- Prefer managed services when the operational burden is high relative to the cost difference.

## Monitoring & Health Checks

### Service Endpoints
- `/health` (liveness): `200` if the process is alive, in-memory only, no DB queries.
- `/ready` (readiness): verifies downstream dependencies (DB, cache, critical APIs); used by load balancers/orchestrators.

### Metrics & Alerting
- Four golden signals: latency, traffic, errors, saturation.
- Define SLOs (e.g. p99 < 500ms, error rate < 0.1%) and alert on **SLO burn rate** (earlier, less noise).
- **Dashboards are for investigation; alerts are for action**, fired only when a human must intervene.
- Every alert links to a runbook (meaning, investigation, mitigation).
- Structured, correlated logs so error spikes trace to requests/users.
- **Synthetic monitoring** from outside your infrastructure catches DNS, CDN and region outages.
- On-call rotation with escalation paths and up-to-date runbooks.

### Observability Stack
- Logs, metrics and traces for production services; OpenTelemetry tracing for multi-service systems.
- Instrument HTTP handlers, DB queries, external API calls, queue consumers.

## Timeouts & Resilience

- Explicit timeouts on HTTP clients, DB connections and all external calls; baseline HTTP 30s, DB query 10s, DB connection 5s, adjusted per workload.
- **Retry only idempotent operations** (or with an idempotency key); exponential backoff with jitter and a max budget (e.g. 3 attempts over 10s).
- **Propagate deadlines**: check for a cancelled context before downstream calls.
- Circuit breakers for external dependencies (open after N failures, half-open to probe).
- Bulkheads: separate resource pools per dependency.
- Graceful degradation when a non-critical dependency is down.
- Graceful shutdown on `SIGTERM`: drain in-flight requests, flush buffers, close DB connections.
- Periodically inject faults (latency, errors, outages) in staging to verify resilience.

## Multi-Environment Awareness

- dev/staging/prod parity: same image, IaC modules, migration tooling; only config differs, via environment variables (never code branches or build-time conditionals).
- Per-environment secrets from a secret manager; never shared across environments.
- Test infrastructure changes in a lower environment first; staging mirrors production topology; run the same post-deploy smoke tests everywhere.
- Production access audited, time-limited, explicitly approved; no standing access for day-to-day work.

## Dependency Updates

- Update on a regular cadence; read changelogs for breaking changes, deprecations and security fixes.
- Batch minor/patch updates; handle major bumps individually with dedicated testing.
- Automated dependency PRs (Dependabot, Renovate).
- Exact pins in application code, ranges only in libraries.
- Audit transitive dependencies; remove unused ones.

## CI/CD

- Every merge to main is automatically deployable (trunk-based).
- Pipeline: lint, test, build, security scan, deploy to staging, smoke test, deploy to prod; gates: no auto-promote to prod if staging smoke tests fail.
- **Branch protection**: CI must pass and at least one reviewer before merging to main; no force-pushes to main; one consistent strategy (linear history via squash/rebase, or merge commits), never mixed.
- **Environment approvals**: GitHub Environments (or equivalent) require manual approval for production; log who and when.
- Build artifacts live in a registry; never rebuild from source for prod. **Immutable artifacts**: the image/binary that passed tests is what deploys.
- Pipeline configs in version control with application-code review rigour; short-lived deploy credentials (OIDC) over long-lived CI secrets.
- **Releases**: semantic version git tags (`v1.2.3`); CHANGELOG; release notes generated from commits or PR titles; container registries also get a `latest` tag on the newest production image.

## Multi-Repo Integration Builds

Assembling unmerged work across several repos, building and testing it fails *silently* in these ways:

- **Enumerate loudly.** A loop over repos, PRs or submodules must surface failures, not print only successes (default branches differ, `main` vs `master`; queries fail; repos get missed). Report counts as "what I found", not "what exists"; re-check before claiming completeness.
- **One clean build exit is not a complete build.** After new targets appear, the first run may regenerate the build graph and run the old one, exiting 0 with work pending. Ask the build system what remains (`ninja -n`).
- **Say which layer is under test.** A component built from integration branches against stock system libraries is not "the integrated stack"; name what is and isn't integrated.
- **Prefer testing a private runtime over installing one**: a locally built runtime exercised from a disposable location beats replacing the machine's. If a project guards that path (setuid binaries ignore environment overrides under `AT_SECURE`), treat the guard as correct and find a supported route.
- **Never install or publish from a tree with uncommitted changes.** Commit first, even to a throwaway branch, so the artifact is attributable to a SHA; record what was actually built.
