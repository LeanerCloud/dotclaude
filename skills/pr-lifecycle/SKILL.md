---
name: pr-lifecycle
description: PR rules, the mandatory PR-creation checklist (label mirror, CR trigger, three watcher
  classes), waiting for human merge, post-merge verification, the issue comment, and filing
  follow-up issues. Invoke when opening a PR or driving one to merge.
---

# Pull requests - creation checklist through post-merge follow-up

CodeRabbit review rounds are the `cr-loop` skill; driving an existing PR to merge-ready is `pr-iterate`.

## PR shape

- **Small**: aim for <=400 lines of meaningful change; large PRs get shallow reviews.
- **One concern per PR**: no refactor mixed with behaviour change, or fix with feature; each PR
  independently revertable.
- Title in conventional-commit format (changelog fallback). Description says *why*, not just *what*.
- Feature branches for non-trivial work, named `type/short-description` (`feat/oauth-login`,
  `fix/nil-pointer-api`).

## Post-PR review loop (CodeRabbit + human merge)

Opening a PR is not the end: the lifecycle runs in background watchers while the main session moves on.
CI watchers (`ci-watch` skill, §"Post-push CI watcher") handle build/test/deploy; the watchers below
handle human-and-bot review. Where a project uses no review bot, skip steps 1-3 and start at §4.

> For **several PRs at once**, the orchestration layer (role split, coordination, the four-gate merge
> check, false-clean traps) is the `pr-orchestration` skill.

### Immediate PR-creation checklist - every step, every time

Within ~30 seconds of `gh pr create` returning, the main session MUST have done ALL of these; skipping
one leaves part of the lifecycle unattended.

1. **Label mirror**: `gh pr edit <#> --add-label <labels-from-closing-issue>` (priority/severity/
   urgency/impact/effort/type + `triaged`, per `triage-labels`).
2. **Trigger CodeRabbit** per `cr-loop` §1, bound atomically to step 4 (invariant below).
3. **CI watcher(s)**: one background shell watcher per Actions run that fired on the push (`ci-watch`
   skill), labelled `ci-watch-<short-sha>-<workflow-slug>-<run-id>`. They handle CI failures only and do
   NOT read CodeRabbit's inline comments.
4. **CR watcher**: ONE `cr-watch-<pr-#>` shell wait per `cr-loop` §2 (wait, triage hand-off and model
   tiers live there); without it CR findings sit unread.
5. **Merge watcher**: arm ONCE, at PR creation, ONE background shell watcher `merge-watch-<pr-#>` (a background shell, not an agent;
   Claude Code: Bash `run_in_background: true`, `TaskStop` to reap; other hosts: `skills/HOSTS.md`) per §4: it polls `gh pr view <#> --json state` every 5-10
   minutes and exits on MERGED or CLOSED. When it exits, spawn an agent to run the §5 post-merge
   verification and file the §7 follow-up issues, so the chain runs without main-session intervention
   and nothing polls at agent cost.

Total: **3+ background watchers per PR** (one per CI run + cr-watch + merge-watch, all shell scripts).
Do not collapse them (different cadences and terminal conditions) and do not omit cr-watch because "CR is
just a bot": its findings weigh like a human reviewer's and unaddressed threads block merges that gate on
review-bot approval. **Common failure**: spawning only the CI watcher. It reports "all checks passed" once
CR's check goes green, while the findings inside the review are invisible to it.

> **Atomic-coupling invariant.** Never post a CodeRabbit trigger unless that PR's `cr-watch-<pr-#>` is
> already live or spawned in the same response: spawn the watcher first, then trigger. N triggers across
> N PRs need N watchers in the same message.

**Reconciliation sweep (session-end gate; single home for this rule).** Before declaring PR work done for
a session, and whenever you notice unaddressed CR comments, list every open PR you authored or touched
(`gh pr list --author @me --state open`, or the session's known PR set). Each must be exactly one of:

- (a) a live follow-up mechanism: a `cr-watch-<pr-#>` polling it, or, in a scheduled runtime, membership
  in the tracked in-flight set the next fire re-examines;
- (b) a clean terminal CR state: latest CR review has zero Actionable, every Nitpick fixed or justified
  (for merge-readiness also CI green and `mergeStateStatus` CLEAN);
- (c) closed.

Any PR with CR findings or an un-responded trigger and no live watcher gets a fresh `cr-watch` (re-arm or
re-ping) immediately. A re-ping with no follow-up mechanism is a defect.

**Watcher teardown invariant.** Every background watcher (`ci-watch-*`, `cr-watch-*`, `merge-watch-*`,
any deploy-watch) is reaped once its terminal condition is met; an idle one is a leak that survives to
session exit. An `idle_notification` means "available for more", not "done": the harness keeps the agent
registered until it returns a terminal final message or is stopped. So:

- Write watcher prompts to return the final report **and stop** on the terminal condition: CI run
  concluded and any fix pushed (`ci-watch`); CR review clean with zero open findings and no re-review
  pending (`cr-watch`); PR merged and §5 verification plus §7 issue-filing complete (the post-merge
  agent; the `merge-watch` shell watcher simply exits on MERGED or CLOSED). Never "report back and keep
  polling".
- During the reconciliation sweep, stop by name every `cr-watch`/`ci-watch`/`merge-watch` still
  running for a MERGED or CLOSED PR. The post-merge agent reaps its siblings (§4/§5); the sweep is the
  backstop. The session is not clean to close until every such watcher is stopped.

### 4. Wait for human merge - do NOT self-merge by default

After CI is green and CR's loop has settled, hand off to the user. The `merge-watch-<pr-#>` shell watcher
armed at PR creation (§3 step 5) polls `gh pr view <#> --json state,merged,mergeCommit,mergedAt` every
5-10 minutes until MERGED or CLOSED, then exits. Only then spawn the post-merge agent. Tiering: the fully
prescribed §5 checks (terraform plan, curl, simple Chrome MCP walkthroughs) are mechanical (`model:
sonnet` at low effort), but judging whether the evidence verifies the change is verification and goes to
**Sonnet 5.5 high**. If §5 turns out to need exploratory UI debugging, re-spawn that step on **Sonnet 5.5
high** (hypothesis iteration per AGENTS.md §2); step down to Sonnet 5.5 medium only once the exploration
narrows to a single decided fix. The watcher's exit state decides the path:

- `merged: true` -> §5.
- `state: CLOSED` and not merged -> terminal: clean up, exit, and tell the user the PR closed unmerged so
  in-flight work can be re-planned.

**Self-merge exception**: if the project's `CLAUDE.md` or a project-level memory entry explicitly
authorises agent self-merge (e.g. a solo project where CI green suffices), the agent may merge after CI
is green and CR is settled. The default is **wait for human review**. `LeanerCloud/dotclaude` must never
be self-merged, regardless of standing authorization.

**Never bypass required checks to merge: no `--admin` / force-merge.** Whenever merging is on the table
(human merge or the exception above), merge ONLY from a settled, green state: `mergeable == MERGEABLE`
AND `mergeStateStatus == CLEAN`, CI green, CR's review actually **completed** (not `pending` / "review in
progress" / `UNSTABLE`). Do NOT use `gh pr merge --admin` (or "merge without waiting for requirements")
to push past a pending or failing check, a required review, or an in-progress CR pass; this is the
merge-time analog of "never `--no-verify`". If a check is merely pending, **wait** and merge normally. If
a check is genuinely stuck or provably irrelevant, get **explicit per-merge authorization from the user
naming the specific check to bypass**; a blanket "go ahead and merge" authorises only a normal merge once
green.

Project-specific review and merge gates live in each project's `CLAUDE.md` and apply on top.

### 5. Post-merge verification

Once merged, the post-merge agent waits for the deploy pipeline (`gh run list --branch <base> --limit 5`
polled until the relevant deploy run is green), then verifies by change type:

- **UI / frontend**: navigate the deployed URL via Chrome MCP (`mcp__claude-in-chrome__*`), exercise the
  affected flow, confirm the originating issue's repro no longer reproduces. For multi-page flows walk
  the golden path AND the previously-broken edge case. Record concrete observations (selectors clicked,
  network tab, API responses).
- **Backend / API**: `curl` the affected endpoint(s) with realistic input; assert response shape, status
  code and key field values. For state-changing endpoints follow with a read to confirm the change.
- **CLI / batch**: run the relevant command on a representative input; capture stdout/stderr.
- **Infrastructure**: `terraform plan` on the affected environment (expect "no changes", or the
  now-applied diff gone).

When verification can't be done remotely (sandbox, gated credentials, needs a real customer scenario),
say so explicitly in the §6 comment; never silently skip and claim done.

**Reclaim the worktree immediately after the merge.** The post-merge agent (spawned when
`merge-watch-<pr-#>` exits) owns cleanup and does it first, before §5 verification (which checks the
merged base, not the worktree; e.g. `terraform plan` runs on the updated base). Run it from the primary
checkout, never from the worktree being removed. The branch tip must equal the PR's `headRefOid` or be
its ancestor, else ask (`worktrees`).
Run the worktree sweep from the `worktrees` skill ("Reclaiming worktrees after the PR merges or closes")
for this PR's branch: safety-gate it (clean tree, nothing unpushed; recover stranded work per
`feedback_recover_stranded_fix_work` if not), then `git worktree remove` it, delete the build dirs
recorded in the plan's `build_dirs:` (exact paths, only if no other plan lists them, never a glob by
number, never shared dirs such as `build-master`), delete **both** the local branch (`git branch -D`)
**and the remote branch** (`git push origin --delete <branch>`, unless an open PR still has it as base:
`gh pr list --base <branch> --state open`; a merged/wontfix head branch is dead and
stale remote refs accumulate), and archive the plan file (`plans/archive/`). If the PR was closed as
wontfix instead of merged, the same sweep runs only after per-item user confirmation (outside the
AGENTS.md tenet 9 exception). Pair it with the periodic no-worktree **branch sweep** in the `worktrees`
skill ("Sweep merged/closed branches that have NO worktree").

**Reap the watchers, then reap yourself.** The merge is the terminal event, so the post-merge agent is
the natural reaper per the Watcher-teardown invariant: after the worktree reclaim, stop this PR's
now-idle siblings (`cr-watch-<pr-#>` and every `ci-watch-<sha>-*`), then **return your own final
report**, which ends the post-merge agent rather than polling on. End state: zero live watchers for that
PR. If you cannot stop a sibling (already gone, not addressable), note it so the reconciliation sweep
catches it.

### 6. Comment on the originating issue with the verification outcome

Post a structured comment on the issue the PR solved:

- **Deployed**: merged PR link + commit SHA.
- **Verified**: concrete steps, what was observed, what passed.
- **Recommendation**: close the issue (if all passed), or what is still pending with links to §7
  follow-ups.

Do NOT close the issue from the agent; "recommend close" is the signal, the human decides.

**An issue auto-closed by a `Closes #N` keyword still gets this comment.** The keyword closes the issue
at merge without recording anything, leaving a merge event and no verification. Post the same comment on
the closed issue, naming what was verified and how, any known gap left open, and the §7 follow-up issue
number. Closed is not documented.

### 7. Capture follow-up tasks as new GitHub issues - MANDATORY

A hard step; skipping it leaves work invisible to future sessions, on par with skipping the pre-commit
review loop. Do NOT exit the post-PR loop without it.

Anything surfaced during implementation, CR review or post-merge verification that is out of scope for
the merged PR gets a fresh issue. Sources:

- **Latent bugs found reading surrounding code** (the pre-commit review loop's Duplication / Correctness
  checks often surface these): file them, don't bundle unless directly entangled.
- **Actionable CR suggestions that were out of scope** (e.g. "this module would benefit from refactor X").
- **Verification observations revealing a separate bug.**
- **TODOs, `// FIXME`, `// remove once X`** added during implementation.
- **Phrases you wrote in the report** such as *"out of scope"*, *"deferred"*, *"separate gap"*, *"not
  addressed"*, *"narrow scope to"*, *"AWS verification still needed"*, *"requires operator action"*: each
  MUST be filed before exit. Re-read your draft commit message + PR body and grep for them.
- **`Refs #N` instead of `Closes #N`**: the original issue isn't fully resolved; either note in the
  parent issue what stays open, or file a follow-up for the deferred work. Pick one explicitly.
- **Pre-flight findings that uncovered a real bug fixed in this PR** but point at a class of issues (e.g.
  "the variable in the godoc didn't exist" suggests other docs have similar drift): file a sweep-audit
  follow-up.

**Exit checklist** (answer before declaring done):

1. Did I write `Refs #N` (not `Closes #N`) for any issue? -> file a follow-up for the deferred portion.
2. Did my report contain "out of scope", "deferred", "separate gap", "not addressed", "narrow scope to",
   "operator action needed" or "verification still needed"? -> file an issue per phrase.
3. Did I dismiss an actionable CR suggestion as out-of-scope instead of fixing it? -> file an issue each.
4. Did pre-flight reveal an unrelated bug or class of similar bugs? -> file an audit issue.
5. Did I add `// TODO:`, `// FIXME:`, `// remove once X`, `t.Skip("until ...")` or similar? -> file an
   issue per marker.

A "yes" with no issue filed means the loop is incomplete; spawn an Agent to file each missing issue if
needed.

Each follow-up issue MUST include: Summary, Current behaviour, Steps to reproduce (or "Steps to verify the
gap"), Expected behaviour, Proposed fix with file paths and line refs, References (parent issue +
commit/PR + relevant `known_issues/*.md`), and Severity, and reference the parent issue + PR so the link
is bidirectional. Prior successful runs listed in `~/.claude/local-paths.md` (see
`local-paths.md.example`) give a template for the issue shape.

**Final report contract**: every PR-workflow Agent's return summary must include a `Follow-up issues
filed:` line; if none, `Follow-up issues filed: none - confirmed against the exit checklist above`. The
orchestrator treats its absence as evidence the agent skipped step 7 and re-spawns an audit agent.

All watchers (`cr-watch-*`, `ci-watch-*`, `merge-watch-*`, any deploy-watch) run in the background; the main session never blocks on PR review.
