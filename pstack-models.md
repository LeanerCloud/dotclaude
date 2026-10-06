# Model roster for the imported multi-model skills

`interrogate`, `arena`, `reflect`, `figure-it-out` and `architect` fan out across models. Upstream
reads this roster from `~/.cursor/rules/pstack-models.mdc`; on Claude Code it lives here. Tiers are
the ones in `CLAUDE.md` and the `subagent-strategy` skill (Sonnet 5.5 low / Sonnet 5.5 medium / Sonnet 5.5 high, Sonnet 5.5 high the top tier);
OpenAI- and Gemini-backed tooling maps them through the table at the top of `CLAUDE.md`. Set `model`
explicitly on every `Agent` call.

## interrogate reviewers

Every lens runs on **Sonnet 5.5 high**: adversarial review is a top-tier task and nothing escalates above it.
Each lens is its own agent, so the diversity comes from independent contexts and lenses, not from a
persona label. Reviewer independence follows the `subagent-strategy` skill: a lens never reviews code
it wrote, and the same lens agent persists across rounds of its own loop.

| Reviewer | Tier | Lens |
|---|---|---|
| A | Sonnet 5.5 high | correctness and money paths |
| B | Sonnet 5.5 high | security and boundary/input validation |
| C | Sonnet 5.5 high | concurrency, error handling, resource leaks |
| D | Sonnet 5.5 high | reuse and over-engineering (CLAUDE.md §1a and the sixth review dimension) |

Drop D on small diffs. Add a fifth independent Sonnet 5.5 high pass on anything touching money or tenant
isolation.

## arena runners

**Sonnet 5.5 high** for each candidate whose design is still open. **Sonnet 5.5 medium** once the direction is settled and
the candidate is execution against a decided shape.

## arena cross-judge pool

**Sonnet 5.5 high**. The judge must not be the agent that produced the front-runner.

## reflect reviewers

**Sonnet 5.5 high** for all three passes (judgment, tooling, divergent): reflection is a judgement task.
