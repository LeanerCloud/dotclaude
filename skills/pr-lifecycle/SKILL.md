---
name: pr-lifecycle
description: PR rules, the mandatory PR-creation checklist (label mirror, CR trigger, three watcher
  classes), waiting for human merge, post-merge verification, the issue comment, and filing
  follow-up issues. Invoke when opening a PR or driving one to merge.
---

# Pull requests — creation checklist through post-merge follow-up

The CodeRabbit review rounds inside this lifecycle are the `cr-loop` skill; invoke that when a review
arrives. Driving an existing PR to merge-ready is the `pr-iterate` skill.

## PR shape

- **Keep PRs small**: aim for ≤400 lines of meaningful change; large PRs get shallow reviews.
- **One concern per PR**: don't mix a refactor with a behaviour change, or a bug fix with a new feature — split them; each PR should be independently revertable.
- PR title should follow the same conventional commits format as commit messages (enables changelog generation from PR titles as a fallback).
- Include a short description of *why* the change is needed, not just *what* it does.
- Create feature branches for non-trivial work; name them `type/short-description` (e.g. `feat/oauth-login`, `fix/nil-pointer-api`).

## Post-PR review loop (CodeRabbit + human merge)

Opening a PR is not the end of the agent's work — there's a full lifecycle to manage in background agents while the main session moves on. This complements the §"Post-push CI watcher" rules: CI watchers handle build/test/deploy automation; the watchers below handle human-and-bot review.

The loop applies to any project that uses CodeRabbit (or an equivalent automated reviewer). Where projects use a different bot or no bot, skip steps 1–3 and start at §4.

> When this loop runs on **several PRs at once**, the orchestration layer on top of it — the agent role split, coordination between actors that share no memory, the four-gate merge check (of which this loop is one gate), and the false-clean traps — is in `~/.claude/multi-agent-pr-orchestration.md`.

### ⚠️ Immediate PR-creation checklist - every step, every time

Within ~30 seconds of `gh pr create` returning, the main session MUST have done ALL of the following. Skipping any one leaves part of the PR lifecycle unattended; CR findings or CI failures will sit silently and the human reviewer ends up doing the loop manually.

1. **Label mirror**: `gh pr edit <#> --add-label <labels-from-closing-issue>` (priority/severity/urgency/impact/effort/type + `triaged`, per `~/.claude/triage.md`).
2. **Trigger CodeRabbit** — bound atomically to step §4 (see invariant below): `gh pr comment <#> --body "@coderabbitai review"` (or the project's trigger phrase - check the project `CLAUDE.md` or memory).
3. **CI watcher(s)**: spawn one background `Agent` per Actions workflow run that fired on the push, named `ci-watch-<short-sha>-<workflow-slug>-<run-id>`. See §"Post-push CI watcher" for the prompt shape. Their job is CI failures only - they do NOT read CodeRabbit's inline comments.
4. **CR watcher**: spawn ONE background `Agent` named `cr-watch-<pr-#>` whose end-to-end remit is steps §2-§3 of this loop (poll for CR review → triage findings → push fixes → re-ping CR → iterate to silence). This is a SEPARATE agent from the CI watcher; do not conflate them. Its prompt should include the full §3 triage rules + the §3a rebase rules + the obligation to ping `@coderabbitai review` after each fix push. If you only spawn a CI watcher, CodeRabbit findings will sit unread because CI watchers do not poll PR comments.
5. **Merge watcher**: spawn ONE background `Agent` named `merge-watch-<pr-#>` per §4. It waits for the human merge AND runs the §5 post-merge verification AND files the §7 follow-up issues. Spawn this immediately at PR-creation time even though it has nothing to do until merge - having it pre-armed means the merge → verification → issue-filing chain runs without main-session intervention.

Total: **3+ background agents per PR** (one per CI run + cr-watch + merge-watch). Do not collapse them into one "do everything" agent - they have different polling cadences, different terminal conditions, and different model tiers. Do not omit cr-watch with the rationale that "CodeRabbit is just a bot" - its findings have the same severity weighting as a human reviewer's, and unaddressed CR threads block the human merge in projects that gate on review-bot approval.

**Common failure mode** (the reason this checklist exists in this prominent form): spawning only the CI watcher and walking away. The CI watcher reports "all checks passed" once CodeRabbit's "check" reaches the green "review submitted" state - but its findings inside the review are invisible to that check. The human reviewer notices the unaddressed comments first; main session has already moved on. Always spawn cr-watch alongside ci-watch.

> **⚠️ Atomic-coupling invariant — the trigger and the watcher are ONE action.** Steps §2 (post `@coderabbitai review`) and §4 (spawn `cr-watch-<pr-#>`) are not two checklist items you can do independently — they are a single indivisible action. **Never post `@coderabbitai review` for a PR unless `cr-watch-<pr-#>` for that PR is being spawned in the same response (or is already live).** This is a defect identical in shape to `git push` without a post-push CI watcher: the trigger kicks off work that nothing is then watching, so the findings sit unread until a human notices. The safe ordering is **spawn the watcher first, then post the trigger** — that way a posted trigger always provably implies a live watcher, and you can never end up trigger-without-watcher. When triggering CR across several PRs at once (e.g. after a sweep that opened or pushed to N PRs, or when the parent session opens PRs from N subagent-pushed branches), spawn **N `cr-watch` agents — one per PR — in the same message as the N triggers**. A batch of triggers with no matching batch of watchers is the precise failure that leaves a wall of CR findings unaddressed (and it is the failure this section was extended to prevent).

> **Reconciliation sweep — the backstop that catches whatever slips through.** The atomic-coupling invariant prevents the miss at trigger time, but watchers crash, agents get reaped, and triggers get posted in a prior session that this one never saw. So: **before declaring PR work done for a session, and any time you notice unaddressed CR comments, reconcile.** List every open PR you authored (`gh pr list --author @me --state open` or the session's known PR set) and confirm each is in exactly one of: (a) a live `cr-watch-<pr-#>` is polling it, (b) it has reached a clean terminal CR state (latest CR review = zero Actionable AND every Nitpick fixed-or-justified), or (c) it is closed. Any PR with CR findings (or an un-responded trigger) and no live watcher gets a fresh `cr-watch` spawned immediately. This sweep is cheap (one `gh` list + a per-PR review-state check) and is the only thing that catches drift after the fact — run it as a session-end gate, not an optional nicety.

> **⚠️ Watcher teardown invariant: a watcher that idles instead of ending is a leak.** Every background watcher (`ci-watch-*`, `cr-watch-*`, `merge-watch-*`, plus any deploy-watch) must be **reaped once its terminal condition is met**; otherwise it lingers as a live background agent, survives until the session exits, and shows up in the exit-time "background work still running" warning even though its actual job finished. Root cause of the leak: an agent that emits an `idle_notification` is saying "finished this turn, available for more," **not** "done forever," so the harness keeps it registered until it either returns a **terminal final message** or is stopped with `TaskStop`. Two-sided fix, do both:
> - **Write watcher prompts to END on their terminal condition.** The prompt must instruct the agent to return its final report (which permanently ends it) the moment its job is done: CI run concluded and any fix pushed (`ci-watch`); CR review clean with zero open findings and re-review not pending (`cr-watch`); PR merged and §5 post-merge verification + §7 issue-filing complete (`merge-watch`). "Report back and then keep polling" is the anti-pattern that produces the leak; it must be "report back **and stop**."
> - **`TaskStop` any watcher still parked at a terminal PR state.** Spawners don't get to assume the agent self-terminated. Fold this into the reconciliation sweep above: for every PR that has reached MERGED or CLOSED, `TaskStop` its `cr-watch`/`ci-watch`/`merge-watch` by name (the `merge-watch` is the natural end-of-lifecycle reaper per §4/§5, but the session-end reconciliation is the backstop). A PR in a terminal state with a still-"running" watcher is a defect to fix, exactly like an untriggered CR review: the session is not clean to close until every such watcher is stopped.

### 4. Wait for human merge — do NOT self-merge by default

After CI is green and CodeRabbit's loop has settled, hand off to the user. Spawn a background agent named `merge-watch-<pr-#>` (`model: haiku` — polling and the routine §5 verifications (terraform plan, curl, simple Chrome MCP walkthroughs) are mechanical; if §5 verification turns out to need exploratory UI debugging, re-spawn that step on **Opus** — exploratory debug is hypothesis iteration per CLAUDE.md §2 and is the kind of "understanding is the hard part" workload that's gotten wrong on Sonnet. Step down to Sonnet only when the exploration narrows to a single decided fix to apply) that polls `gh pr view <#> --json state,merged,mergeCommit,mergedAt` until:

- `merged: true` → proceed to §5.
- `state: CLOSED` and not merged → terminal, clean up, exit (and notify user that the PR was closed unmerged so any in-flight work can be re-planned).

**Self-merge exception**: if the project's `CLAUDE.md` or a project-level memory entry explicitly authorises agent self-merge (e.g., a solo project where the user is also the agent operator and CI green is sufficient), the agent may merge after CI is green and CodeRabbit is settled. The default is **wait for human review**.

**Never bypass required checks to merge — no `--admin` / force-merge.** Whenever merging is on the table (human merge, or the self-merge exception above), merge ONLY from a settled, green state: `mergeable == MERGEABLE` AND `mergeStateStatus == CLEAN`, with CI green and CodeRabbit's review actually **completed** (not `pending` / "review in progress" / an `UNSTABLE` state). Do NOT use `gh pr merge --admin` (or GitHub's "merge without waiting for requirements") to push past a pending or failing status check, a required review, or an in-progress CodeRabbit pass. This is the merge-time analog of "never `--no-verify`" and the "no masking CI debt" directive: a bypassed check is an unreviewed merge, and "it turned out fine" is not a justification. If a check is merely pending, **wait for it to settle** and merge normally. If a check is genuinely stuck or provably irrelevant, get **explicit per-merge authorization from the user that names the specific check to bypass** before using `--admin` — a blanket "go ahead and merge" / "merge them" does NOT authorise a check-bypass, only a normal merge once green.

### 5. Post-merge verification

Once merged, the `merge-watch-<pr-#>` agent waits for the deploy pipeline (`gh run list --branch <base> --limit 5` polled until the relevant deploy run is green), then exercises a verification appropriate to the change type:

- **UI / frontend changes**: navigate the deployed URL via Chrome MCP (`mcp__claude-in-chrome__*` tools), exercise the affected flow, and confirm the bug repro from the originating issue no longer reproduces. For multi-page flows, walk the golden path AND the previously-broken edge case. Record concrete observations (which selectors clicked, what the network tab showed, which API responses came back).
- **Backend / API changes**: `curl` the affected endpoint(s) with realistic input, assert the response shape, status code, and key field values. For state-changing endpoints, follow up with a read to confirm the state actually changed.
- **CLI / batch changes**: run the relevant command on a representative input and capture stdout/stderr.
- **Infrastructure**: `terraform plan` (expect "no changes" if the apply already happened, or expect the now-applied diff to be gone) on the affected environment.

When verification can't be done remotely (sandboxed env, gated credentials, change requires a real customer scenario): say so explicitly in the comment in §6 — never silently skip and claim done.

**Reclaim the worktree once merged.** The `merge-watch-<pr-#>` agent is the one that observes the merge, so it owns cleanup: after the §5 verification, run the worktree sweep from `~/.claude/worktrees.md` ("Reclaiming worktrees after the PR merges or closes") for this PR's branch: safety-gate it (clean tree, nothing unpushed; recover stranded work per `feedback_recover_stranded_fix_work` if not), then `git worktree remove` it, delete **both** the local branch (`git branch -D`) **and the remote branch** (`git push origin --delete <branch>` — a merged/wontfix head branch is dead; leaving it accumulates hundreds of stale remote refs), and archive/delete the plan file. If this PR was closed as wontfix instead of merged, the same sweep applies. This is what keeps `git worktree list` and `git branch -r` from silently accumulating dozens of merged-branch worktrees and refs; pair it with the periodic no-worktree **branch sweep** in `~/.claude/worktrees.md` ("Sweep merged/closed branches that have NO worktree") for branch refs left behind after their worktree is already gone.

**Reap the watchers, then reap yourself.** The merge is the terminal event for the whole PR lifecycle, so the `merge-watch-<pr-#>` agent is the natural reaper per the Watcher-teardown invariant (§"Post-PR review loop"): after the worktree reclaim, `TaskStop` this PR's now-idle siblings: the `cr-watch-<pr-#>` and every `ci-watch-<sha>-*` for it (their jobs ended when CR went clean and CI concluded, but they may be parked on an idle notification). Then **return your own final report**, which permanently ends `merge-watch` itself, rather than continuing to poll. End state after a merge: zero live watchers for that PR. If you cannot stop a sibling (already gone, or not addressable), note it so the session-end reconciliation sweep catches it.

### 6. Comment on the originating issue with the verification outcome

Post a structured comment to the GitHub issue that the PR was solving:

- **Deployed**: link to the merged PR + commit SHA.
- **Verified**: concrete steps taken, what was observed, what passed.
- **Recommendation**: close the issue (if everything passed), or describe what's still pending and link to follow-up issues from §7.

Do NOT close the issue from the agent — leave that to the user. Posting "recommend close" is the agent's signal; the human decides.

**An issue auto-closed by a `Closes #N` keyword still gets this comment.** The keyword closes the issue the moment the PR merges, without recording anything: the issue then carries a merge event and nothing else, so the verification is invisible and the "recommend close" signal never gets written. Post the same structured comment on the closed issue anyway, naming what was verified and how, plus any known gap left open and the §7 follow-up issue number tracking it. Closed is not the same as documented.

### 7. Capture follow-up tasks as new GitHub issues — ⚠️ MANDATORY, NOT OPTIONAL

**This is a hard step of the workflow.** Skipping it leaves work invisible to future sessions and is a process failure on par with skipping the pre-commit review loop. Do NOT exit the post-PR loop without completing this step.

Anything surfaced during implementation, CodeRabbit review, or post-merge verification that's out-of-scope for the just-merged PR gets a fresh GitHub issue. Sources include:

- **Latent bugs found while reading the surrounding code** (the §"Mandatory pre-commit review loop"'s Duplication / Correctness checks often surface these — file them when found, don't bundle into the in-flight PR unless they're directly entangled).
- **CodeRabbit suggestions that were actionable but out of scope** (e.g., "this whole module would benefit from refactor X").
- **Verification observations that revealed a separate bug** not covered by the original issue.
- **TODOs, `// FIXME`, `// remove once X` markers** added during the implementation.
- **Phrases the agent itself wrote in the report** like *"out of scope"*, *"deferred"*, *"separate gap"*, *"not addressed"*, *"narrow scope to"*, *"AWS verification still needed"*, *"requires operator action"* — every one of these is a follow-up that MUST be filed before exit. Re-read your own draft commit message + PR body before exit and grep for these phrases — if any appear, file the corresponding issue.
- **`Refs #N` instead of `Closes #N`** in the commit/PR body — by definition the original issue isn't fully resolved; the unresolved part needs either (a) a clear note in the parent issue explaining what stays open, or (b) a separate follow-up issue tracking the deferred work. Pick one explicitly; don't leave the gap implicit.
- **Pre-flight findings that uncovered a real bug fixed in this PR** but where the bug points at a class of issues — e.g., "the variable referenced in the godoc didn't actually exist" suggests other docs/wiring may have similar drift. File a sweep-audit follow-up.

**Exit checklist — answer these before declaring the PR work done:**

1. Did I write `Refs #N` (not `Closes #N`) for any issue? → must file a follow-up tracking the deferred portion.
2. Did my report contain "out of scope", "deferred", "separate gap", "not addressed", "narrow scope to", "operator action needed", or "verification still needed"? → file an issue per phrase.
3. Did CodeRabbit's review include any actionable suggestions I dismissed as out-of-scope (rather than addressed in a fix commit)? → file an issue per dismissed-but-actionable suggestion.
4. Did pre-flight investigation reveal any unrelated bug or class of similar bugs? → file an audit issue.
5. Did the implementation introduce any `// TODO:`, `// FIXME:`, `// remove once X`, `t.Skip("until …")`, or similar markers? → file an issue per marker.

If the answer to any of 1–5 is yes and the corresponding issue is **not** filed, the loop is incomplete. Spawn an Agent to file each missing issue if needed — it's worth the marginal cost.

Each follow-up issue MUST include: Summary, Current behaviour, Steps to reproduce (or "Steps to verify the gap" for non-bug items), Expected behaviour, Proposed fix with file paths and line refs, References (parent issue + commit/PR + relevant `known_issues/*.md` doc), and Severity. Reference back to the parent issue + PR in the new issue body so the link is bidirectional.

If you have prior successful runs of this loop on your own projects, listing them in `~/.claude/local-paths.md` (see `local-paths.md.example`) gives future sessions a concrete template to mirror when unsure of the issue shape.

**Final report contract**: every PR-workflow Agent's return summary must include a `Follow-up issues filed:` line. If none, write `Follow-up issues filed: none — confirmed against the exit checklist above`. The orchestrator should treat the absence of this line as evidence the agent skipped step 7 and re-spawn an audit agent.

### Lifecycle summary

```
gh pr create
   ↓
@coderabbitai review comment
   ↓
[cr-watch-<pr-#>] background agent → poll 60-120s, handle 429s
   ↓
CodeRabbit review arrives
   ↓
Triage: actionable / dismiss-with-justification / batch-nitpick
   ↓ (commits + pushes follow §pre-commit review loop)
[ci-watch-<sha>-<wf>] watchers per §Post-push CI watcher
   ↓
CI green + CodeRabbit settled → PR comment summarising
   ↓
[merge-watch-<pr-#>] background agent → poll until merged
   ↓
Deploy pipeline green
   ↓
Verification (Chrome MCP / curl / terraform plan / CLI)
   ↓
Comment on originating issue with outcome + close recommendation
   ↓
File follow-up issues for out-of-scope items
```

All four watcher classes (`cr-watch-*`, `ci-watch-*`, `merge-watch-*`, plus any project-specific deploy-watch) run in background — the main session never blocks on PR review and continues with the next task.
