---
name: review-and-implement
description: Review a plan until a pass finds nothing (three consecutive clean passes for
  high-stakes changes), then implement it in distinct atomic commits with tests. Invoke when a plan
  is written and ready to be hardened and built.
---

# Review the plan until it is clean, then implement

Review the plan thoroughly and fix every issue found, then re-review what changed; stop once a pass finds nothing. For high-stakes plans (money or data-mutation paths, security or auth, migrations, or a fix for something a previous "fix" failed to resolve) require three consecutive clean passes instead. In each pass print a summary of the issues found before and after fixing them. Once no changes are needed, implement the plan in distinct commits, writing tests as you go.
