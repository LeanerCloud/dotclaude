# Git Workflow — Commits, Reviews, PRs, and CI

Conventions for commits, pre-commit review, pull requests, and post-push CI handling. Applies to every project unless the project's `CLAUDE.md` overrides a specific rule.

## Commit messages

- Use **conventional commits** format: `type(scope): subject` — e.g. `feat(auth): add OAuth2 login`, `fix(api): handle nil pointer on empty response`.
  - Common types: `feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `perf`, `ci`, `build`.
  - `scope` is optional but useful for larger repos; omit when the repo is small.
  - This enables automated changelog and release notes generation (see `infra-ops.md` CI/CD).
- Subject line: imperative mood, lowercase, no trailing period, ≤72 chars.
- Body (when needed): bullet points explaining the *why*, not the *what*; wrap at 72 chars.
- **NEVER** mention Anthropic or Claude in commit messages (no Co-Authored-By lines).
- **Avoid heredocs for `git commit -m`**: patterns like `git commit -m "$(cat <<'EOF' ... EOF)"` are fragile — the `$(...)` command substitution can trigger approval prompts, the heredoc-inside-substitution interacts badly with pre-commit hooks that stash unstaged changes (the stash/restore cycle has been observed to fail repeatedly), and quoting/escaping bugs are easy to introduce. Instead, for each commit **`Write` a fresh file** to `/tmp/claude/` with a unique name, commit via `git commit -F <path>`, and delete the file once the commit has landed.
  - **Why `/tmp/claude/`**: see `tool-usage.md` § "Two script locations by lifetime" for the rationale. Create the directory lazily with `mkdir -p /tmp/claude` before the first write if it doesn't exist.
  - **One file per commit, always created fresh with `Write`**: use a unique name like `/tmp/claude/commit-<short-subject>.txt` or include a timestamp so consecutive commits never collide. Because `Write` echoes the full file content back on creation, the final message is visible in full before the commit lands — no separate `Read` pass is needed. Do NOT `Edit` an existing commit-msg file in place; always `Write` a fresh one.
  - **Clean up after every commit**: once `git commit -F` has succeeded, `rm` the file. Single-file deletion on an explicit path is safe and avoids leaving stale buffers behind.

## Atomic commits

- **Small atomic commits**: one distinct piece of functionality per commit; independently revertable.
- Stage and commit small chunks within a file separately rather than the whole file at once.
- **Don't commit throwaway or personal-only artefacts**: one-off scripts, debugging helpers, scratch files, and session-specific tooling do NOT belong in the project repo. Before staging, ask: "would another contributor find this useful, or is this just something I needed for this session?" If the answer is personal/temporary, keep it in `/tmp/claude/` (outside any repo) or delete it. Concrete examples of things to *not* commit: ad-hoc data-migration scripts that ran once, curl loops for manual testing, log-parsing one-liners, scaffolding scripts that bootstrap local dev state. If a throwaway script turns out to be genuinely reusable, promote it to a proper committed location (`scripts/`, `tools/`, `Makefile` target) with documentation and tests — don't just leave it in the tree because it was convenient.

## ⚠️ Mandatory pre-commit review loop — NO EXCEPTIONS

Before every commit, enter a review loop (same discipline as the plan review loop). Do NOT commit after a single pass — iterate until **3 consecutive review passes find zero issues**. Do NOT skip, shortcut, or batch this step. The goal is to land clean commits in the first place, so the history doesn't need fix-up commits.

### Each pass

Read the full staged diff (`git diff --cached`) and the relevant unstaged context, and systematically check all five dimensions:

- **Completeness**: Does the commit deliver what it claims? Nothing missing? All touched files consistent with the commit message? Tests updated for the changed behaviour?
- **Correctness**: Any logic errors, off-by-ones, wrong assumptions, broken invariants, stale references, type mismatches, leftover debug code, unused imports?
- **Security**: Any injection vectors, auth bypasses, secrets exposure, missing input validation, OWASP top 10 violations?
- **Bugs**: Race conditions, null derefs, edge cases, resource leaks, error handling gaps, broken tests, stale mocks?
- **Duplication**: Does any new function/type/helper in this diff replicate logic that already exists in the project? Grep for distinctive identifiers, constants, or phrases from the new code to catch near-duplicates. If a duplicate is found, stop and either reuse the existing code or refactor it to cover both cases (per `CLAUDE.md` step 1a) — do NOT commit parallel copies.

### Each iteration

- Print a short summary of issues found before and after fixing them (matches the plan-review-loop format).
- An iteration with fixes resets the clean-pass counter — you need 3 clean passes *after* the last fix.

### Multi-commit work

For a sequence of atomic commits implementing one plan: review each commit's staged diff individually AND think about how it interacts with already-committed work in the sequence.

### Delegation

For staged changes touching multiple concerns (Go + TS + Terraform), launch specialised review agents in parallel (`feature-dev:code-reviewer` works well) and compile findings before committing.

### Fix before committing, never after

If the review finds issues, fix them in the same staged changeset — do not commit and then create a follow-up fix commit. The history should not contain "oops, fixing previous commit" patterns when the issue could have been caught before the commit landed.

## Post-commit sanity check

After committing, run a quick sanity scan (`git show HEAD`) to catch anything the pre-commit loop missed. If this finds issues, treat it as a process failure (the pre-commit loop should have caught them). Fix-forward in a new commit only when strictly necessary (e.g., pre-commit hook caught a legitimate issue that required the commit to land first).

## Post-push CI watcher (background agent)

After every `git push` that publishes new commits, immediately enumerate **all** GitHub Actions workflow runs triggered by the push and launch **one background agent per run** to monitor each independently. A single commit typically triggers multiple workflows (build, lint, test matrix, terraform validate, security scan, deploy) — they run in parallel, fail independently, and need fixes targeted at different parts of the codebase. A single watcher agent serialising across all of them would block on the slowest, miss parallel failures, and conflate diagnoses.

**Setup**:

1. Right after `git push`, run `gh run list --commit <sha> --json databaseId,name,status` to list every run for the pushed commit. Wait briefly (a few seconds) and re-list if the run list looks incomplete — workflows can take a moment to register.
2. For each run ID, spawn a separate background agent (`Agent` tool with `run_in_background: true`) named `ci-watch-<short-sha>-<workflow-slug>` (e.g. `ci-watch-a1b2c3d-build`, `ci-watch-a1b2c3d-test`, `ci-watch-a1b2c3d-tf-validate`). Each name must be unique and addressable via `SendMessage`.
3. Each agent monitors **only its assigned run ID** — pass the run ID explicitly in the prompt so it doesn't poll the wrong workflow.

**Each agent's job**:

1. Poll `gh run watch <run-id>` (or `gh run view <run-id> --json status,conclusion` in a loop) until that specific workflow finishes.
2. On failure, fetch logs with `gh run view <run-id> --log-failed`, diagnose the root cause, and **fix it autonomously** with a follow-up commit + push (e.g., lint failures, broken tests, terraform validation, type errors, missing env vars in CI) — not just report back.
3. Coordinate with sibling watchers via the multi-agent comms bus before pushing a fix: another watcher may already be fixing a related failure, and two parallel pushes can stomp on each other or trigger a fresh round of CI for both. Claim a `git-push` lock first (see `~/.claude/multi-agent-comms.md`).
4. Only escalate to the user if the failure requires a decision (credentials, infra changes, ambiguous design choices) or if its own fix attempt also fails CI.

Do NOT poll any watcher from the foreground — they will each notify on completion. This keeps the main session unblocked while CI runs and ensures broken main never sits unaddressed, regardless of how many workflows fired for the same commit.

## Pull requests

- **Keep PRs small**: aim for ≤400 lines of meaningful change; large PRs get shallow reviews.
- **One concern per PR**: don't mix a refactor with a behaviour change, or a bug fix with a new feature — split them; each PR should be independently revertable.
- PR title should follow the same conventional commits format as commit messages (enables changelog generation from PR titles as a fallback).
- Include a short description of *why* the change is needed, not just *what* it does.
- Create feature branches for non-trivial work; name them `type/short-description` (e.g. `feat/oauth-login`, `fix/nil-pointer-api`).

## Post-PR review loop (CodeRabbit + human merge)

Opening a PR is not the end of the agent's work — there's a full lifecycle to manage in background agents while the main session moves on. This complements the §"Post-push CI watcher" rules: CI watchers handle build/test/deploy automation; the watchers below handle human-and-bot review.

The loop applies to any project that uses CodeRabbit (or an equivalent automated reviewer). Where projects use a different bot or no bot, skip steps 1–3 and start at §4.

### 1. Trigger CodeRabbit immediately after PR creation

- Right after `gh pr create`, post a comment with the literal body `@coderabbitai review` (or whichever trigger phrase the project uses — check the project's `CLAUDE.md` or memory).
- Verify the comment landed with `gh pr view <#> --json comments` before walking away.

### 2. CodeRabbit-watcher background agent

Spawn a background `Agent` named `cr-watch-<pr-#>` that:

- Polls `gh api repos/<owner>/<repo>/pulls/<#>/comments` and `gh pr view <#> --json reviews` every **60–120s** (never faster — CodeRabbit's own backend rate-limits review processing and aggressive polling won't make the review come faster).
- Treats `429`, `403 secondary rate limit`, or any "rate limit" string in the response body as a **soft** error: log, sleep 120s, retry. Do not escalate. Rate-limit responses are normal during high-traffic windows; a watcher that escalates on every 429 wastes user attention.
- Stops polling once one of these terminal states is reached:
  - CodeRabbit posts a review (success — proceed to §3).
  - The PR is closed without merging (terminal — clean up, exit).
  - 24h elapses without a review (escalate to user with a "CodeRabbit hasn't reviewed in 24h" note).

### 3. Address CodeRabbit nitpicks

When CodeRabbit's review arrives, the watcher reads each suggestion and triages it into one of three buckets:

- **Actionable** (real bug, security concern, missing test, broken type, valid code-smell with a clear fix): fix in a new commit on the same branch. Each fix commit follows the §"Mandatory pre-commit review loop" — 3 clean passes, no shortcuts. Push triggers a fresh CI watcher pass per §"Post-push CI watcher".
- **Stylistic preference contrary to project conventions** (or a trade-off that was already considered during implementation): reply on the PR comment with a brief justification linking to the convention in `CLAUDE.md` or the relevant code; do NOT commit a change.
- **Genuine nitpick** (minor, unambiguously safe to apply, not contrary to convention): fix it. **Batch nitpicks into one commit** rather than one-commit-per-nitpick — single-line fix commits create review noise and inflate the history without buying anything.

After the response pass, post one PR comment summarising what was addressed vs. dismissed and why. This avoids leaving CodeRabbit's threads silently unaddressed and gives the human reviewer a clean signal that the bot pass is complete. **End that comment with a fresh `@coderabbitai review` ping** so the bot re-reviews the fix push — without the explicit ping CodeRabbit will sometimes process a re-review automatically and sometimes not, depending on configuration; the explicit ping makes the loop deterministic.

**Iterate until CodeRabbit is silent.** Each fresh CR review starts a new triage round — read every Actionable / Nitpick / Outside-diff finding, apply the bucket rules, push a fix commit (or post a justification reply), and ping `@coderabbitai review` again. CR commonly produces 3–6 review passes on a substantive PR before reaching all-clear; "I addressed pass 1" is **not** loop-exit. The loop only exits when the most recent CR review contains zero **Actionable** items AND every **Nitpick** is either fixed or has a justification reply on the inline thread. Dismissing nitpicks silently by ignoring them is not loop-exit — it leaves the threads unresolved and the human reviewer has to re-triage them. If after a fix push CR returns the SAME finding (i.e., the fix didn't actually address the concern), do not just push another best-effort attempt — read the original finding more carefully and either (a) push a real fix that genuinely closes the gap or (b) reply on the inline thread explaining why the fix as-pushed addresses the concern and that CR's re-flag is a duplicate; never let the same finding ping-pong more than 2 rounds without explicit user direction.

**Rate-limit etiquette during the loop.** CodeRabbit's review processing is rate-limited per repo. Do not push faster than CR can review — let each `@coderabbitai review` ping settle to a posted review before pushing the next fix commit. Pushing 4 fix commits in 60 seconds is wasted effort: CR will batch them under one review pass and your fine-grained commit history won't help anyone read the conversation back later.

### 3a. Resolve merge conflicts during the CR loop

While the CR loop iterates the feature branch can fall behind the base branch — the base picks up other merges, and the next push is rejected as `non-fast-forward` or the PR's `mergeStateStatus` flips to `DIRTY` / `BEHIND`. Address it as part of the same loop, not as a separate ceremony:

- **Detect early.** Before each fix push, check `gh pr view <#> --json mergeStateStatus,mergeable`. `mergeStateStatus: DIRTY` means there are conflicts that must be resolved before merge; `BEHIND` means no conflicts but the branch is stale; `BLOCKED` typically means a required check is failing (CI watcher's job, not this loop).
- **Rebase, don't merge, by default.** Inside the worktree: `git fetch origin && git rebase origin/<base-branch>`. Resolve conflicts atomically — for each conflict file, `git diff --check` to see the markers, decide which side wins (or how to merge both), `git add <file>`, `git rebase --continue`. Do NOT take "ours" or "theirs" wholesale without reading both sides; the conflict often points at a real semantic clash that deserves a third option.
- **Re-run the relevant tests after the rebase resolves**, even if the conflicts were "trivial" (whitespace, import ordering). Rebase semantically replays each commit on top of new base state — a conflict that "looks formatting-only" can still subtly change call sites if the base picked up a refactor. `go test ./...` or the project's equivalent gate is mandatory before pushing.
- **Push with `--force-with-lease`, never `--force`.** `--force-with-lease` aborts if origin moved while you were rebasing (someone else pushed to the same feature branch — rare but possible during a CR loop with multiple agents). Plain `--force` overwrites silently and can lose other agents' work.
- **Comment on the PR after the rebase push** with a brief note: "Rebased onto <base-branch>@<sha> to resolve conflicts in <files>. Tests re-run green. <new-head-sha>." This keeps reviewers (human and bot) oriented when the diff suddenly changes shape.
- **Conflict-resolution commits do NOT count as "addressed CR findings".** A rebase that drops a CR-flagged change is a regression; if the rebase obviated a CR finding (e.g., the base branch already fixed the same bug), explicitly note that on the original CR thread instead of letting the finding silently disappear.

If conflicts span more than the immediate diff (e.g., a base-branch refactor moved the file you were editing), pause and re-evaluate: the original PR's plan may need updating before the rebase. Do not power through a 100-line conflict resolution that effectively re-implements half the PR — that warrants a fresh plan-review pass per `~/.claude/CLAUDE.md` §"Plan review loop".

### 4. Wait for human merge — do NOT self-merge by default

After CI is green and CodeRabbit's loop has settled, hand off to the user. Spawn a background agent named `merge-watch-<pr-#>` that polls `gh pr view <#> --json state,merged,mergeCommit,mergedAt` until:

- `merged: true` → proceed to §5.
- `state: CLOSED` and not merged → terminal, clean up, exit (and notify user that the PR was closed unmerged so any in-flight work can be re-planned).

**Self-merge exception**: if the project's `CLAUDE.md` or a project-level memory entry explicitly authorises agent self-merge (e.g., a solo project where the user is also the agent operator and CI green is sufficient), the agent may merge after CI is green and CodeRabbit is settled. The default is **wait for human review**.

### 5. Post-merge verification

Once merged, the `merge-watch-<pr-#>` agent waits for the deploy pipeline (`gh run list --branch <base> --limit 5` polled until the relevant deploy run is green), then exercises a verification appropriate to the change type:

- **UI / frontend changes**: navigate the deployed URL via Chrome MCP (`mcp__claude-in-chrome__*` tools), exercise the affected flow, and confirm the bug repro from the originating issue no longer reproduces. For multi-page flows, walk the golden path AND the previously-broken edge case. Record concrete observations (which selectors clicked, what the network tab showed, which API responses came back).
- **Backend / API changes**: `curl` the affected endpoint(s) with realistic input, assert the response shape, status code, and key field values. For state-changing endpoints, follow up with a read to confirm the state actually changed.
- **CLI / batch changes**: run the relevant command on a representative input and capture stdout/stderr.
- **Infrastructure**: `terraform plan` (expect "no changes" if the apply already happened, or expect the now-applied diff to be gone) on the affected environment.

When verification can't be done remotely (sandboxed env, gated credentials, change requires a real customer scenario): say so explicitly in the comment in §6 — never silently skip and claim done.

### 6. Comment on the originating issue with the verification outcome

Post a structured comment to the GitHub issue that the PR was solving:

- **Deployed**: link to the merged PR + commit SHA.
- **Verified**: concrete steps taken, what was observed, what passed.
- **Recommendation**: close the issue (if everything passed), or describe what's still pending and link to follow-up issues from §7.

Do NOT close the issue from the agent — leave that to the user. Posting "recommend close" is the agent's signal; the human decides.

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

## Hooks & docs

- **Pre-commit hooks**: projects must have hooks for linting, formatting, and tests. Never skip with `--no-verify`.
- **Docs with code**: each commit includes relevant doc updates (README, CHANGELOG, inline comments) when warranted.
