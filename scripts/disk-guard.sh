#!/usr/bin/env bash
# macOS only (df -g, /System/Volumes/Data). Every 5 min, if free space on the data volume is under
# 30 GB, clear the golangci-lint and Go build caches and post one agent-comms bus note per crossing.
# Deletes only those caches; each clean that fails is logged and named in the note, and is not retried
# until space recovers above the threshold (once per crossing).
set -euo pipefail
THRESHOLD_GB=30
BUS=$HOME/.claude/agent-comms/messages
# Per-machine state dir (already git-ignored); the marker exists while free space is below the threshold.
LOW_MARKER=$HOME/.claude/fleet-run/state/disk-guard.low
LOCK=$HOME/.claude/fleet-run/state/disk-guard.lock # atomic mkdir lock: concurrent instances act once

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

# Only the fixed default cache dir is ever removed; GOLANGCI_LINT_CACHE must never steer a delete.
clean_golangci_cache() {
  if command -v golangci-lint >/dev/null; then
    run_clean "golangci-lint cache" env -u GOLANGCI_LINT_CACHE golangci-lint cache clean
    return 0
  fi
  local dir=$HOME/Library/Caches/golangci-lint home_real
  if [[ $HOME != /?* ]]; then
    echo "$(ts) disk-guard: HOME is not an absolute path, skipping golangci-lint cache" >&2
    return 0
  fi
  [[ -d $dir && ! -L $dir ]] || return 0
  home_real=$(cd -P "$HOME" && pwd -P)
  if [[ $(cd -P "$dir" && pwd -P) == "$home_real/Library/Caches/golangci-lint" ]]; then
    run_clean "golangci-lint cache" rm -rf -- "$dir"
  else
    echo "$(ts) disk-guard: $dir resolves elsewhere, refusing to remove it" >&2
    failed+="${failed:+; }golangci-lint cache (refused: path resolves elsewhere)"
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
  mkdir "$LOCK" 2>/dev/null || return 0
  trap 'rmdir "$LOCK"' EXIT
  if [[ -e $LOW_MARKER ]]; then rmdir "$LOCK"; trap - EXIT; return 0; fi
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
  rmdir "$LOCK"
  trap - EXIT
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
  while true; do
    check_once
    sleep 300
  done
fi
