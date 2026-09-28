---
name: review-and-implement
description: Review a plan until a pass finds nothing (three consecutive clean passes for
  high-stakes changes), then implement it in distinct atomic commits with tests. Invoke when a plan
  is written and ready to be hardened and built.
---

# Review the plan until it is clean, then implement

The planning/review/verification role must hand the implementation role a self-contained plan. It names the exact paths and symbols to change, the chosen design and interfaces or helpers to reuse, expected behavior with real examples, edge and error cases, affected callers or migrations, scope limits, ordered atomic tasks, prerequisites, and exact test or local-scenario commands with expected outcomes and verification limits. Scale the detail to the task and record settled decisions instead of speculative scaffolding.

Before implementation, start the planning/review/verification role in a fresh context separate from the author, then reuse that reviewer across plan revisions, review and fix loops, and final local verification for the same problem. The reviewer re-derives the current requirements, reads the current full relevant artifacts, challenges assumptions and simpler alternatives, and runs independent local verification; it never relies on a previous approval.

A new gate or SHA requires new evidence, not automatically a new agent. Start a new reviewer only for an authorship or role conflict, anchoring or material miss, stale or polluted context, unrelated scope, or an explicitly independent-review gate.

Review the plan thoroughly and fix every issue found, then re-review what changed; stop once a pass finds nothing. For high-stakes plans (money or data-mutation paths, security or auth, migrations, or a fix for something a previous "fix" failed to resolve) require three consecutive clean passes instead. Record each pass's findings and fixes. Material ambiguity or a design choice goes back to the planning/review/verification role; the implementation role does not silently decide it.

The implementation role may run implementation checks, but those checks do not replace independent local verification by the verification role against the final files or revision and the real scenario. Once the review is clean, proceed to implement the plan in distinct atomic commits, writing tests as you go.
