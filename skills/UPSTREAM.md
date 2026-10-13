# Imported skills - what came from where, and what didn't

The skills written for this repo are plain directories. Everything else in `skills/` is curated
from a third-party suite, pinned as a git submodule under [`../upstreams/`](../upstreams/). Nothing
is copy-pasted: an imported skill is either a **symlink** onto its submodule, or a **trampoline**
whose own body is a pointer at the upstream one. Both keep the source of truth upstream, so
`git submodule update --remote` carries edits through without a merge.

Re-sync with [`../scripts/import-upstream-skills.sh`](../scripts/import-upstream-skills.sh). It is
idempotent, and it never clobbers a fork (see "Forking an imported skill" for what counts as one).

## Upstreams

| Submodule | Upstream | License | What we take |
|---|---|---|---|
| `upstreams/cursor-plugins` | [cursor/plugins](https://github.com/cursor/plugins) - `pstack/` (poteto) and `cursor-team-kit/` | MIT | 19 skills |
| `upstreams/superpowers` | [obra/superpowers](https://github.com/obra/superpowers) | MIT | 4 skills |
| `upstreams/gstack` | [garrytan/gstack](https://github.com/garrytan/gstack) | MIT | 4 skills |
| `upstreams/anthropic-skills` | [anthropics/skills](https://github.com/anthropics/skills) | - | 1 skill |
| `upstreams/terraform-skill` | [antonbabenko/terraform-skill](https://github.com/antonbabenko/terraform-skill) | Apache-2.0 | 1 skill |
| `upstreams/owasp-security` | [agamm/claude-code-owasp](https://github.com/agamm/claude-code-owasp) | MIT | 1 skill |

All six are the upstream projects, not forks. pstack's canonical home is the `pstack/` directory of
the `cursor/plugins` monorepo; the several standalone "pstack for Claude Code" repos are ports of it.

## Symlink or trampoline

13 pstack and cursor-team-kit skills carry `disable-model-invocation: true`. In Cursor that means
"user-invocable only". In Claude Code it makes the Skill tool refuse the invocation outright (a
finding from the `michael-denyer/pstack-claude` port, which hit it on a live session and removed the
flag from 12 skills to fix it), so `CLAUDE.md` could never route to them. The trampoline sidesteps it
without editing upstream: model-invocable frontmatter carrying the upstream description, over a body
that points at the upstream `SKILL.md`. The other 17 are straight symlinks.

Cursor primitives named in upstream prose (`Task` tool, `~/.cursor/rules/pstack-models.mdc`, Cursor
model slugs, `.cursor/skills/`) resolve through
[`../upstreams/HOST-MAPPING.md`](../upstreams/HOST-MAPPING.md). The model roster those skills fan out
across is [`../pstack-models.md`](../pstack-models.md). Where an upstream instruction collides with
`CLAUDE.md`, CLAUDE.md wins.

## What was imported, and why it isn't already here

| Skill | From | Gap it fills |
|---|---|---|
| `how`, `why`, `teach` | pstack | Tenet 1 says map before changing, but only ever pointed at the code graph. These are the read paths: runtime behaviour, design rationale, and a plain-language weave of both. |
| `blast-radius` | pstack | §1 asks the plan to list callers, tests and downstream consumers. This finds them, and proves safety by running code rather than asserting it. |
| `architect` | pstack | §1 plans *tasks*; nothing settled types and module shape first. |
| `brainstorming` | superpowers | Everything here starts at "write the plan". This is the step before, where the requirement is still vague. |
| `office-hours` | gstack | Same slot, product side: is this worth building at all. |
| `systematic-debugging` | superpowers | §6 says root-cause, symptom → assumption → fix. This is the executable loop. |
| `tdd` | pstack | §6 demands a regression test that fails pre-fix. Chosen over superpowers' rigid red-green because it also says when *not* to write the test, matching §6's "document why and verify manually instead". |
| `verification-before-completion`, `verify-this` | superpowers, cursor-team-kit | Tenet 6 and §4 forbid "should work". These are the gates. |
| `create-verification-skill`, `maintain-verification-skill` | pstack | §4's per-change-type verification only works if the repo has a scripted way to drive the real app. These generate and then maintain it. |
| `interrogate` | pstack | §4's "adversarially verify with an INDEPENDENT reviewer". Fans out independent Sonnet 5.5 high reviewers, one per lens (`../pstack-models.md`). |
| `arena` | pstack | §5's "is there a simpler way?" answered by N parallel attempts instead of one, then grafting. |
| `thermo-nuclear-code-quality-review` | cursor-team-kit | The harshest maintainability pass. Complements, not replaces, `review-staged-diff`. |
| `figure-it-out`, `show-me-your-work` | pstack | Auditable playbook plus decision trail for long unattended runs - the missing half of `issue-pr-autopilot` and `pr-orchestration`. |
| `reflect` | pstack | §3 saves corrections to memory. This routes a whole transcript's lessons into concrete skill edits. |
| `writing-skills` | superpowers | How to write the skill `reflect` decides you need. |
| `technical-writing`, `unslop`, `deslop` | pstack, cursor-team-kit | The anti-slop rules in Core Principles as executable passes: prose standard, prose cleanup, diff cleanup. |
| `typescript-best-practices` | pstack | Depth under `conventions`' TypeScript section. |
| `terraform-skill` | antonbabenko | Depth under `conventions` and `infra-ops`, from the Terraform community's reference author. |
| `owasp-security`, `cso` | agamm, gstack | Top 10:2025 / ASVS 5.0 detail behind `coding-standards`' pre-launch checklist, plus a STRIDE threat-modelling pass. |
| `mcp-builder` | anthropics | Nothing here covered authoring MCP servers. |
| `health`, `retro` | gstack | Code-quality dashboard and weekly retrospective. No local equivalent. |

## What was deliberately not imported

**Anything that would compete with the PR and CI stack.** These upstreams all ship their own,
tuned to their own conventions; ours are tuned to CodeRabbit and the `triage-labels` rubric.

- pstack / cursor-team-kit: `babysit`, `fix-ci`, `loop-on-ci`, `get-pr-comments`,
  `make-pr-easy-to-review`, `fix-merge-conflicts`, `new-branch-and-pr`, `review-and-ship`,
  `pr-review-canvas`, `what-did-i-get-done`, `weekly-review` - covered by `ci-watch`, `cr-loop`,
  `pr-iterate`, `pr-lifecycle`, `git-commit`.
- gstack: `/ship`, `/land-and-deploy`, `/canary`, `/setup-deploy` - same reason, and they assume
  gstack's own deploy configuration.
- superpowers: `using-git-worktrees`, `writing-plans`, `executing-plans`, `requesting-code-review`,
  `receiving-code-review`, `finishing-a-development-branch`, `subagent-driven-development`,
  `dispatching-parallel-agents` - covered by `worktrees`, `review-and-implement`,
  `review-staged-diff`, `cr-loop`, `pr-lifecycle`, `subagent-strategy`, `pr-orchestration`.

**Routers.** `poteto-mode` (pstack), `using-superpowers`, and gstack's `SKILL.md` all exist to route
a request to the right sub-skill. `CLAUDE.md` is this repo's router; a second one competing with it
is worse than none.

**The 32 `principle-*` leaves** under `pstack/skills/`. They restate Core Tenets and the six review
dimensions in different words, and `poteto-mode` is the only thing that reads them.

**`no-comments`** strips every comment from a diff. Core Principles keeps the comments that carry a
*why*. Direct contradiction, so it stays out; `deslop` covers the same ground without the conflict.

**gstack's installer.** `./setup` writes SessionStart/Stop hooks into `settings.json` and appends a
gstack section to `CLAUDE.md`. It was never run - the four gstack skills are linked directly. Their
descriptions are terse one-liners (`Chief Security Officer mode. (gstack)`) because gstack routes on
its own `triggers:` key, which Claude ignores, so route to them explicitly rather than expecting
auto-invocation.

**Everything in gstack that duplicates an installed plugin**: `/design-review`, `/design-html`,
`/design-shotgun`, `/browse`, `/qa` overlap `frontend-design`, `playwright-verify` and the
`claude-in-chrome` tools already configured in `settings.json`. For the same reason
`anthropics/skills`' `webapp-testing` stays out: `playwright-verify` already owns browser
verification here.

## Forking an imported skill

`import-upstream-skills.sh` treats a skill as its own only while it is a symlink or still carries the
`GENERATED-TRAMPOLINE` marker. Anything else at that path is a fork, and every re-sync leaves it
alone. So there are two ways to fork, and both come down to removing one of those two things:

- **Additive change** (the usual case): keep the trampoline's pointer at the upstream body, add your
  section under it, and **delete the `GENERATED-TRAMPOLINE` comment line**. You keep tracking
  upstream for the bulk of the skill; only your section is frozen.
- **Divergent change**: replace the symlink with a real directory and copy the upstream body in.

Either way, record the upstream commit you forked from at the top of the file, or the next person
cannot tell what has drifted.
