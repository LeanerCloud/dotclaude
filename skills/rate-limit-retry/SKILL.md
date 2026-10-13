---
name: rate-limit-retry
description: >-
  Any API, CLI or model throttle (429, rate limit exceeded, secondary limit, resetting usage
  limit, "try again later"): schedule one self-deleting retry; escalate exhausted credits.
---

# Rate-limit handling: retry when throttled, never stall

## The rule

When an operation is throttled (`429`, `403 secondary rate limit`, "rate limit", a usage limit with a reset time, "try again later" from the GitHub API, CodeRabbit, the model/API, or any CLI reporting a cooldown), do NOT abandon the work and do NOT busy-wait. Schedule a retry for that operation.

**Exhausted credits and billing blocks are not throttles.** `You've run out of usage credits` or a billing error won't clear by waiting: escalate to the user with the verbatim message and schedule no retry (same distinction as `cr-loop`).

Don't start a retry cron pre-emptively; a standing cron burns tokens every tick even if nothing was throttled.

- **Retry at the stated time.** `Retry-After` or reset time plus ~30s; otherwise ~2-5 minutes, backing off on repeated throttles.
- **Use a cron that survives the agent** (`CronCreate`) when the work must resume even if the agent or session is reaped mid-cooldown. An in-agent sleep (e.g. cr-watch "sleep 120s, retry") is fine for a short blip in a live agent.
- **Self-terminate**: the cron deletes itself (`CronDelete`) on success or a terminal non-retryable state; a retry never outlives its work.
- **Cap and escalate** after a sensible ceiling (~24h for a CR review, much less for interactive work).
- **One named retry per throttled operation** (`retry-<operation>-<id>`); don't fold unrelated retries together.

> **CodeRabbit: recover a rate-limited review with `@coderabbitai full review`, not the incremental `@coderabbitai review`.** A CR rate-limit can leave in-flight commits uncovered while the incremental cursor advances past them, so a plain re-ping reviews only the next delta: a false-clean (green check, zero findings). Mechanism: `cr-loop` §2 ("after ANY CodeRabbit rate-limit, force a full review").
