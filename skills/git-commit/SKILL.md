---
name: git-commit
description: Conventional-commit format, atomic-commit rules, the repo-init-at-task-start rule, and
  the mandatory review loop that runs to three clean passes before every commit. Invoke before
  staging or writing a commit message.
---

# Commits — repo init, message format, and the pre-commit review loop

Applies to every project unless the project's `CLAUDE.md` overrides a specific rule. For what happens
after the commit: invoke the `ci-watch` skill after pushing and the `pr-lifecycle` skill when opening
a PR.

## ⚠️ Initialize a repo BEFORE any non-trivial work — never build in an unversioned tree

This is the first thing to check, because everything else here is worthless without a repo to commit to. **The trigger is task start, not commit time** — if you only notice there's no repo when you finally go to commit, the per-step history is already gone.

- **If you're working in a PROJECT directory that is not a git repo, `git init` it immediately** — at the very start of the task, before the first file change. "Not a repo" = the environment reports `Is a git repository: false`, `git rev-parse --git-dir` fails, or there's no `.git`. Fold this check into the `CLAUDE.md` §0 "understand the codebase first" bootstrap so it fires at session start on any project. A "project directory" is any codebase/deliverable you're building or modifying — the thing that would eventually have a repo, a README, a build.
- **Location exceptions — do NOT `git init` these, even for multi-file work**: the home directory itself (`~`), system temp / the session scratchpad (`/tmp`, `/private/tmp/...`, `$TMPDIR`), and ad-hoc non-project dirs like `~/Downloads`, `~/Desktop`, `~/.config`-style dotdirs. These are scratch/staging space, not projects — versioning them is noise. The test: *"am I building a project/deliverable here?"* → repo required. *"Is this a home/temp/downloads scratch location?"* → no repo. When in doubt about whether a dir is a project, it is (init it) — the false-negative (unversioned real work) is far more costly than a stray `.git` in a scratch dir.
- **Creating a repo is safe and purely additive — it is the OPPOSITE of the "never destroy `.git`" rule (`CLAUDE.md` §9).** Do not let caution about *deleting* `.git` bleed into reluctance to *create* one. `git init` on a non-repo cannot lose data.
- **Never do multi-step or multi-phase work in an unversioned tree.** Without a repo you cannot make the small atomic commits this document requires, and — worse — intermediate states are unrecoverable: editing files in place destroys the per-step history you were supposed to commit. A crash, a bad edit, or a botched mid-way refactor then has no fallback, and there is no honest way to reconstruct the per-phase commits after the fact.
- **After `git init`**: add/confirm a `.gitignore`, make an initial commit of the starting scaffold, then commit atomically as each phase/task/change lands (per Atomic commits below). For a long autonomous build this means **a commit per phase**, landed as you go — NOT one giant commit at the end. If you catch yourself many edits deep with zero commits, stop and fix it: `git init` now, commit the current verified state as a baseline (honestly labelled — you can split it into coarse logical commits for navigability but don't fabricate per-phase history that no longer exists), and commit atomically from that point on.
- Exempt only genuinely trivial one-shot actions (answer a question, read/inspect a file). The moment you're about to make more than a couple of related edits, the repo must exist first.

## Commit messages

- Use **conventional commits** format: `type(scope): subject` — e.g. `feat(auth): add OAuth2 login`, `fix(api): handle nil pointer on empty response`.
  - Common types: `feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `perf`, `ci`, `build`.
  - `scope` is optional but useful for larger repos; omit when the repo is small.
  - This enables automated changelog and release notes generation (see the `infra-ops` skill CI/CD).
- Subject line: imperative mood, lowercase, no trailing period, ≤72 chars.
- Body (when needed): bullet points explaining the *why*, not the *what*; wrap at 72 chars.
- **NEVER** mention Anthropic or Claude in commit messages (no Co-Authored-By lines).
- **Avoid heredocs for `git commit -m`**: patterns like `git commit -m "$(cat <<'EOF' ... EOF)"` are fragile — the `$(...)` command substitution can trigger approval prompts, the heredoc-inside-substitution interacts badly with pre-commit hooks that stash unstaged changes (the stash/restore cycle has been observed to fail repeatedly), and quoting/escaping bugs are easy to introduce. Instead, for each commit **`Write` a fresh file** to `/tmp/claude/` with a unique name, commit via `git commit -F <path>`, and delete the file once the commit has landed.
  - **Why `/tmp/claude/`**: see the `tool-usage` skill § "Two script locations by lifetime" for the rationale. Create the directory lazily with `mkdir -p /tmp/claude` before the first write if it doesn't exist.
  - **One file per commit, always created fresh with `Write`**: use a unique name like `/tmp/claude/commit-<short-subject>.txt` or include a timestamp so consecutive commits never collide. Because `Write` echoes the full file content back on creation, the final message is visible in full before the commit lands — no separate `Read` pass is needed. Do NOT `Edit` an existing commit-msg file in place; always `Write` a fresh one.
  - **Clean up after every commit**: once `git commit -F` has succeeded, `rm` the file. Single-file deletion on an explicit path is safe and avoids leaving stale buffers behind.

## Atomic commits

- **Small atomic commits**: one distinct piece of functionality per commit; independently revertable.
- Stage and commit small chunks within a file separately rather than the whole file at once.
- **Don't commit throwaway or personal-only artefacts**: one-off scripts, debugging helpers, scratch files, and session-specific tooling do NOT belong in the project repo. Before staging, ask: "would another contributor find this useful, or is this just something I needed for this session?" If the answer is personal/temporary, keep it in `/tmp/claude/` (outside any repo) or delete it. Concrete examples of things to *not* commit: ad-hoc data-migration scripts that ran once, curl loops for manual testing, log-parsing one-liners, scaffolding scripts that bootstrap local dev state. If a throwaway script turns out to be genuinely reusable, promote it to a proper committed location (`scripts/`, `tools/`, `Makefile` target) with documentation and tests — don't just leave it in the tree because it was convenient.

## ⚠️ Mandatory pre-commit review loop — NO EXCEPTIONS

Before every commit, enter a review loop (same discipline as the plan review loop). Do NOT commit after a single pass — iterate until **3 consecutive review passes find zero issues**. Do NOT skip, shortcut, or batch this step. The goal is to land clean commits in the first place, so the history doesn't need fix-up commits.

**Review on Opus, as comprehensively as possible — CodeRabbit's lens is the floor, not the ceiling.** This review is judgement-heavy, so run it at Opus tier (the §1c local review loop and the plan-review gate are its analogues — both Opus per `CLAUDE.md` §2); escalate to the Fable reserve only for the hardest / highest-stakes money-path diffs. The six dimensions above are the baseline; then go wider than any single reviewer would. Review as CodeRabbit would (its Actionable / Nitpick categories, the project's CR config, recurring past CR findings) AND as a demanding staff engineer would, across at least:

- **Architecture & design fit** — does the change belong where it landed, follow the module's patterns, and avoid leaking abstractions?
- **Type design & invariants** — are illegal states unrepresentable, invariants expressed in types rather than asserted at runtime, encapsulation intact?
- **Silent failures** — swallowed errors, empty catch blocks, fallbacks that mask real problems, `nil`/zero placeholders standing in for absent data (per `CLAUDE.md` §5).
- **Test coverage & edge cases** — are the new paths actually exercised, including boundaries, error paths, and the contract (not just the happy path)?
- **Security** — beyond OWASP basics: trust boundaries, authz on every new path, secret handling, injection via every new input.
- **Over-engineering & scope** — parameters with no caller, abstractions with one consumer, validation of unreachable states, machinery the current requirement doesn't need. Could a competent colleague have written this in half the lines? See the `coding-standards` skill ("Simplicity & Scope (YAGNI)").
- **Comment accuracy & density** — do comments match the code, or did they rot during edits? Is the diff over-commented (restatements of the next line, rationale essays, review-round references)? See the `coding-standards` skill ("Comments").
- **Performance & resources** — N+1s, unbounded growth, leaked handles/goroutines, needless allocation on hot paths.
- **API, naming & convention consistency** — does it match the surrounding code's idiom, naming, and the project's documented conventions?

For multi-concern or substantial diffs, fan out the specialised review agents in parallel (see Delegation below) so each lens gets a dedicated pass, then compile. The goal is a PR that lands clean for CodeRabbit AND human reviewers on the first pass. The economics strongly favour this: catching a finding here costs one local pass, while catching it after CR (or a human) flags it costs a push, a 60–120s review wait, a fix commit, another CI pass, and another review round. It is much faster to ship it well the first time — every issue you preempt locally is a full round-trip you don't pay for later. This does not replace the CR loop (CodeRabbit still reviews and you still iterate to a clean pass), it shrinks it toward one pass.

### Each pass

Read the full staged diff (`git diff --cached`) and the relevant unstaged context, and systematically check all six dimensions:

- **Completeness**: Does the commit deliver what it claims? Nothing missing? All touched files consistent with the commit message? Tests updated for the changed behaviour?
- **Correctness**: Any logic errors, off-by-ones, wrong assumptions, broken invariants, stale references, type mismatches, leftover debug code, unused imports?
- **Security**: Any injection vectors, auth bypasses, secrets exposure, missing input validation, OWASP top 10 violations?
- **Bugs**: Race conditions, null derefs, edge cases, resource leaks, error handling gaps, broken tests, stale mocks?
- **Duplication**: Does any new function/type/helper in this diff replicate logic that already exists in the project? Grep for distinctive identifiers, constants, or phrases from the new code to catch near-duplicates. If a duplicate is found, stop and either reuse the existing code or refactor it to cover both cases (per `CLAUDE.md` step 1a) — do NOT commit parallel copies.
- **Memory garden match**: Scan the per-project memory at `~/.claude/projects/<project-slug>/memory/feedback_*.md` (and any matching `project_*.md`) and apply every entry whose `**How to apply:**` line matches the changeset. This is the **highest-leverage step** because the entries encode patterns CR already taught us on this project — finding a match here means CR will NOT raise the same nit again. If a finding from the current review surfaces a pattern that's NOT in memory but is generalisable, file the new `feedback_<slug>.md` after the commit lands per §"Per-project feedback memory".

**If CR later finds something this review missed, treat it as a §1 process failure** — not just "CR is a useful second pair of eyes." Either the dimension wasn't checked, the memory-garden scan was skipped, or the specialised reviewer fan-out wasn't dispatched on a substantial diff. Save the lesson (new `feedback_*.md` entry) and tighten the next §1 pass.

### Each iteration

- Print a short summary of issues found before and after fixing them (matches the plan-review-loop format).
- An iteration with fixes resets the clean-pass counter — you need 3 clean passes *after* the last fix.

### Multi-commit work

For a sequence of atomic commits implementing one plan: review each commit's staged diff individually AND think about how it interacts with already-committed work in the sequence.

### Delegation

For staged changes touching multiple concerns (Go + TS + Terraform) or any substantial diff, launch specialised review agents in parallel and compile their findings before committing — each agent is one comprehensive lens, and together they approximate a full review board that no single pass matches. Beyond a general reviewer (`feature-dev:code-reviewer` or `pr-review-toolkit:code-reviewer`), use the focused lenses so nothing slips between them:

- `pr-review-toolkit:silent-failure-hunter` — swallowed errors, inadequate error handling, fallbacks that mask failures.
- `pr-review-toolkit:type-design-analyzer` — encapsulation, invariant expression, type-design quality.
- `pr-review-toolkit:pr-test-analyzer` — test coverage and edge-case completeness for the new behaviour.
- `pr-review-toolkit:comment-analyzer` — comment accuracy and rot, especially after large doc/comment edits.
- `pr-review-toolkit:code-simplifier` — clarity, dead code, and duplication that can be collapsed.

Spawn each on the appropriate tier (the review judgement itself is Opus-class, with Fable held in reserve for the hardest money-path diffs; mechanical single-file diffs can drop to Sonnet), aggregate the findings, dedupe overlaps, and resolve every actionable item before the commit lands.

### Fix before committing, never after

If the review finds issues, fix them in the same staged changeset — do not commit and then create a follow-up fix commit. The history should not contain "oops, fixing previous commit" patterns when the issue could have been caught before the commit landed.

## Post-commit sanity check

After committing, run a quick sanity scan (`git show HEAD`) to catch anything the pre-commit loop missed. If this finds issues, treat it as a process failure (the pre-commit loop should have caught them). Fix-forward in a new commit only when strictly necessary (e.g., pre-commit hook caught a legitimate issue that required the commit to land first).

**Hooks may silently not run in a worktree.** When a repo sets `core.hooksPath` to a gitignored, install-generated directory (husky's `.husky/_` is the common case), that path exists only where the install ran — usually the main checkout. Git skips hooks with no warning when it doesn't resolve, so commits from a worktree can quietly bypass lint, formatting and generated-artifact rebuilds. Since §1b puts non-trivial work in worktrees, check once per worktree (`git config core.hooksPath`, then confirm the directory exists) and run the checks by hand if it doesn't. Never paper over it with `--no-verify`.

## Hooks & docs

- **Pre-commit hooks**: projects must have hooks for linting, formatting, and tests. Never skip with `--no-verify`.
- **Docs with code**: each commit includes relevant doc updates (README, CHANGELOG, inline comments) when warranted.
