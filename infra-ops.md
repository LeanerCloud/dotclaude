# Infrastructure & Operations

Read this file when working on infrastructure, deployments, cloud resources, or operational concerns. Project-specific overrides live in the project's `CLAUDE.md`.

## Rollback Awareness

- Before every deploy or Terraform apply, have an explicit rollback plan — know the exact steps to revert
- **Feature flags are the fastest rollback path** — toggling a flag requires no deploy; prefer them for risky changes
- **Database rollbacks are much harder than code rollbacks** — a code rollback doesn't undo schema migrations; always verify the down migration path is tested and safe before running the up migration in production
- Design changes to be reversible: feature flags, backward-compatible schema migrations, blue/green deployments
- For Terraform: destructive operations (`destroy`, forced resource replacement) require user confirmation first; present the plan summary clearly
- **Terraform output must never be truncated**: always tee the full output to a file (`terraform plan 2>&1 | tee /tmp/tf-plan.txt`); never pipe through `head`, `tail`, or any length-limiting filter — truncated plans hide resource deletions and replacement cascades
- **Always plan before apply**: run `terraform plan` and read the full output; explicitly count destroyed/replaced resources before proceeding
- **Never `-auto-approve` in production**: gate production applies behind a manual approval step; `-auto-approve` is acceptable only in dev/CI with no production state
- **Avoid `-target`**: targeted applies leave state inconsistent with config — treat it as a break-glass measure only, never routine workflow
- **State management**: always use a remote backend with state locking; never run concurrent applies; never edit `.tfstate` directly — use `terraform state mv/rm/import`; take a state backup before destructive operations: `terraform state pull > tf-state-backup-$(date +%Y%m%d).json`
- **Commit `.terraform.lock.hcl`**: pins provider versions for reproducible applies across machines and CI — treat it like `package-lock.json`
- **Validate and format in CI**: run `terraform validate` and `terraform fmt -check`; fail the pipeline if fmt produces a diff
- **Security scanning**: run `tfsec` or `checkov` in CI before apply — catches IAM wildcards, public S3 buckets, and unencrypted storage before they reach the cloud
- **Sensitive outputs**: mark secrets and tokens with `sensitive = true` in outputs so they don't appear in plan output or CI logs
- **Drift detection**: schedule a weekly `terraform plan` run against production state in CI; alert on any diff — manual console changes are the most common source of silent drift
- Blue/green and canary deployments reduce rollback to a traffic switch — prefer them for stateless services
- Keep the previous container image / artifact version available until the new version is proven stable
- For database-backed rollbacks: ensure the old code version can safely run against the new schema before switching traffic back

## Secrets Management

- Store secrets in a dedicated secret manager (AWS Secrets Manager, GCP Secret Manager, HashiCorp Vault) — never in environment files committed to the repo, S3/GCS without encryption, or config files
- **Rotation**: rotate secrets on a schedule (API keys quarterly, DB passwords monthly, or immediately on suspected compromise); automate rotation where the service supports it
- **Emergency rotation procedure**: document and test it before you need it — know how to rotate every secret without causing an outage; use blue/green secret rotation (create new → update consumers → delete old) to avoid downtime
- **Scan git history**: use `gitleaks`, `truffleHog`, or `git-secrets` to detect secrets that were ever committed, even if removed; a secret in git history is compromised — rotate it
- Inject secrets at runtime via environment variables or secret manager SDK calls — never bake into images or config files
- Apply least privilege to secret access: each service reads only the secrets it needs; use separate credentials per environment
- Audit secret access logs — alert on unexpected access patterns

## Cost Awareness

- Tag all cloud resources: `project`, `environment`, `owner` at minimum; add `cost-center` for billing attribution
- Prefer spot/preemptible instances for batch workloads, CI runners, and dev environments
- Right-size resources — don't default to large instance types; start small and scale with data
- Set up budget alerts and cost anomaly detection (AWS Cost Anomaly Detection, GCP Budget Alerts) early — before the bill surprises you
- Review and clean up orphaned resources (unattached EBS volumes, unused Elastic IPs, old snapshots) regularly; automate cleanup where possible
- Choose storage classes appropriately: use lifecycle policies to move cold data to cheaper tiers and expire data that's no longer needed
- Consider data transfer costs: egress between regions/AZs/internet adds up; keep services co-located where possible
- Use auto-scaling policies to avoid over-provisioning — scale down aggressively in non-prod environments outside business hours
- Prefer managed services over self-hosted when the operational burden is high relative to cost difference

## Monitoring & Health Checks

### Service Endpoints
- Expose `/health` (liveness) and `/ready` (readiness) on all services
- `/health` returns `200` if the process is alive — no DB queries; keep it in-memory
- `/ready` verifies downstream dependencies (DB, cache, critical APIs) — used by load balancers and orchestrators to route traffic

### Metrics & Alerting
- Track the four golden signals: latency, traffic, errors, saturation
- Define SLOs (e.g. p99 latency < 500ms, error rate < 0.1%) and alert on **SLO burn rate**, not just raw thresholds — burn rate alerts fire earlier and with less noise
- **Dashboards are for investigation; alerts are for action** — dashboards show trends and context; alerts fire only when human intervention is needed
- Every alert must have a runbook: what it means, how to investigate, how to mitigate — link from the alert to the runbook
- Use structured, correlated logs alongside metrics — a spike in errors should be traceable to specific requests/users
- Add **synthetic monitoring** (external uptime checks from outside your infrastructure) — it's the only way to detect issues that affect all infrastructure simultaneously (e.g. DNS, CDN, region outages)
- Define an on-call rotation with clear escalation paths; ensure runbooks are up to date and accessible to anyone on call

### Observability Stack
- Logs → metrics → traces form a complete picture; invest in all three for production services
- Use distributed tracing (OpenTelemetry) for multi-service architectures to trace request flows end-to-end
- Instrument at key boundaries: HTTP handlers, DB queries, external API calls, queue consumers

## Timeouts & Resilience

- Always set explicit timeouts on HTTP clients, DB connections, and all external calls — never rely on OS defaults
- Recommended baseline timeouts: HTTP client 30s, DB query 10s, DB connection 5s — adjust per workload
- **Only retry idempotent operations** — retrying a non-idempotent operation (e.g. charge a card) without an idempotency key causes duplicate side effects
- Use retries with exponential backoff and jitter for transient failures; set a max retry budget (e.g. 3 attempts over 10s)
- **Propagate deadlines**: before making a downstream call, check if the context is already cancelled — don't fan out work that will be discarded
- Implement circuit breakers for external dependencies to prevent cascade failures (open after N failures, half-open to probe recovery)
- Bulkheads: isolate resource pools per dependency so one slow dependency can't exhaust all threads/connections
- Graceful degradation: serve degraded functionality rather than failing completely when a non-critical dependency is down
- Graceful shutdown: drain in-flight requests, flush buffers, close DB connections before exiting — handle `SIGTERM`
- Chaos engineering: periodically inject faults (latency, errors, outages) in staging to verify resilience assumptions hold in practice

## Multi-Environment Awareness

- Maintain dev/staging/prod parity — same Docker image, same IaC modules, same migration tooling; only config differs
- Environment-specific config via environment variables only — never via code branches or build-time conditionals
- Secrets per environment via a secret manager — never shared secrets across environments
- Test infrastructure changes in a lower environment before promoting; staging should mirror production topology
- Production access should be audited, time-limited, and require explicit approval — no standing prod access for day-to-day work
- Keep environment parity by running the same smoke tests post-deploy in every environment

## Dependency Updates

- Keep dependencies current — don't let them drift for months; schedule a regular update cadence
- Read changelogs and release notes before updating; look for breaking changes, deprecations, and security fixes
- Batch minor/patch updates together; handle major version bumps individually with dedicated testing
- Enable automated dependency PRs (Dependabot, Renovate) to surface updates continuously
- Pin to exact versions in application code; use ranges only in library code
- Audit transitive dependencies — a direct dep with a clean record can pull in a vulnerable transitive dep
- Remove unused dependencies — they're attack surface and build overhead with no benefit

## CI/CD

- Every merge to the main branch should be automatically deployable (trunk-based development)
- Deployment pipeline stages: lint → test → build → security scan → deploy to staging → smoke test → deploy to prod
- **Branch protection rules**: require CI to pass and at least one reviewer before merging to main; prohibit force-pushes to main; pick either linear history (squash/rebase) or merge commits — enforce one strategy consistently, never mix
- **Deployment environment approvals**: use GitHub Environments (or equivalent) to require manual approval before deploying to production; log who approved and when
- Use deployment gates: don't auto-promote to prod if staging smoke tests fail
- Store build artifacts in a registry (container registry, artifact repo) — never rebuild from source for prod deploys
- **Immutable artifacts**: the same image/binary that passed tests is what gets deployed — no rebuilds between environments
- Keep pipeline configs in version control; treat CI/CD changes with the same review rigour as application code
- Use short-lived deploy credentials (OIDC tokens) rather than long-lived CI secrets where possible
- **Release strategy**: tag releases in git with semantic versions (`v1.2.3`); maintain a CHANGELOG; automate release notes generation from commit messages or PR titles; in container registries also push a `latest` tag pointing to the most recent production image
