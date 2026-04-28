# Backlog triage + work selection

This file is the playbook for two adjacent activities:

1. **Triaging** the open issue + PR backlog of a repository — applying labels for type, severity, urgency, impact, effort, and priority so the work is rankable.
2. **Picking the next thing to work on** from the triaged backlog — always the highest-impact, most-urgent, most-important item that's actionable, never a random "interesting" one.

These two activities are linked: you can't pick well without triaging first, and triage is wasted effort if it doesn't drive selection.

## When to run a triage pass

Run a **full** triage pass when:

- The user says "triage", "let's triage", "prioritize the backlog", "go over open issues", or asks for a backlog overview by name.
- Before non-trivial sprint planning (more than a few items in flight).
- After a release or merge of a major PR — backlog hygiene before the next cycle.
- When starting work in an unfamiliar repo for the first time and you need to understand what's open and competing for attention.
- Whenever the open count exceeds ~30 untriaged items — the backlog has stopped being a list and started being a swamp; triage is overdue.

For a casual "what should I work on next?" with a **small, already-labelled** backlog (≤10 items, with priority labels visible), **skip the full triage**. Just sort the existing labels per §"Picking the next thing to work on" and surface the top 3 with one-line rationales. The full process is for backlogs that aren't already legible. The CLAUDE.md §7 gating rule formalises this — don't loop back to here if the lighter selection is enough.

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

## The triage pass itself

For each untriaged item:

1. **Read context** — title, body, the last 2–3 comments, any linked code paths or referenced issues. For unfamiliar code areas, dispatch an `Explore` subagent rather than burning main-context tokens.
2. **Classify**:
   - Type, severity, urgency, impact, effort.
   - Then derive priority from the rubric.
3. **Catch dispositions**:
   - **Duplicate**: link the canonical issue, close with `--reason "not planned"` and a "duplicate of #X" comment.
   - **Already resolved by another PR / commit**: when an issue was implicitly fixed by an unrelated PR or commit that didn't auto-close it (the PR title didn't reference the issue, but reading the diff confirms the fix), close with `--reason completed` and a comment linking the resolving PR/commit. The work *did* happen — it just wasn't linked. Distinct from Duplicate (in Already-resolved there's no other open issue to redirect to; the fix is in code) and from Won't-do (in Won't-do the project declined the work; here the work was done).
   - **Stale candidate (matches the stale heuristic from §"What untriaged means" — 90+ days no activity, no recent substantive comment)**: flag with `status/stale-candidate`, **don't close inline** — the dedicated stale-sweep pass (separate section below) handles closure. The regular loop only marks them so the sweep finds them.
   - **Blocked**: label `status/blocked`, comment with what's blocking and who/when.
   - **Needs info**: label `status/needs-info`, comment with the specific question, leave open.
   - **Question** (`type/question`): apply `type/question` + `status/needs-info`, post a clarifying-question comment, leave open. If the question is already answered in the comments, close as `completed` instead with a "answered above, closing — reopen if still ambiguous" comment. Don't assign priority — questions re-enter triage as `type/bug` or `type/feat` once the asker confirms what they actually need.
   - **Won't-do**: when the triager (or maintainer) decides the item is out of scope, below the project's quality bar, or contradicts the project's design direction. Apply `status/wontdo`, close with `--reason "not planned"`, and post a comment explaining *why* — "out of scope: this is feature X which we decided not to pursue in [linked issue/discussion]" or "below quality bar: edge case affects <0.1% of users; not justifying the maintenance cost". The reason matters for the asker and for future triagers who'd otherwise re-open the same kind of request. This is a heavier decision than Duplicate or Already-resolved — when in doubt, surface to the user / maintainer instead of closing inline.
4. **Proactively ask clarifying questions when you find gaps mid-triage** (separate from the *Needs info* / *Question* dispositions, which are for items obviously missing information from the start). Skip this step if step 3 closed the item (Duplicate, Already resolved by another PR / commit, Won't-do, or Question already answered) or treated the whole thing as a question (Question disposition with `type/question`). Triggers: you can't decide severity because you don't know whether the bug is reproducible / which version it hits / how often it fires; you can't decide impact because you don't know if it affects all users or one tenant; you can't decide effort because the underlying root cause is unclear. In each case, post a *specific* question (not "more info please") to the issue, apply `status/needs-info`, and move on — don't block the rest of the triage waiting for an answer. **Apply your best-guess value for the blocked dimension** so step 6's bulk command writes a complete classification — the `status/needs-info` flag tells future readers (and you, when the asker replies) that the value may shift on update. Re-triage from the rubric once the answer comes in.
5. **Edit the *issue* with triage learnings — title and body, not just comments — when it materially helps future readers.** Triage often surfaces context the asker didn't include (root cause hypothesis after reading the linked code, version that introduced the regression, related-PR pointer, environment specifics). Two patterns, plus a hard constraint:
   - **Pattern 1 — title fix**: when the title misleads about the actual problem (e.g. *"app crashes"* → *"app crashes when uploading >2GB file on iOS Safari 17"*) — edit the title via `gh issue edit <num> --title "..."`. Always leave a comment explaining the rename so the asker isn't surprised.
   - **Pattern 2 — body augmentation**: when you've discovered context the body lacks, **append** to the body in a clearly-marked *"Triage notes (added by @\<your-handle\> on YYYY-MM-DD)"* section. Use `gh issue edit <num> --body "$(gh issue view <num> --json body --jq .body)"$'\n\n## Triage notes (...)\n\n...'` or write the new body to a tempfile and pass `--body-file`. Useful for: linked code paths, suspected root cause, related issues / PRs, repro steps you derived, environment matrix.
   - **Constraint — never overwrite the asker's content.** Their original wording stays; your additions are separate, attributed, dated. If you're tempted to delete something the asker wrote, post a comment asking instead.

   **PRs are different — don't edit other contributors' PR titles or bodies.** A PR title and description are the contributor's framing of their change; rewriting them is a courtesy violation even when you have write access. For PR triage, leave clarifying *comments* instead, or request changes via review. The exception is your own PRs (treat them as your own writing). For PRs you didn't author: only edit if the contributor explicitly invited it (e.g. "feel free to clean up my title").
6. **Apply the classification labels (and the `triaged` marker) in one bulk command** — `gh issue edit --add-label "triaged,priority/p1,severity/medium,urgency/this-sprint,impact/many,effort/s,type/bug"`. Disposition labels from step 3 (`status/blocked`, `status/needs-info`, `status/stale-candidate`) and the `status/needs-info` label from step 4 (if applied) are already on the item — `--add-label` is additive, so this command doesn't disturb them. **Disposition-specific exceptions**:
   - **Duplicate** (closed in step 3): skip step 6 entirely — the item is closed and doesn't need classification.
   - **Already resolved by another PR / commit** (closed in step 3): skip step 6 entirely — same reasoning.
   - **Won't-do** (closed in step 3): skip step 6 entirely — same reasoning. The `status/wontdo` label from step 3 is sufficient signal.
   - **Question — open branch** (asker hasn't answered yet, item still open): the step 3 disposition wrote `type/question` + `status/needs-info` and explicitly skipped priority. So in step 6, omit `priority/*`, `severity/*`, `urgency/*`, `impact/*`, `effort/*` for Question items — write only the `triaged` marker (so the item is marked processed) and let it re-enter triage as `type/bug` / `type/feat` once the asker clarifies.
   - **Question — answered-and-closed branch** (already-answered case from step 3, closed as `completed`): skip step 6 entirely — the item is closed and doesn't need classification.
   - **Stale candidate, Blocked, Needs info, no disposition**: full classification as written above.
7. **Leave a triage comment** when the rationale isn't obvious from labels alone — e.g. "P1 because it's blocking onboarding for the multi-account flow that the team is shipping next sprint." Skip for trivial cases ("typo, P3, effort/xs").

**Special case — stale sweep** (separate pass — see §"What untriaged means" — stale axis): a focused pass over items flagged with `status/stale-candidate` (or, on a fresh repo with no flags applied yet, items meeting the stale heuristic per the previous section: 90+ days no activity, no recent substantive comment). For each:

1. Read the latest comment + linked PRs to confirm nobody's working on it silently.
2. Post a comment like *"This issue has had no activity since YYYY-MM-DD. Closing as stale — reopen with a comment if it's still relevant."*
3. Closure timing depends on context:
   - **Calendar-driven sweep** (monthly/quarterly): post the stale-warning comment, leave the item open. The *next* sweep iteration detects past-grace items via `gh issue list --search "is:open label:status/stale-candidate updated:<$GRACE_CUTOFF"` where `GRACE_CUTOFF=$(date -v-7d -u +%Y-%m-%d 2>/dev/null || date -u -d '7 days ago' +%Y-%m-%d)` (BSD/macOS or Linux) — those are the close candidates (`status/stale-candidate` label is set AND the issue hasn't been touched in the grace window). Items that got a substantive reply during the grace window will have `updatedAt` inside the window and naturally fall out of that filter; drop the `status/stale-candidate` label and re-triage them.
   - **Ad-hoc sweep at user request** ("clean up stale issues"): close immediately. The user has implicitly approved skipping the grace window.
4. Close with `--reason "not planned"` and label `status/wontdo` so the disposition is greppable later.

The stale sweep is a separate pass, not part of the regular triage loop — run it on demand or on a calendar (monthly/quarterly), not every triage. `gh`'s `--reason` flag accepts two values:

- **`not planned`** — used by Duplicate, Stale-sweep closures, Won't-do (anything where the project decided the work won't happen). Always pair with a clarifying comment.
- **`completed`** — used by Already-resolved-by-another-PR/commit (the work happened in code, the issue just wasn't auto-closed) and Question-answered-in-comments (the asker's question has been answered). Reserved for genuinely-resolved cases.

## Parallel triage across multiple agents

For mid-to-large backlogs (~20+ untriaged items) or when the user explicitly asks for a parallel pass: split the work across several `Agent` subagents, one per chunk. The break-even point is roughly where serial triage would burn more main-context tokens than the orchestration overhead of fanning out — around 20 items in practice. Use judgement: a backlog of 25 simple-looking items might still be cheaper to triage serially; a backlog of 18 dense ones benefits from parallelization.

**Pattern**:

1. **Main session** lists the untriaged set, partitions into chunks of ~10–15 items each (size the chunk so each agent comfortably fits the bodies + comments in its context window). **Cap concurrency at ~6 agents per wave** — anything beyond strains API rate limits and floods the main session with reports faster than it can sensibly aggregate. For backlogs that produce more chunks than the cap, run sequential waves of up to 6 parallel agents each.
2. **Spawn one `Agent` per chunk in parallel — within the per-wave cap from step 1.** Use a single message with multiple `Agent` tool-uses so the agents in this wave actually fan out (Anthropic API runs them concurrently). When chunks > cap, complete the current wave's reports before launching the next wave; never spawn the next wave's agents alongside an in-flight wave just because chunks remain. Per-agent behaviour:
   - Each agent gets the slice of issue/PR numbers it owns, the canonical label vocabulary (paste verbatim — it's compact), and the path to this file (`~/.claude/triage.md`) for the deeper rubric / dispositions / anti-patterns. Pasting the priority rubric table and the dispositions list inline keeps the agent fast on common cases without forcing a full file read.
   - Each agent does the per-item read + classify + label + comment loop for its chunk only.
   - Each agent reports back: a compact JSON-shaped summary, e.g. `{ "triaged": 12, "closed_dups": 2, "closed_already_resolved": 1, "closed_completed": 1, "wontdo_surfaced": [173], "stale_candidates": 3, "p0_p1": [142, 156], "needs_info": [149], "questions": [161] }`. Treat as pseudo-schema — the keys above are the canonical ones (all plural for collection-shaped values, scalar counts for scalar values; `wontdo_surfaced` is a list of issue numbers the agent flagged for maintainer review per the Pass 2 rule, not actually closed). Agents may include additional context fields per chunk if useful. Omit keys with zero / empty values to keep the report compact.
3. **Main session aggregates** the agent reports into a single decisions table, runs a calibration sweep (do the priorities make relative sense across chunks? Often agent A and agent B will independently land different "P1" thresholds — adjust), and posts the focus list.
4. **Coordinate via the multi-agent-comms bus** (see `~/.claude/multi-agent-comms.md`) — within a single session, disjoint chunks won't race on label edits. The race to worry about is **another concurrent session** triaging the same repo (or a project bot rewriting labels mid-pass). Acquire the appropriate `triage-<repo>` lock when starting; release when done.

**Don't fan out for trivially small backlogs** (<15 items) — orchestration overhead exceeds the savings. Just triage serially.

**Choose model tier per agent** (`Agent` tool's `model` parameter):

- **Haiku**: chunks where most items are clearly typo / dup / stale on a quick read.
- **Sonnet**: most chunks. Default.
- **Opus**: chunks heavy with security findings, architecture proposals, or anything that needs judgement on priority calibration.

## Three-pass approach for very large backlogs (100+ items)

When a single triage pass would burn too much budget, split the work into three sequential passes (matching the section heading: Pass 1, Pass 2, Pass 3). Within each pass, parallelize across `Agent` subagents per the §"Parallel triage" pattern. Two distinct concepts share the file:

- **Pass** (used in this section): one of three sequential stages — Pass 2 cannot start until Pass 1 finishes for every item.
- **Wave** (used in §"Parallel triage"): a batch of ≤6 concurrent agents inside a single pass.

So the structure is: three sequential passes; each pass fans out into one or more parallel waves of agents.

1. **Pass 1 — coarse triage from title + first-paragraph signal** (fast, cheap).
   - Read each item's title and the first paragraph of its body. Apply `type/*` and a conservative `severity/*` *initial* default. Don't assign priority yet. Both labels are provisional — Pass 2 will refine severity (and may revise type if the body contradicts the title-paragraph read).
   - Also flag obvious stale candidates with `status/stale-candidate` when the title-and-first-paragraph signal is strong enough on its own (vague title + empty / one-line body + no recent activity = abandoned). Don't close — the dedicated stale sweep does that.
   - Outcome: every item has a type and a coarse-but-readable severity, and obvious abandonments are parked.
   - This is *not* the same as the "Bulk-labeling unread for *final* labels" anti-pattern below: that anti-pattern is about applying *final* labels from the title alone. Here we apply *provisional* labels and refine in Pass 2.
2. **Pass 2 — body-level deep-read for severity ≥ medium**.
   - Skip items Pass 1 tagged with `severity/low` AND a low-investment type (`type/docs` or `type/chore`) — these are the typo / polish / idea long tail and won't reward a body-level read. Focus on the unknowns and the high-severity items.
   - During the body read, apply the **full classification** the per-item triage rubric expects: refine `severity/*`, revise `type/*` if the body contradicts Pass 1's title-paragraph guess, and add `urgency/*`, `impact/*`, `effort/*` (these dimensions can't be inferred from a title alone — Pass 2 is the pass that adds them). Catch the *low-judgement* closing dispositions from §"The triage pass itself" step 3 — **Duplicate** and **Already-resolved-by-another-PR/commit** — since the body read makes them visible and they're objectively verifiable (the canonical issue exists; the resolving PR's diff confirms the fix). **Don't unilaterally Won't-do-close in Pass 2** — that's a heavier judgement call (see step 3's Won't-do disposition); flag those candidates with a triage comment ("body suggests out-of-scope; surface to maintainer") and let the main session decide post-aggregation. Flag any additional stale candidates that Pass 1 missed (don't close stale-candidates — the dedicated stale sweep does that). When a Pass 1 stale-candidate turns out to be a confirmed Duplicate or Already-resolved on Pass 2's body read, **closing disposition wins**: drop the `status/stale-candidate` label and close per the disposition's standard reason. A confirmed closure is resolvable now; staying parked for the stale sweep would be a wasted detour.
   - Outcome: every item that's still open AND not parked as `status/stale-candidate` (i.e. every item Pass 3 will calibrate) has all five non-priority dimensions filled in. Stale-candidates are still open but skipped by Pass 3 — they wait for the dedicated stale sweep.
3. **Pass 3 — relative priority calibration** across the surviving set.
   - Now apply `priority/*`. Calibrate items *against each other* (relative ranking, not absolute) — sort by `severity desc, impact desc, urgency desc` and apply priority bands so that the rubric's calibration sanity-check holds (~5–10% P0+P1 combined). The sort works because Pass 2 wrote all three dimensions.
   - Surface the focus list to the user only after this pass.

This avoids over-investing in items that turn out to be duplicates or trivial after the first read. For a 200-item backlog: Pass 1 doesn't close anything (the title-plus-first-paragraph signal isn't enough for a confident closing-disposition call — that needs a full body read, which happens in Pass 2) but it does flag ~10% as stale candidates from the title-and-first-paragraph signal (low-quality issue, no detail, looks abandoned). Pass 2 closes ~20% via the low-judgement closing dispositions (Duplicate + Already-resolved — usually mostly Duplicate) after the body read, and flags another ~10% as stale candidates. Won't-do candidates surfaced during Pass 2 stay open with a triage comment, awaiting maintainer decision. Pass 3 then calibrates priority across the items that are still open AND not parked as stale candidates — so for a 200-item start, ~160 items remain open after Pass 2 closures, of which ~40 are stale-candidates parked for the sweep, leaving ~120 items in active priority calibration. Numbers vary wildly by repo health; treat as order-of-magnitude. The stale-candidates eventually disappear via the dedicated stale sweep, not via the triage pass itself.

**Composing with parallel-agent fan-out**: each pass can be parallelized — partition items into chunks, spawn `Agent` per chunk, aggregate. After each pass the main session does a calibration sweep before launching the next. Don't fan out for a pass over <15 items.

## Anti-patterns

- **"Everything is P0"** — the rubric is broken; recalibrate. Most items are P2/P3.
- **Closing without a comment** — even "duplicate of #X" is more useful than silent close. The next reader needs to know why.
- **Bulk-labeling unread for *final* labels** — labels must reflect understanding, not pattern-matching on titles. The exception is the explicit Pass 1 of the three-pass approach for huge backlogs, where labels are *provisional* and refined in Pass 2; that's not this anti-pattern.
- **Triaging from the title alone** — at minimum read the body and the last 2 comments. Titles are routinely misleading.
- **Skipping linked PRs** — a closed PR may render the issue stale, or a draft PR may be the intended fix. Always check.
- **Using `priority/*` as a synonym for severity** — they measure different things; the rubric distinguishes them deliberately.
- **Re-triaging an already-`triaged` item without a reason** — once an item is triaged, the labels stick unless context has changed. Re-triage is for recalibration, not for repeating the work.
- **Closing stale items inline during the regular loop** — the regular loop only *flags* stale candidates with `status/stale-candidate`. Closure is the dedicated stale-sweep pass's job and respects a grace window. Closing inline skips that grace and surprises issue authors who'd reply if asked.

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

## Reporting back to the user

End every triage pass with:

1. **Counts**, with explicit semantics so the numbers add up. The exact set differs by pass type:

   **Regular triage pass** (default — does not close stale items). Report issues and PRs separately when both were in scope (per the two §Mechanics queries) — don't lump them together, since the disposition mix and the focus list are different for each:
   - **Open before** (issues / PRs): total open items at the start of the pass.
   - **Newly triaged** (issues / PRs): items that got the `triaged` label this pass (still open).
   - **Stale candidates flagged** (issues / PRs): items that got `status/stale-candidate` (still open, parked for the stale sweep).
   - **Closed as duplicate** (issues / PRs): closed with reason "not planned" + a "duplicate of #X" comment.
   - **Closed as won't-do** (issues / PRs): closed with reason "not planned" + a `status/wontdo` label and an explanatory comment ("out of scope" / "below quality bar" / etc.). Distinct from duplicate — there's no canonical-other-issue, the project just decided not to pursue this.
   - **Closed as completed** (issues / PRs): closed with reason "completed" — e.g. Question items where the answer was already in the comments, or items rendered moot by another PR/commit. Distinct from "duplicate" and "won't-do" (both of which use "not planned").
   - **Open after** (issues / PRs): re-query at end-of-pass via `gh issue list --state open` for issues and `gh pr list --state open` for PRs (don't subtract from the start-of-pass snapshot — items can be opened or closed by others during the pass; the live count is authoritative). Of those, how many are now `triaged` (= cumulative triaged including pre-existing). If only one category was in scope (e.g. "triage just the issues"), report only that category. Reconciliation hint: `(Open before) - (Open after) ≈ (Closed as duplicate) + (Closed as won't-do) + (Closed as completed)`, give or take items opened/closed by others mid-pass.

   **Stale-sweep pass** (separate — closes stale items):
   - **Stale-candidate count before**: items with `status/stale-candidate` at the start.
   - **Closed as stale / won't-do**: closed with reason "not planned" + a stale or won't-do explanation, after the grace window.
   - **Reactivated**: items where the stale-grace comment got a reply that proves the issue is still relevant — drop the `status/stale-candidate` label, leave open for re-triage.
   - **Stale-candidate count after**: items still flagged but not yet past the grace window.
2. **Focus list**: top 3–5 items the user should pick up next. Show **all five** sort dimensions used in §"Picking the next thing to work on" — priority, urgency, impact, effort, and (when relevant) the unblocks-others note — plus a 1-line "why now". Showing all five lets the user see at a glance why two items at the same priority+urgency are ranked differently.
3. **Hygiene flags**: anything weird worth surfacing —
   - "5 P1 items are over 6 months old — either someone's working on them silently, or they should be P3."
   - "30% of items have `effort/xl` — your backlog is full of mountains; consider breaking the top ones into smaller pieces."
   - "10 items closed as duplicates of #N — that issue may need its own attention."
4. **A pointer to the next action**: *"Start with #X. Want me to open a worktree?"*

The triage isn't done until the user has a clear, ranked next-action list — not just a labelled backlog.

## Cadence + proactive flagging

- **Ad-hoc** when the user asks.
- **Before sprint kickoff** (every 1–2 weeks for active projects).
- **After major merges** when the merged PR closed or invalidated several issues — re-triage the related items.
- **Calendar-driven** for slow-moving projects: monthly stale-sweep + light re-triage.

The cadence should match the project's velocity. A high-velocity project benefits from weekly triage; a low-velocity one drowns in process if you triage weekly.

**Proactive flagging from the assistant side**: at session start, when reading a project's CLAUDE.md / known-issues / project state, if the open backlog appears overwhelming (>30 untriaged items, OR >5 open PRs not touched in the last 7 days, OR a P0 issue without recent activity), surface this to the user with a one-line offer: *"This project has 47 open issues with no triage labels — want a triage pass before we pick up other work?"* Don't run the triage uninvited; surface and let the user decide. Skip the prompt for projects where the user is clearly mid-task — wait for a natural break.
