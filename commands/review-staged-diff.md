Run a CR-style review of the staged diff against `git-workflow.md` §1's six dimensions PLUS this project's `feedback_*.md` memory garden. Goal: catch what CodeRabbit would catch, BEFORE the commit lands, so the post-push CR loop converges in one pass.

**Inputs to gather first** (in this order):

1. `git diff --cached` — the staged changeset under review. If empty, also try `git diff HEAD~1..HEAD` to review the most recent commit instead and tell the user that's what you're doing.
2. `ls ~/.claude/projects/<project-slug>/memory/feedback_*.md` — the per-project memory garden. Resolve `<project-slug>` from the working directory + the format in `~/.claude/CLAUDE.md` §3. Read the entries whose names hint at relevance to the diff (e.g. if the diff is Go code, read the `feedback_*go*` and any concurrency/context entries; if Terraform, read the `feedback_tf_*` entries).
3. The project's `CLAUDE.md` (root or `~/.claude/projects/<slug>/CLAUDE.md`) — for project-specific overrides.

**Per-dimension pass** (read the full staged diff with each lens; do not parallelise within a single pass — humans + CR review serially across dimensions, so do the same):

| Dimension | What to check |
|---|---|
| **Completeness** | Does the diff deliver what its commit message claims? Tests updated for the new behaviour? All touched files consistent? |
| **Correctness** | Logic errors, off-by-ones, wrong assumptions, broken invariants, stale references, type mismatches, leftover debug code, unused imports? |
| **Security** | Injection, auth bypass, secrets exposure, missing input validation, OWASP top 10? |
| **Bugs** | Race conditions, null derefs, edge cases, resource leaks, error handling gaps, broken tests, stale mocks? |
| **Duplication** | Does any new helper replicate logic that already exists in the project? Grep for distinctive identifiers from the new code. |
| **Memory garden match** | For each `feedback_*.md` entry whose `**How to apply:**` matches the changeset, verify the diff conforms. Cite the entry name when reporting a match. |

**Findings table format**:

```
| # | Severity | Dimension | File:Line | Finding | Suggested fix |
|---|---|---|---|---|---|
| 1 | Major | Bugs | foo.go:42 | nil deref when X | guard with `if x != nil` before access |
```

Severity: Major (would break behaviour or hide a bug), Minor (style / nitpick), Info (worth flagging but not blocking). One row per finding; do not bundle.

**Fan-out for substantial diffs**: if the staged diff touches more than one of (Go, TypeScript, Terraform, frontend HTML/CSS, SQL/migration, IaC, Docker), or exceeds ~400 lines, spawn the specialised review agents from `git-workflow.md` §"Delegation" in parallel (silent-failure-hunter, type-design-analyzer, pr-test-analyzer, comment-analyzer, code-simplifier) and compile their findings into the same table. Otherwise inline the review.

**Output**:

1. The findings table (or "No issues found" if clean).
2. A short summary: dimension counts, memory-garden entries that matched, total Major / Minor / Info.
3. If any finding suggests memorialising a new pattern (a recurring class CR has not yet flagged but is generalisable), call it out at the end so the human can decide whether to add it to memory after the commit lands per `git-workflow.md` §"Per-project feedback memory".

Do NOT modify the staged diff yourself; this command is a review pass, not an edit pass. The user reviews the findings, fixes locally, and re-runs the command until 3 consecutive runs return "No issues found" (the §1 three-clean-pass gate).
