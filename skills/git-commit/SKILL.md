---
name: git-commit
description: Conventional-commit format, atomic commits, the repo-init-at-task-start rule, the
  mandatory review loop (2 passes, whatever the stakes), and the
  per-project feedback memory garden.
  Invoke before staging or writing a commit message.
---

# Commits — repo init, message format, and the pre-commit review loop

Applies to every project unless the project's `CLAUDE.md` overrides a specific rule. For what happens
after the commit: invoke the `ci-watch` skill after pushing and the `pr-lifecycle` skill when opening
a PR.

## ⚠️ Initialize a repo BEFORE any non-trivial work

The rule is `CLAUDE.md` Git Workflow ("Repo first"). In short:

- **At task start**, not commit time, offer to `git init` a project directory that is not a repo; init without asking only when the user asked you to create the project there.
- **Location exceptions**: never init `~`, system temp or the session scratchpad, `~/Downloads`, `~/Desktop` or similar scratch dirs. When unsure whether a dir is a project, treat it as one.
- **After `git init`**: add a `.gitignore`, commit the starting scaffold, then commit per phase as work lands, never one giant commit at the end.

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

Before every commit, enter a review loop (same discipline as the plan review loop): run **2 review passes** over the staged diff (per `CLAUDE.md` §1), fix what they find, then commit. Do NOT skip, shortcut, or batch this step. The goal is to land clean commits in the first place, so the history doesn't need fix-up commits.

**Review on Opus, as comprehensively as possible: CodeRabbit's lens is the floor, not the ceiling.** This review is judgement-heavy, so run it at Opus tier (the §1c local review loop and the plan-review gate are its analogues, both Opus per `CLAUDE.md` §2), including the hardest, highest-stakes money-path diffs. The six dimensions in `CLAUDE.md` §1 are the baseline; then go wider than any single reviewer would. Review as CodeRabbit would (its Actionable / Nitpick categories, the project's CR config, recurring past CR findings) AND as a demanding staff engineer would, across at least the lenses below. Where a lens names an agent, that agent is its dedicated reviewer for the Delegation fan-out.

- **Architecture & design fit**: does the change belong where it landed, follow the module's patterns, and avoid leaking abstractions?
- **Type design & invariants**: are illegal states unrepresentable, invariants expressed in types rather than asserted at runtime, encapsulation intact? (`pr-review-toolkit:type-design-analyzer`)
- **Silent failures**: swallowed errors, empty catch blocks, fallbacks that mask real problems, `nil`/zero placeholders standing in for absent data (per `CLAUDE.md` §5). (`pr-review-toolkit:silent-failure-hunter`)
- **Test coverage & edge cases**: are the new paths actually exercised, including boundaries, error paths, and the contract (not just the happy path)? (`pr-review-toolkit:pr-test-analyzer`)
- **Security**: beyond OWASP basics: trust boundaries, authz on every new path, secret handling, injection via every new input.
- **Over-engineering & scope**: parameters with no caller, abstractions with one consumer, validation of unreachable states, machinery the current requirement doesn't need. Could a competent colleague have written this in half the lines? See the `coding-standards` skill ("Simplicity & Scope (YAGNI)"). (`pr-review-toolkit:code-simplifier`)
- **Comment accuracy & density**: do comments match the code, or did they rot during edits? Is the diff over-commented (restatements of the next line, rationale essays, review-round references)? **Measure it, don't eyeball it**: run the one-liner in the `coding-standards` skill ("Comments") and check the two per-comment rules - nothing over 2 lines, nothing describing what the code does. Over ~15% means delete until under it or name the exception. (`pr-review-toolkit:comment-analyzer`)
- **Performance & resources**: N+1s, unbounded growth, leaked handles/goroutines, needless allocation on hot paths.
- **API, naming & convention consistency**: does it match the surrounding code's idiom, naming, and the project's documented conventions?

For multi-concern or substantial diffs, fan out the specialised review agents in parallel (see Delegation below) so each lens gets a dedicated pass, then compile. The goal is a PR that lands clean for CodeRabbit AND human reviewers on the first pass. The economics strongly favour this: catching a finding here costs one local pass, while catching it after CR (or a human) flags it costs a push, a 60–120s review wait, a fix commit, another CI pass, and another review round. It is much faster to ship it well the first time — every issue you preempt locally is a full round-trip you don't pay for later. This does not replace the CR loop (CodeRabbit still reviews and you still iterate to a clean pass), it shrinks it toward one pass.

### Each pass

Read the full staged diff (`git diff --cached`) and the relevant unstaged context, and check the six dimensions in `CLAUDE.md` §1, plus the memory garden match:

- **Memory garden match**: Scan the per-project memory at `~/.claude/projects/<project-slug>/memory/feedback_*.md` (and any matching `project_*.md`) and apply every entry whose `**How to apply:**` line matches the changeset. This is the **highest-leverage step** because the entries encode patterns CR already taught us on this project — finding a match here means CR will NOT raise the same nit again. If a finding from the current review surfaces a pattern that's NOT in memory but is generalisable, file the new `feedback_<slug>.md` after the commit lands per §"Per-project feedback memory".

**If CR later finds something this review missed, treat it as a §1 process failure** — not just "CR is a useful second pair of eyes." Either the dimension wasn't checked, the memory-garden scan was skipped, or the specialised reviewer fan-out wasn't dispatched on a substantial diff. Save the lesson (new `feedback_*.md` entry) and tighten the next §1 pass.

### Each iteration

- Print a short summary of issues found before and after fixing them (matches the plan-review-loop format).
- Fixes from pass 1 are reviewed by pass 2; fixes from pass 2 are not reviewed again. No counter resets; after the second pass, commit.

### Multi-commit work

For a sequence of atomic commits implementing one plan: review each commit's staged diff individually AND think about how it interacts with already-committed work in the sequence.

### Delegation

For staged changes touching multiple concerns (Go + TS + Terraform) or any substantial diff, launch specialised review agents in parallel and compile their findings before committing; each agent is one comprehensive lens, and together they approximate a full review board that no single pass matches. Beyond a general reviewer (`feature-dev:code-reviewer` or `pr-review-toolkit:code-reviewer`), run each lens agent named in the list above so nothing slips between them.

Spawn each on the appropriate tier (the review judgement itself is Opus-class, including for the hardest money-path diffs; mechanical single-file diffs can drop to Sonnet), aggregate the findings, dedupe overlaps, and resolve every actionable item before the commit lands.

### Fix before committing, never after

If the review finds issues, fix them in the same staged changeset — do not commit and then create a follow-up fix commit. The history should not contain "oops, fixing previous commit" patterns when the issue could have been caught before the commit landed.

## Post-commit sanity check

After committing, run a quick sanity scan (`git show HEAD`) to catch anything the pre-commit loop missed. If this finds issues, treat it as a process failure (the pre-commit loop should have caught them). Fix-forward in a new commit only when strictly necessary (e.g., pre-commit hook caught a legitimate issue that required the commit to land first).

**Hooks may silently not run in a worktree.** When a repo sets `core.hooksPath` to a gitignored, install-generated directory (husky's `.husky/_` is the common case), that path exists only where the install ran — usually the main checkout. Git skips hooks with no warning when it doesn't resolve, so commits from a worktree can quietly bypass lint, formatting and generated-artifact rebuilds. Since §1b puts non-trivial work in worktrees, check once per worktree (`git config core.hooksPath`, then confirm the directory exists) and run the checks by hand if it doesn't. Never paper over it with `--no-verify`.

## Hooks & docs

- **Pre-commit hooks**: projects must have hooks for linting, formatting, and tests. Never skip with `--no-verify`.
- **Docs with code**: each commit includes relevant doc updates (README, CHANGELOG, inline comments) when warranted.

## Per-project feedback memory

The per-project memory garden (`~/.claude/projects/<project-slug>/memory/feedback_*.md`, entry structure per `CLAUDE.md` §3) is read in four contexts:

1. **Before / during writing new code on any branch.** Skim relevant entries up front and apply them proactively. This is the cheapest place to apply a known rule and the biggest leverage.
2. **During the pre-commit review gate**, as the memory garden match above.
3. **During the §1 post-implementation review gate.** Same usage.
4. **Before pushing CR-fix commits.** After CodeRabbit lands a review pass, scan the memory to catch other matching entries CR might raise next round.

**Write to it after any CR review pass** when an addressed finding is a recurring class of issue (style / idiom / cross-cutting concern), searching first and preferring a new PR citation on an existing entry over a parallel file. The quality bar: a clear one-line rule, a `**Why:**` with at least one PR citation, and a `**How to apply:**` naming the concrete code location or pattern to scan for. One-off PR-specific bugs (a typo, a logic error unique to this PR's feature, a wrong fixture) do not qualify.
