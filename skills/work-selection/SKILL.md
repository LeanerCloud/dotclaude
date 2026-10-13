---
name: work-selection
description: Picking the next thing to work on from a triaged backlog - PRs almost always outrank
  issues, then priority, urgency, impact, unblocks-others, effort - and filing follow-ups found
  mid-issue. Invoke on "what should I work on next?".
---

# Picking the next thing to work on

Assumes a triaged backlog; if items aren't labelled, invoke `triage-pass` first (or `triage-labels` for a handful).

## Picking the next thing to work on (after triage)

The selection rule when the user asks "what should I work on next?" or after a triage pass.

### Issues

1. **Filter** to `is:open is:issue -label:status/blocked -label:status/needs-info -label:status/wontdo` (or `gh issue list --state open`). Don't pre-filter `type/question`: one without `status/needs-info` means the asker replied and awaits a response; surface it.
2. **Sort highest-priority first**, breaking ties in order (each only when the previous are equal):
   - **Priority band** P0 -> P3 (map the project's scheme to bands if not numeric).
   - **Urgency**: a P1 `urgency/now` beats a P1 `urgency/this-quarter`.
   - **Impact**: wider blast radius first.
   - **Unblocks-others**: items with downstream dependencies first.
   - **Effort**: cheaper fix first.
3. **Surface the top 3-5** as a focus list with a one-line "why now" each.
4. **Name tradeoffs when substituting.** If the user might prefer a lower-ranked item for non-obvious reasons (strategic bet, customer commitment), say so: *"Top of the queue is #X (P1, blocks onboarding for new tenants). You might prefer #Y (P2, but adjacent to yesterday's work) - your call."*

### Open PRs (separate ranking - PRs almost always outrank issues)

Open PRs are more urgent than issues because the work is done and someone is waiting. Scan the PR queue before recommending any issue.

**Identifying "yours"**: probe once with `gh api user --jq .login` (authoritative; don't derive it from `git config user.email`) and cache it. "Yours" = `--author <login>`; reviewer = `--search "review-requested:<login>"`.

1. **Filter out drafts**: `gh pr list --state open --search '-is:draft' --json number,title,labels,author,statusCheckRollup,reviewDecision`.
2. **Sort highest-urgency first**:
   - **CI red on yours** -> fix immediately (red CI on a PR targeting the default/trunk branch is P0; on a non-trunk branch P1 unless the user says otherwise).
   - **Unaddressed review feedback on yours** -> respond/push fixes.
   - **Mergeable + green CI on yours** -> merge, or ping the user if they want to review first; never auto-merge without explicit authorization.
   - **Teammate PRs you're a reviewer on** -> review.
3. Only after the PR queue is clear do issues take precedence.

Overall order: your PRs (CI red / awaiting your fix / mergeable) -> teammate PRs you review -> highest-priority issues. Override only with explicit user direction.

## Capturing follow-ups discovered while working an issue

When you notice something that *could* be done but isn't part of the current issue, **file it as a separate GitHub issue immediately and triage it at creation**; don't expand the current PR's scope, drop the observation, or leave a TODO comment as the only record. Complements `git-commit`/`pr-lifecycle` §"Capture follow-up tasks as new GitHub issues" (fires at PR completion; this fires when you spot it).

**File when you notice**: a related-but-separate bug; a worthwhile TODO/FIXME/`XXX` nearby; an infra or hygiene gap (missing test, stale doc, broken local-dev path); a refactor the change makes possible but doesn't need; a scope/intent question for the maintainer you won't block on.

**How**:
1. `gh issue create --title "..." --body "..."` with a specific title; start the body with *"Discovered while working #123: ..."*.
2. **Triage at creation**: `triaged` plus the full label set (§"Default label set" in `triage-labels`; rubric as `triage-pass` §"The triage pass itself"). If a dimension is undecidable, best-guess plus `status/needs-info` and a clarifying question. Untriaged follow-ups lose the context that makes labelling cheap now.
3. **Link both ways**: comment on the originating issue ("Filed #<new> for the follow-up on X") and reference it in the new body.
4. **Skip trivia** (typos in your own draft, scratch observations). Bar: would a future agent/maintainer be annoyed to rediscover this? If yes, file.

Applies recursively. One issue per follow-up, each triaged at creation; no mega-issues.
