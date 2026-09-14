---
name: triage-labels
description: The five-dimension label rubric (type, severity, urgency, impact, effort) plus derived
  priority, what "untriaged" and "stale" mean, and the `gh` mechanics. Invoke when creating an issue or PR in a
  repo that uses the rubric, or when asked to label or triage items.
---

# Triage labels and the per-item rule

Running a whole backlog pass is the `triage-pass` skill; choosing what to work on next is
`work-selection`. This skill is the rubric those two apply.

### Per-item rule (separate from full passes)

> **When you create an issue or PR** in a repo that uses this rubric, label it on the same call. Don't label or comment on items other people own as a side effect of reading them.

- **Creating** (`gh issue create`, `gh pr create`): pass `--label` with the full rubric on the same call. Don't ship a creation without labels and rely on a later sweep to clean up.
- **Updating an item you own or were asked to work on** (editing body/title, posting a comment, applying any `gh issue edit`/`gh pr edit`): if the item has no `triaged` label, fold a triage pass into the same edit. Either apply labels yourself if you can decide them, or apply `triaged` + `status/needs-info` + post a specific clarifying-question comment per §"Mechanics".
- **Reading** someone else's item as part of a larger task: leave it untouched. Mention untriaged items to the user as a hygiene note, and offer a `triage-pass` if there are many.
- **Exception — `type/question` items** still skip the priority rubric per the §"Picking the next thing to work on" rule: apply `type/question` + `status/needs-info`, post the clarifying question, mark `triaged`, and leave open.

The rubric to apply is the same as in a full pass — see §"Default label set" + §"Priority rubric". The cheap moment to label an item correctly is when you create it; labelling other people's items belongs in a triage pass the user asked for.

## What "untriaged" means (and what "stale" means — independently)

**Untriaged**: pick the heuristic that fits the repo, in this order of preference:

1. **If the repo has a `triaged` label**: an item is untriaged iff it doesn't have that label. Cleanest signal.
2. **Otherwise** (no `triaged` convention): an item is untriaged iff it's open AND it has no priority label (`priority/p[0-3]` or whatever ordinal scheme the project uses). Closed items, regardless of close reason, are out of scope.

**Stale** is a separate, orthogonal axis: an item is stale iff it's open AND has had no activity for 90+ days AND has no recent substantive comment. An item can be triaged-and-then-neglected (stale but not untriaged) or never-triaged-and-stale (both untriaged AND stale) — both qualify. The regular triage flags stale items with `status/stale-candidate` regardless of their triaged or priority-label state; the dedicated stale sweep pass closes them. Don't close silently from the regular loop.

Conform to whatever convention the project already uses; the `triaged` label is the most reliable positive marker. If the project has none, propose the label set in the next section. Before the first pass, run `gh label list --limit 200` to learn what already exists, and align on it instead of fragmenting.

## Default label set (use the project's existing labels if present)

If the project doesn't already have a label scheme, propose these and create them with `gh label create` before the first pass. Otherwise use what exists — don't fragment.

| Dimension | Label values | Meaning |
|---|---|---|
| **Type** | `type/bug` `type/feat` `type/chore` `type/docs` `type/security` `type/question` | What category of work. |
| **Severity** | `severity/critical` `severity/high` `severity/medium` `severity/low` | How bad it is when it happens. Independent of how often. |
| **Urgency** | `urgency/now` `urgency/this-sprint` `urgency/this-quarter` `urgency/eventually` | When does it need fixing? |
| **Impact** | `impact/all-users` `impact/many` `impact/few` `impact/internal` | Who's affected. Audience size + blast radius. |
| **Effort** | `effort/xs` `effort/s` `effort/m` `effort/l` `effort/xl` | XS = 1-line fix; XL = multi-week refactor. Estimate based on the touch points, not the difficulty. |
| **Priority** | `priority/p0` `priority/p1` `priority/p2` `priority/p3` | Derived. See rubric below. |
| **Status** | `triaged` `status/blocked` `status/needs-info` `status/stale-candidate` `status/wontdo` | Procedural. `triaged` is the positive marker that the item has been processed; `status/stale-candidate` is parking it for the next stale-sweep pass. |

## Priority rubric (importance × urgency × impact)

Priority is derived, not declared. Apply the rubric.

**Terminology — importance ≡ severity in this file.** The user-facing framing is "importance × urgency × impact" (matching how the conversation usually phrases prioritisation), but the *label* dimension that captures importance is `severity/*` — there's no separate `importance/*` label. Severity is "how bad is the harm when it happens" — the closest single-axis encoding of importance. The other two factors (urgency, impact) have direct label dimensions. So when this file says "importance" think `severity/*`.

| Priority | When |
|---|---|
| **P0** | Production broken, data loss, security incident actively exploitable, deploy pipeline red on the default / trunk branch (whatever the project calls it — `main`, `master`, `trunk`, `develop`), blocking the team from shipping. Drop everything; same-day fix. |
| **P1** | High severity AND (high urgency OR high impact). Affects most users, no acceptable workaround, regression vs. prior release, or a security finding that's not yet exploitable but should be. Next thing up. |
| **P2** | Medium severity OR medium urgency OR limited impact. Has a workaround or a small audience. Backlog-worthy. |
| **P3** | Polish, idea, exploratory, "nice to have". May never ship. Don't be afraid to use this label — it's not an insult to the issue. |

Severity ≠ priority: a critical bug affecting one obscure code path is high severity but possibly P2 (most users won't hit it). Urgency ≠ priority: a deadline-driven nice-to-have for a single internal demo is high urgency but possibly P2 (low impact on the actual product).

If you keep wanting to label everything P0/P1, the rubric is broken — recalibrate. As a sanity check: at most ~5–10% of open items should be P0+P1 combined. If it's more, you've miscalibrated.

## Mechanics

```sh
# Untriaged items (adjust to whatever the project's "needs triage" convention is).
# NOTE: `gh`'s default --limit is 30 and the CLI silently truncates without warning.
# Bump --limit to comfortably exceed the count from the sensor below; for very
# large backlogs paginate via `--search "... updated:<cursor-date"` instead of
# trying to one-shot the list.
gh issue list --search "is:open is:issue -label:triaged" --limit 500 --json number,title,labels,createdAt,updatedAt
gh pr list    --search "is:open is:pr     -label:triaged" --limit 500 --json number,title,labels,createdAt,updatedAt,isDraft

# Per-item triage actions:
gh issue view <num> --json title,body,labels,comments,createdAt,updatedAt
gh issue edit <num> --add-label "triaged,priority/p1,severity/medium,urgency/this-sprint,impact/many,effort/s,type/bug"
gh issue comment <num> --body "<rationale>"
gh issue close   <num> --reason "not planned" --comment "Duplicate of #X — closing in favour of that one."
# Two close-reasons exist: `not planned` (Duplicate / Stale-sweep / Won't-do — most triage closes)
# and `completed` (genuinely resolved — e.g. an answered question, or fixed by another PR):
gh issue close   <num> --reason completed     --comment "Answered above — closing. Reopen if still ambiguous."
```

Pre-create missing labels once per repo with **`scripts/bootstrap-triage-labels.sh`** (in this
repo), which creates the entire rubric below in one pass:

```sh
scripts/bootstrap-triage-labels.sh              # the repo you are standing in
scripts/bootstrap-triage-labels.sh owner/repo   # a specific repo
```

It uses `gh label create --force`, so re-running updates colours and descriptions rather than
failing, and it only ever creates or updates - it never deletes a label, including ones outside this
set. The script is the single definition of the label set; this section is the rationale for it.

Skip dimensions the project already covers under different names — don't fragment. If the project uses `Pri-Critical` instead of `priority/p0`, conform.

Helpful additional one-liners:

```sh
# Count untriaged items (the "should we trigger a triage pass?" sensor):
gh issue list --search "is:open is:issue -label:triaged" --json number --jq 'length'

# Open PRs not touched in the last 7 days (the "PR queue is stalling" sensor):
# Use `updated:<…` not `created:<…` — last-touch is the meaningful stall signal;
# a 6-month-old PR with active recent commits isn't "stalling" in this sense.
# `date -v-7d` is BSD/macOS; on Linux (CI, Codespaces, devcontainers) use
# `date -d '7 days ago' +%Y-%m-%d`. The portable form below picks whichever works.
SEVEN_DAYS_AGO=$(date -v-7d -u +%Y-%m-%d 2>/dev/null || date -u -d '7 days ago' +%Y-%m-%d)
gh pr list --state open --search "updated:<$SEVEN_DAYS_AGO" --json number --jq 'length'

# Bulk-label an item with the full rubric in one go:
gh issue edit <num> --add-label "triaged,priority/p1,severity/medium,urgency/this-sprint,impact/many,effort/s,type/bug"
```
