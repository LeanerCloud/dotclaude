#!/usr/bin/env bash
# PreToolUse(Bash) hook: restore the "always applies" guarantee for the few rules that used to be
# in every session's context and are now inside skills.
#
# Skills load only when invoked, and nothing enforces invocation. For most guidance that is fine —
# the trigger lines in AGENTS.md carry the headline rule. For a handful of commands the cost of
# missing the rule is high and the command is rare, so a one-line nudge next to the tool call is
# worth it. Deliberately NOT fired on every Bash call: a reminder that appears constantly stops
# being read and just burns context.
#
# Each reminder type fires at most once per agent (marker file per agent_id, else session_id, and
# type); a payload with neither always reminds. Subagent payloads carry their own agent_id
# (verified 2026-10-13), the main session's has none. Markers live under TMPDIR, are
# cleared at reboot, and survive context compaction.
#
# Emits hookSpecificOutput.additionalContext and always exits 0 — it never blocks a command.
set -euo pipefail

input=$(cat)
command=$(printf '%s' "$input" | jq -r '.tool_input.command // ""' || true)
session_id=$(printf '%s' "$input" | jq -r '(.agent_id // .session_id // "")' || true)

remind() {
  if [ -n "$session_id" ]; then
    marker_dir="${TMPDIR:-/tmp}/claude-skill-reminders"
    marker="$marker_dir/$(printf '%s' "$session_id" | tr -c 'A-Za-z0-9_-' '_').$2"
    mkdir -p "$marker_dir" 2>/dev/null || true
    # A failed claim with the marker present means already reminded; with no marker, TMPDIR is unwritable, so remind.
    ( set -C; : > "$marker" ) 2>/dev/null || { [ -e "$marker" ] && exit 0; }
  fi
  jq -n --arg ctx "$1" \
    '{hookSpecificOutput: {hookEventName: "PreToolUse", additionalContext: $ctx}}' || true
  exit 0
}

case "$command" in
  *"git commit"*)
    remind "Reminder (git-commit skill): the pre-commit review loop is mandatory and runs 2 review passes, whatever the stakes; fix findings in the same changeset, never in a follow-up commit. Conventional-commit format, keep the harness attribution trailer, no heredoc -m. Invoke the git-commit skill if it is not already loaded." git-commit
    ;;
  *"git push"*)
    remind "Reminder (ci-watch skill): after this push, enumerate every workflow run for the pushed commit and launch one background watcher per run. If this is an open PR branch, also re-request CodeRabbit and arm its watcher in the same action (cr-loop skill)." ci-watch
    ;;
  *"gh pr create"*)
    remind "Reminder (pr-lifecycle skill): within ~30s of this returning — mirror the closing issue's triage labels onto the PR, spawn cr-watch BEFORE posting '@coderabbitai review', spawn one ci-watch per workflow run, and arm the merge-watch shell watcher. A trigger without a watcher is the defect this checklist exists to prevent." pr-lifecycle
    ;;
esac

exit 0
