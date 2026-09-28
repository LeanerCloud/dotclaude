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

pathspecs=(CLAUDE.md skills scripts ':(exclude)scripts/validate-worktree-policy.sh')
failures=0

fail() {
  printf 'error: %s\n' "$1" >&2
  failures=$((failures + 1))
}

read_path() {
  local path="$1"

  if [ "$mode" = staged ]; then
    git -C "$repo_root" show ":$path"
  else
    cat "$repo_root/$path"
  fi
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

assert_contains CLAUDE.md '.claude/worktrees/' 'always-on guidance must name the durable worktree root'
assert_contains skills/worktrees/SKILL.md '.claude/worktrees/' 'worktree creation must use the durable root'
assert_contains skills/pr-orchestration/SKILL.md '.claude/worktrees/' 'orchestration must use the durable root'
assert_contains skills/pr-iterate/SKILL.md '.claude/worktrees/' 'PR iteration must use the durable root'
assert_not_contains skills/worktrees/SKILL.md 'worktree add ../<repo>-<slug>' 'worktrees skill still creates sibling worktrees'
assert_not_contains skills/pr-orchestration/SKILL.md "\$TMPDIR/claude/" 'orchestration still routes worktrees through TMPDIR'
assert_not_contains skills/pr-iterate/SKILL.md '.worktrees/<repo>/<slug>' 'PR iteration still uses a repo-local worktree root'

# Every `git ... worktree add <dest>` example must put <dest> under the durable
# root. Lines are split at ; & | so a safe command cannot vouch for an unsafe
# one beside it. A quoted ~ is not expanded, so only a bare ~ counts.
add_cmd='git[[:space:]].*worktree[[:space:]]+add[[:space:]]+[^[:space:]]'
# shellcheck disable=SC2016 # literal $HOME in the pattern
durable='(\$HOME|\$\{HOME\}|(^|[[:space:]])~)/\.claude/worktrees/'
escape='(^|[[:space:]/"])\.\.(/|$)'
if lines="$(git -C "$repo_root" "${grep_args[@]}" -n -E "$add_cmd" -- "${pathspecs[@]}")"; then
  while IFS= read -r line; do
    while IFS= read -r segment; do
      if [[ $segment =~ $add_cmd ]] && { ! [[ $segment =~ $durable ]] || [[ $segment =~ $escape ]]; }; then
        fail "$line; expected the worktree destination to be under \$HOME/.claude/worktrees/"
      fi
    done <<< "${line//[;&|]/$'\n'}"
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
