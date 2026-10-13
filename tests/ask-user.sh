#!/usr/bin/env bash
# Stub-based tests for scripts/ask-user.sh: fake osascript, zenity, kdialog and powershell.exe on
# PATH record their argv and environment; the tests assert those and the exit-code mapping.
# Nothing here opens a real dialog or exercises a real Linux/Windows desktop.
# shellcheck disable=SC2016,SC2046,SC2319
set -uo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
script="${ASK_USER_SCRIPT:-$here/../scripts/ask-user.sh}"
bash_bin=$(command -v bash)
root=$(mktemp -d)
trap 'rm -rf "$root"' EXIT
fail=0 total=0

ok() { total=$((total + 1)); if [ "$1" = 0 ]; then :; else fail=$((fail + 1)); echo "FAIL: $2"; fi; }
has() { [[ "$1" == *"$2"* ]]; }

# Fresh PATH dir with only rm plus the stubs named in $@.
mkstubs() {
  stubs="$root/bin.$((++seq))"; log="$root/log.$seq"; mkdir -p "$stubs" "$log"
  local tool; for tool in rm mktemp base64; do ln -s "$(command -v "$tool")" "$stubs/$tool"; done
  local name
  for name in "$@"; do
    cat >"$stubs/$name" <<STUB
#!$bash_bin
i=0; for a in "\$@"; do i=\$((i+1)); printf '%s' "\$a" >"$log/$name.arg\$i"; done
for v in ASK_BODY ASK_TITLE ASK_LABELS ASK_TEXT ASK_TIMEOUT ASK_DEFAULT WSLENV MSYS_NO_PATHCONV MSYS2_ARG_CONV_EXCL MSYS2_ENV_CONV_EXCL; do printf '%s' "\${!v-}" >"$log/$name.env.\$v"; done
[ -n "\${STUB_OUT-}" ] && printf '%s' "\$STUB_OUT"
exit "\${STUB_RC:-0}"
STUB
    chmod +x "$stubs/$name"
  done
  # timeout stub: drop the duration and run the rest
  if [ "${WITH_TIMEOUT:-0}" = 1 ]; then printf '#!%s\nshift\nexec "$@"\n' "$bash_bin" >"$stubs/timeout"; chmod +x "$stubs/timeout"; fi
}
seq=0
run() { # run ENV... -- ARGS...; sets out rc err
  local envs=()
  while [ "$1" != -- ]; do envs+=("$1"); shift; done; shift
  out=$(env -i PATH="$stubs" HOME="$root" TMPDIR="$root" ${envs[@]+"${envs[@]}"} "$bash_bin" "$script" "$@" 2>"$root/err"); rc=$?
  err=$(<"$root/err")
}
logged() { cat "$log"/* 2>/dev/null; }
read_log() { [ -e "$log/$1" ] && cat "$log/$1"; }

OPTS=('Merge|merges the PR|cannot be undone' 'Keep|leaves it open|slower' 'Bare')
COMMON=(-t 90 --default Keep --recommend Merge --context 'Checked CI and diff.')

echo "== usage"
mkstubs
run -- "q" 'A'; ok $((rc != 64)) "no --default -> 64"
run -- --default A "q" 'A' 'B' 'C' 'D'; ok $((rc != 64)) "4 options -> 64"
run -- --default Z "q" 'A'; ok $((rc != 64)) "default not an option -> 64"
run -- --default A --recommend Z "q" 'A'; ok $((rc != 64)) "recommend not an option -> 64"
run -- -t abc --default A "q" 'A'; ok $((rc != 64)) "non-numeric -t -> 64"
run -- -t --default A "q" 'A'; ok $((rc != 64)) "flag as value -> 64"
run -- --default A --context x --context-file /nonexistent "q" 'A'; ok $((rc != 64)) "context + file -> 64"
run -- -t 86401 --default A "q" 'A'; ok $((rc != 64)) "-t above 86400 -> 64"
run -- -t 0 --default A "q" 'A'; ok $((rc != 64)) "-t 0 -> 64"
run -- --default A "q" 'A' ''; ok $((rc != 64)) "empty option label -> 64"
run -- --default A "q" '|only consequence'; ok $((rc != 64)) "empty label before | -> 64"

echo "== no backend"
mkstubs
run ASK_USER_OS=Linux ASK_USER_PROC_VERSION=/nonexistent -- -t 90 --default Keep "q" 'Keep'
ok $((rc != 1)) "no DISPLAY -> 1"; has "$err" "ask the user in chat" ; ok $? "fallback hint printed"
run ASK_USER_OS=Linux DISPLAY=:0 ASK_USER_PROC_VERSION=/nonexistent -- -t 90 --default Keep "q" 'Keep'
ok $((rc != 1)) "DISPLAY but no zenity/kdialog -> 1"
run ASK_USER_OS=Darwin -- -t 90 --default Keep "q" 'Keep'; ok $((rc != 1)) "darwin without osascript -> 1"

echo "== macOS (stub osascript)"
mkstubs osascript
q='He said "hi" $(touch PWN) `touch PWN2` '"'x'"
run ASK_USER_OS=Darwin STUB_OUT='BUTTON:Merge' -- "${COMMON[@]}" "$q" "${OPTS[@]}"
ok $rc "osascript answered rc0"; ok $([ "$out" = Merge ]; echo $?) "prints label"
body=$(read_log osascript.arg6)
has "$body" "$q"; ok $? "question passed literally via argv"
has "$body" "Merge (recommended): merges the PR"; ok $? "recommended + consequence rendered"
has "$body" "    Tradeoffs: cannot be undone"; ok $? "tradeoffs rendered"
has "$body" "No answer in 2 min: Keep (safe default)"; ok $? "default line"
ok $([ "$(read_log osascript.arg4)" = 2 ]; echo $?) "default button index = default option (Keep=2)"
ok $([ ! -e "$root/PWN" ] && [ ! -e PWN ]; echo $?) "no injection side effect"
run ASK_USER_OS=Darwin STUB_OUT='BUTTON:Bare' -- -t 5 --default Bare "q" "${OPTS[@]}"
ok $([ "$(read_log osascript.arg4)" = 3 ]; echo $?) "default button index follows --default (Bare=3)"
run ASK_USER_OS=Darwin STUB_OUT='BUTTON:Merge' -- -t 5 --default Merge --recommend Keep "q" "${OPTS[@]}"
ok $([ "$(read_log osascript.arg4)" = 1 ]; echo $?) "default button index ignores --recommend (Merge=1)"
run ASK_USER_OS=Darwin STUB_OUT=TIMEOUT -- "${COMMON[@]}" "q" "${OPTS[@]}"
ok $([ $rc = 2 ] && [ "$out" = Keep ]; echo $?) "timeout -> 2 and default on stdout"
run ASK_USER_OS=Darwin STUB_OUT=CANCELLED -- "${COMMON[@]}" "q" "${OPTS[@]}"; ok $((rc != 3)) "cancel -> 3"
run ASK_USER_OS=Darwin STUB_OUT='BUTTON:Cancel' -- --default Cancel "q" 'Cancel' 'Other'; ok $((rc != 3)) "button named Cancel -> 3"
run ASK_USER_OS=Darwin STUB_OUT='TEXT:free answer' -- --text --default none "q"; ok $([ $rc = 0 ] && [ "$out" = "free answer" ]; echo $?) "text answer"
long=$(printf 'x%.0s' $(seq 1 5000))
run ASK_USER_OS=Darwin STUB_OUT='BUTTON:Keep' -- "${COMMON[@]}" --context "$long" "q" "${OPTS[@]}"
body=$(read_log osascript.arg6)
has "$body" "[truncated; see the terminal or PR"; ok $? "long context truncated with pointer"
has "$body" "safe default"; ok $? "default line survives truncation"
ok $([ ${#body} -le 2000 ]; echo $?) "body bounded"
printf 'from a file' >"$root/ctx"
run ASK_USER_OS=Darwin STUB_OUT='BUTTON:Keep' -- --default Keep --context-file "$root/ctx" "q" 'Keep'
has "$(read_log osascript.arg6)" "from a file"; ok $? "--context-file read"

echo "== Windows (stub powershell.exe)"
mkstubs powershell.exe
run ASK_USER_OS=MINGW64_NT-10.0 STUB_OUT='BUTTON:0' -- "${COMMON[@]}" "$q" "${OPTS[@]}"
ok $([ $rc = 0 ] && [ "$out" = Merge ]; echo $?) "button index 0 maps back to label Merge"
run ASK_USER_OS=MINGW64_NT-10.0 STUB_OUT='BUTTON:2' -- -t 90 --default Keep "q" 'Keep' 'Other' 'Übernehmen'
ok $([ "$out" = 'Übernehmen' ]; echo $?) "non-ASCII label returned from bash, not from the console"
run ASK_USER_OS=MINGW64_NT-10.0 STUB_OUT='BUTTON:0' -- "${COMMON[@]}" "$q" "${OPTS[@]}"
ok $([ "$(read_log powershell.exe.arg1)" = -NoProfile ]; echo $?) "-NoProfile"
ok $([ "$(read_log powershell.exe.arg3)" = -Command ]; echo $?) "-Command"
ps=$(read_log powershell.exe.arg4)
has "$ps" "$q"; ok $((! $? )) "question NOT interpolated into PowerShell source"
has "$ps" 'TopMost = $true'; ok $? "TopMost"
has "$ps" "ScrollBars"; ok $? "scrollable text box"
has "$ps" "ReadOnly"; ok $? "read-only body"
has "$ps" "AcceptButton"; ok $? "AcceptButton set (Enter picks the default)"
has "$ps" "ToBase64String"; ok $? "text answer returned as UTF-8 base64"
src=$(<"$script"); has "$src" "default button defIdx"; ok $? "mac dialog default button uses the --default index"
has "$ps" "IndexOf"; ok $? "button answer returned as index"
has "$ps" "GrowAndShrink"; ok $? "button panel autosizes"
body=$(read_log powershell.exe.env.ASK_BODY)
has "$body" "$q"; ok $? "question passed via env literally"
has "$body" "Tradeoffs: slower"; ok $? "tradeoffs in env body"
ok $([ "$(read_log powershell.exe.env.ASK_DEFAULT)" = Keep ]; echo $?) "default passed for Enter button"
ok $([ "$(read_log powershell.exe.env.ASK_TIMEOUT)" = 90 ]; echo $?) "timeout passed"
ok $([ "$(read_log powershell.exe.env.MSYS2_ENV_CONV_EXCL)" = 'ASK_*' ]; echo $?) "MSYS2_ENV_CONV_EXCL protects ASK_* from path conversion"
ok $([ "$(read_log powershell.exe.env.MSYS_NO_PATHCONV)" = 1 ]; echo $?) "MSYS_NO_PATHCONV set"
has "$(read_log powershell.exe.env.ASK_LABELS)" "Bare"; ok $? "labels passed"
run ASK_USER_OS=MINGW64_NT-10.0 STUB_OUT='TIMEOUT' -- "${COMMON[@]}" "q" "${OPTS[@]}"
ok $([ $rc = 2 ] && [ "$out" = Keep ]; echo $?) "timeout mapping"
run ASK_USER_OS=MINGW64_NT-10.0 STUB_OUT='' -- "${COMMON[@]}" "q" "${OPTS[@]}"; ok $((rc != 3)) "window closed -> cancelled 3"
enc=$(printf '%s' 'héllo wörld ✓' | base64)
run ASK_USER_OS=MINGW64_NT-10.0 STUB_OUT="TEXT64:$enc" -- --text --default none "q"
ok $([ "$out" = 'héllo wörld ✓' ] && [ $rc = 0 ]; echo $?) "base64 UTF-8 text decoded"
run ASK_USER_OS=MINGW64_NT-10.0 STUB_OUT="TEXT64:" -- --text --default none "q"
ok $([ -z "$out" ] && [ $rc = 0 ]; echo $?) "empty text answer"
run ASK_USER_OS=MINGW64_NT-10.0 STUB_OUT='TEXT64:'"$(printf 'hello' | base64)" -- --text --default none "q"
ok $([ "$out" = hello ] && [ "$(read_log powershell.exe.env.ASK_TEXT)" = 1 ]; echo $?) "text mode"
echo "Linux version 5.15.0-microsoft-standard-WSL2" >"$root/pv"
run ASK_USER_OS=Linux ASK_USER_PROC_VERSION="$root/pv" STUB_OUT='BUTTON:Keep' -- "${COMMON[@]}" "q" "${OPTS[@]}"
ok $([ $rc = 0 ] && [ "$out" = Keep ]; echo $?) "WSL uses powershell.exe"
has "$(read_log powershell.exe.env.WSLENV)" "ASK_BODY"; ok $? "WSLENV forwards ASK_BODY"

echo "== Linux zenity (stub)"
mkstubs zenity kdialog
run ASK_USER_OS=Linux DISPLAY=:0 ASK_USER_PROC_VERSION=/nonexistent STUB_OUT='Merge' -- "${COMMON[@]}" "$q" "${OPTS[@]}"
ok $([ $rc = 0 ] && [ "$out" = Merge ]; echo $?) "zenity answer"
all=$(logged)
has "$all" "--list"; ok $? "zenity --list"
has "$all" "--no-markup"; ok $? "zenity --no-markup (text is not markup)"
has "$all" "--timeout=90"; ok $? "zenity timeout"
has "$all" "$q"; ok $? "question passed literally"
has "$all" "Tradeoffs: cannot be undone"; ok $? "zenity tradeoffs"
a_first=$(read_log zenity.arg$(for i in 1 2 3 4 5 6 7 8 9 10 11 12 13; do [ "$(read_log zenity.arg$i 2>/dev/null)" = -- ] && echo $((i+1)); done | head -1))
ok $([ "$a_first" = Keep ]; echo $?) "default option listed first"
ok $([ ! -e "$root/PWN" ] && [ ! -e PWN ]; echo $?) "no injection side effect"
run ASK_USER_OS=Linux DISPLAY=:0 ASK_USER_PROC_VERSION=/nonexistent STUB_RC=5 -- "${COMMON[@]}" "q" "${OPTS[@]}"
ok $([ $rc = 2 ] && [ "$out" = Keep ]; echo $?) "zenity 5 -> timeout"
run ASK_USER_OS=Linux WAYLAND_DISPLAY=w0 ASK_USER_PROC_VERSION=/nonexistent STUB_RC=1 -- "${COMMON[@]}" "q" "${OPTS[@]}"
ok $((rc != 3)) "zenity 1 -> cancelled (Wayland)"
run ASK_USER_OS=Linux DISPLAY=:0 ASK_USER_PROC_VERSION=/nonexistent STUB_RC=9 -- "${COMMON[@]}" "q" "${OPTS[@]}"
ok $((rc != 1)) "zenity unexpected rc -> 1"
run ASK_USER_OS=Linux DISPLAY=:0 ASK_USER_PROC_VERSION=/nonexistent STUB_OUT='typed' -- --text --default none "q"
ok $([ "$out" = typed ] && has "$(logged)" "--entry"; echo $?) "zenity --entry for text"

echo "== Linux kdialog (stub, no zenity)"
WITH_TIMEOUT=1 mkstubs kdialog
run ASK_USER_OS=Linux DISPLAY=:0 ASK_USER_PROC_VERSION=/nonexistent STUB_OUT='Merge' -- "${COMMON[@]}" "$q" "${OPTS[@]}"
ok $([ $rc = 0 ] && [ "$out" = Merge ]; echo $?) "kdialog answer"
all=$(logged)
has "$all" "--menu"; ok $? "kdialog --menu"
has "$all" "$q"; ok $? "kdialog question literal"
ok $([ "$(read_log kdialog.arg5)" = Keep ]; echo $?) "kdialog default first"
run ASK_USER_OS=Linux DISPLAY=:0 ASK_USER_PROC_VERSION=/nonexistent STUB_RC=124 -- "${COMMON[@]}" "q" "${OPTS[@]}"
ok $([ $rc = 2 ] && [ "$out" = Keep ]; echo $?) "kdialog 124 -> timeout"
run ASK_USER_OS=Linux DISPLAY=:0 ASK_USER_PROC_VERSION=/nonexistent STUB_RC=1 -- "${COMMON[@]}" "q" "${OPTS[@]}"
ok $((rc != 3)) "kdialog 1 -> cancelled"

echo "== kdialog without timeout, long context"
mkstubs kdialog
run ASK_USER_OS=Linux DISPLAY=:0 ASK_USER_PROC_VERSION=/nonexistent STUB_OUT=Keep -- "${COMMON[@]}" "q" "${OPTS[@]}"
ok $([ $rc = 1 ] && has "$err" "refusing to block forever"; echo $?) "kdialog without timeout -> 1 with explanation"
ok $([ ! -e "$log/kdialog.arg1" ]; echo $?) "kdialog not launched without timeout"
WITH_TIMEOUT=1 mkstubs zenity kdialog
run ASK_USER_OS=Linux DISPLAY=:0 ASK_USER_PROC_VERSION=/nonexistent STUB_OUT=Keep -- "${COMMON[@]}" --context "$long" "q" "${OPTS[@]}"
has "$(logged)" "[truncated; see the terminal or PR"; ok $? "zenity context capped"
ok $([ "$(wc -c <"$log/zenity.arg4")" -le 2400 ]; echo $?) "zenity body bounded"
WITH_TIMEOUT=1 mkstubs kdialog
run ASK_USER_OS=Linux DISPLAY=:0 ASK_USER_PROC_VERSION=/nonexistent STUB_OUT=Keep -- "${COMMON[@]}" --context "$long" "q" "${OPTS[@]}"
has "$(logged)" "[truncated; see the terminal or PR"; ok $? "kdialog context capped"

echo "$((total - fail))/$total passed"
[ "$fail" = 0 ]
