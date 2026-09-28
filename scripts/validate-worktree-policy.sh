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
assert_contains skills/worktrees/SKILL.md "\$HOME/.claude/worktrees/" 'worktree creation must use the durable root'
assert_contains skills/pr-orchestration/SKILL.md "\$HOME/.claude/worktrees/" 'orchestration must use the durable root'
assert_contains skills/pr-iterate/SKILL.md "\$HOME/.claude/worktrees/" 'PR iteration must use the durable root'
assert_not_contains skills/worktrees/SKILL.md 'worktree add ../<repo>-<slug>' 'worktrees skill still creates sibling worktrees'
assert_not_contains skills/pr-orchestration/SKILL.md "\$TMPDIR/claude/" 'orchestration still routes worktrees through TMPDIR'
assert_not_contains skills/pr-iterate/SKILL.md '.worktrees/<repo>/<slug>' 'PR iteration still uses a repo-local worktree root'

if matches="$(git -C "$repo_root" "${grep_args[@]}" -n -F 'worktree add' -- "${pathspecs[@]}")"; then
  while IFS= read -r match; do
    case "$match" in
      *'.claude/worktrees/'*) ;;
      *) fail "$match must use the durable worktree root" ;;
    esac
  done <<< "$matches"
else
  status=$?
  if [ "$status" -ne 1 ]; then
    fail "git grep failed while scanning worktree creation commands (exit $status)"
  fi
fi

if [ "$failures" -gt 0 ]; then
  printf '%d worktree policy error(s)\n' "$failures" >&2
  exit 1
fi

printf 'ok: persistent worktrees use %s\n' "\$HOME/.claude/worktrees/"
