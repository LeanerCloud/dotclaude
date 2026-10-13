#!/usr/bin/env bash
# Tests for scripts/ask-user.sh. Openers (open, xdg-open, wslview, cmd, cmd.exe, wslpath) are stubs
# on a private PATH that log their argv; the "user" is a background subshell that rewrites the doc.
# Nothing here opens a real app or exercises a real Linux/Windows desktop.
# Set ASK_USER_SCRIPT to test a mutated copy of the script.
# shellcheck disable=SC2016
set -uo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
script="${ASK_USER_SCRIPT:-$here/../scripts/ask-user.sh}"
bash_bin=$(command -v bash)
root=$(mktemp -d)
trap 'rm -rf "$root"' EXIT
fail=0 total=0

ok() { total=$((total + 1)); if [ "$1" = 0 ]; then :; else fail=$((fail + 1)); echo "FAIL: $2"; fi; }
has() { [[ "$1" == *"$2"* ]]; }
check() { local desc="$1"; shift; "$@"; ok $? "$desc"; }

seq=0
# mkpath STUB...: private PATH with the POSIX tools the script needs plus the named opener stubs.
mkpath() {
  stubs="$root/bin.$((++seq))"; log="$stubs.log"; mkdir -p "$stubs"; : >"$log"
  local t; for t in awk sed stat date sleep cat uname dirname basename; do ln -s "$(command -v "$t")" "$stubs/$t"; done
  local name
  for name in "$@"; do
    printf '#!%s\necho "%s $*" >>"%s"\nif [ "%s" = wslpath ]; then echo "C:\\\\win\\\\doc.md"; fi\nexit 0\n' \
      "$bash_bin" "$name" "$log" "$name" >"$stubs/$name"
    chmod +x "$stubs/$name"
  done
}
calls() { grep -c "^$1 " "$log" || true; }
# Openers run in the background, so their log lines land asynchronously: poll (up to ~2 s) for the
# expected evidence. Assertions that something did NOT run read the log directly.
waitgrep() { local _; for _ in $(seq 40); do grep -q "$1" "$log" && return 0; sleep 0.05; done; return 1; }
waitcalls() { # waitcalls NAME N [ge]: exactly N calls (or at least N with "ge")
  local _; for _ in $(seq 40); do [ "$(calls "$1")" -ge "$2" ] && break; sleep 0.05; done
  if [ "${3-}" = ge ]; then [ "$(calls "$1")" -ge "$2" ]; else [ "$(calls "$1")" = "$2" ]; fi
}

# User-answer fixtures
two='# Decision needed: t
Asked by x, today. If unanswered by later: each question'"'"'s Default (safe, non-destructive).

## 1. First?
Context: c
Options:
- **A** (recommended): a. Tradeoffs: t
Default: A
Decision:
Notes:

## 2. Second?
Context: c
Default: B
Decision:
Notes:
'
newdoc() { doc="$root/doc.$((++seq)).md"; printf '%s\n' "${1-$two}" >"$doc"; }
# later SECS FILE-CONTENT: replace the doc after SECS (the simulated user).
later() { ( sleep "$1"; printf '%s\n' "$2" >"$doc.tmp"; mv "$doc.tmp" "$doc" ) & }
fill() { # fill DOC-TEXT N VALUE: set the Nth empty "Decision:" line
  awk -v n="$2" -v v="$3" '/^Decision:[ \t]*$/ { if (++c == n) { print "Decision: " v; next } } { print }' <<<"$1"
}
run() { # run ENV... -- ARGS...; sets out err rc elapsed
  local envs=()
  while [ "$1" != -- ]; do envs+=("$1"); shift; done; shift
  local t0=$SECONDS
  out=$(env -i PATH="$stubs" HOME="$root" ASK_USER_OS=Darwin ASK_USER_POLL=1 ASK_USER_SETTLE=2 \
    ${envs[@]+"${envs[@]}"} "$bash_bin" "$script" "$@" 2>"$root/err"); rc=$?
  err=$(<"$root/err"); elapsed=$((SECONDS - t0))
}

# --- answered ---------------------------------------------------------------------------------
mkpath open; newdoc
a=$(fill "$two" 1 "Option A"); a=$(fill "$a" 1 "Option B")   # second call fills the next empty (now 1st)
a=${a//Notes:$'\n\n## 2'/Notes: keep it small$'\n\n## 2'}
later 1 "$a"
run -- -t 30 "$doc"
check "answered: exit 0" [ "$rc" = 0 ]
check "answered: section 1" has "$out" "1. First?: Option A"
check "answered: section 2" has "$out" "2. Second?: Option B"
check "answered: notes printed" has "$out" "Notes: keep it small"
check "answered: opened once" waitcalls open 1
check "answered: waited for the settle window" [ "$elapsed" -ge 2 ]

# --- partial answers keep waiting, then time out with defaults ---------------------------------
mkpath open; newdoc
later 1 "$(fill "$two" 1 "Option A")"
run -- -t 5 "$doc"
check "partial: exit 2" [ "$rc" = 2 ]
check "partial: answered section kept" has "$out" "1. First?: Option A"
check "partial: default for unanswered" has "$out" "2. Second?: B (default, no answer)"

# --- timeout with nothing answered -------------------------------------------------------------
mkpath open; newdoc
run -- -t 2 "$doc"
check "timeout: exit 2" [ "$rc" = 2 ]
check "timeout: default 1" has "$out" "1. First?: A (default, no answer)"
check "timeout: default 2" has "$out" "2. Second?: B (default, no answer)"
check "timeout: took about -t" [ "$elapsed" -ge 2 ]

# --- settle window: a freshly touched, fully answered doc is not read yet ---------------------
mkpath open; newdoc "$(fill "$(fill "$two" 1 X)" 1 Y)"
run ASK_USER_SETTLE=4 -- -t 30 "$doc"
check "settle: exit 0" [ "$rc" = 0 ]
check "settle: waited for quiet period" [ "$elapsed" -ge 3 ]

# --- Q: without A: exits 4 ---------------------------------------------------------------------
mkpath open; newdoc
later 1 "$two
Q: Does A touch prod?"
run -- -t 30 "$doc"
check "Q without A: exit 4" [ "$rc" = 4 ]
check "Q without A: question printed" has "$out" "Q: Does A touch prod?"

# --- Q: with A: does not block ----------------------------------------------------------------
mkpath open
newdoc "$(fill "$(fill "$two" 1 X)" 1 Y)
Q: Does A touch prod?
A: No, staging only."
run -- -t 30 "$doc"
check "Q with A: exit 0" [ "$rc" = 0 ]
check "Q with A: answers printed" has "$out" "1. First?: X"

# a later Q after an A is still open
mkpath open
newdoc "$(fill "$(fill "$two" 1 X)" 1 Y)
Q: one?
A: yes
Q: two?"
run -- -t 30 "$doc"
check "second Q open: exit 4" [ "$rc" = 4 ]
check "second Q open: only the open one" bash -c '[[ "$1" == "Q: two?" ]]' _ "$out"

# --- invalid docs and usage --------------------------------------------------------------------
mkpath open
newdoc "# Decision needed: t

## 1. Q?
Default: A"
run -- -t 5 "$doc"
check "no Decision line: exit 64" [ "$rc" = 64 ]
newdoc "## 1. Q?
Decision:"
run -- -t 5 "$doc"
check "no Default line: exit 64" [ "$rc" = 64 ]
newdoc "## 1. Q?
Default:
Decision:"
run -- -t 5 "$doc"
check "empty Default: exit 64" [ "$rc" = 64 ]
newdoc "## 1. A?
Default: A
Decision:

## 2. B?
Decision:"
run -- -t 5 "$doc"
check "second section lacks Default: exit 64" [ "$rc" = 64 ]
check "invalid doc never opened" [ "$(calls open)" = 0 ]
run --
check "no args: exit 64" [ "$rc" = 64 ]
run -- "$root/missing.md"
check "missing file: exit 64" [ "$rc" = 64 ]
newdoc
run -- -t abc "$doc"
check "non-numeric -t: exit 64" [ "$rc" = 64 ]
run -- -t 0 "$doc"
check "-t 0: exit 64" [ "$rc" = 64 ]
run -- -t 86401 "$doc"
check "-t over max: exit 64" [ "$rc" = 64 ]
run -- --reopen 0 "$doc"
check "--reopen 0: exit 64" [ "$rc" = 64 ]
run -- --bogus "$doc"
check "unknown flag: exit 64" [ "$rc" = 64 ]

# --- file vanishes -----------------------------------------------------------------------------
mkpath open; newdoc
( sleep 1; rm -f "$doc" ) &
run -- -t 30 "$doc"
check "vanished: exit 1" [ "$rc" = 1 ]

# --- reopen ------------------------------------------------------------------------------------
mkpath open; newdoc
run -- -t 8 --reopen 2 "$doc"
check "reopen: exit 2" [ "$rc" = 2 ]
check "reopen: opened initially and again" waitcalls open 3 ge
mkpath open; newdoc
run -- -t 3 "$doc"
check "default reopen not reached: opened once" waitcalls open 1

# --- doc content is never evaluated ------------------------------------------------------------
mkpath open; newdoc
pwn="$root/pwned"
evil='$(touch '"$pwn"') `touch '"$pwn"'`'
e=$(fill "$two" 1 "$evil"); e=$(fill "$e" 1 ok)
later 1 "$e"
run -- -t 30 "$doc"
check "no eval: exit 0" [ "$rc" = 0 ]
check "no eval: nothing executed" [ ! -e "$pwn" ]
check "no eval: literal echoed" has "$out" "$evil"

# --- opener that blocks must not outlast -t ----------------------------------------------------
mkpath open; newdoc
printf '#!%s\nsleep 97\n' "$bash_bin" >"$stubs/open"
run -- -t 3 "$doc"
check "blocking opener: still times out on schedule" [ "$rc" = 2 ]
check "blocking opener: not held up by it" [ "$elapsed" -le 6 ]
pkill -f "[s]leep 97" 2>/dev/null || true

# --- save via rename leaves a gap: tolerated within the settle window ---------------------------
mkpath open; newdoc
a=$(fill "$(fill "$two" 1 X)" 1 Y)
( sleep 1; mv "$doc" "$doc.bak"; sleep 2; printf '%s\n' "$a" >"$doc" ) &
run ASK_USER_SETTLE=4 -- -t 30 "$doc"
check "rename gap: exit 0, not 1" [ "$rc" = 0 ]
check "rename gap: answers read" has "$out" "2. Second?: Y"

# --- a section that loses its Decision line mid-wait -> 64 --------------------------------------
mkpath open; newdoc
later 1 "$(awk '/^Decision:/ { if (++c == 2) next } { print }' <<<"$two")"
run -- -t 30 "$doc"
check "Decision removed mid-wait: exit 64" [ "$rc" = 64 ]
check "Decision removed mid-wait: says why" has "$err" "needs a Decision:"
mkpath open
newdoc "## 1. A?
Default: A
Decision:

## 2. B?
Default: B"
run -- -t 5 "$doc"
check "section without Decision at start: exit 64" [ "$rc" = 64 ]

# --- Q:/A: only count at column 0 ---------------------------------------------------------------
mkpath open
newdoc "$(fill "$(fill "$two" 1 X)" 1 Y)
  Q: indented, not a question"
run -- -t 30 "$doc"
check "indented Q: ignored: exit 0" [ "$rc" = 0 ]

# --- multi-line Decision and Notes --------------------------------------------------------------
mkpath open
newdoc "## 1. First?
Default: A
Decision: Use
  option A
  with care
Notes: first
second

stray text after a blank line
## 2. Second?
Default: B
Decision:
  typed on the next line"
run -- -t 30 "$doc"
check "multi-line: exit 0" [ "$rc" = 0 ]
check "multi-line Decision joined" has "$out" "1. First?: Use option A with care"
check "multi-line Notes joined" has "$out" "Notes: first second"
check "multi-line: blank line ends the field" bash -c '[[ "$1" != *stray* ]]' _ "$out"
check "Decision on the next line counts" has "$out" "2. Second?: typed on the next line"

# --- --no-open ---------------------------------------------------------------------------------
mkpath open
newdoc "$(fill "$(fill "$two" 1 X)" 1 Y)"
run -- -t 30 --no-open "$doc"
check "--no-open: exit 0" [ "$rc" = 0 ]
check "--no-open: nothing opened" [ "$(calls open)" = 0 ]

# --- future mtime settles on our own clock ------------------------------------------------------
mkpath open
newdoc "$(fill "$(fill "$two" 1 X)" 1 Y)"
touch -t 203001010000 "$doc"
run -- -t 8 "$doc"
check "future mtime: still read after the settle window" [ "$rc" = 0 ]

# --- OS detection ------------------------------------------------------------------------------
osrun() { newdoc; run "$@" -- -t 1 "$doc"; }
mkpath open xdg-open wslview cmd cmd.exe wslpath
osrun ASK_USER_OS=Darwin
check "Darwin uses open" waitcalls open 1
check "Darwin does not use xdg-open" [ "$(calls xdg-open)" = 0 ]
mkpath open xdg-open wslview cmd cmd.exe wslpath
osrun ASK_USER_OS=Linux ASK_USER_PROC_VERSION="Linux version 6.1 (gcc)"
check "Linux uses xdg-open" waitcalls xdg-open 1
check "Linux does not use open" [ "$(calls open)" = 0 ]
mkpath open xdg-open wslview cmd cmd.exe wslpath
osrun ASK_USER_OS=Linux ASK_USER_PROC_VERSION="Linux version 5.15-microsoft-standard-WSL2"
check "WSL prefers wslview" waitcalls wslview 1
check "WSL: no xdg-open" [ "$(calls xdg-open)" = 0 ]
mkpath xdg-open cmd.exe wslpath
osrun ASK_USER_OS=Linux ASK_USER_PROC_VERSION="Linux version 5.15-microsoft-standard-WSL2"
check "WSL without wslview uses cmd.exe start" waitgrep '^cmd.exe /c start  C:'
check "WSL converts the path with wslpath" waitcalls wslpath 1
mkpath open cmd.exe
osrun ASK_USER_OS=MINGW64_NT-10.0
check "Git Bash uses cmd.exe //c start" waitgrep '^cmd.exe //c start  '
mkpath open xdg-open cmd
osrun ASK_USER_OS=MSYS_NT-10.0
check "MSYS prefers cmd //c start" waitgrep '^cmd //c start  '
mkpath
osrun ASK_USER_OS=Linux ASK_USER_PROC_VERSION="Linux"
check "no opener: still waits and times out" [ "$rc" = 2 ]
check "no opener: prints the doc path" has "$err" "please open: $doc"

mkpath xdg-open cmd.exe; osrun ASK_USER_OS=Linux ASK_USER_PROC_VERSION="Linux version 5.15-microsoft-standard-WSL2"
check "WSL without wslpath: falls back to printing the path" has "$err" "please open: $doc"
check "WSL without wslpath: cmd.exe not run" [ "$(calls cmd.exe)" = 0 ]

echo "$((total - fail))/$total passed"
[ "$fail" = 0 ]
