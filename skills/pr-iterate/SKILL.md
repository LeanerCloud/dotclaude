---
name: pr-iterate
description: Drive a PR (or list of PRs, or all eligible open PRs in the current repo) to merge-ready state by resolving conflicts, addressing CodeRabbit (CR) findings, re-pinging CR, and iterating until CR has zero actionables and CI is green. Invocations - `/pr-iterate <PR#>` (single), `/pr-iterate <PR#> <PR#> ...` (parallel fan-out across the listed PRs), `/pr-iterate` (fan out across all eligible open PRs in current repo, with a confirmation gate). Triggered by user requests like "address CR on #NNN", "address CR comments on #NNN", "fix conflicts on #NNN", "iterate on #NNN", "push fixes and re-ping CR on #NNN", "iterate on all my open PRs", "fan out pr-iterate on my backlog". Use whenever the next action on one or more PRs is some combination of {resolve conflicts, fix CR findings, push, re-ping `@coderabbitai review`}. Do NOT use for opening a brand-new PR or for merging.
---

<!-- NOTE: reconstructed copy committed to dotclaude so the scheduled issue-pr-autopilot
     remote agent (which clones dotclaude) can read the CR-iteration flow. The original
     local copy was lost; the per-pr-agent-prompt.template.md is intentionally absent and
     the skill's documented fallback (Phase 0) covers that case. -->

# pr-iterate - automate the recurring PR-iteration loop

This skill encodes the recurring loop for driving an existing PR to merge-ready.
Read this file end-to-end before doing anything. The skill NEVER opens a brand-new
PR and NEVER merges - it only edits existing PRs (resolve conflicts, address CR
findings, push, re-ping CR) and reports when a PR is ready for the human to merge.

## Model tier policy - required

**Pattern: plan = Opus, impl = Sonnet, parse = Haiku.** Every `Agent` dispatch this
skill makes MUST pass an explicit `model:` parameter - never rely on inheritance from
the calling session. This follows the global CLAUDE.md rules:

- **"Delegate to the cheapest sufficient tier - actively, not just when in doubt."**
  Every Opus dispatch below is justified because the step requires judgement / holding
  many constraints in working memory; everything else drops to Sonnet or Haiku.
- **"Set the `model` parameter on EVERY `Agent` call - never rely on inheritance."**
- **"Routine PR-shipping splits across tiers: planning + iteration loops on Opus,
  implementation on Sonnet."**

### Phase-to-model mapping

| Work | Model | Why |
|---|---|---|
| **Phase 1 triage** - read PR metadata, find worktree, decide entry phase | **Opus** | Hold the full PR shape + history + invariants while choosing the right entry phase. |
| **Phase 1 CR-comment parsing** - extract findings into a structured list | **Haiku** | Pure text-extraction / classification against a known rubric. |
| **Phase 2 rebase + conflict resolution** | **Opus** | Judgement on semantic divergence vs additive-merge. |
| **Phase 3 per-finding triage** (fix / skip-stale / out-of-scope-issue / CR-misread) | **Opus** | Each finding needs a real judgement. |
| **Phase 3 implementation** - applying the fix per finding | **Sonnet** | Once the plan is decided, the diff is mechanical. |
| **Phase 4 push + re-ping mechanics** | **Sonnet** (or main session) | Mechanical: lock, push, comment, unlock. |
| **Phase 5 iteration check** (poll CR's next pass) | **Haiku** | Parse the new CR review body for "Actionable comments posted: 0" or extract new findings. |
| **Phase 6 cleanup report** | **Main session** | One-shot summary. |

The main session reads each subagent's output, then dispatches the next phase's agent
with the right `model:`. The main session itself stays in plan/dispatch mode.

## Runtime-adaptive operation (local session vs constrained/remote agent)

This skill runs in two very different runtimes. **Detect which by tool availability**
(is the `Agent`/`Task` tool present?) and adapt:

- **Local interactive session** (has `Agent`/`Task`, persistent across turns, can
  `ScheduleWakeup`): use everything below as written - fan out per-PR Opus orchestrators,
  spawn `cr-watch-<pr-#>` / `ci-watch-<sha>-<wf>` background agents, schedule wakeups for
  CR responses. This is the default.
- **Constrained / remote agent** (NO `Agent`/`Task` tool, bounded single session, e.g. the
  scheduled `issue-pr-autopilot` cloud run): you CANNOT spawn subagents, and any background
  process dies when the run ends. So:
  - Do every phase INLINE, single-threaded, in this one session. No fan-out, no per-PR
    orchestrators, no background watchers, no `ScheduleWakeup`.
  - The **atomic-coupling invariant** (never request a CR review without a watcher that acts
    on the response) is satisfied **structurally** by the caller's re-invocation cadence: a
    PR you re-ping must stay in the tracked in-flight set so the NEXT scheduled fire
    re-examines it. The cron / re-invocation IS the durable watcher. Only re-ping a PR that
    remains tracked; never re-ping a PR you are about to drop.
  - Replace "schedule a wakeup and exit" with "do a single bounded in-run wait (e.g. ~5 min)
    then move on; the next fire continues."
  - The push lock is irrelevant (single agent) but harmless; skip it if the lock dir tooling
    is unavailable.

Everything else (the triage buckets, the fix shapes, the hard constraints) applies identically
in both runtimes.

## Multi-PR fan-out mode

The skill accepts one OR many PR numbers as positional args. When >1 PR is given, the
main session **fans out one Opus orchestrator agent per PR in a single parallel-dispatch
message** rather than running them sequentially.

### Arg-shape acceptance (parsed in Phase 0)

| Shape | Example | Notes |
|---|---|---|
| Zero args | `/pr-iterate` | Auto-discover eligible open PRs in current repo. Confirmation gate before fan-out. |
| Single PR | `/pr-iterate 922` | Single-PR path. |
| Space-separated list | `/pr-iterate 922 912 889` | Fan-out. No gate - user named the PRs. |
| Comma-separated list | `/pr-iterate 922,912,889` | Strip whitespace around commas. |
| `#`-prefixed | `/pr-iterate #922,#912` | Strip leading `#`. |
| Range (NOT supported) | `/pr-iterate 922-925` | Error: list each number explicitly. |

Parsing rule: split on `[\s,]+`, strip leading `#`, validate each is `^[0-9]+$`. Reject
the whole input if any token fails. Zero tokens after parsing triggers auto-discovery.

### Zero-arg auto-discovery (bare `/pr-iterate`)

1. Detect repo: `gh repo view --json nameWithOwner --jq .nameWithOwner`. If it errors,
   stop with: "No GitHub repo detected. Pass an explicit PR# list."
2. Resolve user: `gh api user --jq .login`.
3. Enumerate: `gh pr list --state open --json number,title,mergeStateStatus,isDraft,author,headRefName,baseRefName,updatedAt --limit 100`.
4. Filter to "ours to iterate" = ALL of:
   - `isDraft == false`.
   - `author.login == <current-user>`.
   - has-work: `mergeStateStatus IN {DIRTY, UNSTABLE, CONFLICTING, BLOCKED, UNKNOWN, BEHIND}`,
     OR `mergeStateStatus == CLEAN` but the most recent CR review's body indicates
     `Actionable comments posted: N` with `N > 0` not yet addressed in a later commit.
   - Exclude: drafts; PRs authored by someone else; CLEAN with no pending CR actionables.
5. Stack detection: if any eligible PR's base equals another eligible PR's head, you have
   a stack; proceed bottom-up.
6. Confirmation gate (zero-arg only): show the eligibility list + parallelism plan, prompt
   y/n. Explicit-list mode never gates.
7. Empty eligibility: report "No eligible PRs" and exit.

### Parallelism cap and wave logic

**Hard cap: 6 parallel `Agent` dispatches per wave.** N<=6 no stacks -> single wave.
N>6 -> sequential waves of 6. Stacks -> bottom-of-stack + non-stacked first, then up.

### When to fan out vs serialize

Fan out (parallel) when each PR is on a divergent branch with isolated worktrees and none
depends on another's merge order. Serialize when PRs form a stack (#A -> #B rebased on #A
-> #C rebased on #B); invoke one at a time, bottom-up.

### Main-session pre-flight (read-only, before any agent dispatch)

1. Validate every PR with `gh pr view <N> --json state,mergeStateStatus,baseRefName,headRefName,title,isDraft`.
   Serialize reads if N >= 10 to avoid bursting the gh rate limit.
2. Stop with a clear error if ANY PR is `state != OPEN`.
3. Stack-detection across the input list.
4. Summary line before dispatch.

### Collision and lock concerns

1. Worktree paths are independent per PR (`.worktrees/<repo>/<slug>`).
2. The push lock `~/.claude/agent-comms/locks/git-push.lock` serializes pushes across all
   parallel agents (mkdir-based poll-and-retry). This is correct - it prevents simultaneous
   force-pushes from corrupting refs.
3. Shared base files: when one PR merges first, later PRs rebase to pick up the new base.
4. gh API rate limits: parallel calls are typically fine (5000 req/hr authenticated).

## Phase 0 - Inputs

- Skill arguments: zero or more PR numbers (positional). Zero -> auto-discover + gate.
  Single -> Phase 1-6 in main session. Multi -> fan-out (cap 6/wave).
- Repo derived from cwd (`gh repo view --json nameWithOwner --jq .nameWithOwner`).
- Per the user's standing rules: delegate the actual implementation to a Sonnet subagent;
  no `--yes` on project CLIs; no mid-loop confirmation prompts (EXCEPTION: the zero-arg
  auto-discovery y/n gate is a pre-loop guard, allowed).
- The per-PR agent prompt template lives at `per-pr-agent-prompt.template.md` (same dir)
  with `{{PR_NUMBER}}` substituted. **If that file is absent, fall back to:** "Run Phase 1
  through Phase 6 of this SKILL.md for PR #<N>. Honor every hard constraint in the body."

## Phase 1 - Triage (read-only)

1. Read PR metadata: `gh pr view <N> --json title,headRefName,headRefOid,baseRefName,mergeStateStatus,state,isDraft`.
   If `state != OPEN`, report and stop. If `isDraft`, surface but continue.
2. Find the worktree: `git -C <repo-root> worktree list`, match by branch name. The on-disk
   path may not match the branch verbatim - git is path-agnostic; only the branch matters.
   If no worktree exists, create one off the existing remote branch:
   `git -C <repo-root> fetch origin <branch>` then
   `git -C <repo-root> worktree add .worktrees/<repo>/<slug> <branch>` (do NOT create a fresh branch).
3. Pull CR signal into `/tmp/claude/`:
   ```
   gh api repos/<owner>/<repo>/pulls/<N>/reviews \
     --jq '.[]|select(.user.login=="coderabbitai[bot]")|{at:.submitted_at,state:.state,body:.body}' \
     > /tmp/claude/cr-<N>-reviews.json
   gh api repos/<owner>/<repo>/pulls/<N>/comments --paginate \
     --jq '[.[]|select(.user.login=="coderabbitai[bot]")|{at:.created_at,path:.path,line:.line,body:.body}]|sort_by(.at)|reverse' \
     > /tmp/claude/cr-<N>-inline.json
   ```
   Read both. The most recent review body has CR's headline findings + the standing
   instruction (verify each finding against current code; fix only still-valid ones).
4. Decide entry phase:
   - `mergeStateStatus IN {DIRTY, CONFLICTING}` -> Phase 2 first (rebase before addressing CR).
   - Otherwise -> Phase 3 (address CR findings).
   - If neither -> Phase 5/6 and report status.

### Issue <-> PR link detection (load-bearing on non-default base branches)

When a repo's PRs target a **non-default base branch** (e.g. CUDly PRs target
`feat/multicloud-web-frontend`, not `main`), GitHub does NOT auto-close the linked issue
and the GraphQL `closingIssuesReferences` field is **EMPTY** - so you cannot rely on either
to find the issue a PR closes or the PR that serves an issue. Detect the link by inspecting
the **PR body** for a reference to the issue number, in this precedence:

1. A closing keyword adjacent to the number: `(?i)(clos|fix|resolv|address|implement|complet)\w*\W{0,12}#<issue>`.
2. A `(#<issue>)` parenthetical.
3. An exact title-mirror (PR title == issue title).

**Do NOT trust timeline cross-references / mentions** to establish the link: a big audit PR
that says "tracked in issue #1083" or "deferred from #1047" creates a cross-reference WITHOUT
closing that issue. The proximity-to-a-closing-keyword test above is what distinguishes a real
close from a prose mention. (This is the exact detection the `issue-pr-autopilot` routine uses
for its `pr-created` / `pr-merged` label state machine, and for mirroring the closing issue's
triage rubric onto the PR.)

### Active-human guard (before you touch a PR you did not open)

If the PR's most recent commit is by a **human** (not you) and is NEWER than the latest
`coderabbitai[bot]` review, the author is actively working it - DEFER this PR and do not race
their edits. Re-evaluate on a later pass once they have gone quiet. (Critical when iterating
across PRs you did not author, e.g. autopilot Phase 3 advancing human PRs.)

## Phase 2 - Rebase (only when there are conflicts)

1. `git -C <worktree> fetch origin <base-branch>` then `git -C <worktree> rebase origin/<base-branch>`.
2. Resolve conflicts. Common shapes:
   - Terraform tfvars (dev/staging/prod): usually additive - keep both blocks.
   - `internal/config/interfaces.go` + mocks: when base adds an interface method, satisfy it
     across all mock files.
   - `frontend/src/state.ts`, `pkg/common/types.go`: usually additive - keep both.
   - Service-client import-line collisions: keep both imports.
   - Migration-number collisions: only fail in CI (`pre-commit --all-files` on the merge ref);
     on UNSTABLE, renumber the new migration via `git mv` to the next free slot vs base.
3. STOP and report if a conflict involves renamed-field or semantic divergence. Don't silently
   take `--ours`/`--theirs`.
4. Verify post-rebase from the worktree root: `gofmt -l ./...`, `go vet ./...`, `go build ./...`,
   `go test ./<touched-pkg>/... -count=1`, `terraform -chdir=<dir> validate` if iac touched.
   (Frontend: `npm test -- <pattern>` + `npx tsc --noEmit`.)
5. Push the rebase (lock-protected force-push):
   ```
   [ -f ~/.claude/agent-comms/locks/git-push.lock ] && rm ~/.claude/agent-comms/locks/git-push.lock
   until mkdir ~/.claude/agent-comms/locks/git-push.lock 2>/dev/null; do sleep 2; done
   git -C <worktree> push --force-with-lease
   rmdir ~/.claude/agent-comms/locks/git-push.lock
   ```
   ONLY ever push the PR's own branch - never a shared `feat/*` branch or `main`.
6. If there were real content conflicts, drop a short rebase note as a PR comment.

## Phase 3 - Address CR findings

Per CR's standing instruction: verify each finding against current code; fix only still-valid
issues, skip the rest with a brief reason.

1. Triage each finding into one of four buckets:
   - **Fix in this PR**: still present AND in scope. Add to the commit batch.
   - **Skip - stale**: already addressed in a later commit. Confirm via
     `git log --oneline <base>..HEAD -- <path>`. Note the fixing SHA + reason in the PR comment.
   - **Skip - out-of-scope**: file a separate GitHub issue with the full triage label set
     (`triaged,priority/p[0-3],severity/<s>,urgency/<u>,impact/<i>,effort/<e>,type/<t>`), cite
     CR's comment URL. Never silently drop a finding.
   - **Skip - CR misread**: brief reason citing the actual line content.
2. Apply each fix minimally. Common shapes: extract a helper for a gocyclo violation; add the
   missing field-copy + a regression test for a field-drop bug; `require.NoError` for ignored
   err; `context.Background()` for nil ctx; `bytes.NewReader(bs)` over
   `strings.NewReader(string(bs))`; ctx-aware sleep in retry loops; distinguish error-state from
   empty-state in UI; `t.Cleanup(func(){ mock.AssertExpectations(t) })` for missing assertions.
3. Regression test discipline: any real-bug finding gets a regression test that **replicates
   the REAL failing scenario** (same inputs / data shape that reproduced the bug, not a narrower
   unit that can stay green while the bug lives) and FAILS pre-fix / PASSES post-fix. For the
   highest-severity finding, stash-verify (stash the prod change, run the test, confirm failure,
   stash pop). **Green tests are NOT proof the feature works** (CLAUDE.md §4): after fixing,
   trace the actual end-to-end path the user triggers and confirm the observed behavior matches
   expected. For any finding that a prior "fix" already claimed to resolve, verify adversarially
   against the committed code before declaring it done.
4. Atomic commits grouped per-file or per-concern. Conventional commits `type(scope): subject`.
   NO `Co-Authored-By: claude-flow`; NO Anthropic/Claude mentions; NO em-dashes. Write the
   message to a unique file under `/tmp/claude/` and `git commit -F <path>`. NO heredoc `-m`.
5. Verify before push (every check must pass): `gofmt -l ./...`, `go vet ./...`, `go build ./...`,
   `go test ./<touched-pkg>/... -count=1`; frontend `npm test` + `npx tsc --noEmit` if touched.
   Pre-commit hooks run on `git commit -F` and MUST pass - NEVER `--no-verify`. On transient
   `tflint --init` 403 or a stash collision, sleep 2 min and retry.

## Phase 4 - Push + re-ping CR

1. Acquire the push lock (same mkdir protocol as Phase 2.5).
2. Push: `git push` (additive) or `git push --force-with-lease` (rebased).
3. Release the lock (`rmdir`).
4. Post a per-finding-summary comment + re-ping CR in one comment. Before posting, run the
   dedup guard: skip the post if a user-authored `@coderabbitai review` was already sent to this
   PR in the last 5 minutes. The trailing `@coderabbitai review` triggers the next CR pass -
   never the `resolve` shortcut. Echo `pinged #<N>` after a successful post so broken-iterator
   bugs are visible in transcript output.
5. Arm the CI watcher per git-workflow.md's post-push rule (launch one background `Agent` per
   workflow run, named `ci-watch-<short-sha>-<workflow-slug>`, that fixes CI failures
   autonomously via follow-up commits coordinated through the same push lock).

## Phase 5 - Iterate (or terminate)

1. Don't block-poll the CR response. Either schedule a wake-up (`ScheduleWakeup`, 5-15 min) that
   re-invokes `/pr-iterate <N>`, OR report "Pushed pass-<K> fixes + re-pinged CR; re-invoke after
   CR responds (typically 5-15 min)."
2. On re-entry, re-pull CR signal (Phase 1.3). If the most recent CR review says
   `Actionable comments posted: 0` (or LGTM) -> Phase 6. If new findings -> loop to Phase 3.
3. CI gate: `gh pr checks <N>` should be green. Transient `tflint --init` 403 -> `gh run rerun
   <run-id> --failed`. Real gocyclo -> extract helpers. Migration collision (UNSTABLE) ->
   renumber via `git mv`. Stash collision -> sleep 2 min, retry; do NOT `--no-verify`.

## Phase 5b - CR rate-limit handling

After every `@coderabbitai review` ping, CR may respond with a rate-limit message instead of a
review. Two asymmetric cases:

| State | Trigger | Action |
|---|---|---|
| A. Normal review | CR posts a review body within ~5 min | Phase 3 (normal loop) |
| B. Triggered, no body | "Review triggered" but no review after 10 min | Keep polling |
| C. Per-hour rate limit | "exceeded the limit ... Please wait **N minutes** before retrying" | Parse duration, schedule wakeup retry |
| D. Org credits exhausted | "out of usage credits" / "billing" (NO duration) | STOP and report; do NOT retry |
| E. No activity | No CR comment of any kind after 15 min | Report "CR unreachable"; no auto-retry |

### Case A (per-hour rate limit) - AUTO-RETRYABLE
Parse the wait duration (hours/minutes/seconds). Cap at 3900s. Add a 60s buffer. Write the
wave-level marker file, then `ScheduleWakeup({delay_seconds, reason, prompt: "/pr-iterate <N>"})`.
Do NOT keep the session open polling. Fallback if ScheduleWakeup is unavailable: emit a clear
human-readable handoff with the local resume time.

### Case B (org credits exhausted) - NOT AUTO-RETRYABLE
Stop. Post a one-time PR comment (NOT a re-ping). Write the `BILLING_BLOCKED` marker. Mark the
PR `ready-for-merge-without-CR` (if CLEAN + green) or `blocked-on-cr-billing`.

### Wave-level coordination (multi-PR)
CR's rate-limit is org-level. Marker file `~/.claude/agent-comms/cr-rate-limit-deadline.txt`:
Case A content = the UTC ISO-8601 deadline; Case B content = literal `BILLING_BLOCKED`. Every
agent checks the marker before a re-ping; if still within the window, skip the ping and schedule
a wakeup. Atomic `mv` write to avoid sibling races. First agent after the deadline removes the
stale Case-A marker; Case-B markers persist until manually cleared.

### Rate-limit recovery review command
On rate-limit recovery, re-request with `@coderabbitai full review` (NOT the incremental
`@coderabbitai review`), because a throttled incremental pass silently skips the in-flight
commits and yields a false-clean.

## Phase 6 - Cleanup / report (never self-merge)

Once ALL THREE are true: CR's latest review says `Actionable comments posted: 0`; CI green
(`gh pr checks <N>`); `mergeStateStatus == CLEAN`. Then:
1. Report: "PR #<N> is ready for your merge. Summary: <X> CR findings addressed across <Y>
   commits over <Z> CR passes. CI green. Merge state CLEAN. Follow-up issues filed: <#a,#b>."
2. Do NOT self-merge - leave it for human approval.
3. Worktree cleanup is the user's call after merge.

## Hard constraints (always)

- NEVER post `@coderabbitai review` without first running the dedup guard (no user-authored
  re-ping in the last 5 minutes).
- NEVER dispatch a list-iteration agent (iterating over N>1 PRs/issues with side-effects) on
  `model: haiku` - use sonnet or opus (haiku mis-indexes bash arrays and re-targets the same item).
- ALWAYS `echo "pinged #<N>"` after each successful re-ping when iterating multiple PRs.
- NEVER `git push --no-verify` or any pre-commit bypass.
- NEVER heredoc `git commit -m` - always `git commit -F <path>` from `/tmp/claude/`.
- NEVER add `Co-Authored-By: claude-flow` or any Anthropic/Claude mention to commits.
- NEVER use em-dashes (U+2014) in code, comments, commit messages, PR bodies, or comments.
- NEVER self-merge - the user merges.
- NEVER silently drop a CR finding - always fix / mark-stale-with-SHA / file-follow-up-issue.
- NEVER use `@coderabbitai resolve` - always `@coderabbitai review` (or `full review` on recovery).
- NEVER push to a shared branch (e.g. `feat/*`, `main`) - only the PR's own branch.
- NEVER pass `--yes` to any project CLI.
- ALWAYS lock the push via `mkdir ~/.claude/agent-comms/locks/git-push.lock`.
- ALWAYS delegate the actual implementation to a Sonnet subagent; main session plans/dispatches.
- ALWAYS file out-of-scope CR findings as separate triaged issues with the full label set.
- ALWAYS run pre-commit hooks; on transient `tflint --init` 403 or stash collision, sleep 2 min, retry.
- ALWAYS mirror the closing-issue triage labels onto the PR if not already mirrored. On a
  non-default base branch, find that closing issue via the body-reference detection above, not
  `closingIssuesReferences`.
- ALWAYS strip any runtime-injected attribution footer (`Generated by Claude Code` /
  `claude.ai/code/session...`) from any PR body you edit - it violates the no-Claude-mention rule.
- ALWAYS wrap `gh` / `git` network calls in a small retry loop (a few attempts with a short
  sleep) - transient TLS-handshake / i-o timeouts are common and must not abort a run.
- In a constrained/remote runtime (no `Agent` tool): do NOT spawn watchers or subagents; rely
  on the caller's re-invocation cadence as the structural watcher (see Runtime-adaptive section).

## Reconciliation sweep (before declaring PR work done)

Before declaring the iteration done for a session, sweep every PR you touched: each must be in a
clean terminal CR state (latest review `Actionable comments posted: 0`, CI green, `mergeStateStatus
CLEAN`), OR still have a live follow-up mechanism (a local `cr-watch`, or - in the scheduled
runtime - remain in the tracked in-flight set so the next fire re-examines it), OR be closed.
Re-arm / re-ping any that silently fell out of the loop. A re-ping with no follow-up mechanism is
a defect.
