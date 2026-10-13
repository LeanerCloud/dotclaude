---
name: git-commit
description: Conventional-commit format, atomic commits, the repo-init-at-task-start rule, the
  mandatory review loop (2 passes, whatever the stakes), and the
  per-project feedback memory garden.
  Invoke before staging or writing a commit message.
---

# Commits - repo init, message format, and the pre-commit review loop

Applies to every project unless its `CLAUDE.md` overrides a rule. After the commit: `ci-watch` after pushing, `pr-lifecycle` when opening a PR.

## ⚠️ Initialize a repo BEFORE any non-trivial work

The rule is `AGENTS.md` section Git (repo init at task start). In short:

- **At task start**, not commit time, offer to `git init` a project directory that is not a repo; init without asking only when the user asked you to create the project there.
- **Location exceptions**: never init `~`, system temp or the session scratchpad, `~/Downloads`, `~/Desktop` or similar scratch dirs. When unsure whether a dir is a project, treat it as one.
- **After `git init`**: add a `.gitignore`, commit the starting scaffold, then commit per phase as work lands, never one giant commit at the end.

## Commit messages

- **Conventional commits**: `type(scope): subject`, e.g. `feat(auth): add OAuth2 login`, `fix(api): handle nil pointer on empty response`. Types: `feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `perf`, `ci`, `build`. Scope optional (omit in small repos). Enables changelog/release notes (`infra-ops` CI/CD).
- Subject: imperative, lowercase, no trailing period, <=72 chars. Body (when needed): bullets on the *why*, wrapped at 72.
- **NEVER** mention Anthropic or Claude in commit messages (no Co-Authored-By lines).
- **No heredocs for `git commit -m`** (`$(cat <<'EOF' ...)` triggers approval prompts and breaks pre-commit hooks that stash unstaged changes). For each commit **`Write` a fresh file** under `/tmp/claude/` (`mkdir -p /tmp/claude` first; rationale in `tool-usage` § "Two script locations by lifetime"), commit with `git commit -F <path>`, then `rm` that file.
  - Use a unique name per commit (`/tmp/claude/commit-<short-subject>.txt` or timestamped). `Write` echoes the content, so no separate `Read`. Never `Edit` an existing message file; always `Write` a new one.

## Atomic commits

- One distinct, independently revertable piece of functionality per commit; stage small chunks within a file separately.
- **Don't commit throwaway or personal-only artefacts** (one-off scripts, debugging helpers, scratch files, session tooling, ad-hoc migration scripts, curl loops, log one-liners). Ask: would another contributor find this useful? If not, keep it in `/tmp/claude/` or delete it. If genuinely reusable, promote it to `scripts/`, `tools/` or a `Makefile` target with docs and tests.

## ⚠️ Mandatory pre-commit review loop - NO EXCEPTIONS

Before every commit run **2 review passes** over the staged diff (per `AGENTS.md` §1), fix what they find, then commit. A staged diff whose tree hash was already reviewed clean is not re-reviewed (`AGENTS.md` §1, same-hash rule). Don't skip, shortcut or batch this step; the goal is clean commits with no fix-up history.

**Review on Sonnet 5.5 high, as comprehensively as possible: CodeRabbit's lens is the floor, not the ceiling.** The judgement is top-tier (like the §1c loop and plan-review gate, per `AGENTS.md` §2), even for the hardest money-path diffs. The six `AGENTS.md` §1 dimensions are the baseline; review as CodeRabbit would (Actionable / Nitpick categories, the project's CR config, recurring past findings) AND as a demanding staff engineer, across at least:

- **Architecture & design fit**: belongs where it landed, follows the module's patterns, no leaked abstractions.
- **Type design & invariants**: illegal states unrepresentable, invariants in types not runtime asserts, encapsulation intact (brief: `lenses/type-design.md`).
- **Silent failures**: swallowed errors, empty catch blocks, masking fallbacks, `nil`/zero placeholders for absent data (`AGENTS.md` §5) (brief: `lenses/silent-failures.md`).
- **Test coverage & edge cases**: new paths exercised, including boundaries, error paths and the contract, not just the happy path.
- **Security**: beyond OWASP basics: trust boundaries, authz on every new path, secret handling, injection via every new input.
- **Over-engineering & scope**: parameters with no caller, single-consumer abstractions, validation of unreachable states, machinery the requirement doesn't need (`coding-standards` "Simplicity & Scope (YAGNI)").
- **Comment accuracy & density**: comments match the code; no restatements, rationale essays or review-round references. **Measure it**: run the one-liner in `coding-standards` ("Comments") and check the two per-comment rules (nothing over 2 lines, nothing describing what the code does). Over ~15% means delete until under it or name the exception.
- **Performance & resources**: N+1s, unbounded growth, leaked handles/goroutines, hot-path allocation.
- **API, naming & convention consistency** with surrounding code and documented conventions.

For multi-concern or substantial diffs, fan out specialised review agents in parallel (Delegation below), then compile. A finding caught here costs one local pass; after CR or a human flags it, it costs a push, a 60-120s wait, a fix commit, another CI pass and another review round. This doesn't replace the CR loop; it shrinks it toward one pass.

### Each pass

Read the full staged diff (`git diff --cached`) and the relevant unstaged context; check the six `AGENTS.md` §1 dimensions plus:

- **Sensitive info in the message and the diff**: no secret, account/subscription/project ID, ARN with an account number, OS/cloud username, email, home-dir path or internal host (AGENTS.md Git); pasted logs and output redacted. Before `git commit -F`, scan the message file and the added lines (`git diff --cached | grep '^+'`) with `grep -inE '\b[0-9]{12}\b|[0-9]{4}-[0-9]{4}-[0-9]{4}|/Users/|/home/|[a-z0-9._-]+@[a-z0-9-]+\.[a-z]+|AKIA|ghp_|github_pat_|xox[bp]-|BEGIN [A-Z ]*PRIVATE KEY|password *=' | grep -viE 'noreply@|123456789012|@example\.'` and resolve every hit. Use the same scan on PR bodies and comments before posting.
- **Memory garden match**: scan `~/.claude/projects/<project-slug>/memory/feedback_*.md` (and matching `project_*.md`) and apply every entry whose `**How to apply:**` matches the changeset. This is the **highest-leverage step**: the entries encode patterns CR already taught on this project. If this review surfaces a generalisable pattern not in memory, file a new `feedback_<slug>.md` after the commit lands (§"Per-project feedback memory").

**If CR later finds something this review missed, treat it as a §1 process failure** (a dimension unchecked, the memory scan skipped, or no fan-out on a substantial diff). Save the lesson as a new `feedback_*.md` entry and tighten the next pass.

### Each iteration

- Print a short summary of issues found before and after fixing them.
- Pass 2 reviews pass 1's fixes; pass 2's fixes are not reviewed again. No counter resets; after the second pass, commit.

### Multi-commit work

For a sequence of atomic commits implementing one plan, review each commit's staged diff individually AND how it interacts with already-committed work.

### Delegation

For diffs touching multiple concerns (Go + TS + Terraform) or any substantial diff, fan out up to 3-4 `general-purpose` subagents in parallel, each given the full text of the lens brief file(s) it owns (`~/.claude/skills/git-commit/lenses/`; group related lenses) plus one general code reviewer. Spawn each on the appropriate tier (the review judgement is top-tier, even for the hardest money-path diffs; mechanical single-file diffs can drop to Sonnet 5.5 low), then aggregate, dedupe overlaps and resolve every actionable item before committing.

### Fix before committing, never after

Fix findings in the same staged changeset; don't commit and then add a "fixing previous commit" commit.

## Post-commit sanity check

After committing, scan `git show HEAD`. If it finds something, the pre-commit loop failed; fix-forward in a new commit only when strictly necessary (e.g. a hook caught a legitimate issue that required the commit to land first).

**Hooks may silently not run in a worktree.** When `core.hooksPath` points at a gitignored, install-generated directory (husky's `.husky/_`), it exists only where the install ran (usually the main checkout); git skips hooks with no warning when it doesn't resolve, so worktree commits can bypass lint, formatting and generated-artifact rebuilds. Check once per worktree (`git config core.hooksPath`, then confirm the directory exists) and run the checks by hand if it doesn't. Never paper over it with `--no-verify`.

## Hooks & docs

- Projects must have hooks for linting, formatting and tests. Never `--no-verify`.
- Each commit includes relevant doc updates (README, CHANGELOG, inline comments) when warranted.

## Per-project feedback memory

The garden (`~/.claude/projects/<project-slug>/memory/feedback_*.md`, entry structure per `AGENTS.md` §3) is read in four contexts:

1. **Before / during writing new code on any branch**: skim relevant entries and apply proactively (cheapest place, biggest leverage).
2. **During the pre-commit review gate** (the memory garden match above).
3. **During the §1 post-implementation review gate.**
4. **Before pushing CR-fix commits**: scan for other matching entries CR might raise next round.

**Write to it after any CR review pass** when an addressed finding is a recurring class of issue (style / idiom / cross-cutting concern). Search first and prefer a new PR citation on an existing entry over a parallel file. Quality bar: a clear one-line rule, a `**Why:**` with at least one PR citation, and a `**How to apply:**` naming the code location or pattern to scan for. One-off PR-specific bugs (typo, logic error unique to the feature, wrong fixture) don't qualify.
