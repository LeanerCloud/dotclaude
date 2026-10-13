---
name: review-staged-diff
description: Run a CodeRabbit-style review of the staged diff across the six review dimensions plus
  the project's feedback memory, and report findings as a table. Invoke before committing, to catch
  locally what CR would otherwise catch after the push.
---

# Review the staged diff

CR-style review of the staged diff against the six review dimensions PLUS this project's `feedback_*.md` memory garden (`git-commit` "Per-project feedback memory"), to catch what CodeRabbit would before the commit lands.

**Inputs, in order**:
1. `git diff --cached`. If empty, use `git diff HEAD~1..HEAD` (the last commit) and tell the user.
2. `ls ~/.claude/projects/<project-slug>/memory/feedback_*.md` (`<project-slug>` = absolute working directory with each `/` replaced by `-`, e.g. `-Users-cristi-src-mslc`); read entries whose names suggest relevance (Go diff: `feedback_*go*`, concurrency/context entries; Terraform: `feedback_tf_*`).
3. The project's `CLAUDE.md` (root or `~/.claude/projects/<slug>/CLAUDE.md`).

**Per-dimension pass** (read the full diff once per lens, serially, not in parallel within a pass):

| Dimension | What to check |
|---|---|
| **Completeness** | Delivers what the commit message claims? Tests updated? Touched files consistent? |
| **Correctness** | Logic errors, off-by-ones, wrong assumptions, broken invariants, stale references, type mismatches, leftover debug code, unused imports? |
| **Security** | Injection, auth bypass, secrets exposure, missing input validation, OWASP top 10? |
| **Bugs** | Races, null derefs, edge cases, resource leaks, error-handling gaps, broken tests, stale mocks? |
| **Duplication** | Does a new helper replicate existing logic? Grep distinctive identifiers from the new code. |
| **Memory garden match** | For each `feedback_*.md` whose `**How to apply:**` matches, verify conformance; cite the entry name. |

**Findings table**:

```
| # | Severity | Dimension | File:Line | Finding | Suggested fix |
|---|---|---|---|---|---|
| 1 | Major | Bugs | foo.go:42 | nil deref when X | guard with `if x != nil` before access |
```

Severity: Major (breaks behaviour or hides a bug), Minor (style/nitpick), Info (not blocking). One row per finding.

**Fan-out**: if the diff touches more than one of (Go, TypeScript, Terraform, frontend HTML/CSS, SQL/migration, IaC, Docker) or exceeds ~400 lines, spawn the specialised review agents from `git-commit` ("Delegation") in parallel (silent-failure-hunter, type-design-analyzer, pr-test-analyzer, comment-analyzer, code-simplifier) and compile into the same table. Otherwise review inline.

**Output**:
1. The findings table (or "No issues found").
2. Summary: dimension counts, matched memory-garden entries, total Major / Minor / Info.
3. Any generalisable new pattern worth memorialising, called out for the human to decide after the commit lands (`git-commit` "Per-project feedback memory").

Do NOT modify the staged diff; this is a review pass. The user fixes and re-runs once more: 2 runs in total, whatever the stakes (§1 review gate); a staged diff whose tree hash was already reviewed clean is not re-reviewed (`AGENTS.md` §1, same-hash rule).
