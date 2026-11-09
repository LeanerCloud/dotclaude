#!/usr/bin/env bash
# macOS only (df -g, /System/Volumes/Data). Every 5 min, if free space on the data volume is under
# 30 GB, clear the golangci-lint and Go build caches and post an agent-comms bus note. Deletes only those
# caches. Each clean runs once per crossing: one that ran or failed (logged, named in the note) waits until
# space recovers above the threshold. Each clean holds every heavy lock, so no running build or lint loses
# its cache; a clean skipped because a lock stayed busy is retried next round, alone.
set -euo pipefail
THRESHOLD_GB=30
BUS=$HOME/.claude/agent-comms/messages
# Per-machine state dir (already git-ignored). The marker exists while free space is below the threshold
# and lists the cleans still owed this crossing (skipped for a busy lock); empty means none.
LOW_MARKER=$HOME/.claude/fleet-run/state/disk-guard.low
LOCK=$HOME/.claude/fleet-run/state/disk-guard.lock # atomic mkdir lock: concurrent instances act once
# The lane locks every heavy build, test and lint runs under (fleet-run/scratch/safe-go-clean.sh holds the same set).
# The DISK_GUARD_TEST_* overrides exist for scripts/tests/disk-guard.test.sh only.
HEAVY_LOCK_DIR=${DISK_GUARD_TEST_LOCK_DIR:-/tmp/claude}
HEAVY_LOCKS=(heavy heavy-3 heavy-4 heavy-review heavy-review-2 heavy-review-3)
HEAVY_LOCK_WAIT=${DISK_GUARD_TEST_LOCK_WAIT:-30}
LOCK_BUSY_RC=75 # lockf's EX_TEMPFAIL when a lock stays busy past its wait

ts() { date -u +%Y%m%dT%H%M%SZ; }

free_gb() {
  local out n
  out=$(df -g /System/Volumes/Data) || { echo "$(ts) disk-guard: df failed" >&2; return 1; }
  n=$(awk 'NR==2 {print $4}' <<<"$out")
  [[ $n =~ ^[0-9]+$ ]] || { echo "$(ts) disk-guard: df printed no free-space number" >&2; return 1; }
  echo "$n"
}

# with_heavy_locks CMD...: run CMD holding every heavy lock, taken in one fixed order.
with_heavy_locks() {
  local cmd=("$@") i
  for ((i = ${#HEAVY_LOCKS[@]} - 1; i >= 0; i--)); do
    cmd=(lockf -k -t "$HEAVY_LOCK_WAIT" "$HEAVY_LOCK_DIR/${HEAVY_LOCKS[i]}.lock" "${cmd[@]}")
  done
  "${cmd[@]}"
}

cleaned="" failed="" busy="" owed=""
# run_clean ID LABEL CMD...: run CMD under every heavy lock; record LABEL as cleaned, busy (a lock stayed
# held: ID is owed next round), or failed (logged); never abort.
run_clean() {
  local id=$1 label=$2 out rc last
  shift 2
  if out=$(with_heavy_locks "$@" 2>&1); then
    cleaned+="${cleaned:+, }$label"
  else
    rc=$?
    if ((rc == LOCK_BUSY_RC)); then
      echo "$(ts) disk-guard: a heavy lock stayed busy, skipped: $label" >&2
      busy+="${busy:+, }$label"
      owed+="${owed:+ }$id"
      return 0
    fi
    last=$(printf '%s\n' "$out" | tail -n 1)
    echo "$(ts) disk-guard: FAILED rc=$rc: $*: $last" >&2
    failed+="${failed:+; }$label (rc=$rc: $last)"
  fi
}

# Only the fixed default cache dir is ever removed; GOLANGCI_LINT_CACHE must never steer a delete.
clean_golangci_cache() {
  if command -v golangci-lint >/dev/null; then
    run_clean golangci "golangci-lint cache" env -u GOLANGCI_LINT_CACHE golangci-lint cache clean
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
    # Checked again under the locks, which can take minutes to acquire.
    # shellcheck disable=SC2016
    run_clean golangci "golangci-lint cache" /bin/sh -c '[ "$(cd -P "$1" 2>/dev/null && pwd -P)" = "$2" ] || { echo "$1 now resolves elsewhere, refusing" >&2; exit 1; }; rm -rf -- "$1"' _ "$dir" "$home_real/Library/Caches/golangci-lint"
  else
    echo "$(ts) disk-guard: $dir resolves elsewhere, refusing to remove it" >&2
    failed+="${failed:+; }golangci-lint cache (refused: path resolves elsewhere)"
  fi
}

release_lock() { rm -f "$LOCK/pid"; rmdir "$LOCK"; }

# A live pid only counts as the holder if its command line mentions disk-guard (pids get reused).
# 0 when this process now owns the lock; 1 (after logging why) when it must skip this round.
take_lock() {
  local pid
  if ! mkdir "$LOCK" 2>/dev/null; then
    pid=$(cat "$LOCK/pid" 2>/dev/null || true)
    if [[ $pid =~ ^[0-9]+$ ]] && kill -0 "$pid" 2>/dev/null && ps -p "$pid" -o command= 2>/dev/null | grep -q disk-guard; then
      echo "$(ts) disk-guard: another instance (pid $pid) holds the lock, skipping" >&2
      return 1
    fi
    echo "$(ts) disk-guard: removing stale lock from pid ${pid:-unknown}" >&2
    rm -f "$LOCK/pid"
    rmdir "$LOCK" 2>/dev/null || true
    mkdir "$LOCK" 2>/dev/null || { echo "$(ts) disk-guard: cannot take over the lock, skipping" >&2; return 1; }
  fi
  echo $$ >"$LOCK/pid"
}

check_once() {
  local free after t summary todo
  free=$(free_gb) || exit 1
  if (( free >= THRESHOLD_GB )); then
    rm -f "$LOW_MARKER"
    return 0
  fi
  mkdir -p "$BUS" "$(dirname "$LOW_MARKER")" "$HEAVY_LOCK_DIR" || { echo "$(ts) disk-guard: cannot create state, bus or lock dir" >&2; exit 1; }
  take_lock || return 0
  trap release_lock EXIT
  todo="golangci go"
  [[ -e $LOW_MARKER ]] && todo=$(cat "$LOW_MARKER")
  t=$(ts)
  cleaned="" failed="" busy="" owed=""
  [[ " $todo " == *" golangci "* ]] && clean_golangci_cache
  [[ " $todo " == *" go "* ]] && run_clean go "Go build cache" go clean -cache
  printf '%s' "$owed" >"$LOW_MARKER"
  if [[ -z $cleaned && -z $failed ]]; then # only skipped: nothing to report yet
    release_lock
    trap - EXIT
    return 0
  fi
  after=$(free_gb) || after="unknown"
  if [[ -z $cleaned ]]; then
    summary="every clean failed, nothing was cleared: $failed"
  else
    summary="cleared: $cleaned${failed:+; FAILED: $failed}"
  fi
  summary+="${busy:+; skipped while a heavy lock was busy, retrying in 5 min: $busy}"
  echo "result $t disk-guard: free space was ${free} GB (< ${THRESHOLD_GB} GB); $summary; free now ${after} GB. Re-run any build or test that failed with IO, cache or space errors." > "$BUS/result-$t-disk-guard.txt"
  echo "$t $summary: ${free} GB -> ${after} GB"
  release_lock
  trap - EXIT
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
  while true; do
    check_once
    sleep 300
  done
fi
