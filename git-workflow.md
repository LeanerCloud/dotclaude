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
- **Avoid heredocs for `git commit -m`**: patterns like `git commit -m "$(cat <<'EOF' ... EOF)"` are fragile — the `$(...)` command substitution can trigger approval prompts, the heredoc-inside-substitution interacts badly with pre-commit hooks that stash unstaged changes (the stash/restore cycle has been observed to fail repeatedly), and quoting/escaping bugs are easy to introduce. Instead, keep a single persistent file at `.claude/scripts/tmp/commit-msg.txt` and commit via `git commit -F .claude/scripts/tmp/commit-msg.txt`.
  - **First commit in a session**: create the file with the `Write` tool.
  - **Every subsequent commit**: **edit the same file in place with the `Edit` tool** — replace the previous message body with the new one. Do NOT delete the file between commits and do NOT create a fresh file each time; both are wasted Bash calls and wasted approval prompts. The file is session-scoped scratch space, and its content is only meaningful while a commit is being staged — overwriting the previous message is the expected behaviour, not a loss.
  - **After every edit, `Read` the file back before running `git commit -F`**: the `Edit` tool only shows the diff, not the full resulting content. Reading the file gives you the complete message in context so you can proof-read it as a whole — catch stale bullets left over from the previous message, wrong subject, broken formatting, accidental duplication across paragraphs — before the commit lands. This is one cheap tool call that prevents a fix-forward commit.
  - Leave the file in place at end of session. The `.claude/scripts/tmp/` cleanup rule in `tool-usage.md` still applies for actual throw-away scripts, but `commit-msg.txt` is a stable reusable buffer — treat it like a workbench, not an artefact.

## Atomic commits

- **Small atomic commits**: one distinct piece of functionality per commit; independently revertable.
- Stage and commit small chunks within a file separately rather than the whole file at once.

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

## Hooks & docs

- **Pre-commit hooks**: projects must have hooks for linting, formatting, and tests. Never skip with `--no-verify`.
- **Docs with code**: each commit includes relevant doc updates (README, CHANGELOG, inline comments) when warranted.
