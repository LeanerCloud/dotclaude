#!/usr/bin/env bash
# PreToolUse(Bash) hook: restore the "always applies" guarantee for the few rules that used to be
# in every session's context and are now inside skills.
#
# Skills load only when invoked, and nothing enforces invocation. For most guidance that is fine —
# the trigger lines in CLAUDE.md carry the headline rule. For a handful of commands the cost of
# missing the rule is high and the command is rare, so a one-line nudge next to the tool call is
# worth it. Deliberately NOT fired on every Bash call: a reminder that appears constantly stops
# being read and just burns context.
#
# Emits hookSpecificOutput.additionalContext and always exits 0 — it never blocks a command.
set -euo pipefail

input=$(cat)
command=$(printf '%s' "$input" | jq -r '.tool_input.command // ""')

remind() {
  jq -n --arg ctx "$1" \
    '{hookSpecificOutput: {hookEventName: "PreToolUse", additionalContext: $ctx}}'
  exit 0
}

case "$command" in
  *"git commit"*)
    remind "Reminder (git-commit skill): the pre-commit review loop is mandatory and runs until 3 consecutive passes find zero issues — fix findings in the same changeset, never in a follow-up commit. Conventional-commit format, no Anthropic/Claude mention, no heredoc -m. Invoke the git-commit skill if it is not already loaded."
    ;;
  *"git push"*)
    remind "Reminder (ci-watch skill): after this push, enumerate every workflow run for the pushed commit and launch one background watcher per run. If this is an open PR branch, also re-request CodeRabbit and arm its watcher in the same action (cr-loop skill)."
    ;;
  *"gh pr create"*)
    remind "Reminder (pr-lifecycle skill): within ~30s of this returning — mirror the closing issue's triage labels onto the PR, spawn cr-watch BEFORE posting '@coderabbitai review', spawn one ci-watch per workflow run, and spawn merge-watch. A trigger without a watcher is the defect this checklist exists to prevent."
    ;;
esac

exit 0
