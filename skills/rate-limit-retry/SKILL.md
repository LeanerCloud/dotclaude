---
name: rate-limit-retry
description: What to do when an operation is throttled (429, secondary rate limit, usage limit,
  "try again later") by any API, model or CLI - schedule one self-deleting retry instead of
  stalling or busy-waiting. Invoke when a throttle actually happens.
---

# Rate-limit handling: retry when throttled, never stall

## The rule

When an operation is throttled (a `429` / `403 secondary rate limit` / "rate limit" / "usage limit" / "try again later" from the GitHub API, CodeRabbit, the model/API itself, or any CLI reporting a cooldown), do NOT abandon the work and do NOT block the session busy-waiting. Schedule a retry for that operation.

Don't start a retry cron pre-emptively at the start of a request. A standing cron that fires every couple of minutes costs tokens and context on every tick, even when nothing was ever throttled.

- **Retry at the stated time.** If the provider returns a `Retry-After` or a reset time, retry then plus ~30s. Otherwise retry after ~2-5 minutes and back off on repeated throttles.
- **Use a cron that survives the agent** (`CronCreate` in Claude Code) when the work must resume even if the current agent or session is reaped mid-cooldown. An in-agent sleep (e.g. the cr-watch "sleep 120s, retry") is fine for a short blip inside a live agent.
- **Self-terminate.** Once the operation succeeds (or hits a terminal non-retryable state), the cron deletes itself (`CronDelete`). A retry must never outlive the work it was created for.
- **Cap and escalate.** Give up after a sensible ceiling (e.g. ~24h for a CR review, much shorter for interactive work) and escalate to the user rather than retrying forever.
- **One retry per throttled operation**, named so it is identifiable (e.g. `retry-<operation>-<id>`). Don't fold unrelated retries into one cron.

> **CodeRabbit-specific: recover a rate-limited review with `@coderabbitai full review`, not the incremental `@coderabbitai review`.** A CR rate-limit can leave the in-flight commits uncovered while CR's incremental cursor advances past them, so a plain re-ping reviews only the next delta and the skipped commits never get looked at — a false-clean (green check, zero findings). After any CR throttle, the recovery trigger must be `@coderabbitai full review`. Full rationale and the exact mechanism are in §"Post-PR review loop" → §2 ("after ANY CodeRabbit rate-limit, force a full review").
