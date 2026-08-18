#!/usr/bin/env bash
# Create the full triage-rubric label set in a repository, once, before the first triage pass.
# This is the executable form of the label set documented in skills/triage-labels/SKILL.md
# ("Default label set") — that skill is the rationale, this file is the thing you run.
#
# Usage:
#   scripts/bootstrap-triage-labels.sh              # current repo (from cwd)
#   scripts/bootstrap-triage-labels.sh owner/repo   # a specific repo
#
# Idempotent: uses `gh label create --force`, so re-running updates colours and descriptions on
# labels that already exist rather than failing. It only ever creates or updates — it never deletes
# a label, including ones not in this set.
#
# Before running in a repo that already has a scheme, check `gh label list --limit 200` and skip the
# dimensions it already covers under different names. Conforming beats fragmenting: if a project
# uses `Pri-Critical` instead of `priority/p0`, keep theirs.
set -uo pipefail

repo_args=()
if [ "$#" -gt 1 ]; then
  echo "usage: $(basename "$0") [owner/repo]" >&2
  exit 2
elif [ "$#" -eq 1 ]; then
  repo_args=(--repo "$1")
fi

failures=0

label() {
  name="$1"; color="$2"; desc="$3"
  if gh label create "$name" --color "$color" --description "$desc" --force "${repo_args[@]+"${repo_args[@]}"}" >/dev/null 2>&1; then
    echo "ok: $name"
  else
    echo "FAILED: $name" >&2
    failures=$((failures + 1))
  fi
}

# Status (procedural)
label "triaged"                 "0e8a16" "Item has been triaged"
label "status/blocked"          "b60205" "Blocked on something external"
label "status/needs-info"       "fbca04" "Awaiting clarification from the asker"
label "status/stale-candidate"  "cccccc" "Flagged for the next stale sweep"
label "status/wontdo"           "ededed" "Closed as not-planned"

# Priority (derived — see the rubric in the skill)
label "priority/p0" "b60205" "Drop everything; same-day fix"
label "priority/p1" "d93f0b" "Next up; this sprint"
label "priority/p2" "fbca04" "Backlog-worthy"
label "priority/p3" "c5def5" "Polish / idea / may never ship"

# Severity (independent of how often)
label "severity/critical" "b60205" "Major harm when it happens"
label "severity/high"     "d93f0b" "Significant harm"
label "severity/medium"   "fbca04" "Moderate harm"
label "severity/low"      "c5def5" "Minor harm"

# Urgency
label "urgency/now"          "b60205" "Drop other things"
label "urgency/this-sprint"  "d93f0b" "Within the current sprint"
label "urgency/this-quarter" "fbca04" "Within the quarter"
label "urgency/eventually"   "c5def5" "No deadline"

# Impact (audience size + blast radius)
label "impact/all-users" "b60205" "Affects every user"
label "impact/many"      "d93f0b" "Affects most users"
label "impact/few"       "fbca04" "Limited audience"
label "impact/internal"  "c5def5" "Team-internal only"

# Effort
label "effort/xs" "c5def5" "Trivial / one-liner"
label "effort/s"  "c5def5" "Hours"
label "effort/m"  "fbca04" "Days"
label "effort/l"  "d93f0b" "Weeks"
label "effort/xl" "b60205" "Multi-week / refactor"

# Type
label "type/bug"      "ee0701" "Defect"
label "type/feat"     "0e8a16" "New capability"
label "type/chore"    "c5def5" "Maintenance / non-user-visible"
label "type/docs"     "0075ca" "Documentation"
label "type/security" "b60205" "Security finding"
label "type/question" "d876e3" "Question for the project / asker"

if [ "$failures" -gt 0 ]; then
  echo "$failures label(s) failed - check 'gh auth status' and repo write access" >&2
  exit 1
fi

echo "triage label set bootstrapped"
