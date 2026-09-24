---
name: review-and-implement
description: Run 2 adversarial review passes over a plan, then implement it in distinct atomic
  commits with tests. Invoke when a plan is written and ready to be hardened and built.
---

# Review the plan twice, agree with the reviewer, then implement

Run **2 adversarial review passes** over the plan, act on what each finds, then go with it. Two passes whatever the stakes, and do not re-review until a pass finds nothing, because on a long plan that bar is never reached: later rounds start reporting contradictions the previous round's own fixes introduced, and the loop stops converging.

Keep the SAME reviewer for both passes and continue it via `SendMessage` rather than respawning a fresh one, so it keeps its context. When the second pass still leaves findings you disagree with, talk them through with that reviewer until you both agree the plan is good, and proceed on that agreement instead of running another round.

In each pass print a summary of the issues found before and after fixing them. Then implement the plan in distinct commits, writing tests as you go.

If two passes in a row fail to reduce the serious findings, the plan's structure is the problem: restructure or shrink it rather than reviewing it again.
