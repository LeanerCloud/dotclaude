---
name: cr-loop
description: The CodeRabbit round-trip - trigger, the cr-watch agent, rate-limit recovery via full
  review, triaging findings into actionable/dismiss/nitpick, rebasing mid-loop, and posting evidence.
  Invoke when a CR review is pending or has arrived.
---

# The CodeRabbit review loop

Steps 1-3b of the PR lifecycle; the surrounding lifecycle (creation checklist, merge, post-merge
verification) is the `pr-lifecycle` skill. The loop applies to any project using CodeRabbit or an
equivalent automated reviewer; with none, skip to `pr-lifecycle` §4.

### 1. Trigger CodeRabbit immediately after PR creation

- Right after `gh pr create`, comment with the literal body `@coderabbitai review` (or the project's
  trigger phrase; check its `CLAUDE.md` or memory).
- Verify it landed with `gh pr view <#> --json comments` before walking away.

### 2. CodeRabbit watcher: a background shell wait, then a triage agent

Arm one `cr-watch-<pr-#>` per PR as a background shell script (Claude Code: Bash `run_in_background: true`; other hosts: `skills/HOSTS.md`), not an
agent: it polls review state with `gh` and exits when a review, a rate-limit or billing message, or a
terminal state below lands; its exit wakes the session that armed it. Same pattern and reason as the
`ci-watch` skill (a polling agent re-sends its whole context each poll). It is separate from the CI
watchers, which never read PR comments.

**Model tiers (the single statement; other skills point here):** no model waits. When the script exits,
the session or agents on the tiers `subagent-strategy` assigns to iteration loops handle the result. The
§3 triage (fix vs. dismiss-with-justification, and whether a "tiny fix" reveals an architectural gap) is
review and runs on **Sonnet 5.5 high**; the fix commits, the re-ping and re-arming the script are coding
and run on **Sonnet 5.5 medium** once triage has decided each fix, iterating to silence. The triage brief
carries the full §3 triage rules, the §3a rebase rules and the obligation to re-ping after each fix push.
Use Sonnet 5.5 high for triage however localised the diff looks (medium has empirically gotten it
wrong). Step down to Sonnet 5.5 low only to apply a fully-prescribed diff verbatim.

The script, and whoever its exit wakes:

- **Waits ~10 minutes after the `@coderabbitai review` trigger before its first read** (typical CR time
  on a non-trivial diff; earlier reads see a stale or absent review), then polls
  `gh api repos/<owner>/<repo>/pulls/<#>/comments` and `gh pr view <#> --json reviews` every
  **60-120s** for the first 20 minutes after the trigger and every **180s** after (never faster: CR
  rate-limits review processing).
- Treats `429`, `403 secondary rate limit`, or any "rate limit" string in a `gh` response body as a
  **soft** error: log, sleep 120s, retry; do not escalate. If the cooldown is long or the session may
  end first, also schedule a retry cron per `rate-limit-retry`.
- **Detect CodeRabbit's posted rate-limit message and honor its stated cooldown exactly.** CR signals
  throttling as a *posted comment/review* as well as HTTP `429`, in (at least) **two wordings**; detect
  BOTH:
  - **Per-commit limit**: `Rate limit exceeded` / `@<user> has exceeded the limit for the number of
    commits that can be reviewed per hour. Please wait 10 minutes and 38 seconds before requesting
    another review.`
  - **Per-developer fair-usage limit** (more common): a comment headed **`Review limit reached`**:
    `@<user>, we couldn't start this review because you've reached your PR review rate limit. More
    reviews will be available in 31 minutes and 43 seconds. ...` Per developer per organization per
    hour, so a burst of triggers across many PRs trips ALL of them.
  - **Detection grep** (case-insensitive, any): `Review limit reached`, `reached your PR review rate
    limit`, `rate limit`, `exceeded the limit`, `More reviews will be available in`, `please wait .*
    before requesting`. **Cooldown parse**: `Please wait Xm Ys before requesting another review` OR
    `More reviews will be available in X minutes and Y seconds`, whichever is present.
  - On either, schedule a retry cron for **the stated wait (capped at 3900s) plus a 60s buffer** (per
    `rate-limit-retry`, using the stated cooldown instead of its default; do not poll early). When it
    fires, re-post **`@coderabbitai full review`** (NOT the incremental form; next bullet) and confirm a
    real review arrives before §3. The warning comment carries no findings: it is not loop-exit and must
    not count as a clean pass. **The fair-usage limit is per-developer-per-hour, so do not fan out `full
    review` across many PRs at once; space the triggers (or queue them via the retry cron).** One trigger
    that reviews beats ten that bounce.
- **After ANY CodeRabbit rate-limit, force a full review; never re-ping with the incremental
  `@coderabbitai review`.** When CR throttles (either posted wording, an HTTP `429`, or a review that
  visibly skipped commits), commits in-flight during the cooldown can be silently dropped from CR's
  incremental cursor, so a later plain `review` covers only the *next* delta and the skipped commits are
  never reviewed: a green CR check with zero findings that is not clean. So when the trigger you are
  recovering from was rate-limited, the recovery ping MUST be `@coderabbitai full review`. Use plain
  `review` only for a normal, non-throttled fix-push re-ping. When unsure whether a pass was throttled,
  prefer `full review`.
- **`out of usage credits` / billing messages: escalate, do not retry.** If credits are exhausted (e.g.
  `You've run out of usage credits. Purchase more in the billing tab.`), waiting will not help: escalate
  to the user with the verbatim message and pause the loop until they confirm credits are restored. A
  rate limit clears on its own; credit exhaustion does not.
- Stops polling at a terminal state:
  - CodeRabbit posts a review (success; proceed to §3).
  - The PR is closed without merging (clean up, exit).
  - No CodeRabbit comment of any kind (not even a "Review triggered" acknowledgement) 15 minutes after
    the trigger: report "CR unreachable", no auto-retry. An acknowledgement with no review body yet is
    not terminal; keep polling.
  - 24h without a review: escalate to the user ("CodeRabbit hasn't reviewed in 24h").

### 3. Address CodeRabbit nitpicks

When the review arrives, the triage agent (or session) woken by the §2 script triages each suggestion:

- **Actionable** (real bug, security concern, missing test, broken type, valid code-smell with a clear
  fix): fix in a new commit on the same branch. Each fix commit follows the §"Mandatory pre-commit review
  loop" (2 review passes, whatever the stakes). The push triggers a fresh CI watcher pass per
  §"Post-push CI watcher".
- **Stylistic preference contrary to project conventions** (or a trade-off already considered): reply on
  the PR comment with a brief justification linking the convention in `CLAUDE.md` or the relevant code;
  do NOT commit a change.
- **Genuine nitpick** (minor, unambiguously safe, not contrary to convention): fix it. **Batch nitpicks
  into one commit**; single-line fix commits are review noise.

**Whichever bucket, close the thread in writing.** A fix gets a one-line reply on that finding's inline
thread naming the commit; a dismissal gets the justification reply. A thread left silent after a fix is
indistinguishable from a missed one and forces the human to re-triage it. This is the opposite of
resolving a thread to silence it (below): the reply records what happened and CR still re-reviews.

After the response pass, post one PR comment summarising what was addressed vs. dismissed and why. **End
it with a fresh `@coderabbitai review` ping** so the bot re-reviews the fix push (without the explicit
ping CR sometimes does and sometimes does not re-review). **After this re-ping, arm the ~10-minute timer
again before reading the new review** (§2); every fix push restarts that clock. This re-ping is the most
common trigger for CR's per-commit rate limit; if hit, do not abandon the round: recover per §2 (stated
cooldown, then `@coderabbitai full review`).

**Iterate until CodeRabbit is silent, within the round cap.** Each fresh CR review is a new triage round:
read every Actionable / Nitpick / Outside-diff finding, apply the buckets, push a fix (or justification
reply), ping `@coderabbitai review` again. "I addressed pass 1" is **not** loop-exit. Exit only when the
most recent CR review has zero **Actionable** items AND every **Nitpick** is fixed or dismissed, with a
reply on its inline thread either way (ignoring nitpicks silently is not exit). If CR returns the SAME
finding after a fix, read it more carefully and either (a) push a real fix or (b) reply on the inline
thread explaining why the pushed fix addresses it and the re-flag is a duplicate; never let the same
finding ping-pong more than 2 rounds without explicit user direction.

**Round cap (the single statement; other skills point here):** at most 3 CR rounds per PR, where a round
is a CR review that posts findings (re-pings after a rate limit don't count; §1d and `AGENTS.md` §1
review rounds are separate). From round 2, fix only Actionables and batch nitpicks into the same push or
a follow-up issue; a nitpick deferred to an issue still gets an inline reply linking it, and the exit
condition above is unchanged. If round 3 still returns Actionables, stop and escalate to the user (or the
`AGENTS.md` §1d reviewer) instead of starting round 4. The PR is then not merge-ready: any project gate
requiring CR clean on the final HEAD still applies, or needs an explicit user waiver.

**Never use `@coderabbitai resolve`, and never click "Resolve conversation" to quiet a thread.** The only
sanctioned way through is to address every finding (a real fix commit, or a justification reply on the
inline thread) and ping `@coderabbitai review` for a fresh pass. `resolve` clears comments from the UI
while the bugs stay in the diff and strips the human reviewer of the signal. The loop is done only when a
fresh CR review comes back clean per the exit condition.

**Rate-limit etiquette.** CR review processing is rate-limited per repo: let each `@coderabbitai review`
settle to a posted review before pushing the next fix commit. Four fix commits in 60 seconds just get
batched under one review.

### 3a. Resolve merge conflicts during the CR loop

The branch can fall behind base mid-loop (push rejected `non-fast-forward`, or `mergeStateStatus` flips to
`DIRTY` / `BEHIND`). Handle it inside the loop:

- **Detect early.** Before each fix push, `gh pr view <#> --json mergeStateStatus,mergeable`. `DIRTY` =
  conflicts to resolve before merge; `BEHIND` = stale, no conflicts; `BLOCKED` usually = a failing
  required check (the CI watcher's job).
- **Rebase, don't merge, by default.** In the worktree: `git fetch origin && git rebase
  origin/<base-branch>`. For each conflict file, `git diff --check`, decide which side wins (or how to
  merge both), `git add <file>`, `git rebase --continue`. Never take "ours" or "theirs" wholesale without
  reading both sides; a conflict often points at a semantic clash that deserves a third option.
- **Re-run the relevant tests after the rebase**, even for "trivial" conflicts: replaying commits on new
  base state can change call sites after a base refactor. `go test ./...` or the project's equivalent
  gate is mandatory before pushing.
- **Push with `--force-with-lease`, never `--force`**: it aborts if origin moved mid-rebase; plain
  `--force` silently overwrites other agents' work.
- **Comment on the PR after the rebase push**: "Rebased onto <base-branch>@<sha> to resolve conflicts in
  <files>. Tests re-run green. <new-head-sha>."
- **Conflict-resolution commits do NOT count as "addressed CR findings".** A rebase that drops a
  CR-flagged change is a regression; if the rebase obviated a finding (base already fixed it), say so on
  the original CR thread.

If conflicts span more than the immediate diff (e.g., a base refactor moved the file), pause and
re-evaluate: the plan may need updating. Do not power through a 100-line resolution that re-implements
half the PR; that warrants a fresh plan-review pass per `AGENTS.md` §1.

### 3b. The PR is the record: post the evidence, not only the outcome

Everything an implementer or reviewer learns lives in agent-to-orchestrator messages that evaporate with
the session; the PR and its issue are the only durable record. Anything that informed a merge decision
gets posted there **as it happens**. Test: could a reader arriving at the merged PR cold tell what was
checked, by whom, and what was left unverified?

- **After pushing a fix commit**, comment what changed, why, and the verification: tests run and their
  outcome, and for a bug fix the pre-fix failure plus the post-fix pass (`AGENTS.md` §4). §3a's rebase
  note is the same habit.
- **After an independent or adversarial review**, post the verdict *and* the evidence: what was verified
  with which commands or queries, what was tested and cleared, and what could NOT be verified and why. A
  bare "looks good" is not a record; the failed attacks let a later reader judge coverage.
  `pr-orchestration` §5 makes this posted verdict part of the merge gate.
- **State when a bot's green status is not a real review.** A bot can publish `state: success` while its
  `description` says "Review rate limited" (it reviewed nothing; detection and the both-directions
  caveat are in `pr-orchestration` §7). Whenever a PR advances on a throttled, skipped or otherwise
  non-review pass, say so on the PR.

Keep it proportionate: one comment per event, evidence rather than narration, nothing posted for work that
changed neither the code nor a merge decision.
