---
name: ci-watch
description: After every push, arm one background shell watcher per GitHub Actions run, then fetch
  failed logs and fix failures autonomously when it exits. Invoke immediately after any `git push`
  that publishes commits.
---

# Post-push CI watcher (background shell)

Host mapping (Claude Code: Bash `run_in_background: true`, `ScheduleWakeup`; other hosts: `skills/HOSTS.md`).

After every `git push` that publishes new commits, enumerate **all** GitHub Actions runs the push
triggered and arm **one background shell watcher per run**. Workflows (build, lint, test matrix,
terraform validate, security scan, deploy) run in parallel, fail independently and need different fixes;
a single serial watcher would block on the slowest and conflate diagnoses.

> **CI watchers are NOT CodeRabbit watchers.** A CI watcher ends when Actions reports the run
> conclusion; CR's inline comments are invisible to it (CR's check goes green once the review is
> *submitted*, whatever its findings). If the push opened a PR or is on an open PR branch, also arm a
> `cr-watch-<pr-#>` per `pr-lifecycle` §"Immediate PR-creation checklist". They run in parallel with
> different terminal conditions.

> **After every push to an open PR branch, re-request CodeRabbit and arm the 10-minute timer, both at
> once.** Post `@coderabbitai review` (a push without it leaves the new commits unreviewed) and arm a
> ~10-minute timer before reading CR comments. The cr-watch (`cr-loop` §2) already owns that delay, so a
> live cr-watch implements the rule; without one, set the timer yourself (a scheduled wakeup or cron, ~10 min) and triage only when it fires.

**Setup**:

1. Right after `git push`, `gh run list --commit <sha> --json databaseId,name,status` lists every run
   for the pushed commit. Re-list after a few seconds if it looks incomplete.
2. For each run ID, start `gh run watch <run-id> --exit-status > /dev/null` as a background shell command (description label `ci-watch-<short-sha>-<workflow-slug>-<run-id>`), then
   end the turn. The command loops internally and its exit wakes the session, so waiting costs no model
   calls. **Never wait by polling from an agent's own loop** (`gh run view` every N seconds, `sleep`
   loops, a low-tier agent re-checking): each poll re-sends the whole context, a leading cost in a
   2026-09-28 burndown. On non-zero exit, `gh run view <run-id> --log-failed | tail -n 80` and classify.
   If a fix is needed, split per AGENTS.md §2: diagnosing the failure (what to change without breaking
   what's green) runs on **Sonnet 5.5 high**; writing and pushing the decided fix runs on **Sonnet 5.5
   medium**. A fully-prescribed fix (e.g. a CR-suggested diff applied verbatim) needs no high-tier
   diagnosis.
3. Each watcher monitors **only its run ID**, so a failure maps to one workflow.

**On failure, the fix agent**:

1. Gets the run ID and failed-log excerpt in its brief; it does not re-watch the run.
2. Diagnoses the root cause from `gh run view <run-id> --log-failed` and **fixes it autonomously** with a
   follow-up commit + push (lint, broken tests, terraform validation, type errors, missing CI env vars),
   not just a report.
3. Coordinates with sibling fix agents before pushing (`multi-agent-comms`): two parallel pushes can
   stomp each other or trigger duplicate CI rounds. Push under that skill's
   `<repo>-git-push-<branch-key>` lock, the same lock `pr-iterate` takes for the branch.
4. Escalates to the user only if the failure needs a decision (credentials, infra changes, ambiguous
   design) or its own fix attempt also fails CI.

Do NOT poll any watcher from the foreground; each notifies on exit. An agent that pushed and armed its
watchers reports and stops, and is woken (or its orchestrator notified) when a watcher exits.
