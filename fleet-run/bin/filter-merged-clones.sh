#!/usr/bin/env bash
# READ-ONLY second pass over list-pushed-clones.sh output: keep only clones where EVERY local branch tip
# and HEAD is merged: an ancestor of the remote default branch, or the head of a MERGED PR.
# Usage: filter-merged-clones.sh <pushed-clones.tsv>  ->  writes <same>.merged.tsv; deletes nothing.
set -euo pipefail

IN=$1
OUT=${IN%.tsv}.merged.tsv
: >"$OUT"

merged_tip() {
  local dir=$1 slug=$2 default=$3 sha=$4
  git -C "$dir" merge-base --is-ancestor "$sha" "origin/$default" 2>/dev/null && return 0
  [[ $(gh api "repos/$slug/commits/$sha/pulls" -q '[.[] | select(.merged_at != null and .head.sha == "'"$sha"'")] | length' 2>/dev/null) =~ ^[1-9] ]]
}

kept=0 dropped=0 kb_total=0
while IFS=$'\t' read -r dir kb origin; do
  slug=$(sed -E 's#^(https://github.com/|git@github.com:)##; s#\.git$##' <<<"$origin")
  default=$(git -C "$dir" symbolic-ref --short refs/remotes/origin/HEAD 2>/dev/null | sed 's#^origin/##') || default=""
  [[ -n $default ]] || default=$(gh repo view "$slug" --json defaultBranchRef -q .defaultBranchRef.name 2>/dev/null) || default=""
  ok=1
  if [[ -z $default ]] || ! tips=$( { git -C "$dir" for-each-ref --format='%(objectname)' refs/heads; git -C "$dir" rev-parse HEAD; } | sort -u); then
    ok=0
  else
    while read -r sha; do
      merged_tip "$dir" "$slug" "$default" "$sha" || { ok=0; break; }
    done <<<"$tips"
  fi
  if (( ok )); then
    printf '%s\t%s\t%s\n' "$dir" "$kb" "$origin" >>"$OUT"
    kept=$((kept + 1)) kb_total=$((kb_total + kb))
  else
    printf 'unmerged-branch\t%s\n' "${dir/#$HOME/~}"
    dropped=$((dropped + 1))
  fi
done <"$IN"

printf 'summary\tmerged=%d dropped=%d size=%dMB list=%s\n' "$kept" "$dropped" $((kb_total / 1024)) "${OUT/#$HOME/~}"
