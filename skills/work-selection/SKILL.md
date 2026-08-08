---
name: work-selection
description: Picking the next thing to work on from a triaged backlog - PRs almost always outrank
  issues, then priority, urgency, impact, unblocks-others, effort - and filing follow-ups found
  mid-issue. Invoke on "what should I work on next?".
---

# Picking the next thing to work on

Assumes a triaged backlog; if the items aren't labelled, invoke `triage-pass` first (or
`triage-labels` for a handful of items).

## Picking the next thing to work on (after triage)

Triage is preparation. The actual selection rule when the user asks "what should I work on next?" or after a triage pass.

### Issues

1. **Filter** to `is:open is:issue -label:status/blocked -label:status/needs-info -label:status/wontdo` (use `is:open` consistently with the §Mechanics queries; `state:open` is the equivalent CLI flag form when using `gh issue list --state open` directly without `--search`). Don't pre-filter `type/question` — a question with no `status/needs-info` means the asker provided info and is awaiting a response; surface those so they get answered or closed.
2. **Sort highest-priority first**, breaking ties in this order (apply each criterion only when the previous ones are equal):
   - **Priority band**: P0 → P1 → P2 → P3 (use whatever ordinal scheme the project's labels expose; map to bands manually if the names aren't numeric).
   - **Urgency**: a P1 with `urgency/now` beats a P1 with `urgency/this-quarter`.
   - **Impact**: when priority and urgency match, prefer the wider-blast-radius item.
   - **Unblocks-others**: among items at the same priority/urgency/impact, prefer items that have downstream dependencies over standalone items — clearing them unblocks more work.
   - **Effort**: at all-else-equal, prefer the cheaper fix (more wins per hour).
3. **Surface the top 3–5** to the user as a focus list, with a one-line "why now" per item.
4. **Name tradeoffs explicitly when substituting.** If you think the user might prefer a lower-ranked item for non-obvious reasons (a strategic bet, a customer commitment), say so out loud rather than silently substituting: *"Top of the queue is #X (P1, blocks onboarding for new tenants). You might prefer #Y (P2, but it's adjacent to the work you finished yesterday and would compose nicely) — your call."*

### Open PRs (separate ranking — PRs almost always outrank issues)

Open PRs are typically more urgent than open issues because someone is waiting on them — the work is already done; the cost of leaving it stranded is high. Before recommending any issue, scan the PR queue.

**Identifying "yours"**: probe the user's GitHub login once at start of selection via `gh api user --jq .login` — that's the authoritative source. (Don't try to derive the login from `git config user.email`; private-email mappings make that unreliable.) "Yours" = `--author <login>`; "you're a reviewer on" = `--search "review-requested:<login>"`. Cache the login for the session — it doesn't change.

1. **Filter** out drafts via `gh pr list --state open --search '-is:draft' --json number,title,labels,author,statusCheckRollup,reviewDecision` (drafts are intentionally not-yet-ready). The `is:open` qualifier is implied by `--state open`; no need to repeat in the search string.
2. **Sort highest-urgency first**:
   - **CI red on yours** → fix immediately (a red CI on a PR targeting the default / trunk branch is a P0 since it blocks landing; a red CI on a non-trunk branch — e.g. an in-progress feature branch — is P1 unless the user says otherwise).
   - **Unaddressed review feedback on yours** → respond / push fixes (the reviewer is blocked by you).
   - **Mergeable + green CI on yours** → merge (or ping the user if they want to review first; never auto-merge without explicit authorization).
   - **PRs from teammates that you're a reviewer on** (`review-requested:<login>`) → review.
3. Only after the PR queue is clear should issue selection take precedence.

The full prioritization the user gets is: open-yours-PRs (CI red / awaiting-your-fix / mergeable) → open-teammate-PRs-you-review → highest-priority issues. Override only with explicit user direction.

## Capturing follow-ups discovered while working an issue

Whenever processing a GitHub issue (implementing the fix, investigating the bug, exercising the feature) and noticing something that *could* be done but isn't strictly part of the current issue, **file it as a separate GitHub issue immediately and triage it at creation** — don't silently expand the scope of the current PR, don't drop the observation, don't leave a TODO comment in code as the only record. This complements the `git-commit` skill §"Capture follow-up tasks as new GitHub issues" — that rule fires at PR-completion time; this one fires the moment you spot the side-quest, regardless of where you are in the issue's lifecycle.

**Triggers — file a new issue when you notice**:
- a related-but-separate bug while reproducing the current one
- a TODO / FIXME / `XXX` in nearby code that's worth tracking
- an infra or hygiene gap surfaced during repro setup (missing test, stale doc, broken local-dev path)
- a refactor opportunity that the current change makes *possible* but doesn't itself require
- a question for the maintainer about scope or intent that you don't want to block the current issue on

**How to file**:

1. `gh issue create --title "..." --body "..."` with a title specific enough that triage doesn't need to re-derive context. Reference the originating issue in the title or first line of the body — *"Discovered while working #123: ..."* — so the link survives even if labels get reshuffled later.
2. **Triage at creation** — apply `triaged` plus the full label set per §"Default label set" (priority, severity, urgency, impact, effort, type). The rubric is the same as §"The triage pass itself"; you have the context cold from the issue you're already working, so this is the cheapest possible moment to triage. If you can't decide a dimension, apply a best-guess value plus `status/needs-info` and post the clarifying question — same flow as during a regular triage pass. Untriaged follow-ups are a regression: they push triage cost into the future and lose the context that made labelling cheap right now.
3. **Link both ways**: comment on the originating issue ("Filed #<new> for the follow-up on X") and reference the originating issue in the new issue's body. Bidirectional links survive re-organisation better than one-way ones.
4. **Skip filing for trivia** — typos in your own draft, scratch observations, things only a future-you-in-this-session would care about. The bar is *"would a future agent / future maintainer be annoyed having to rediscover this?"* If yes, file; if no, drop it.

This rule applies recursively: if while filing a follow-up you notice yet *another* follow-up, file that one too. Don't batch into one mega-issue — each follow-up gets its own issue, triaged at creation. Batching defeats the point; the goal is granular, individually-triaged tracking that the next triage / work-selection pass can rank against everything else.
