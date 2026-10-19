---
name: pr-iterate
description: >-
  Drive existing PRs to merge-ready: fix conflicts, address CodeRabbit findings, re-ping CR until
  clean and CI green. Triggers: "address CR on #N", "fix conflicts on #N", "iterate on #N",
  "iterate on all my PRs". No argument fans out across all eligible open PRs behind a confirmation
  gate. Never opens or merges.
---

<!-- NOTE: committed to dotclaude so the scheduled issue-pr-autopilot remote agent (which
     clones dotclaude) can read the CR-iteration flow. Phase 0 documents a fallback for
     environments where per-pr-agent-prompt.template.md is unavailable. -->

# pr-iterate - automate the recurring PR-iteration loop

Drives an existing PR to merge-ready. Read this file end-to-end first. The skill NEVER opens a new PR
and NEVER merges: it resolves conflicts, addresses CR findings, pushes, re-pings CR, and reports when
a PR is ready for the human to merge.

## Agent layout

Set the model on every dispatch (tiers: `subagent-strategy`; Claude Code: `Agent`/`Task` with `model`, `ScheduleWakeup`; other hosts: `skills/HOSTS.md`). Per PR, one orchestrator agent on the
planning tier does Phase 1 triage, CR parsing, rebase and conflict judgement, per-finding disposition
and the Phase 4 push inline. It dispatches one fresh implementer on the implementation tier only when
a decided fix is non-trivial (a PR's batch of fixes goes to the same implementer; a mechanical
one-liner the orchestrator applies itself). Phase 5 waits run as a background shell per `cr-loop` §2,
never a polling agent; the orchestrator parses the new review when the shell exits (`pr-orchestration`
§"Cost control").

## Runtime-adaptive operation (local session vs constrained/remote agent)

Detect the runtime by tool availability (can you spawn subagents?):

- **Local interactive session** (can spawn subagents, persistent, can schedule a wakeup): use everything
  below as written (per-PR orchestrators, `cr-watch-<pr-#>` / `ci-watch-<sha>-<wf>` watchers, wakeups
  for CR responses). This is the default.
- **Constrained / remote agent** (NO subagent tool, bounded single session, e.g. the scheduled
  `issue-pr-autopilot` cloud run): you cannot spawn subagents and background processes die when the
  run ends. So:
  - Do every phase INLINE, single-threaded. No fan-out, per-PR orchestrators, watchers or
    scheduled wakeups.
  - The **atomic-coupling invariant** (never request a CR review without a watcher that acts on the
    response) is satisfied **structurally** by the caller's re-invocation cadence: a PR you re-ping
    must stay in the tracked in-flight set so the NEXT scheduled fire re-examines it. The cron IS the
    durable watcher. Only re-ping a PR that remains tracked; never re-ping one you are about to drop.
  - Replace "schedule a wakeup and exit" with "do a single bounded in-run wait (~5 min) then move on;
    the next fire continues."
  - The push lock is irrelevant (single agent) but harmless; skip it if the lock tooling is missing.

Triage buckets, fix shapes and hard constraints apply identically in both runtimes.

## Multi-PR fan-out mode

One OR many PR numbers as positional args. With >1, the main session **fans out one orchestrator
agent per PR in a single parallel-dispatch message**.

### Arg-shape acceptance (parsed in Phase 0)

| Shape | Example | Notes |
|---|---|---|
| Zero args | `/pr-iterate` | Auto-discover eligible open PRs in the current repo; confirmation gate before fan-out. |
| Single PR | `/pr-iterate 922` | Single-PR path. |
| Space-separated | `/pr-iterate 922 912 889` | Fan-out, no gate (user named the PRs). |
| Comma-separated | `/pr-iterate 922,912,889` | Strip whitespace around commas. |
| `#`-prefixed | `/pr-iterate #922,#912` | Strip leading `#`. |
| Range | `/pr-iterate 922-925` | NOT supported: error, list each number. |

Parsing: split on `[\s,]+`, strip leading `#`, validate each is `^[0-9]+$`; reject the whole input if
any token fails. Zero tokens triggers auto-discovery.

### Zero-arg auto-discovery (bare `/pr-iterate`)

1. Detect repo: `gh repo view --json nameWithOwner --jq .nameWithOwner`. On error stop with "No
   GitHub repo detected. Pass an explicit PR# list."
2. Resolve user: `gh api user --jq .login`.
3. Enumerate: `gh pr list --state open --json number,title,mergeStateStatus,isDraft,author,headRefName,baseRefName,updatedAt --limit 100`.
4. Eligible ("ours to iterate") = ALL of:
   - `isDraft == false`; `author.login == <current-user>`.
   - has-work: `mergeStateStatus IN {DIRTY, UNSTABLE, CONFLICTING, BLOCKED, UNKNOWN, BEHIND}`, OR
     `CLEAN` with the latest CR review body showing `Actionable comments posted: N`, `N > 0`, not yet
     addressed in a later commit.
   - Excluded: drafts, others' PRs, CLEAN with no pending CR actionables.
5. Stack detection: if an eligible PR's base equals another eligible PR's head, it is a stack;
   proceed bottom-up.
6. Confirmation gate (zero-arg only): show the eligibility list + parallelism plan, prompt y/n.
7. Empty eligibility: report "No eligible PRs" and exit.

### Parallelism cap and wave logic

**Hard cap: 4 parallel subagent dispatches per wave** (`pr-orchestration` §"Cost control"). N<=4 with no
stacks: one wave. N>4: sequential waves of 4. Stacks: bottom-of-stack + non-stacked first, then up.

Fan out when each PR is on a divergent branch with an isolated worktree and none depends on another's
merge order. Serialize stacks (#A -> #B rebased on #A -> #C rebased on #B), one at a time, bottom-up.

### Main-session pre-flight (read-only, before any dispatch)

1. Validate every PR: `gh pr view <N> --json state,mergeStateStatus,baseRefName,headRefName,title,isDraft`
   (serialize reads if N >= 10).
2. Stop with a clear error if ANY PR is `state != OPEN`.
3. Stack-detect across the input list.
4. Print a summary line before dispatch.

### Collision and lock concerns

1. Choose new worktree names with the `worktrees` skill: issue-linked work uses
   `"$HOME/.claude/worktrees/<repo-name>-issue-<issue-number>-<slug>"`; use its no-issue fallback
   when no issue is associated. Preserve existing paths. Never place a persistent PR worktree
   under `/tmp`, `$TMPDIR`, or another reboot-cleared directory.
2. The push lock `/tmp/agent-locks/<repo>-git-push-<branch-key>.lock` (`<branch-key>` = branch name
   with every `/` replaced by `-`; `flock`, or `lockf` on macOS, per `multi-agent-comms`) serializes
   every push to that PR branch, including `ci-watch` fix pushes (same lock). Different PRs push in
   parallel.
3. When one PR merges first, later PRs rebase onto the new base.
4. gh API limits: parallel calls are fine (5000 req/hr authenticated).

## Phase 0 - Inputs

- Args: zero or more PR numbers. Zero -> auto-discover + gate. Single -> Phases 1-6 in the main
  session. Multi -> fan-out (cap 4/wave).
- Repo derived from cwd (`gh repo view --json nameWithOwner --jq .nameWithOwner`).
- No mid-loop confirmation prompts (the zero-arg y/n gate is a pre-loop guard, allowed).
- The per-PR agent prompt template is `per-pr-agent-prompt.template.md` (same dir) with
  `{{PR_NUMBER}}` substituted. **If absent, fall back to:** "Run Phase 1 through Phase 6 of this
  SKILL.md for PR #<N>. Honor every hard constraint in the body."

## Phase 1 - Triage (read-only)

1. `gh pr view <N> --json title,body,closingIssuesReferences,headRefName,headRefOid,baseRefName,mergeStateStatus,state,isDraft`.
   If `state != OPEN`, report and stop. If `isDraft`, surface but continue.
2. Find the worktree: `git -C <repo-root> worktree list`, match by branch (path may differ). If none,
   first resolve the linked issue (`closingIssuesReferences`, or the PR body per "Issue <-> PR link
   detection" below on a non-default base), then create one off the existing remote branch (do NOT
   create a fresh branch):
   `git -C <repo-root> fetch origin <branch>`, `mkdir -p "$HOME/.claude/worktrees"`,
   `git -C <repo-root> worktree add "$HOME/.claude/worktrees/<repo-name>-issue-<issue-number>-<slug>" <branch>`.
   This example has a linked issue; use `<repo-name>-no-issue-<slug>` per the `worktrees` skill only
   after both checks find none.
3. Pull CR signal into `/tmp/claude/`:
   ```
   gh api repos/<owner>/<repo>/pulls/<N>/reviews \
     --jq '.[]|select(.user.login=="coderabbitai[bot]")|{at:.submitted_at,state:.state,body:.body}' \
     > /tmp/claude/cr-<N>-reviews.json
   gh api repos/<owner>/<repo>/pulls/<N>/comments --paginate \
     --jq '[.[]|select(.user.login=="coderabbitai[bot]")|{at:.created_at,path:.path,line:.line,body:.body}]|sort_by(.at)|reverse' \
     > /tmp/claude/cr-<N>-inline.json
   ```
   Read both. The latest review body carries CR's headline findings and the standing instruction:
   verify each finding against current code; fix only still-valid ones.
4. Entry phase: `mergeStateStatus IN {DIRTY, CONFLICTING}` -> Phase 2 first; otherwise Phase 3; if
   neither applies -> Phase 5/6 and report status.

### Issue <-> PR link detection (matters on non-default base branches)

When PRs target a **non-default base branch** (e.g. CUDly PRs target `feat/multicloud-web-frontend`),
GitHub does NOT auto-close the linked issue and GraphQL `closingIssuesReferences` is **EMPTY**. Detect
the link from the **PR body**, in this precedence:

1. A closing keyword adjacent to the number: `(?i)(clos|fix|resolv|address|implement|complet)\w*\W{0,12}#<issue>`.
2. A `(#<issue>)` parenthetical.
3. An exact title-mirror (PR title == issue title).

**Do NOT trust timeline cross-references / mentions**: a PR saying "tracked in issue #1083" or
"deferred from #1047" cross-references without closing. (The `issue-pr-autopilot` routine uses this
same detection for its `pr-created` / `pr-merged` label state machine and for mirroring the closing
issue's triage rubric onto the PR.)

### Active-human guard (before touching a PR you did not open)

If the PR's most recent commit is by a **human** (not you) and is NEWER than the latest
`coderabbitai[bot]` review, the author is actively working it: DEFER, do not race their edits;
re-evaluate on a later pass.

## Phase 2 - Rebase (only when there are conflicts)

1. `git -C <worktree> fetch origin <base-branch>` then `git -C <worktree> rebase origin/<base-branch>`.
2. Resolve conflicts. Common shapes:
   - Terraform tfvars (dev/staging/prod): usually additive, keep both blocks.
   - `internal/config/interfaces.go` + mocks: when base adds an interface method, satisfy it across
     all mock files.
   - `frontend/src/state.ts`, `pkg/common/types.go`: usually additive, keep both.
   - Service-client import-line collisions: keep both imports.
   - Migration-number collisions: fail only in CI (`pre-commit --all-files` on the merge ref); on
     UNSTABLE, renumber the new migration via `git mv` to the next free slot vs base.
3. STOP and report if a conflict involves renamed-field or semantic divergence. Never silently take
   `--ours`/`--theirs`.
4. Verify post-rebase from the worktree root: `gofmt -l ./...`, `go vet ./...`, `go build ./...`,
   `go test ./<touched-pkg>/... -count=1`, `terraform -chdir=<dir> validate` if iac touched
   (frontend: `npm test -- <pattern>` + `npx tsc --noEmit`).
5. Push the rebase under the lock (macOS: `lockf -t 600` instead of `flock -w 600`; the lock drops
   when the push exits):
   ```
   mkdir -p /tmp/agent-locks
   flock -w 600 /tmp/agent-locks/<repo>-git-push-<branch-key>.lock git -C <worktree> push --force-with-lease
   ```
   ONLY ever push the PR's own branch, never a shared `feat/*` branch or `main`.
6. For real content conflicts, add a short rebase note as a PR comment.

## Phase 3 - Address CR findings

Verify each finding against current code; fix only still-valid issues, skip the rest with a reason.

1. Triage each finding:
   - **Fix in this PR**: still present AND in scope; add to the commit batch.
   - **Skip - stale**: already addressed later. Confirm via `git log --oneline <base>..HEAD -- <path>`;
     note the fixing SHA + reason in the PR comment.
   - **Skip - out-of-scope**: file a separate issue with the full triage label set
     (`triaged,priority/p[0-3],severity/<s>,urgency/<u>,impact/<i>,effort/<e>,type/<t>`), citing CR's
     comment URL. Never silently drop a finding.
   - **Skip - CR misread**: brief reason citing the actual line content.
2. Apply each fix minimally. Common shapes: extract a helper for a gocyclo violation; add the missing
   field-copy + regression test for a field-drop bug; `require.NoError` for ignored err;
   `context.Background()` for nil ctx; `bytes.NewReader(bs)` over `strings.NewReader(string(bs))`;
   ctx-aware sleep in retry loops; distinguish error-state from empty-state in UI;
   `t.Cleanup(func(){ mock.AssertExpectations(t) })` for missing assertions.
3. Regression tests: any real-bug finding gets one that **replicates the REAL failing scenario** (same
   inputs / data shape, not a narrower unit) and FAILS pre-fix / PASSES post-fix. For the
   highest-severity finding, stash-verify (stash the prod change, run the test, confirm failure, pop).
   Green tests are NOT proof (AGENTS.md §4): trace the real end-to-end path after fixing. For a
   finding a prior "fix" already claimed to resolve, verify adversarially against the committed code.
4. Atomic commits per file or concern, `type(scope): subject`. NO `Co-Authored-By: claude-flow`; keep the
   harness attribution trailer; NO em-dashes. Write the message to a unique file under `/tmp/claude/`
   and `git commit -F <path>`. NO heredoc `-m`.
5. Verify before push (all must pass): `gofmt -l ./...`, `go vet ./...`, `go build ./...`,
   `go test ./<touched-pkg>/... -count=1`; frontend `npm test` + `npx tsc --noEmit` if touched.
   Pre-commit hooks run on `git commit -F` and MUST pass; NEVER `--no-verify`. On a transient
   `tflint --init` 403 or stash collision, sleep 2 min and retry.

## Phase 4 - Push + re-ping CR

1. Push under the push lock (same `flock` / `lockf` form as Phase 2.5): `git push` (additive) or
   `git push --force-with-lease` (rebased). The lock drops when the push exits.
2. Post one comment with the per-finding summary + re-ping. First run the dedup guard: skip the post
   if a user-authored `@coderabbitai review` was already sent to this PR in the last 5 minutes. The
   trailing `@coderabbitai review` triggers the next pass; never the `resolve` shortcut. Echo
   `pinged #<N>` after a successful post.
3. Arm the CI watcher per the `ci-watch` skill (one background shell watcher per workflow run; a fix
   agent repairs failures via follow-up commits through the same push lock).

## Phase 5 - Iterate (or terminate)

1. Don't block-poll the CR response. Either schedule a wakeup (5-15 min) re-invoking `/pr-iterate <N>`,
   OR report "Pushed pass-<K> fixes + re-pinged CR; re-invoke after CR responds (typically 5-15 min)."
2. On re-entry, re-pull CR signal (Phase 1.3). Latest review `Actionable comments posted: 0` (or
   LGTM) -> Phase 6. New findings -> Phase 3, within the CR round cap in `cr-loop` §3 (escalate
   instead of a round 4) and the `AGENTS.md` §1 2-pass rule.
3. CI gate: `gh pr checks <N>` green. Transient `tflint --init` 403 -> `gh run rerun <run-id> --failed`.
   Real gocyclo -> extract helpers. Migration collision (UNSTABLE) -> renumber via `git mv`. Stash
   collision -> sleep 2 min, retry; do NOT `--no-verify`.

## Phase 5b - CR rate-limit handling

Detection (both rate-limit wordings, billing messages, no-review and unreachable states), cooldown
parsing and the `@coderabbitai full review` recovery are `cr-loop` §2; retry scheduling is
`rate-limit-retry`. This section adds only the wave-level marker that stops N parallel agents from
each re-tripping an org-level limit.

- **Per-hour rate limit (auto-retryable):** write the deadline marker, then
  a wakeup (delay, reason, prompt `/pr-iterate <N>`) for the `cr-loop` §2 cooldown.
  Do NOT keep the session open polling. Without wakeup support, emit a human-readable handoff with
  the local resume time.
- **Org credits exhausted (not auto-retryable):** stop. Post a one-time PR comment (NOT a re-ping).
  Write the `BILLING_BLOCKED` marker. Mark the PR `ready-for-merge-without-CR` (if CLEAN + green) or
  `blocked-on-cr-billing`.

### Wave-level coordination (multi-PR)
CR's rate limit is org-level. Marker file `~/.claude/agent-comms/cr-rate-limit-deadline.txt`: per-hour
content = the UTC ISO-8601 deadline; credits content = literal `BILLING_BLOCKED`. Every agent checks
the marker before a re-ping; within the window, skip the ping and schedule a wakeup. Create the
directory first (`mkdir -p ~/.claude/agent-comms`), then write a temp file in that same directory and
`mv` it over the marker (a temp file on another filesystem such as `/tmp` makes `mv` a non-atomic
copy). The first agent after the deadline removes the stale deadline marker; `BILLING_BLOCKED` persists
until manually cleared.

## Phase 6 - Cleanup / report (never self-merge)

When ALL THREE hold: CR's latest review says `Actionable comments posted: 0`; CI green
(`gh pr checks <N>`); `mergeStateStatus == CLEAN`:
1. Report: "PR #<N> is ready for your merge. Summary: <X> CR findings addressed across <Y> commits
   over <Z> CR passes. CI green. Merge state CLEAN. Follow-up issues filed: <#a,#b>."
2. Do NOT self-merge; leave it for human approval.
3. Worktree cleanup is the user's call after merge.

## Hard constraints (always)

Global rules (commit format, pre-commit, em-dashes, self-merge, push lock, label mirror) come from
`CLAUDE.md` and the skills it routes to. Specific to this loop:

- NEVER post `@coderabbitai review` without first running the dedup guard (no user-authored re-ping in
  the last 5 minutes); use `full review` on rate-limit recovery.
- NEVER dispatch a list-iteration agent (N>1 PRs/issues with side-effects) on `model: sonnet` at low
  effort; use medium effort (low effort has re-targeted the same item when iterating lists).
- ALWAYS `echo "pinged #<N>"` after each successful re-ping when iterating multiple PRs.
- NEVER silently drop a CR finding: fix / mark-stale-with-SHA / file-follow-up-issue.
- On a transient `tflint --init` 403 or pre-commit stash collision, sleep 2 min and retry.
- NEVER push anything but the PR's own branch (never `main` or a shared `feat/*` branch).
- ALWAYS mirror the closing-issue triage labels onto the PR if not already mirrored. On a non-default
  base branch, find the closing issue via the body-reference detection above, not
  `closingIssuesReferences`.
- NEVER edit a runtime-injected attribution footer (`Generated with Claude Code` /
  `claude.ai/code/session...`) out of a PR body you touch; the runtime adds it on purpose. Commit
  messages likewise keep the harness attribution trailer.
- ALWAYS wrap `gh` / `git` network calls in a small retry loop (a few attempts, short sleep);
  transient TLS-handshake / i-o timeouts must not abort a run.
- In a constrained/remote runtime (no subagent tool): do NOT spawn watchers or subagents; rely on the
  caller's re-invocation cadence as the structural watcher (see Runtime-adaptive section).

## Reconciliation sweep (before declaring PR work done)

Run the `pr-lifecycle` skill's reconciliation sweep over every PR you touched before declaring the
iteration done for a session.
