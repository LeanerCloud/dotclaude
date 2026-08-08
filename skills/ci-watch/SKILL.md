---
name: ci-watch
description: After every push, spawn one background watcher per GitHub Actions run to poll it, fetch
  failed logs, and fix failures autonomously. Invoke immediately after any `git push` that publishes
  commits.
---

# Post-push CI watcher (background agent)

After every `git push` that publishes new commits, immediately enumerate **all** GitHub Actions workflow runs triggered by the push and launch **one background agent per run** to monitor each independently. A single commit typically triggers multiple workflows (build, lint, test matrix, terraform validate, security scan, deploy) — they run in parallel, fail independently, and need fixes targeted at different parts of the codebase. A single watcher agent serialising across all of them would block on the slowest, miss parallel failures, and conflate diagnoses.

> **CI watchers are NOT CodeRabbit watchers.** A CI watcher's check-list ends when GitHub Actions reports the run conclusion. CodeRabbit's inline review comments are invisible to it - CR's "check" goes green once the review is *submitted*, regardless of findings inside. If this push opened a PR (or is on an open PR branch), spawn a separate `cr-watch-<pr-#>` per §"Post-PR review loop" → §"Immediate PR-creation checklist" alongside the CI watchers. The two agent kinds run in parallel with different terminal conditions and different model tiers.

> **After every push to an open PR branch, re-request CodeRabbit and arm the 10-minute timer — do both, at the same time.** Immediately post `@coderabbitai review` (a push without a re-request leaves the new commits unreviewed), and at the same moment arm a ~10-minute timer before reading or triaging CR comments. CodeRabbit needs several minutes to post; triaging earlier just reads a stale or absent review. The cr-watch agent (§2) owns this delay — its first read is ~10 minutes after the trigger — so a live cr-watch already implements the rule. If you are not running a cr-watch for this push, set the timer yourself (`ScheduleWakeup` ~10 min or a cron) and only triage when it fires.

**Setup**:

1. Right after `git push`, run `gh run list --commit <sha> --json databaseId,name,status` to list every run for the pushed commit. Wait briefly (a few seconds) and re-list if the run list looks incomplete — workflows can take a moment to register.
2. For each run ID, spawn a separate background agent (`Agent` tool with `run_in_background: true`, `model: haiku`) named `ci-watch-<short-sha>-<workflow-slug>-<run-id>` (e.g. `ci-watch-a1b2c3d-build-123456789`, `ci-watch-a1b2c3d-test-123456790`). Each name must be unique and addressable via `SendMessage`. The watcher's core work — polling `gh run view`, fetching failed logs, classifying the failure — fits Haiku per CLAUDE.md §2. If a fix is needed, re-spawn the fix step on **Opus** — fix-push after a failed CI run is an iteration loop per CLAUDE.md §2 and runs on Opus regardless of how mechanical the diff looks, because deciding what to change and why (and not breaking what's green) is the judgement the Opus tier earns. Only step down (to Sonnet/Haiku) for a single-step mechanical fix where the diff is fully prescribed (e.g., applying a CR-suggested diff verbatim).
3. Each agent monitors **only its assigned run ID** — pass the run ID explicitly in the prompt so it doesn't poll the wrong workflow.

**Each agent's job**:

1. Poll `gh run watch <run-id>` (or `gh run view <run-id> --json status,conclusion` in a loop) until that specific workflow finishes.
2. On failure, fetch logs with `gh run view <run-id> --log-failed`, diagnose the root cause, and **fix it autonomously** with a follow-up commit + push (e.g., lint failures, broken tests, terraform validation, type errors, missing env vars in CI) — not just report back.
3. Coordinate with sibling watchers via the multi-agent comms bus before pushing a fix: another watcher may already be fixing a related failure, and two parallel pushes can stomp on each other or trigger a fresh round of CI for both. Claim a `git-push` lock first (see `~/.claude/multi-agent-comms.md`).
4. Only escalate to the user if the failure requires a decision (credentials, infra changes, ambiguous design choices) or if its own fix attempt also fails CI.

Do NOT poll any watcher from the foreground — they will each notify on completion. This keeps the main session unblocked while CI runs and ensures broken main never sits unaddressed, regardless of how many workflows fired for the same commit.
