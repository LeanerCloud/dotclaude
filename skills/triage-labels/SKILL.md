---
name: triage-labels
description: The five-dimension label rubric (type, severity, urgency, impact, effort) plus derived
  priority, what "untriaged" and "stale" mean, and the `gh` mechanics. Invoke whenever you read,
  create or update any issue or PR that lacks the `triaged` marker.
---

# Triage labels and the always-on per-item rule

Running a whole backlog pass is the `triage-pass` skill; choosing what to work on next is
`work-selection`. This skill is the rubric those two apply.

### Always-on per-item rule (separate from full passes)

> **Whenever you read, create, or update an issue or PR**, apply the rubric inline if the item lacks the `triaged` marker. Don't leave untriaged items in your wake.

- **Creating** (`gh issue create`, `gh pr create`): pass `--label` with the full rubric on the same call. Don't ship a creation without labels and rely on a later sweep to clean up.
- **Updating** (editing body/title, posting a comment, applying any `gh issue edit`/`gh pr edit`): if the item has no `triaged` label, fold a triage pass into the same edit. Either apply labels yourself if you can decide them, or apply `status/needs-info` + post a specific clarifying-question comment per §"Mechanics".
- **Reading** as part of a larger task: if you'd be the next human-attention checkpoint that item gets, apply the rubric. Skip if the item is genuinely incidental to your task with no signal to apply the rubric on; in that case, mention it to the user as a hygiene note.
- **Exception — `type/question` items** still skip the priority rubric per the §"Picking the next thing to work on" rule: apply `type/question` + `status/needs-info`, post the clarifying question, mark `triaged`, and leave open.

The rubric to apply is the same as in a full pass — see §"Default label set" + §"Priority rubric". The point of the always-on rule is that untriaged items accumulate silently between scheduled sweeps; the cheap moment to label them correctly is when they're already in your context.

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

Pre-create missing labels once per repo. Full bootstrap (covers the entire rubric — run idempotently with `--force` to update colours/descriptions on re-run):

```sh
# Status (procedural)
gh label create "triaged"                 --color "0e8a16" --description "Item has been triaged" --force
gh label create "status/blocked"          --color "b60205" --description "Blocked on something external" --force
gh label create "status/needs-info"       --color "fbca04" --description "Awaiting clarification from the asker" --force
gh label create "status/stale-candidate"  --color "cccccc" --description "Flagged for the next stale sweep" --force
gh label create "status/wontdo"           --color "ededed" --description "Closed as not-planned" --force

# Priority (derived — see rubric)
gh label create "priority/p0" --color "b60205" --description "Drop everything; same-day fix" --force
gh label create "priority/p1" --color "d93f0b" --description "Next up; this sprint" --force
gh label create "priority/p2" --color "fbca04" --description "Backlog-worthy" --force
gh label create "priority/p3" --color "c5def5" --description "Polish / idea / may never ship" --force

# Severity (independent of how often)
gh label create "severity/critical" --color "b60205" --description "Major harm when it happens" --force
gh label create "severity/high"     --color "d93f0b" --description "Significant harm" --force
gh label create "severity/medium"   --color "fbca04" --description "Moderate harm" --force
gh label create "severity/low"      --color "c5def5" --description "Minor harm" --force

# Urgency
gh label create "urgency/now"          --color "b60205" --description "Drop other things" --force
gh label create "urgency/this-sprint"  --color "d93f0b" --description "Within the current sprint" --force
gh label create "urgency/this-quarter" --color "fbca04" --description "Within the quarter" --force
gh label create "urgency/eventually"   --color "c5def5" --description "No deadline" --force

# Impact (audience size + blast radius)
gh label create "impact/all-users" --color "b60205" --description "Affects every user" --force
gh label create "impact/many"      --color "d93f0b" --description "Affects most users" --force
gh label create "impact/few"       --color "fbca04" --description "Limited audience" --force
gh label create "impact/internal"  --color "c5def5" --description "Team-internal only" --force

# Effort
gh label create "effort/xs" --color "c5def5" --description "Trivial / one-liner" --force
gh label create "effort/s"  --color "c5def5" --description "Hours" --force
gh label create "effort/m"  --color "fbca04" --description "Days" --force
gh label create "effort/l"  --color "d93f0b" --description "Weeks" --force
gh label create "effort/xl" --color "b60205" --description "Multi-week / refactor" --force

# Type
gh label create "type/bug"      --color "ee0701" --description "Defect" --force
gh label create "type/feat"     --color "0e8a16" --description "New capability" --force
gh label create "type/chore"    --color "c5def5" --description "Maintenance / non-user-visible" --force
gh label create "type/docs"     --color "0075ca" --description "Documentation" --force
gh label create "type/security" --color "b60205" --description "Security finding" --force
gh label create "type/question" --color "d876e3" --description "Question for the project / asker" --force
```

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
