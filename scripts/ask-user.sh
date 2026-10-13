#!/usr/bin/env bash
# Ask the user a blocking question in a desktop modal and print the answer to stdout.
#
#   ask-user.sh [-t SECS] [--text] [--context TEXT | --context-file PATH]
#               [--recommend LABEL] --default LABEL "Question" "Label|what happens|tradeoffs" ...
#
# At most 3 options; "what happens" and "tradeoffs" are optional ("Label" alone is fine). The dialog
# shows the question, the context, each option as "Label: what happens" plus "Tradeoffs: ...", the
# recommended option marked, and "No answer in N min: <default> (safe default)". --default is
# required (the timeout, default 3600 s, always applies) and must be the safe, non-destructive
# choice: it is printed on stdout on timeout so the caller can act on it. With --text the answer is
# free text and --default is the text used on timeout.
#
# Exit: 0 answered (answer on stdout); 2 timeout (default on stdout); 3 cancelled; 1 no GUI
# backend (ask in chat instead); 64 usage. Run it with run_in_background so the wait doesn't block.
#
# A button labelled "Cancel" is treated as cancelling (exit 3). Pressing Enter picks the --default
# option on macOS and Windows; zenity and kdialog list it first, which should make Enter pick it
# (not verified on a real Linux desktop).
#
# Backends in order: macOS osascript; Windows powershell.exe (Git Bash/MSYS/Cygwin, or WSL);
# Linux zenity, else kdialog (needs DISPLAY or WAYLAND_DISPLAY). Strings reach every backend as
# argv or environment variables, never spliced into code.
# Test overrides: ASK_USER_OS (replaces `uname -s`), ASK_USER_PROC_VERSION (replaces /proc/version).
set -euo pipefail

TITLE="Claude Code needs your input"
DIALOG_MAX_BODY=1800
usage() { echo "usage: ask-user.sh [-t SECS] [--text] [--context T|--context-file P] [--recommend L] --default L QUESTION [LABEL|WHAT|TRADEOFFS ...]" >&2; exit 64; }
die() { echo "error: $*" >&2; exit 64; }
need_val() {
  [ $# -ge 2 ] || usage
  case "$2" in -t|--text|--context|--context-file|--recommend|--default) die "$1 needs a value, got $2" ;; esac
}

timeout=3600 mode=buttons context="" context_file="" recommend="" default=""
while [ $# -gt 0 ]; do
  case "$1" in
    -t) need_val "$@"; timeout="$2"; shift 2 ;;
    --text) mode=text; shift ;;
    --context) need_val "$@"; context="$2"; shift 2 ;;
    --context-file) need_val "$@"; context_file="$2"; shift 2 ;;
    --recommend) need_val "$@"; recommend="$2"; shift 2 ;;
    --default) need_val "$@"; default="$2"; shift 2 ;;
    -h|--help) usage ;;
    *) break ;;
  esac
done
[ $# -ge 1 ] || usage
question="$1"; shift
case "$timeout" in ''|*[!0-9]*) die "-t must be a whole number of seconds" ;; esac
[ "$timeout" -gt 0 ] && [ "$timeout" -le 86400 ] || die "-t must be 1..86400 seconds"
[ -n "$default" ] || die "--default is required: name the safe, non-destructive choice taken on timeout"
if [ -n "$context_file" ]; then
  [ -z "$context" ] || die "use --context or --context-file, not both"
  [ -r "$context_file" ] || die "cannot read $context_file"
  context=$(<"$context_file")
fi

labels=() cons=() trades=()
for a in "$@"; do
  [ -n "${a%%|*}" ] || die "empty option label in: $a"
  labels+=("${a%%|*}")
  rest=""; [ "$a" = "${a#*|}" ] || rest="${a#*|}"
  cons+=("${rest%%|*}")
  trade=""; [ "$rest" = "${rest#*|}" ] || trade="${rest#*|}"
  trades+=("$trade")
done
n=${#labels[@]}
index_of() { local i; for i in "${!labels[@]}"; do [ "${labels[$i]}" = "$1" ] && { echo $((i + 1)); return; }; done; echo 0; }
if [ "$mode" = buttons ]; then
  [ "$n" -ge 1 ] || die "give at least one option, or use --text"
  [ "$n" -le 3 ] || die "at most 3 options"
  [ "$(index_of "$default")" -gt 0 ] || die "--default must be one of the option labels"
  [ -z "$recommend" ] || [ "$(index_of "$recommend")" -gt 0 ] || die "--recommend must be one of the option labels"
else
  [ "$n" -eq 0 ] || die "--text takes no options"
fi

mins=$(((timeout + 59) / 60))
nl=$'\n'
errf=$(mktemp "${TMPDIR:-/tmp}/ask-user.XXXXXX")
trap 'rm -f "$errf"' EXIT
fail() { echo "error: $1: $(<"$errf")" >&2; exit 1; }

# build_body MAX_CONTEXT_CHARS (0 = unlimited): sets $body; only the context is ever truncated.
build_body() {
  local max="$1" ctx="$context" rest="" i line
  if [ "$mode" = buttons ]; then
    rest+="${nl}${nl}Options:"
    for i in "${!labels[@]}"; do
      line="  ${labels[$i]}"
      [ "${labels[$i]}" = "$recommend" ] && line+=" (recommended)"
      [ -z "${cons[$i]}" ] || line+=": ${cons[$i]}"
      rest+="${nl}${line}"
      [ -z "${trades[$i]}" ] || rest+="${nl}    Tradeoffs: ${trades[$i]}"
    done
  fi
  rest+="${nl}${nl}No answer in ${mins} min: ${default} (safe default)"
  if [ "$max" -gt 0 ]; then
    local avail=$((max - ${#question} - ${#rest} - 60))
    [ "$avail" -ge 0 ] || avail=0
    if [ "${#ctx}" -gt "$avail" ]; then
      ctx="${ctx:0:$avail}${nl}[truncated; see the terminal or PR for the full text]"
    fi
  fi
  body="$question"
  [ -z "$ctx" ] || body+="${nl}${nl}${ctx}"
  body+="$rest"
}

# Each backend sets $res to BUTTON:<label>, TEXT:<text>, TIMEOUT or CANCELLED.
backend_mac() {
  build_body "$DIALOG_MAX_BODY"
  local out
  # Arguments reach AppleScript through argv, never by string splicing.
  out=$(osascript - "$mode" "$timeout" "$(index_of "$default")" "$TITLE" "$body" "${labels[@]+"${labels[@]}"}" 2>"$errf" <<'APPLESCRIPT'
on run argv
  set mode to item 1 of argv
  set secs to (item 2 of argv) as integer
  set defIdx to (item 3 of argv) as integer
  set ttl to item 4 of argv
  set q to item 5 of argv
  set btns to {}
  repeat with i from 6 to count of argv
    set end of btns to item i of argv
  end repeat
  try
    tell current application to activate
    if mode is "text" then
      set r to display dialog q with title ttl default answer "" buttons {"Cancel", "Send"} default button "Send" cancel button "Cancel" giving up after secs with icon caution
      if gave up of r then return "TIMEOUT"
      return "TEXT:" & text returned of r
    end if
    set r to display dialog q with title ttl buttons btns default button defIdx giving up after secs with icon caution
    if gave up of r then return "TIMEOUT"
    return "BUTTON:" & button returned of r
  on error number -128
    return "CANCELLED"
  end try
end run
APPLESCRIPT
  ) || fail "osascript failed"
  res="$out"
}

IFS= read -r -d '' PS_SOURCE <<'POWERSHELL' || true
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
[System.Windows.Forms.Application]::EnableVisualStyles()
$global:r = ''
$isText = $env:ASK_TEXT -eq '1'
$labels = @($env:ASK_LABELS -split [char]10 | Where-Object { $_ })
$f = New-Object System.Windows.Forms.Form
$f.Text = $env:ASK_TITLE
$f.TopMost = $true
$f.StartPosition = 'CenterScreen'
$f.Size = New-Object System.Drawing.Size(700,580)
$tb = New-Object System.Windows.Forms.TextBox
$tb.Multiline = $true
$tb.ReadOnly = $true
$tb.ScrollBars = 'Vertical'
$tb.Dock = 'Fill'
$tb.Text = ($env:ASK_BODY -replace '\r?\n', [Environment]::NewLine)
$f.Controls.Add($tb)
$in = $null
if ($isText) {
  $in = New-Object System.Windows.Forms.TextBox
  $in.Multiline = $true
  $in.Height = 80
  $in.Dock = 'Bottom'
  $f.Controls.Add($in)
}
$p = New-Object System.Windows.Forms.FlowLayoutPanel
$p.Dock = 'Bottom'
$p.Height = 54
$p.AutoSize = $true
$p.AutoSizeMode = 'GrowAndShrink'
$f.Controls.Add($p)
foreach ($l in $labels) {
  $b = New-Object System.Windows.Forms.Button
  $b.Text = $l
  $b.AutoSize = $true
  $b.Add_Click({
    if ($in) { $global:r = 'TEXT64:' + [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($in.Text)) }
    else { $global:r = 'BUTTON:' + [array]::IndexOf($labels, $this.Text) }
    $f.Close()
  })
  $p.Controls.Add($b)
  if ($l -eq $env:ASK_DEFAULT) { $f.AcceptButton = $b }
}
if ($isText) {
  $c = New-Object System.Windows.Forms.Button
  $c.Text = 'Cancel'
  $c.Add_Click({ $f.Close() })
  $p.Controls.Add($c)
}
$t = New-Object System.Windows.Forms.Timer
$t.Interval = [int]$env:ASK_TIMEOUT * 1000
$t.Add_Tick({ $t.Stop(); $global:r = 'TIMEOUT'; $f.Close() })
$t.Start()
$f.Add_Shown({ $f.Activate() })
[void]$f.ShowDialog()
[Console]::Out.Write($global:r)
POWERSHELL

backend_windows() {
  build_body 0
  local labs="" l out
  if [ "$mode" = text ]; then labs="Send"; else for l in "${labels[@]}"; do labs+="$l$nl"; done; fi
  out=$(MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*' MSYS2_ENV_CONV_EXCL='ASK_*' \
    ASK_BODY="$body" ASK_TITLE="$TITLE" ASK_LABELS="$labs" ASK_TEXT="$([ "$mode" = text ] && echo 1 || echo 0)" \
    ASK_TIMEOUT="$timeout" ASK_DEFAULT="$default" \
    WSLENV="ASK_BODY:ASK_TITLE:ASK_LABELS:ASK_TEXT:ASK_TIMEOUT:ASK_DEFAULT${WSLENV:+:$WSLENV}" \
    powershell.exe -NoProfile -Sta -Command "$PS_SOURCE" 2>"$errf") || fail "powershell failed"
  out=${out//$'\r'/}
  case "$out" in
    BUTTON:[0-9]*) res="BUTTON:${labels[${out#BUTTON:}]}" ;;
    TEXT64:*) res="TEXT:$(printf '%s' "${out#TEXT64:}" | { base64 -d 2>/dev/null || base64 -D; })" ;;
    *) res="${out:-CANCELLED}" ;;
  esac
}

# ordered_labels: the default first, so Enter on a list backend picks the safe choice.
ordered_labels() {
  local l; printf '%s\n' "$default"
  for l in "${labels[@]}"; do [ "$l" = "$default" ] || printf '%s\n' "$l"; done
}

backend_zenity() {
  build_body "$DIALOG_MAX_BODY"
  local out rc=0 ordered=() l
  if [ "$mode" = text ]; then
    out=$(zenity --entry --no-markup --title="$TITLE" --text="$body" --width=720 --timeout="$timeout" 2>"$errf") || rc=$?
  else
    while IFS= read -r l; do ordered+=("$l"); done < <(ordered_labels)
    out=$(zenity --list --no-markup --title="$TITLE" --text="$body" --column=Option --hide-header \
      --width=720 --height=600 --timeout="$timeout" -- "${ordered[@]}" 2>"$errf") || rc=$?
  fi
  case "$rc" in
    0) if [ "$mode" = text ]; then res="TEXT:$out"; elif [ -n "$out" ]; then res="BUTTON:${out%%|*}"; else res=CANCELLED; fi ;;
    5) res=TIMEOUT ;;
    1) res=CANCELLED ;;
    *) fail "zenity failed (exit $rc)" ;;
  esac
}

backend_kdialog() {
  build_body "$DIALOG_MAX_BODY"
  local out rc=0 l args=()
  command -v timeout >/dev/null 2>&1 || { echo "error: kdialog has no timeout option and 'timeout' (coreutils) is not installed; refusing to block forever. Ask the user in chat instead" >&2; exit 1; }
  if [ "$mode" = text ]; then
    args=(--inputbox "$body" "")
  else
    args=(--menu "$body")
    while IFS= read -r l; do args+=("$l" "$l"); done < <(ordered_labels)
  fi
  out=$(timeout "$timeout" kdialog --title "$TITLE" "${args[@]}" 2>"$errf") || rc=$?
  case "$rc" in
    0) if [ "$mode" = text ]; then res="TEXT:$out"; elif [ -n "$out" ]; then res="BUTTON:$out"; else res=CANCELLED; fi ;;
    124) res=TIMEOUT ;;
    1) res=CANCELLED ;;
    *) fail "kdialog failed (exit $rc)" ;;
  esac
}

os="${ASK_USER_OS:-$(uname -s)}"
proc_version="${ASK_USER_PROC_VERSION:-/proc/version}"
res=""
case "$os" in
  Darwin) command -v osascript >/dev/null 2>&1 && backend_mac ;;
  MINGW*|MSYS*|CYGWIN*) command -v powershell.exe >/dev/null 2>&1 && backend_windows ;;
  Linux)
    if [ -r "$proc_version" ] && [[ "$(<"$proc_version")" == *[Mm]icrosoft* ]] && command -v powershell.exe >/dev/null 2>&1; then
      backend_windows
    elif [ -n "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ]; then
      if command -v zenity >/dev/null 2>&1; then backend_zenity
      elif command -v kdialog >/dev/null 2>&1; then backend_kdialog
      fi
    fi ;;
esac
if [ -z "$res" ]; then
  echo "error: no GUI dialog backend available on this machine (need osascript, powershell.exe, or zenity/kdialog with DISPLAY/WAYLAND_DISPLAY); ask the user in chat instead" >&2
  exit 1
fi

case "$res" in
  TIMEOUT) echo "timeout after ${timeout}s; using default" >&2; printf '%s\n' "$default"; exit 2 ;;
  CANCELLED|BUTTON:Cancel) echo "cancelled" >&2; exit 3 ;;
  BUTTON:*) printf '%s\n' "${res#BUTTON:}" ;;
  TEXT:*) printf '%s\n' "${res#TEXT:}" ;;
  *) echo "error: unexpected dialog result: $res" >&2; exit 1 ;;
esac
