#!/usr/bin/env bash
# macOS only (df -g, /System/Volumes/Data). Every 5 min, if free space on the data volume is under
# 30 GB, clear the golangci-lint and Go build caches and post one agent-comms bus note per crossing.
# Deletes only those caches; each clean that fails is logged and named in the note.
set -euo pipefail
THRESHOLD_GB=30
BUS=$HOME/.claude/agent-comms/messages
# Per-machine state dir (already git-ignored); the marker exists while free space is below the threshold.
LOW_MARKER=$HOME/.claude/fleet-run/state/disk-guard.low

ts() { date -u +%Y%m%dT%H%M%SZ; }

free_gb() {
  local out n
  out=$(df -g /System/Volumes/Data) || { echo "$(ts) disk-guard: df failed" >&2; return 1; }
  n=$(awk 'NR==2 {print $4}' <<<"$out")
  [[ $n =~ ^[0-9]+$ ]] || { echo "$(ts) disk-guard: df printed no free-space number" >&2; return 1; }
  echo "$n"
}

cleaned="" failed=""
# run_clean LABEL CMD...: run CMD; record LABEL as cleaned, or log and record the failure; never abort.
run_clean() {
  local label=$1 out rc last
  shift
  if out=$("$@" 2>&1); then
    cleaned+="${cleaned:+, }$label"
  else
    rc=$?
    last=$(printf '%s\n' "$out" | tail -n 1)
    echo "$(ts) disk-guard: FAILED rc=$rc: $*: $last" >&2
    failed+="${failed:+; }$label (rc=$rc: $last)"
  fi
}

clean_golangci_cache() {
  local dir=${GOLANGCI_LINT_CACHE:-$HOME/Library/Caches/golangci-lint}
  if command -v golangci-lint >/dev/null; then
    run_clean "golangci-lint cache" golangci-lint cache clean
  elif [[ -n $dir && -d $dir && ! -L $dir ]]; then
    run_clean "golangci-lint cache" rm -rf -- "$dir"
  fi
}

check_once() {
  local free after t summary
  free=$(free_gb) || exit 1
  if (( free >= THRESHOLD_GB )); then
    rm -f "$LOW_MARKER"
    return 0
  fi
  [[ -e $LOW_MARKER ]] && return 0
  mkdir -p "$BUS" "$(dirname "$LOW_MARKER")" || { echo "$(ts) disk-guard: cannot create state or bus dir" >&2; exit 1; }
  : >"$LOW_MARKER"
  t=$(ts)
  cleaned="" failed=""
  clean_golangci_cache
  run_clean "Go build cache" go clean -cache
  after=$(free_gb) || after="unknown"
  if [[ -z $cleaned ]]; then
    summary="every clean failed, nothing was cleared: $failed"
  else
    summary="cleared: $cleaned${failed:+; FAILED: $failed}"
  fi
  echo "result $t disk-guard: free space was ${free} GB (< ${THRESHOLD_GB} GB); $summary; free now ${after} GB. Re-run any build or test that failed with IO, cache or space errors." > "$BUS/result-$t-disk-guard.txt"
  echo "$t $summary: ${free} GB -> ${after} GB"
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
  while true; do
    check_once
    sleep 300
  done
fi
