---
name: review-and-implement
description: Review a plan in a loop until three consecutive passes find nothing, then implement it
  in distinct atomic commits with tests. Invoke when a plan is written and ready to be hardened and
  built.
---

# Review the plan to three clean passes, then implement

Enter a loop in which you do a thorough review of the plan and fix any issues with it, until no more issues are found for 3 consecutive thorough review loops. In each loop print the summary of the found issues before/after you fix them. Once no changes are needed please proceed to implement the plan in distinct commits and writing tests as you go.
