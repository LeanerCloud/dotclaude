---
name: triage-labels
description: >-
  Five-dimension label rubric (type, severity, urgency, impact, effort), derived priority, gh
  mechanics. Use when creating or labelling issues/PRs in repos using the rubric.
---

# Triage labels and the per-item rule

A whole backlog pass is the `triage-pass` skill; choosing what to work on next is `work-selection`. This skill is the rubric they apply.

### Per-item rule (separate from full passes)

> **When you create an issue or PR** in a repo that uses this rubric, label it on the same call. Don't label or comment on items other people own as a side effect of reading them.

- **Creating** (`gh issue create`, `gh pr create`): pass `--label` with the full rubric on the same call; don't rely on a later sweep.
- **Updating an item you own or were asked to work on** (edit, comment, any `gh issue edit`/`gh pr edit`): if it lacks `triaged`, fold a triage pass into the same edit: apply labels you can decide, or apply `triaged` + `status/needs-info` + a specific clarifying-question comment (§"Mechanics").
- **Reading** someone else's item as part of a larger task: leave it untouched; mention untriaged items to the user as a hygiene note and offer a `triage-pass` if there are many.
- **Exception, `type/question`**: skip the priority rubric; apply `type/question` + `status/needs-info`, post the clarifying question, mark `triaged`, leave open.

The rubric is the same as in a full pass (§"Default label set" + §"Priority rubric"). Label at creation; labelling other people's items belongs in a user-requested triage pass.

## What "untriaged" means (and what "stale" means - independently)

**Untriaged**, in order of preference:
1. Repo has a `triaged` label: untriaged iff the item lacks it (cleanest signal).
2. Otherwise: untriaged iff open AND no priority label (`priority/p[0-3]` or the project's ordinal scheme). Closed items are out of scope.

**Stale** is an orthogonal axis: open AND no activity for 90+ days AND no recent substantive comment. An item can be stale but triaged, or both untriaged and stale. Regular triage flags stale items `status/stale-candidate` regardless of other labels; the dedicated stale sweep closes them. Never close silently from the regular loop.

Conform to the project's existing convention; if none, propose the label set below. Before the first pass run `gh label list --limit 200` and align on existing labels rather than fragmenting.

## Default label set (use the project's existing labels if present)

If the project has no scheme, propose these and create them with `gh label create` before the first pass; otherwise use what exists. Skip dimensions the project covers under other names (`Pri-Critical` instead of `priority/p0`: conform).

| Dimension | Label values | Meaning |
|---|---|---|
| **Type** | `type/bug` `type/feat` `type/chore` `type/docs` `type/security` `type/question` | Category of work. |
| **Severity** | `severity/critical` `severity/high` `severity/medium` `severity/low` | How bad it is when it happens, independent of frequency. |
| **Urgency** | `urgency/now` `urgency/this-sprint` `urgency/this-quarter` `urgency/eventually` | When it needs fixing. |
| **Impact** | `impact/all-users` `impact/many` `impact/few` `impact/internal` | Audience size + blast radius. |
| **Effort** | `effort/xs` `effort/s` `effort/m` `effort/l` `effort/xl` | XS = 1-line fix; XL = multi-week refactor. By touch points, not difficulty. |
| **Priority** | `priority/p0` `priority/p1` `priority/p2` `priority/p3` | Derived; see rubric. |
| **Status** | `triaged` `status/blocked` `status/needs-info` `status/stale-candidate` `status/wontdo` | Procedural. `triaged` is the positive "processed" marker; `status/stale-candidate` parks the item for the stale sweep. |

## Priority rubric (importance × urgency × impact)

Priority is derived, not declared. **Importance ≡ severity here**: there is no `importance/*` label; `severity/*` (how bad the harm is when it happens) is its encoding. Urgency and impact have their own dimensions.

| Priority | When |
|---|---|
| **P0** | Production broken, data loss, actively exploitable security incident, or deploy pipeline red on the default/trunk branch (`main`, `master`, `trunk`, `develop`) blocking shipping. Drop everything; same-day fix. |
| **P1** | High severity AND (high urgency OR high impact): affects most users, no acceptable workaround, regression vs prior release, or a not-yet-exploitable security finding. Next thing up. |
| **P2** | Medium severity OR medium urgency OR limited impact; has a workaround or small audience. Backlog-worthy. |
| **P3** | Polish, idea, exploratory, "nice to have". May never ship; don't be afraid to use it. |

Severity ≠ priority (a critical bug on one obscure path is high severity but possibly P2). Urgency ≠ priority (a deadline-driven nice-to-have for one internal demo is high urgency but possibly P2).

Calibration check: at most ~5-10% of open items should be P0+P1 combined; more means recalibrate.

## Mechanics

```sh
# NOTE: `gh`'s default --limit is 30 and silently truncates; set --limit above the count from the sensor below, and for very large backlogs paginate via `--search "... updated:<cursor-date"`.
gh issue list --search "is:open is:issue -label:triaged" --limit 500 --json number,title,labels,createdAt,updatedAt
gh pr list    --search "is:open is:pr     -label:triaged" --limit 500 --json number,title,labels,createdAt,updatedAt,isDraft

gh issue view <num> --json title,body,labels,comments,createdAt,updatedAt
gh issue edit <num> --add-label "triaged,priority/p1,severity/medium,urgency/this-sprint,impact/many,effort/s,type/bug"
gh issue comment <num> --body "<rationale>"
# Close reasons: `not planned` (Duplicate / Stale-sweep / Won't-do, most closes) or `completed` (genuinely resolved: answered question, fixed by another PR)
gh issue close   <num> --reason "not planned" --comment "Duplicate of #X - closing in favour of that one."
gh issue close   <num> --reason completed     --comment "Answered above - closing. Reopen if still ambiguous."
```

Create missing labels once per repo with **`scripts/bootstrap-triage-labels.sh`** (this repo), the single definition of the label set (this section is its rationale):

```sh
scripts/bootstrap-triage-labels.sh              # the repo you are standing in
scripts/bootstrap-triage-labels.sh owner/repo   # a specific repo
```

It uses `gh label create --force` (re-running updates colours/descriptions) and never deletes a label, including ones outside the set.

Sensors:

```sh
# Count untriaged items (should we trigger a triage pass?):
gh issue list --search "is:open is:issue -label:triaged" --json number --jq 'length'

# Open PRs not touched in 7 days (use `updated:<`, not `created:<`: last touch is the stall signal).
# `date -v-7d` is BSD/macOS; Linux needs `date -d '7 days ago'`; the form below picks whichever works.
SEVEN_DAYS_AGO=$(date -v-7d -u +%Y-%m-%d 2>/dev/null || date -u -d '7 days ago' +%Y-%m-%d)
gh pr list --state open --search "updated:<$SEVEN_DAYS_AGO" --json number --jq 'length'
```
