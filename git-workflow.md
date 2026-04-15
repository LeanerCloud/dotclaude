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
- **Avoid heredocs for `git commit -m`**: patterns like `git commit -m "$(cat <<'EOF' ... EOF)"` are fragile — the `$(...)` command substitution can trigger approval prompts, the heredoc-inside-substitution interacts badly with pre-commit hooks that stash unstaged changes (the stash/restore cycle has been observed to fail repeatedly), and quoting/escaping bugs are easy to introduce. Instead, write the message to `.claude/scripts/tmp/commit-msg.txt` with the `Write` tool, run `git commit -F .claude/scripts/tmp/commit-msg.txt`, and delete the file after a successful commit.

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

After every `git push` that publishes new commits, immediately launch a background agent (`Agent` tool with `run_in_background: true`) to monitor the GitHub Actions run(s) for the pushed commit(s). The agent's job is to:

1. Poll `gh run list --commit <sha>` and `gh run watch` until each workflow finishes.
2. On failure, fetch logs with `gh run view --log-failed`, diagnose the root cause, and **fix it autonomously** with a follow-up commit + push (e.g., lint failures, broken tests, terraform validation, type errors, missing env vars in CI) — not just report back.
3. Only escalate to the user if the failure requires a decision (credentials, infra changes, ambiguous design choices) or if its own fix attempt also fails CI.

Use a clear background-agent name like `ci-watch-<short-sha>` so it's addressable via `SendMessage`. Do NOT poll the watcher from the foreground — it will notify on completion. This keeps the main session unblocked while CI runs and ensures broken main never sits unaddressed.

## Pull requests

- **Keep PRs small**: aim for ≤400 lines of meaningful change; large PRs get shallow reviews.
- **One concern per PR**: don't mix a refactor with a behaviour change, or a bug fix with a new feature — split them; each PR should be independently revertable.
- PR title should follow the same conventional commits format as commit messages (enables changelog generation from PR titles as a fallback).
- Include a short description of *why* the change is needed, not just *what* it does.
- Create feature branches for non-trivial work; name them `type/short-description` (e.g. `feat/oauth-login`, `fix/nil-pointer-api`).

## Hooks & docs

- **Pre-commit hooks**: projects must have hooks for linting, formatting, and tests. Never skip with `--no-verify`.
- **Docs with code**: each commit includes relevant doc updates (README, CHANGELOG, inline comments) when warranted.
