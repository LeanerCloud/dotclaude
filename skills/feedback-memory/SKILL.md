---
name: feedback-memory
description: The per-project `feedback_*.md` memory garden - when to read it (write-time, both
  review gates, before CR-fix pushes), when to write to it, and the quality bar for an entry. Invoke
  when writing code or reviewing a diff on a project that has one.
---

# Per-project feedback memory

Every project has a memory garden at `~/.claude/projects/<project-slug>/memory/` containing `feedback_*.md` entries -- transferable patterns that have been agreed upon (often via prior CR rounds, sometimes filed proactively). Each entry encodes: a one-line rule, a `**Why:**` line with a PR citation, and a `**How to apply:**` line naming the concrete code location or scan pattern.

**Read the memory garden in four contexts:**

1. **Before / during writing new code on any branch.** Skim relevant `feedback_*.md` entries up front and apply them proactively. Catches patterns at write-time and saves a downstream CR cycle. This is the cheapest place to apply a known rule.
2. **During the §1 pre-commit 3-pass review gate.** Use the memory as one input to the checklist alongside Completeness / Correctness / Security / Bugs / Duplication. Any pattern that matches the changeset should be cross-checked.
3. **During the §1 post-implementation review gate.** Same usage -- the memory is a fast checklist source the reviewer can apply alongside the other dimensions.
4. **Before pushing CR-fix commits.** After CodeRabbit lands a review pass, scan the memory before pushing fixes to catch any other matching entries CR might raise next round.

**Write to the memory garden after any CR review pass.** Evaluate each addressed finding: if it represents a recurring class of issue (style / idiom / cross-cutting concern -- NOT a one-off PR-specific bug), write a new `feedback_<slug>.md` entry following the schema in that project's `MEMORY.md`. Before creating a new file, search existing entries to avoid duplicates -- prefer updating the `**Why:**` line of an existing entry with the new PR citation over creating a parallel entry.

The quality bar for a new memory entry: a clear one-line rule + a `**Why:**` with at least one PR citation + a `**How to apply:**` that names the concrete code location or pattern to scan for. Entries that are too vague to trigger a specific check are not useful. One-off PR-specific bugs (a typo, a logic error unique to this PR's feature, a test fixture that was just wrong) do not qualify.

This keeps the memory garden growing AND used. The biggest leverage is the write-time application (context 1) -- catching the issue before it ships, not after CR raises it.
