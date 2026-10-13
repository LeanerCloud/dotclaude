---
name: triage-pass
description: >-
  Full backlog triage with parallel fan-out. Triggers: "triage", "prioritize the backlog", "go
  over open issues", backlog overview, before sprint planning, after a major merge.
---

# The triage pass

The label rubric is the `triage-labels` skill; picking what to work on afterwards is `work-selection`.

## When to run a triage pass

Run a **full** pass when:

- The user says "triage", "prioritize the backlog", "go over open issues", or asks for a backlog overview.
- Before non-trivial sprint planning, or after a release or major PR merge.
- On first work in an unfamiliar repo where you need to see what is open.
- More than ~30 untriaged items are open (triage is overdue).

For "what should I work on next?" with a **small, already-labelled** backlog (<=10 items, priority labels visible), **skip the full triage**: sort the labels per `work-selection` §"Picking the next thing to work on" and surface the top 3 with one-line rationales (AGENTS.md §7).

## The triage pass itself

For each untriaged item:

1. **Read context**: title, body, last 2-3 comments, linked code paths and issues. For unfamiliar code areas dispatch an `Explore` subagent.
2. **Classify**: type, severity, urgency, impact, effort; derive priority from the rubric.
3. **Catch dispositions**:
   - **Duplicate**: link the canonical issue, close with `--reason "not planned"` and a "duplicate of #X" comment.
   - **Already resolved by another PR / commit** (fixed implicitly, not auto-closed): close with `--reason completed` and a comment linking the resolving PR/commit. Differs from Duplicate (no open issue to redirect to) and Won't-do (the work happened).
   - **Stale candidate** (the stale heuristic from `triage-labels` §"What untriaged means": 90+ days no activity, no recent substantive comment): label `status/stale-candidate`, **don't close inline**; the stale sweep closes.
   - **Blocked**: `status/blocked`, comment with what blocks and who/when.
   - **Needs info**: `status/needs-info`, comment with the specific question, leave open.
   - **Question**: `type/question` + `status/needs-info`, post a clarifying question, leave open, assign no priority (it re-enters triage as `type/bug` / `type/feat` once clarified). If already answered in the comments, close as `completed` with "answered above, closing - reopen if still ambiguous".
   - **Won't-do** (out of scope, below the quality bar, or against design direction): `status/wontdo`, close with `--reason "not planned"` and a comment explaining *why* (for the asker and future triagers). Heavier than Duplicate or Already-resolved; when in doubt surface to the user/maintainer instead of closing.
4. **Ask clarifying questions when gaps appear mid-triage** (beyond items that were obviously missing information). Skip if step 3 closed the item or made it a Question. Triggers: severity undecidable (reproducibility, version, frequency unknown), impact undecidable (all users or one tenant), effort undecidable (root cause unclear). Post a *specific* question, apply `status/needs-info`, and move on without blocking. **Apply your best-guess value for the blocked dimension** so step 6 writes a complete classification; re-triage when the answer arrives.
5. **Edit the *issue* with triage learnings (title and body) when it materially helps future readers** (root-cause hypothesis, introducing version, related PR, environment):
   - **Title fix** when it misleads (*"app crashes"* -> *"app crashes when uploading >2GB file on iOS Safari 17"*): `gh issue edit <num> --title "..."`, plus a comment explaining the rename.
   - **Body augmentation**: **append** a *"Triage notes (added by @<your-handle> on YYYY-MM-DD)"* section, via `gh issue edit <num> --body "$(gh issue view <num> --json body --jq .body)"$'\n\n## Triage notes (...)\n\n...'` or `--body-file`.
   - **Never overwrite the asker's content.** To remove something they wrote, ask in a comment.

   **Don't edit other contributors' PR titles or bodies**; leave comments or request changes. Own PRs are fine; others only if the contributor explicitly invited it.
6. **Apply the classification labels and the `triaged` marker in one bulk command**: `gh issue edit --add-label "triaged,priority/p1,severity/medium,urgency/this-sprint,impact/many,effort/s,type/bug"`. Labels from steps 3-4 (`status/blocked`, `status/needs-info`, `status/stale-candidate`) stay (`--add-label` is additive). **Exceptions**:
   - **Closed in step 3** (Duplicate, Already resolved, Won't-do, Question answered-and-closed): skip step 6.
   - **Question, still open**: write only `triaged`; omit `priority/*`, `severity/*`, `urgency/*`, `impact/*`, `effort/*`.
   - **Stale candidate, Blocked, Needs info, no disposition**: full classification.
7. **Leave a triage comment** when the rationale isn't obvious from labels ("P1 because it blocks onboarding for the multi-account flow shipping next sprint"). Skip trivial cases.

**Stale sweep** (separate pass, on demand or monthly/quarterly, not every triage; see `triage-labels` §"What untriaged means"): over items flagged `status/stale-candidate` (on a fresh repo, items meeting the stale heuristic). For each:

1. Read the latest comment and linked PRs to confirm nobody is working on it silently.
2. Comment: *"This issue has had no activity since YYYY-MM-DD. Closing as stale - reopen with a comment if it's still relevant."*
3. Closure timing:
   - **Calendar-driven sweep**: post the warning, leave open. The *next* sweep finds past-grace items with `gh issue list --search "is:open label:status/stale-candidate updated:<$GRACE_CUTOFF"`, `GRACE_CUTOFF=$(date -v-7d -u +%Y-%m-%d 2>/dev/null || date -u -d '7 days ago' +%Y-%m-%d)` (BSD/macOS or Linux). Items with a substantive reply fall out of that filter: drop the label and re-triage.
   - **Ad-hoc sweep at user request** ("clean up stale issues"): close immediately (the user waived the grace window).
4. Close with `--reason "not planned"` and label `status/wontdo` (greppable).

`gh --reason` values: **`not planned`** for Duplicate, stale-sweep and Won't-do closures (always with a comment); **`completed`** for Already-resolved and Question-answered.

## Parallel triage across multiple agents

For ~20+ untriaged items, or when the user asks for a parallel pass, split across `Agent` subagents, one per chunk. Smaller backlogs (<15) triage serially; between 15 and 20 use judgement (dense items favour fan-out).

1. **Main session** partitions the untriaged set into chunks of ~10-15 items. **Cap concurrency at ~6 agents per wave**; more chunks run as sequential waves, and the next wave never launches alongside an in-flight one.
2. **Spawn one `Agent` per chunk in a single message** (so they run concurrently). Each agent gets: its issue/PR numbers, the canonical label vocabulary pasted verbatim (plus the priority rubric table and dispositions list inline), and the path to the `triage-labels` skill. It runs read + classify + label + comment for its chunk only and reports a compact JSON-shaped summary, e.g. `{ "triaged": 12, "closed_dups": 2, "closed_already_resolved": 1, "closed_completed": 1, "wontdo_surfaced": [173], "stale_candidates": 3, "p0_p1": [142, 156], "needs_info": [149], "questions": [161] }`. Keys are canonical (plural for collections, scalar counts otherwise); `wontdo_surfaced` lists issues flagged for maintainer review, not closed. Omit zero/empty keys; extra context fields are allowed.
3. **Main session aggregates** into one decisions table, runs a calibration sweep (agents land different "P1" thresholds; adjust), and posts the focus list.
4. **Check for other sessions first** (`multi-agent-comms`): disjoint chunks don't race, but another session or project bot triaging the same repo does. If one runs, agree who runs the pass. Without session messaging (Codex CLI, Gemini CLI, scheduled routines) ask the user or coordinate on a shared issue.

**Model tier per agent** (`Agent` `model` parameter; put effort in the brief):
- **Sonnet 5.5 low** (default): mechanical labelling, typo/dup/stale, clear severity/effort calls.
- **Sonnet 5.5 medium**: priority calibration needing judgement (severity vs impact tradeoffs, project context).
- **Sonnet 5.5 high**: rare; security findings needing exploitability assessment, or architecture proposals.

## Three-pass approach for very large backlogs (100+ items)

Three sequential **passes**; within each, parallelize into **waves** (<=6 agents) per §"Parallel triage". Pass 2 starts only after Pass 1 finishes every item. Don't fan out a pass over <15 items; calibrate in the main session between passes.

1. **Pass 1 - coarse triage from title + first paragraph.** Apply `type/*` and a conservative *provisional* `severity/*` (Pass 2 may revise both). No priority. Flag obvious abandonments (vague title, empty/one-line body, no recent activity) `status/stale-candidate`; close nothing. Provisional labels are why this isn't the "bulk-labeling unread" anti-pattern.
2. **Pass 2 - body-level deep read for severity >= medium.** Skip items Pass 1 tagged `severity/low` with `type/docs` or `type/chore`. Apply the full classification: refine `severity/*`, revise `type/*`, add `urgency/*`, `impact/*`, `effort/*`. Close the low-judgement dispositions (**Duplicate**, **Already-resolved**); **don't unilaterally Won't-do-close**: flag with a triage comment ("body suggests out-of-scope; surface to maintainer") for the main session. Flag missed stale candidates. When a Pass 1 stale-candidate turns out to be a confirmed Duplicate or Already-resolved, **the closing disposition wins**: drop `status/stale-candidate` and close. Outcome: every open, non-stale item has all five non-priority dimensions.
3. **Pass 3 - relative priority calibration** over the open, non-stale set. Apply `priority/*` by sorting `severity desc, impact desc, urgency desc` and banding so the rubric's calibration check holds (~5-10% P0+P1 combined). Surface the focus list only after this pass.

Rough shape for 200 items: Pass 1 flags ~10% stale; Pass 2 closes ~20% (mostly Duplicate) and flags ~10% more stale; ~120 items reach Pass 3. Order-of-magnitude only. Stale candidates leave via the stale sweep.

## Anti-patterns

- **"Everything is P0"**: recalibrate; most items are P2/P3.
- **Closing without a comment**: even "duplicate of #X" tells the next reader why.
- **Bulk-labeling unread for *final* labels** (except Pass 1's provisional labels).
- **Triaging from the title alone**: read the body and last 2 comments.
- **Skipping linked PRs**: a closed PR may make the issue stale; a draft PR may be the fix.
- **Using `priority/*` as a synonym for severity.**
- **Re-triaging an already-`triaged` item without changed context.**
- **Closing stale items inline in the regular loop**: only flag; the sweep closes after a grace window.

## Reporting back to the user

End every pass with:

1. **Counts**, issues and PRs reported separately when both were in scope.

   **Regular pass** (does not close stale items):
   - **Open before**: total open at the start.
   - **Newly triaged**: got `triaged` this pass, still open.
   - **Stale candidates flagged**: got `status/stale-candidate`, still open.
   - **Closed as duplicate**: reason "not planned" + "duplicate of #X".
   - **Closed as won't-do**: reason "not planned" + `status/wontdo` + explanatory comment.
   - **Closed as completed**: reason "completed" (answered Questions, items made moot by another PR/commit).
   - **Open after**: re-query live (`gh issue list --state open`, `gh pr list --state open`); don't subtract from the start snapshot. Also how many are now `triaged` cumulatively. Report only categories in scope. Check: `(Open before) - (Open after) ~ duplicates + won't-do + completed`, give or take items others opened/closed mid-pass.

   **Stale-sweep pass**:
   - **Stale-candidate count before** / **after** (still flagged, not yet past grace).
   - **Closed as stale / won't-do**: "not planned" after the grace window.
   - **Reactivated**: grace comment got a reply proving relevance; drop the label, leave open for re-triage.
2. **Focus list**: top 3-5 items to pick up next, showing **all five** `work-selection` sort dimensions (priority, urgency, impact, effort, unblocks-others note when relevant) plus a one-line "why now".
3. **Hygiene flags**: e.g. "5 P1 items are over 6 months old - someone's working on them silently, or they should be P3"; "30% of items are `effort/xl` - break the top ones down"; "10 items closed as duplicates of #N - that issue may need attention".
4. **Next action**: *"Start with #X. Want me to open a worktree?"*

Triage isn't done until the user has a clear ranked next-action list, not just a labelled backlog.

## Cadence + proactive flagging

Ad-hoc on request; before sprint kickoff (every 1-2 weeks on active projects); after major merges that closed or invalidated issues; monthly stale-sweep plus light re-triage for slow projects. Match the project's velocity.

**Proactive flagging**: at session start, if the backlog looks overwhelming (>30 untriaged items, OR >5 open PRs untouched in 7 days, OR a P0 without recent activity), offer once: *"This project has 47 open issues with no triage labels - want a triage pass before we pick up other work?"* Don't run it uninvited; skip the offer when the user is mid-task.
