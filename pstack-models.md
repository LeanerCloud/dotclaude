# Model roster for the imported multi-model skills

`interrogate`, `arena`, `reflect`, `figure-it-out` and `architect` fan out across models. Upstream
reads this roster from `~/.cursor/rules/pstack-models.mdc`; on Claude Code it lives here. Entries use
stable workflow roles resolved through `CLAUDE.md`; host model IDs are selected there.

## interrogate reviewers

The planning/review/verification role runs every adversarial lens and cross-judge. Adversarial lenses remain separate independent agents at the initial review, even when they use the same model. Reuse each lens reviewer on subsequent rounds for the same problem, while requiring it to re-derive current requirements and artifacts rather than rely on prior approval. A fresh reviewer is needed for an authorship or role conflict, anchoring or material miss, stale or polluted context, unrelated scope, or an explicitly independent-review gate.

| Reviewer | Role | Lens |
|---|---|---|
| A | review role | correctness and money paths, max effort |
| B | review role | security and boundary/input validation |
| C | review role | concurrency, error handling, resource leaks |
| D | review role | reuse and over-engineering (CLAUDE.md §1a and the sixth review dimension) |

Drop D on small diffs. Add a fifth independent review-role pass on anything touching money or tenant
isolation.

## arena runners

The planning/review/verification role runs one planning pass per design direction. Use the
implementation role to build a settled arena candidate.

## arena cross-judge pool

The review role runs the cross-judge. The judge must be independent from the agent that produced the
front-runner; reuse it on subsequent rounds for the same problem after it re-derives the current
requirements and artifacts.

## reflect reviewers

The review role runs all three (judgment, tooling, divergent) passes - reflection is a judgement task.
