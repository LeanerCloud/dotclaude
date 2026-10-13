---
name: ci-watch
description: After every push, arm one background shell watcher per GitHub Actions run, then fetch
  failed logs and fix failures autonomously when it exits. Invoke immediately after any `git push`
  that publishes commits.
---

# Post-push CI watcher (background shell)

After every `git push` that publishes new commits, immediately enumerate **all** GitHub Actions workflow runs triggered by the push and arm **one background shell watcher per run** to monitor each independently. A single commit typically triggers multiple workflows (build, lint, test matrix, terraform validate, security scan, deploy) — they run in parallel, fail independently, and need fixes targeted at different parts of the codebase. A single watcher serialising across all of them would block on the slowest, miss parallel failures, and conflate diagnoses.

> **CI watchers are NOT CodeRabbit watchers.** A CI watcher's check-list ends when GitHub Actions reports the run conclusion. CodeRabbit's inline review comments are invisible to it - CR's "check" goes green once the review is *submitted*, regardless of findings inside. If this push opened a PR (or is on an open PR branch), spawn a separate `cr-watch-<pr-#>` per §"Post-PR review loop" → §"Immediate PR-creation checklist" alongside the CI watchers. The two watcher kinds run in parallel with different terminal conditions.

> **After every push to an open PR branch, re-request CodeRabbit and arm the 10-minute timer — do both, at the same time.** Immediately post `@coderabbitai review` (a push without a re-request leaves the new commits unreviewed), and at the same moment arm a ~10-minute timer before reading or triaging CR comments. CodeRabbit needs several minutes to post; triaging earlier just reads a stale or absent review. The cr-watch agent (§2) owns this delay — its first read is ~10 minutes after the trigger — so a live cr-watch already implements the rule. If you are not running a cr-watch for this push, set the timer yourself (`ScheduleWakeup` ~10 min or a cron) and only triage when it fires.

**Setup**:

1. Right after `git push`, run `gh run list --commit <sha> --json databaseId,name,status` to list every run for the pushed commit. Wait briefly (a few seconds) and re-list if the run list looks incomplete — workflows can take a moment to register.
2. For each run ID, start `gh run watch <run-id> --exit-status > /dev/null` with Bash `run_in_background: true` (label it `ci-watch-<short-sha>-<workflow-slug>-<run-id>` in the description), then end the turn. The command loops internally and its exit wakes the session that armed it, so waiting costs no model calls. **Never wait by polling from an agent's own loop** (`gh run view` every N seconds, `sleep` loops, a Sonnet 5.5 low agent that re-checks): each poll re-sends that agent's whole context, and on a 2026-09-28 burndown that was a leading cost. When it exits non-zero, run `gh run view <run-id> --log-failed | tail -n 80` and classify the failure. If a fix is needed, split it per CLAUDE.md §2: diagnosing the failure (what to change and why, without breaking what's green) is judgement and runs on **Sonnet 5.5 high**; writing and pushing the decided fix is coding and runs on **Sonnet 5.5 medium**. A single-step fix whose diff is fully prescribed (e.g., applying a CR-suggested diff verbatim) needs no high-tier diagnosis.
3. Each watcher monitors **only its assigned run ID**, so a failure maps to one workflow.

**On failure, the fix agent's job**:

1. Take the run ID and the failed-log excerpt in its brief; it does not re-watch the run.
2. Diagnose the root cause from `gh run view <run-id> --log-failed`, and **fix it autonomously** with a follow-up commit + push (e.g., lint failures, broken tests, terraform validation, type errors, missing env vars in CI) — not just report back.
3. Coordinate with sibling fix agents before pushing a fix (message them, per the `multi-agent-comms` skill): another agent may already be fixing a related failure, and two parallel pushes can stomp on each other or trigger a fresh round of CI for both. Run the push under that skill's `<repo>-git-push-<branch-key>` lock, the same lock `pr-iterate` takes for the branch.
4. Only escalate to the user if the failure requires a decision (credentials, infra changes, ambiguous design choices) or if its own fix attempt also fails CI.

Do NOT poll any watcher from the foreground; each notifies on exit. An agent that pushed and armed its watchers reports and stops; it is woken, or its orchestrator is notified, when a watcher exits. This keeps the main session unblocked while CI runs and ensures broken main never sits unaddressed, regardless of how many workflows fired for the same commit.
