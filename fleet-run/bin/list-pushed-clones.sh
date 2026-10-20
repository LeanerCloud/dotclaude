#!/usr/bin/env bash
# READ-ONLY: list standalone git clones under ~/.claude/worktrees whose every local commit is on the remote,
# with no dirty/untracked files, stashes, linked worktrees or live processes. Deletes nothing.
# Output: state/pushed-clones-<date>.tsv (eligible) and stdout skip reasons.
set -euo pipefail
TILDE='~'

ROOT="$HOME/.claude/worktrees"
OUT="$HOME/.claude/fleet-run/state/pushed-clones-$(date +%Y%m%d).tsv"
command -v lsof >/dev/null || { echo "list-pushed-clones: lsof not found on PATH" >&2; exit 1; }
live_cwds=$(lsof -nP -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' | sort -u)

has_live_process() {
  grep -qxF "$1" <<<"$live_cwds" || grep -qF "$1/" <<<"$live_cwds"
}

# 0 when every commit reachable from local branches and HEAD is on the remote: either in a
# remote-tracking ref, or (for tips whose remote branch is gone, e.g. squash-merged PRs) known to GitHub.
fully_pushed() {
  local dir=$1 slug sha unpushed
  unpushed=$(git -C "$dir" rev-list --branches HEAD --not --remotes 2>/dev/null) || return 1
  [[ -z $unpushed ]] && return 0
  slug=$(git -C "$dir" remote get-url origin 2>/dev/null | sed -E 's#^(https://github.com/|git@github.com:)##; s#\.git$##')
  [[ $slug =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || return 1
  while read -r sha; do
    gh api "repos/$slug/commits/$sha" --silent 2>/dev/null || return 1
  done <<<"$unpushed"
  return 0
}

mkdir -p "${OUT%/*}" || { echo "list-pushed-clones: cannot create ${OUT%/*}" >&2; exit 1; }
: >"$OUT"
eligible=0 total_kb=0
while IFS= read -r gitdir; do
  dir=${gitdir%/.git}
  reason=""
  if ! status=$(git -C "$dir" status --porcelain --untracked-files=all 2>/dev/null) \
     || ! stashes=$(git -C "$dir" stash list 2>/dev/null); then reason="git-error"
  elif [[ -n $status ]]; then reason="dirty"
  elif [[ -n $stashes ]]; then reason="stash"
  elif (( $(git -C "$dir" worktree list --porcelain 2>/dev/null | grep -c '^worktree ') > 1 )); then reason="has-linked-worktrees"
  elif has_live_process "$dir"; then reason="live-process"
  elif ! fully_pushed "$dir"; then reason="unpushed"
  fi
  if [[ -n $reason ]]; then
    printf 'skip\t%s\t%s\n' "$reason" "${dir/#"$HOME"/$TILDE}"
    continue
  fi
  kb=$(du -sk "$dir" | cut -f1)
  origin=$(git -C "$dir" remote get-url origin 2>/dev/null || echo none)
  printf '%s\t%s\t%s\n' "$dir" "$kb" "$origin" >>"$OUT"
  eligible=$((eligible + 1)) total_kb=$((total_kb + kb))
done < <(find "$ROOT" -mindepth 2 -maxdepth 3 -type d -name .git 2>/dev/null)

printf 'summary\teligible=%d size=%dMB list=%s\n' "$eligible" $((total_kb / 1024)) "${OUT/#"$HOME"/$TILDE}"
