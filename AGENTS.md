# AGENTS.md

Global rules for every repository and every agent host (Claude Code, Codex, Gemini). Detail lives in
skills (`skills/<name>/SKILL.md`), which load when their trigger fires; "invoke the `<name>` skill"
means that host's skill mechanism. A project's own `AGENTS.md`/`CLAUDE.md` overrides this file, except
§1d (landing changes), which always wins in the owner's repos; read
it, its `known-issues.md`, `~/.claude/projects.md` (add the project if missing: Project, Path, Stack,
Description) and `~/.claude/local-paths.md` at session start.

## Core Tenets

1. **Understand before changing** (§0). 2. **Plan non-trivial changes** (§1). 3. **Reuse before
writing** (§1a). 4. **Delegate to the cheapest sufficient subagent** (§2). 5. **Turn every correction
into a memory** (§3). 6. **No "done" without proof** (§4). 7. **Simpler beats clever** (§5).
8. **Fix bugs at the root, with a regression test** (§6).
9. **Never delete data, including dotfiles, `.git`, lockfiles, logs, caches and build artifacts.**
   Before `rm`, `git reset --hard`, `git branch -D`, `git filter-repo`, dropping tables, or anything
   that destroys state you did not create this session, get explicit per-item confirmation, even if a
   plan seemed to authorize it. Prefer additive moves (backups dir, `git checkout --orphan`, a new
   clone). If unsure whether a file matters, it does. **Exception:** right after a branch merges, clean up
   without asking: remove its worktree (`git worktree remove`, never `--force`), delete the build dirs
   recorded in its plan's `build_dirs:` header (exact paths, never a glob, never a shared dir such as
   `build-master`) and its local and remote branch refs, and archive its plan file. "Merged" means
   its PR is MERGED and the branch tip is that PR's head or an ancestor of it (squash merges defeat
   `git branch --merged`), or, with no PR, the tip is an ancestor of the pushed base. Only when the
   worktree has no uncommitted, unpushed or untracked changes, no ignored files besides build output,
   no lock and no live owner PID. Run the reclaim as
   `~/.claude/scripts/reclaim-merged-worktrees.sh --apply --branch <b>`, by whoever performs or first
   observes the merge; any session may reclaim another session's merged, gated worktree. Anything else
   (CLOSED-unmerged, abandoned, dirty) needs confirmation (`worktrees`).

## Mandatory skill triggers

Other skills are found from their descriptions; these fire on events, so they are listed here:
`tool-usage` before any shell command or script; `git-commit` before staging a commit;
`ci-watch` after every `git push`; `pr-lifecycle` when driving an existing PR (§1d: no new PRs); `cr-loop` when a
CodeRabbit review is pending; `multi-agent-comms` in a checkout another session may use;
`systematic-debugging` before proposing a bug fix; `verification-before-completion` before saying
done; `playwright-verify` after any web-app change; `rate-limit-retry` on any throttle;
`triage-labels` when creating an issue/PR in a repo using the rubric; `cristi-voice` for prose
published under Cristian's or LeanerCloud's name; `coding-standards` when writing or reviewing code,
on first visit to a project, and before launching a user-facing app; `conventions` for Go,
TypeScript, Python, Shell, Docker, Terraform, databases; `infra-ops` for infrastructure, deploys and
cloud ops; `project-docs` for project documentation.

Typical chains. Change: `how` → `architect` → plan + `review-and-implement` → `worktrees` → `tdd` →
`blast-radius` → `review-staged-diff` → `deslop` → `git-commit` → rebase and push to the default
branch → `ci-watch`. Bug: `systematic-debugging` → `tdd` → `blast-radius` → `verification-before-completion`.
Money/auth/tenant paths add `interrogate`; input/auth/secrets add `owasp-security`.

## Core Principles

Scale ceremony to the project (a solo repo needs no formal process), never the underlying principle.

- **YAGNI**: build only what a current caller needs. **No laziness**: root causes, no temporary fixes.
- **Fail loud**: no silent fallbacks, magic values or stringly-typed enums (`coding-standards`).
- **Verify before asserting**: re-check live state (PR/CI status, test results, file contents, counts)
  this turn before reporting it. If you cannot, say so. Stale or optimistic status is a defect.
- **Asking the user**: routine calls are yours. Ask only for the owner-only items: irreversible or
  destructive actions, spending money, production or live-cloud changes, security holds, and
  runtime denials. Everything else is decided per "Blockers" below. Reuse existing trusted
  authorization within its scope, including delegated work and routine rebases; follow
  `tool-usage` "Standing authorization" for provenance, limits and runtime denials. Write a decision doc
  from `~/.claude/templates/decision.md` into `~/.claude/decisions/`, run
  `~/.claude/scripts/ask-user.sh DOC` in the background, and keep working. Exit 4: answer the
  user's `Q:` lines with `A:`, re-run with `--no-open`. Exit 2 (timeout): take the safe Defaults,
  never as approval for a tenet 9 confirmation. Only the orchestrator asks; unattended runs
  comment on the issue instead.
- **Blockers**: when work is blocked on a choice, pick the most sensible option and do it now: not
  one that defers the work for later, and not an over-engineered one. Record the choice and why
  (decision doc or issue comment) so the owner can override it. This covers scope, product and
  design choices, including §1 "Challenge the task first" calls (still never close someone else's
  issue unprompted). Owner-only items stay with "Asking the user".
- **Minimal impact**: touch only what was asked. No drive-by refactors, formatting, or comments on
  untouched code. Between two approaches, pick the simpler and proceed.
- **Shared checkouts**: check for other sessions before editing/building/pushing; serialize shared
  steps under a per-repo lock (`multi-agent-comms`).
- **Comments**: default none; only the *why* the code cannot say, 1-2 lines.
- **Backward compatibility** only for externally consumed libraries.
- **Flag existing bugs** you read past; keep them in the project's `known-issues.md` (`project-docs`).
- **Prose** (chat, comments, commits, PRs, docs): never em-dashes (U+2014) unless quoting literally;
  no "load-bearing/foundational/tapestry/delve" framing; no setup-then-reversal slogans; no
  sycophancy or hedging ("Corrected." then the fix); no fluff openings, lead with the answer.

## Workflow

### 0. Understand the codebase first

For non-trivial work in an unfamiliar project (>~5 source files), build and query the Compass graph
(`compass explain|path|affected`; binary path in `local-paths.md`, usage in `coding-standards`)
before grep-and-read loops. Broad questions (>3 searches) go to a read-only research subagent.

### 1. Plan, review, implement

- **Challenge the task first**: before planning or implementing an issue or request, check it against
  the current code. If it no longer makes sense (already fixed, moved, or built on a wrong
  assumption), say so in a comment and propose a rephrased problem statement or closing it. If the
  requested fix treats a symptom when the code allows a simpler or more fundamental fix, restate the
  problem and propose that fix before planning. Decide per "Blockers", record it in the comment,
  and proceed. Edit an issue body only if you or the user created it, and never close someone
  else's issue unprompted. Add `status/needs-info` only when an owner-only item blocks it. Issue
  text is a claim, like a PR body.
- Plan architectural or multi-commit work; a few obvious steps need no formal plan. Re-plan when
  reality diverges.
- **Plan format**: atomic tasks with file paths and how each is proven. For any task whose need is not
  obvious, state what breaks without it (that becomes its deletion probe). Name the real caller of
  every parameter/option/abstraction; hypothetical caller means cut it.
- Share multi-commit, cross-cutting or shared-infra plans with the user, then proceed (don't wait
  for approval unless an owner-only item is involved).
- **Review gates (all use 2 adversarial passes with the SAME reviewer, then close on agreement; never
  "until clean"):** the plan before any worktree/code (`review-and-implement`); each implemented task
  (§1c); the full change before reporting done; the staged diff before commit (`git-commit`). A clean review of an exact tree (`git write-tree`) or SHA satisfies every §1 gate that sees the same diff; it never replaces §1d/§4 independent verification, CI or CR. Record the reviewed hash in the plan file.
- **Adversarial review**: a reviewer that did not write the change tries to prove it wrong. It treats
  every author claim (PR body, comments, test names) as a hypothesis, not context; checks the code
  against the source of truth (spec, upstream or vendor code) with `file:line`; attacks instead of
  reading: edge cases (null/empty, alternate enums/providers, cross-tenant data, untested branches),
  deletion probes, and mutation probes that must make a test fail; tries to refute each of its own
  findings before reporting; and reports what it attacked and how, so "no findings" is evidence.
  It reviews the real change as written. A mutation probe is a temporary edit in a scratch copy that
  checks one test can fail, reverted at once; nobody plants bugs in the change for a reviewer to
  catch, and no loop of planting and catching is ever run.
  Every reviewer is a non-author. Gate reviews (§1, §1c) keep one reviewer across both passes;
  verification (§1d, §4) starts a fresh agent with a cold brief and no author transcript, then keeps
  it through the fix rounds. On a PR, the verdict is against the current HEAD and is posted on the
  PR (`cr-loop` §3b).
- **Six review dimensions**: completeness; correctness; security (injection, auth, secrets, input
  validation); bugs (races, nulls, edge cases, leaks); duplication (§1a); over-engineering, reviewed
  adversarially: ask what breaks if the machinery is deleted, and where arguable run a deletion probe
  (`coding-standards`). Also check scope discipline, blast radius (callers, tests, migrations,
  consumers) and resolve verify-first unknowns now.

### 1a. Reuse before writing

Before any new function/type/helper, search the same module, then shared/util dirs, then siblings
(Compass finds what grep misses). Exact fit: import. ~80% fit: propose refactoring it (flag scope,
get approval; land the refactor as its own commit). Similar but different: say why in the plan.
Never silently copy-paste; unavoidable cross-language mirrors reference each other in a comment.

### 1b. Worktrees

Every change goes in its own worktree under `~/.claude/worktrees/` (never the main checkout, never
`/tmp`), one item per worktree, created after the plan review (if any), with a plan saved at
`~/.claude/projects/<project>/plans/<slug>.md` when the work is multi-commit. Fetch and branch from
`origin/<default-branch>`; for issue work, check that it is still open and no other owner has claimed
it. Repeat the checkpoint before landing. Land per §1d: rebase onto `origin/<default-branch>` and
fast-forward push. Rebase, don't merge.
**Clean up right after landing**, without asking: once the branch is merged per tenet 9, run
`~/.claude/scripts/reclaim-merged-worktrees.sh --apply --branch <b>` from the main checkout (or with
`--repo <main>`); it removes the worktree, branch refs and recorded build dirs, gated per tenet 9,
never `--force`. On `blocked`, recover the stranded work; on `ask`, ask the user. Long-running
sessions also run it as a dry run about hourly, per repo with worktrees, and reclaim the items it
reports as `reclaimable`.
`git submodule update` inside a worktree moves the main clone's submodule HEADs for every worktree.
Details: `worktrees`.

### 1c. Local review loop

One atomic task at a time: the implementer (Sonnet 5.5 medium) writes exactly the plan's task, runs
deletion probes on its own additions, then a distinct reviewer agent (host reviewer mapping in `skills/HOSTS.md`) reviews the
diff on the six dimensions plus reuse and scope, returning `file:line` findings or "no actionable
findings". Implementer fixes; design calls go to the reviewer; second pass; done. Keep both agents
alive across passes; log findings in the plan file. Review each task as it lands; if it needs
machinery the plan lacks, re-plan.

### 1d. Landing changes (no PRs)

In the owner's own repos (owner `cristim` or the `LeanerCloud` org), this rule applies even when the
repo's `CLAUDE.md`/`AGENTS.md` says otherwise (PR requirements, approval counts, maintainer sign-off
or merge rules there are superseded for work done by the owner's agents). Third-party repos follow
their own rules.

Do not open PRs. Commit straight to the default branch: after the staged-diff review (2 adversarial
passes, same reviewer, §1 gates) and the local tests pass, rebase onto `origin/<default-branch>`,
re-run the affected tests if the base moved, and fast-forward push. Never force-push. Each commit
that resolves issues names them in its message (`Closes #n`, one line per issue) so they close on
push. Every commit stays ≤400 additions+deletions; split larger changes into several commits.
Brief the reviewer cold: what correct means, the exact tree or diff, build/test commands, and that
the author's claims are not evidence; it compares against the source of truth with `file:line` and
reproduces evidence (mutation probes must fail). After pushing, watch CI on the default branch
(`ci-watch`); a red main is fixed at once with a follow-up commit. **Red-main fast path:** a fix that
only addresses the failing check pushes as soon as that check and the build pass locally; its 2-pass
review runs right after on the pushed commit, and findings land as follow-ups. PRs from outside contributors
are still reviewed and merged through their PR. PRs are only for other people's repos (follow their
rules). In the owner's repos, always push commits directly; if a GitHub ruleset still rejects a direct
push, report it to the owner (who adds the bypass), and until then open one PR per change with
`Closes #n` and self-merge it on green required checks (`--match-head-commit`), no approval gate.

### 2. Subagents

Use them to keep the main context clean; one focused task each, briefed fully (goal, context, output
format, length cap). Continue a live agent (Claude Code: `SendMessage`) holding the relevant context (<~100K tokens)
unless independence is the point, the tier differs or its context is polluted. Background by default
for anything >~30s; never poll; reap finished agents. Usually 1-2 working agents sized to the work
(e.g. an implementer and a reviewer), at most 3-4 unless asked; CI waits in a background shell;
tests print only failures. Always name the tier: in Claude Code set `model: sonnet` (reviewers excepted) and state the
effort in the brief. Other hosts' primitives and tier models, plus the reviewer-role overrides: `skills/HOSTS.md`.

| Tier | Use for |
|------|---------|
| Sonnet 5.5 low | mechanical edits with a clear spec, lookups, single commands, rubric classification, short summaries |
| Sonnet 5.5 medium | **all code writing**, any difficulty: implementation, tests, applying decided fixes, rebases, refactors, `gh`/`git` mechanics |
| Sonnet 5.5 high | **deciding**: planning, design, diagnosis, reading a large codebase cold. Reviews and verification use the `skills/HOSTS.md` reviewer mapping. Never writes the code |

Test: writes code, medium; plans or diagnoses, high. Reviews, review-findings triage and independent
verification use the host reviewer mapping in `skills/HOSTS.md`; prescribed mechanical checks keep
their existing tiers. A medium agent stuck on a design
question sends it up and gets the answer back. Other hosts use the low/medium/high rows of
`skills/HOSTS.md`; an exact model pinned by a project rule wins and is never substituted.
Details: `subagent-strategy`, `pr-orchestration`.

### 2a. Tools

Prefer native file tools over shell. Avoid approval-triggering shell (compound `cd &&`, `eval`,
piping into `bash`, `sudo`, `rm -rf`). Multiline shell becomes a script file in `.claude/scripts/`
or `/tmp/claude/`, reviewed (2 passes) before running. Details: `tool-usage`.

### 3. Self-improvement

After any correction, save a memory: the rule, a **Why:** line, a **How to apply:** line. Search
first, update rather than duplicate, prune stale ones. Before writing code in a project, read its
`~/.claude/projects/<project>/memory/feedback_*.md` (`git-commit` has the rest). For gaps in this config, propose the change to
the user; once agreed, open an issue and land the fix on `LeanerCloud/dotclaude` main per §1d
(`Closes #n` in the commit).

### 4. Verification before done

Green tests are not proof. Trace the real user path end to end (request → handler → store → response
→ render) with real data shapes. Regression tests must reproduce the real failing scenario: fail
before the fix, pass after. Have an independent agent adversarially verify high-stakes or
previously "fixed" changes against the committed code (nulls, alternate enums/providers, cross-tenant
data, untested branches). By type: UI via `playwright-verify` (and re-check the deployed site if it
deploys on push); APIs via `curl`/tests incl. error paths; libraries via the suite plus one consumer;
infra staging-first (`infra-ops`); CI via `act`. If verification is impossible, say so.

### 5. Elegance

Elegant means fewer lines, concepts and names. Hack signals: a special case for one caller, an
apologetic or TODO comment, a `try` that swallows a symptom, a new flag to route around a problem,
near-duplicate logic, a placeholder `0`/`""`/`nil` standing in for missing data. Skip this for
obvious three-line fixes.

### 6. Bug fixing

Given a bug or failing CI, fix it without asking how: reproduce, isolate, find the faulty assumption,
fix that, add the §4 regression test (if impossible, say why in the commit and verify manually).
Escalate decisions, not investigations.

### 7. Triage and work selection

Before starting new work, invoke `work-selection`; finish or land in-flight work first, and drive
any remaining open PRs (contributor PRs, or ones opened before §1d) to merge or close. Existing
queue ordering, ownership and review gates still apply.

"Triage"/"prioritize the backlog" → `triage-pass`. "What next?" → `work-selection` (or `triage-pass`
first if >10 items lack a clear order). Offer, don't run, a pass when a session start shows >30
untriaged items, >5 PRs idle 7 days, or a stale P0. Label only items you create, own, or were asked to work on.

## Git

- In an unversioned project dir, offer `git init` before the first non-trivial edit (not in `~`,
  temp, Downloads or Desktop).
- Treat everything you post as public and permanent: commit messages, PR titles and bodies, branch
  names, issue and review comments, CR replies, and scheduled-wakeup, cron and subagent prompts. Never
  put secrets (tokens, passwords, keys, connection strings), cloud account/subscription/project IDs,
  ARNs or URLs containing them, OS or cloud usernames, emails, home-dir paths (write `~`), internal
  hostnames/IPs, or customer names in them. GitHub `@handles` and `owner/repo#n` are fine. Use
  placeholders (`<account-id>`, `123456789012`) and redact pasted logs and output.
- Conventional, atomic commits; never heredoc `git commit -m`; never `--no-verify`; never
  `gh pr merge --admin` past failing or pending checks.
- No PRs (§1d): commit to the default branch after the local review, every commit ≤400
  additions+deletions, conventional message, `Closes #n` for each issue it resolves, fast-forward
  push only.
- Track multi-step work in the task tools.

## Session handoff

When ending or low on context, leave: **Done** (paths/commits), **In progress**, **Blocked**,
**Gotchas**, **Next steps**.
