#!/usr/bin/env bash
set -euo pipefail

mode=worktree
case "${1:-}" in
  "") ;;
  --staged) mode=staged ;;
  --worktree) mode=worktree ;;
  *)
    printf 'usage: %s [--staged|--worktree]\n' "$0" >&2
    exit 2
    ;;
esac

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
grep_args=(grep)
if [ "$mode" = staged ]; then
  grep_args+=(--cached)
fi

# The reclaim fixture suite creates throwaway worktrees under /private/tmp on purpose.
pathspecs=(AGENTS.md CLAUDE.md skills scripts ':(exclude)scripts/validate-worktree-policy.sh' ':(exclude)scripts/tests/reclaim-merged-worktrees.test.sh')
failures=0

fail() {
  printf 'error: %s\n' "$1" >&2
  failures=$((failures + 1))
}

assert_contains() {
  local path="$1"
  local text="$2"
  local label="$3"
  local status

  if git -C "$repo_root" "${grep_args[@]}" -q -F "$text" -- "$path"; then
    return
  else
    status=$?
  fi

  if [ "$status" -eq 1 ]; then
    fail "$label ($path)"
  else
    fail "could not read $path from the $mode view (git grep exit $status)"
  fi
}

assert_not_contains() {
  local path="$1"
  local text="$2"
  local label="$3"
  local status

  if git -C "$repo_root" "${grep_args[@]}" -q -F "$text" -- "$path"; then
    fail "$label ($path)"
  else
    status=$?
    if [ "$status" -gt 1 ]; then
      fail "could not read $path from the $mode view (git grep exit $status)"
    fi
  fi
}

assert_contains AGENTS.md '.claude/worktrees/' 'always-on guidance must name the durable worktree root'
assert_contains skills/worktrees/SKILL.md '.claude/worktrees/' 'worktree creation must use the durable root'
assert_contains skills/pr-orchestration/SKILL.md '.claude/worktrees/' 'orchestration must use the durable root'
assert_contains skills/pr-iterate/SKILL.md '.claude/worktrees/' 'PR iteration must use the durable root'
assert_contains AGENTS.md 'reclaim-merged-worktrees.sh' 'tenet 9 must name the post-merge reclaim script'
assert_contains skills/worktrees/SKILL.md 'reclaim-merged-worktrees.sh' 'worktrees skill must point at the reclaim script'
assert_contains skills/pr-orchestration/SKILL.md 'reclaim-merged-worktrees.sh' 'merge driver must run the reclaim script'
assert_not_contains skills/worktrees/SKILL.md 'worktree add ../<repo>-<slug>' 'worktrees skill still creates sibling worktrees'
assert_not_contains skills/pr-orchestration/SKILL.md "\$TMPDIR/claude/" 'orchestration still routes worktrees through TMPDIR'
assert_not_contains skills/pr-iterate/SKILL.md '.worktrees/<repo>/<slug>' 'PR iteration still uses a repo-local worktree root'

# Worktree-destination contract. Scanned text is first-party Markdown/shell prose, not parsed shell.
# In: the text after `worktree add` must contain $HOME, ${HOME} or a bare ~ followed by
#   /.claude/worktrees/, unquoted or double-quoted, with no `..`; ; & | split the line so a safe
#   command cannot vouch for an unsafe one; quotes and backslashes inside the command words are
#   ignored when detecting the command; a command ending in `add \` (destination on the next
#   line) and a single-quoted destination are rejected. `#138` in prose is scanned like any text.
#   Prose containing `git worktree add` plus a non-durable word is scanned like a command and fails.
# Out of contract (not detected, pinned by "out of contract:" cases in tests/): a segment starting
#   with a run of # and a space or EOL (heading or comment), text after a space-#-space comment, a `#`
#   glued to the next word (`add /tmp/job #"$HOME/.claude/worktrees/"`), a durable path in an
#   argument other than the destination (`add /tmp/x "$HOME/.claude/worktrees/y"`, `add -b
#   "$HOME/.claude/worktrees/x" /tmp/job`), a command broken before `worktree` or a non-`git`
#   command word (`$GIT worktree add`), and `"$HOME"/...` spellings (rejected as false positives).
q="['\"]*"
add_cmd="git${q}[[:space:]].*worktree${q}[[:space:]]+${q}add${q}[[:space:]]+([^[:space:]].*)\$"
wrapped="git${q}[[:space:]].*worktree${q}[[:space:]]+${q}add${q}[[:space:]]*\$"
# shellcheck disable=SC2016 # literal $HOME in the pattern
durable='((^|[[:space:]"])(\$HOME|\$\{HOME\})|(^|[[:space:]])~)/\.claude/worktrees/'
escape='(^|[[:space:]/"])\.\.([^.]|$)'
# Pre-filter: the word worktree with optional quotes or backslashes between its letters.
prefilter=
for c in w o r k t r e e; do prefilter+="${c}[\\'\"]*"; done
if lines="$(git -C "$repo_root" "${grep_args[@]}" -n -E "$prefilter" -- "${pathspecs[@]}")"; then
  while IFS= read -r line; do
    body="${line#*:}"
    body="${body#*:}"
    while IFS= read -r segment; do
      if [[ $segment =~ ^[[:space:]]*#+([[:space:]]|$) ]]; then continue; fi
      segment="${segment%%[[:space:]]#[[:space:]]*}"
      unesc="${segment//\\/}"
      if [[ $unesc =~ $wrapped ]] && [[ $segment =~ \\[[:space:]]*$ ]]; then
        fail "$line; put the worktree destination on the same line (see the contract in $0)"
      elif [[ $unesc =~ $add_cmd ]]; then
        dest="${BASH_REMATCH[1]}"
        if ! [[ $dest =~ $durable ]] || [[ $dest =~ $escape ]]; then
          fail "$line; expected the worktree destination to be under \$HOME/.claude/worktrees/ (see the contract in $0)"
        fi
      fi
    done <<< "${body//[;&|]/$'\n'}"
  done <<< "$lines"
else
  status=$?
  if [ "$status" -ne 1 ]; then
    fail "git grep failed while locating worktree creation commands (exit $status)"
  fi
fi

if [ "$failures" -gt 0 ]; then
  printf '%d worktree policy error(s)\n' "$failures" >&2
  exit 1
fi

printf 'ok: persistent worktrees use %s\n' "\$HOME/.claude/worktrees/"
