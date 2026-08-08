---
name: rate-limit-retry
description: Keep a self-deleting retry cron running so throttled work resumes instead of stalling.
  Invoke on any 429, secondary rate limit, usage limit, or "try again later" from any API, model or
  CLI - and at the start of any request that will make many API calls.
---

# Rate-limit handling — always run a retry cron, never stall

## The rule

This is a global rule (see `CLAUDE.md` Core Principles): on every request, keep a retry cron running so any throttling is caught and retried automatically rather than stalling the work. When an operation is throttled — a `429` / `403 secondary rate limit` / "rate limit" / "usage limit" / "try again later" from the GitHub API, CodeRabbit, the model/API itself, or any CLI reporting a cooldown — do NOT abandon the work and do NOT block the session busy-waiting; let the standing cron catch it and retry.

- **Run a cron on a ~2-minute cadence** (`CronCreate`, e.g. `*/2 * * * *`) whose job is to catch any throttled operation and re-attempt it. Rate-limit cooldowns are typically a few minutes, so a 2-minute tick retries soon after the window clears without hammering the limit.
- **Each firing checks first, then retries.** If the limit has cleared, run the retry; if still limited, log and wait for the next tick (back the effective retry off toward "a few minutes" by skipping ticks when the provider returns a `Retry-After`).
- **Self-terminate.** Once the operation succeeds (or hits a terminal non-retryable state), the cron deletes itself (`CronDelete`). A retry cron must never outlive the work it was created for.
- **Cap and escalate.** Give up after a sensible ceiling (e.g. ~24h for a CR review, much shorter for interactive work) and escalate to the user rather than retrying forever.
- **One cron per throttled operation**, named so it is identifiable (e.g. `retry-<operation>-<id>`). Don't fold unrelated retries into one cron.

This sits on top of, not instead of, any in-agent soft-retry (e.g. the cr-watch "sleep 120s, retry" in §2): the in-agent sleep handles transient blips within a live agent, while the cron survives the agent or session being reaped mid-cooldown, so progress resumes even if the original process is gone.

> **CodeRabbit-specific: recover a rate-limited review with `@coderabbitai full review`, not the incremental `@coderabbitai review`.** A CR rate-limit can leave the in-flight commits uncovered while CR's incremental cursor advances past them, so a plain re-ping reviews only the next delta and the skipped commits never get looked at — a false-clean (green check, zero findings). After any CR throttle, the recovery trigger must be `@coderabbitai full review`. Full rationale and the exact mechanism are in §"Post-PR review loop" → §2 ("after ANY CodeRabbit rate-limit, force a full review").
