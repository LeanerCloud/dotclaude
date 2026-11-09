#!/usr/bin/env bash
# Fixture suite for scripts/disk-guard.sh. Run it with /bin/bash (3.2) and the PATH bash.
#
#   /bin/bash scripts/tests/disk-guard.test.sh
#   TEST_BASH=bash bash scripts/tests/disk-guard.test.sh   # run the guard under another bash
#
# Each case uses a fake HOME in a mktemp dir under $TMPDIR (removed on exit) and a PATH of stubs
# (df, go, rm) plus system dirs. The rm stub only deletes inside the scratch tree and logs anything
# else as BLOCKED, so a regression cannot delete real files. SCRIPT may name a mutated copy.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="${SCRIPT:-$HERE/../disk-guard.sh}"
PREFIX="${TMPDIR:-/tmp}/disk-guard-test."
ROOT="$(mktemp -d "${PREFIX}XXXXXX")" || exit 1
ROOT="$(cd -P "$ROOT" && pwd -P)" || exit 1
trap '[ -d "$ROOT" ] && case $ROOT in *"/disk-guard-test."*) /bin/rm -rf -- "$ROOT" ;; esac' EXIT
mkdir -p "$ROOT/bin"

printf '#!/bin/sh\nprintf "Filesystem 1G-blocks Used Available Capacity\\n/dev/x 500 490 10 98%%%%\\n"\n' > "$ROOT/bin/df"
cat > "$ROOT/bin/go" <<'EOS'
#!/bin/sh
[ -n "${GO_RC:-}" ] && { echo "go: locked" >&2; exit "$GO_RC"; }
if [ -n "${GO_LOG:-}" ]; then
  r=held
  for l in heavy heavy-3 heavy-4 heavy-review heavy-review-2 heavy-review-3; do
    lockf -t 0 "$DISK_GUARD_TEST_LOCK_DIR/$l.lock" true 2>/dev/null && r="free:$l"
  done
  echo "$r" >> "$GO_LOG"
fi
exit 0
EOS
cat > "$ROOT/bin/rm" <<EOS
#!/bin/bash
# Deletes only paths whose canonical parent plus basename is inside the scratch tree.
for a in "\$@"; do
  case \$a in -*) continue ;; esac
  base=\$(basename "\$a")
  parent=\$(cd -P "\$(dirname "\$a")" 2>/dev/null && pwd -P) || parent=""
  case \$base in .|..) parent="" ;; esac
  case "\$parent/\$base" in
    "$ROOT"/*) /bin/rm -rf -- "\$parent/\$base" ;;
    *) echo "BLOCKED \$a" >> "$ROOT/rm.log" ;;
  esac
done
EOS
mkdir -p "$ROOT/gl"
cat > "$ROOT/gl/golangci-lint" <<EOS
#!/bin/sh
env > "$ROOT/gl.env"
echo "\$*" > "$ROOT/gl.args"
EOS
chmod +x "$ROOT/bin/"* "$ROOT/gl/golangci-lint"

PATH="$ROOT/bin:/usr/bin:/bin:/usr/sbin"
if command -v golangci-lint >/dev/null; then echo "golangci-lint is reachable on PATH; test setup is wrong" >&2; exit 1; fi
SYSBASH=${TEST_BASH:-$BASH}
printf 'guard runs under: %s (%s)\n' "$SYSBASH" "$("$SYSBASH" --version | head -1)"

PASSED=0 TOTAL_FAILS=0 FAILED_NAMES="" CASE_FAILS=0
bad() { printf '     %s\n' "$*"; CASE_FAILS=$((CASE_FAILS + 1)); }

setup() {
  H="$ROOT/home.$1"; rm -rf "$H"
  mkdir -p "$H/Library/Caches/golangci-lint" "$H/keep"
  echo x > "$H/Library/Caches/golangci-lint/entry"; echo x > "$H/keep/sentinel"
  : > "$ROOT/rm.log"
  BUS="$H/.claude/agent-comms/messages"
  LD="$ROOT/locks.$1"; mkdir -p "$LD"
}
# shellcheck disable=SC2016
run_guard() { HOME="$H" DISK_GUARD_TEST_LOCK_DIR="$LD" DISK_GUARD_TEST_LOCK_WAIT=1 "$SYSBASH" -c 'source "$1"; check_once' _ "$SCRIPT" >/dev/null 2>"$ROOT/err"; }
note() { cat "$BUS"/result-*-disk-guard.txt 2>/dev/null; }

case_env_home() {
  setup env_home
  GOLANGCI_LINT_CACHE="$H" run_guard
  [ -f "$H/keep/sentinel" ] || bad "GOLANGCI_LINT_CACHE=\$HOME deleted HOME contents"
  [ ! -d "$H/Library/Caches/golangci-lint" ] || bad "default cache dir was not removed"
}
case_env_root() {
  setup env_root
  GOLANGCI_LINT_CACHE=/ run_guard
  [ -f "$H/keep/sentinel" ] || bad "GOLANGCI_LINT_CACHE=/ deleted HOME contents"
  ! grep -q BLOCKED "$ROOT/rm.log" || bad "rm was asked to delete outside the scratch tree: $(cat "$ROOT/rm.log")"
}
case_default_removed() {
  setup default
  run_guard
  [ ! -e "$H/Library/Caches/golangci-lint" ] || bad "default cache dir still present"
  [ -f "$H/keep/sentinel" ] || bad "unrelated file removed"
  note | grep -q 'cleared: golangci-lint cache, Go build cache' || bad "note does not list both cleans: $(note)"
}
case_symlink_refused() {
  setup symlink
  rm -rf "$H/Library/Caches/golangci-lint"; mkdir "$H/target"; echo x > "$H/target/precious"
  ln -s "$H/target" "$H/Library/Caches/golangci-lint"
  run_guard
  [ -f "$H/target/precious" ] || bad "symlink target was deleted"
  ! grep -q BLOCKED "$ROOT/rm.log" || bad "rm called outside the scratch tree"
}
case_intermediate_symlink_refused() {
  setup parent
  rm -rf "$H/Library"; mkdir -p "$ROOT/outside.lib/Caches/golangci-lint"
  echo x > "$ROOT/outside.lib/Caches/golangci-lint/precious"
  ln -s "$ROOT/outside.lib" "$H/Library"
  run_guard
  [ -f "$ROOT/outside.lib/Caches/golangci-lint/precious" ] || bad "cache behind a symlinked Library was deleted"
  grep -q 'resolves elsewhere, refusing' "$ROOT/err" || bad "no refusal on stderr: $(cat "$ROOT/err")"
  note | grep -q 'refused' || bad "note does not mention the refusal: $(note)"
}
case_binary_ignores_env_cache() {
  setup binary
  PATH="$ROOT/gl:$PATH" GOLANGCI_LINT_CACHE="$H" run_guard
  grep -qx 'cache clean' "$ROOT/gl.args" 2>/dev/null || bad "golangci-lint cache clean was not run: $(cat "$ROOT/gl.args" 2>/dev/null)"
  ! grep -q '^GOLANGCI_LINT_CACHE=' "$ROOT/gl.env" || bad "GOLANGCI_LINT_CACHE leaked into golangci-lint: $(grep '^GOLANGCI_LINT_CACHE=' "$ROOT/gl.env")"
}
case_failure_reported() {
  setup failure
  GO_RC=3 run_guard
  grep -q 'FAILED rc=3: go clean -cache: go: locked' "$ROOT/err" || bad "failure not logged to stderr: $(cat "$ROOT/err")"
  note | grep -q 'FAILED: Go build cache (rc=3: go: locked)' || bad "note does not list the failure: $(note)"
  note | grep -q 'cleared: golangci-lint cache;' || bad "note should name only the successful clean: $(note)"
  GO_LOG="$ROOT/go.failure" run_guard
  [ ! -e "$ROOT/go.failure" ] || bad "a failed clean was retried in the same crossing"
  [ "$(find "$BUS" -name "result-*-disk-guard.txt" | wc -l)" -eq 1 ] || bad "posted a second note in the same crossing"
}

case_lock_held() {
  setup lock
  L="$H/.claude/fleet-run/state/disk-guard.lock"
  bash -c 'sleep 30; :' disk-guard-holder & holder=$!
  mkdir -p "$L"; echo "$holder" > "$L/pid"
  run_guard
  pkill -P "$holder" 2>/dev/null; kill "$holder" 2>/dev/null; wait "$holder" 2>/dev/null
  [ -z "$(note)" ] || bad "acted while a live instance held the lock"
  grep -q "another instance (pid $holder) holds the lock, skipping" "$ROOT/err" || bad "no skip message: $(cat "$ROOT/err")"
  [ -f "$L/pid" ] || bad "live holder's lock was touched"
  rm -f "$L/pid"; rmdir "$L"
  run_guard
  [ -n "$(note)" ] || bad "did not act once the lock was free"
  [ ! -e "$L" ] || bad "lock dir left behind"
}
case_lock_stale() {
  setup stale
  L="$H/.claude/fleet-run/state/disk-guard.lock"
  sleep 0 & dead=$!; wait "$dead"
  mkdir -p "$L"; echo "$dead" > "$L/pid"
  run_guard
  grep -q "removing stale lock from pid $dead" "$ROOT/err" || bad "no stale message: $(cat "$ROOT/err")"
  [ -n "$(note)" ] || bad "did not clean after taking over a stale lock"
  [ ! -e "$L" ] || bad "lock dir left behind"
}

case_lock_pid_reused() {
  setup reused
  L="$H/.claude/fleet-run/state/disk-guard.lock"
  sleep 30 & other=$!
  mkdir -p "$L"; echo "$other" > "$L/pid"
  run_guard
  kill "$other" 2>/dev/null; wait "$other" 2>/dev/null
  grep -q "removing stale lock from pid $other" "$ROOT/err" || bad "unrelated live pid was treated as the holder: $(cat "$ROOT/err")"
  [ -n "$(note)" ] || bad "did not clean despite the reused pid"
}

case_cleans_hold_heavy_locks() {
  setup locked_clean
  GO_LOG="$ROOT/go.locked_clean" run_guard
  [ "$(cat "$ROOT/go.locked_clean" 2>/dev/null)" = held ] || bad "go clean ran without holding the heavy locks: $(cat "$ROOT/go.locked_clean" 2>/dev/null)"
}
case_busy_lock_retries() {
  setup busy
  lockf -k "$LD/heavy-review.lock" sleep 30 & holder=$!
  until ! lockf -k -t 0 "$LD/heavy-review.lock" true 2>/dev/null; do sleep 0.1; done
  GO_LOG="$ROOT/go.busy" run_guard
  [ ! -e "$ROOT/go.busy" ] || bad "go clean ran while a heavy lock was held"
  [ -d "$H/Library/Caches/golangci-lint" ] || bad "golangci-lint cache removed while a heavy lock was held"
  [ -z "$(note)" ] || bad "posted a note although nothing ran: $(note)"
  grep -q 'a heavy lock stayed busy, skipped: Go build cache' "$ROOT/err" || bad "no busy message: $(cat "$ROOT/err")"
  [ "$(cat "$H/.claude/fleet-run/state/disk-guard.low" 2>/dev/null)" = "golangci go" ] || bad "marker does not owe both cleans: $(cat "$H/.claude/fleet-run/state/disk-guard.low" 2>/dev/null)"
  [ ! -e "$H/.claude/fleet-run/state/disk-guard.lock" ] || bad "disk-guard lock dir left behind"
  kill "$holder" 2>/dev/null; wait "$holder" 2>/dev/null
  GO_LOG="$ROOT/go.busy" run_guard
  [ "$(cat "$ROOT/go.busy" 2>/dev/null)" = held ] || bad "did not clean once the lock was free"
  note | grep -q 'cleared: golangci-lint cache, Go build cache' || bad "retry note does not list both cleans: $(note)"
}

case_owed_clean_runs_alone() {
  setup owed
  M="$H/.claude/fleet-run/state/disk-guard.low"; mkdir -p "$(dirname "$M")"; printf go > "$M"
  GO_LOG="$ROOT/go.owed" run_guard
  [ "$(cat "$ROOT/go.owed" 2>/dev/null)" = held ] || bad "the owed Go clean did not run under the locks"
  [ -d "$H/Library/Caches/golangci-lint" ] || bad "a clean already done this crossing ran again"
  [ ! -s "$M" ] || bad "marker still owes: $(cat "$M")"
  note | grep -q 'cleared: Go build cache;' || bad "note does not list only the owed clean: $(note)"
  rm -f "$BUS"/result-*; GO_LOG="$ROOT/go.owed" run_guard
  [ "$(wc -l < "$ROOT/go.owed")" -eq 1 ] || bad "a third round re-ran a finished clean"
  [ -z "$(note)" ] || bad "posted again after the crossing was done: $(note)"
}

for t in case_env_home case_env_root case_default_removed case_symlink_refused case_intermediate_symlink_refused case_binary_ignores_env_cache case_failure_reported case_lock_held case_lock_stale case_lock_pid_reused case_cleans_hold_heavy_locks case_busy_lock_retries case_owed_clean_runs_alone; do
  CASE_FAILS=0
  "$t"
  if [ "$CASE_FAILS" -eq 0 ]; then
    PASSED=$((PASSED + 1)); printf 'ok   %s\n' "$t"
  else
    TOTAL_FAILS=$((TOTAL_FAILS + 1)); FAILED_NAMES="$FAILED_NAMES $t"; printf 'FAIL %s\n' "$t"
  fi
done

printf '%d passed, %d failed%s\n' "$PASSED" "$TOTAL_FAILS" "${FAILED_NAMES:+ ($FAILED_NAMES )}"
[ "$TOTAL_FAILS" -eq 0 ]
