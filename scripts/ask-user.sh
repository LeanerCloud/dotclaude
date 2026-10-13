#!/usr/bin/env bash
# Put a decision to the user as a markdown doc they edit, and wait for the answers.
#
#   ask-user.sh [-t SECS] [--reopen SECS] [--no-open] DOC.md
#
# The agent writes DOC.md (conventionally ~/.claude/decisions/<YYYY-MM-DD>-<slug>.md, gitignored)
# from templates/decision.md, composing the prose itself:
#
#   # Decision needed: <title>
#   Asked by <session/agent>, <date>. If unanswered by <time>: each question's Default (safe, non-destructive).
#
#   ## 1. <question>
#   Context: <what is being decided, why now, what was checked>
#   Options:
#   - **<A>** (recommended): <what happens>. Tradeoffs: <cost, risk, what you give up>
#   - **<B>**: <what happens>. Tradeoffs: <...>
#   Default: <A>
#   Decision:
#   Notes:
#
# One doc may hold several numbered "## n. question" sections. Each needs a Default: line (the
# safe, non-destructive choice) and has a Decision: line the user fills in.
#
# The script opens the doc in the user's default app for .md (macOS open; Linux xdg-open; WSL
# wslview or cmd.exe start; Git Bash/MSYS/Cygwin cmd start), then polls every 5 s until every Decision: line
# has text and the file has been unchanged for 10 s (so a half-typed answer is never read). With
# no opener it prints the path and still waits. The doc is re-opened every --reopen seconds
# (default 900) while unanswered. Doc content is only ever parsed by awk, never evaluated.
#
# Collaborative loop: the user may add a line "Q: ..." at column 0 anywhere in a section. A Q: with
# no "A:" line (also column 0) after it, before the next Q:, makes the script exit 4 and print the
# open questions. The agent writes "A: ..." under each and re-runs the script with --no-open (skips
# the first open so a second window doesn't appear; --reopen still applies).
#
# Decision: and Notes: may continue on following lines, up to a blank line or the next Field:
# line (Context, Options, Default, Decision, Notes, Q, A) or "## ". Continuations are joined with
# single spaces. Every section needs a Decision: and a Default: line; a doc that loses one while
# waiting exits 64.
#
# Openers run in the background so a blocking one (xdg-open on some setups) cannot outlast -t.
# A briefly missing file (an editor saving via rename) is tolerated for up to the settle window
# before exit 1. A modification time in the future is treated as settled once the settle window
# has passed on our own clock.
#
# Exit: 0 answered; 2 timeout (-t, default 3600, max 86400): each unanswered section's Default is
# printed as its decision, marked "(default, no answer)"; 4 user asked questions; 64 usage or
# invalid doc; 1 the file vanished. A timeout default is never approval for anything destructive.
# Run it with run_in_background so the wait doesn't block.
#
# Output on 0 (and 2): "<n>. <question>: <decision>" per section, plus "   Notes: ..." if set.
# Test overrides: ASK_USER_OS (replaces `uname -s`), ASK_USER_PROC_VERSION (replaces /proc/version),
# ASK_USER_POLL (poll seconds, default 5), ASK_USER_SETTLE (quiet seconds, default 10).
set -euo pipefail

poll=${ASK_USER_POLL:-5} settle=${ASK_USER_SETTLE:-10}
usage() { echo "usage: ask-user.sh [-t SECS] [--reopen SECS] [--no-open] DOC.md" >&2; exit 64; }
die() { echo "error: $*" >&2; exit 64; }
whole() { case "$2" in ''|*[!0-9]*) die "$1 must be a whole number of seconds" ;; esac; }

timeout=3600 reopen=900 no_open=0
while [ $# -gt 0 ]; do
  case "$1" in
    -t) [ $# -ge 2 ] || usage; timeout="$2"; shift 2 ;;
    --reopen) [ $# -ge 2 ] || usage; reopen="$2"; shift 2 ;;
    --no-open) no_open=1; shift ;;
    -h|--help) usage ;;
    -*) usage ;;
    *) break ;;
  esac
done
[ $# -eq 1 ] || usage
doc="$1"
whole -t "$timeout"; whole --reopen "$reopen"
[ "$timeout" -ge 1 ] && [ "$timeout" -le 86400 ] || die "-t must be 1..86400 seconds"
[ "$reopen" -ge 1 ] || die "--reopen must be at least 1 second"
[ -f "$doc" ] && [ -r "$doc" ] || die "cannot read $doc"
doc="$(cd "$(dirname "$doc")" && pwd)/$(basename "$doc")"

# awk MODE: check | openq | report. Sections start at "## "; Q:/A:/Decision:/Default:/Notes: are
# matched at line start (leading blanks allowed).
parse() {
  awk -v mode="$1" '
    function trim(s) { sub(/^[ \t\r]+/, "", s); sub(/[ \t\r]+$/, "", s); return s }
    function join(a, b) { b = trim(b); return a == "" ? b : a " " b }
    function val(s) { sub(/^[^:]*:/, "", s); return trim(s) }
    /^##[ \t]/ {
      n++; q = $0; sub(/^##[ \t]+/, "", q); sub(/^[0-9]+\.[ \t]*/, "", q); ques[n] = trim(q); next
    }
    n == 0 { next }
    /^[ \t\r]*$/ { cont = ""; next }
    /^[ \t]*Decision:/ { hasdec[n] = 1; dec[n] = val($0); decs++; cont = "dec"; next }
    /^[ \t]*Default:/ { hasdef[n] = 1; def[n] = val($0); cont = ""; next }
    /^[ \t]*Notes:/ { notes[n] = val($0); cont = "notes"; next }
    /^[ \t]*(Context|Options):/ { cont = ""; next }
    /^Q:/ { nq++; qtext[nq] = val($0); qopen[nq] = 1; cont = ""; next }
    /^A:/ { if (nq > 0) qopen[nq] = 0; cont = ""; next }
    cont == "dec" { dec[n] = join(dec[n], $0); next }
    cont == "notes" { notes[n] = join(notes[n], $0); next }
    END {
      if (mode == "check") {
        nodef = 0; open = 0; unans = 0; nodec = 0
        for (i = 1; i <= n; i++) { if (!hasdef[i] || def[i] == "") nodef++; if (dec[i] == "") unans++; if (!hasdec[i]) nodec++ }
        for (i = 1; i <= nq; i++) if (qopen[i]) open++
        print "decisions=" decs + 0, "sections=" n + 0, "nodefault=" nodef, "nodecision=" nodec, "unanswered=" unans, "openq=" open
      } else if (mode == "openq") {
        for (i = 1; i <= nq; i++) if (qopen[i]) print "Q: " qtext[i]
      } else {
        for (i = 1; i <= n; i++) {
          if (dec[i] != "") print i ". " ques[i] ": " dec[i]
          else print i ". " ques[i] ": " def[i] " (default, no answer)"
          if (notes[i] != "") print "   Notes: " notes[i]
        }
      }
    }' "$doc"
}

field() { # field NAME <check-output>
  local kv
  for kv in $2; do [ "${kv%%=*}" = "$1" ] && { echo "${kv#*=}"; return; }; done
}

# validate_doc SUMMARY: print the problem and return 1 if the doc is not a usable decision doc.
validate_doc() {
  [ "$(field decisions "$1")" -ge 1 ] || { echo "$doc has no Decision: line in a '## n. question' section"; return 1; }
  [ "$(field nodecision "$1")" -eq 0 ] || { echo "$doc: every '## ' section needs a Decision: line"; return 1; }
  [ "$(field nodefault "$1")" -eq 0 ] || { echo "$doc: every '## ' section needs a non-empty Default: line"; return 1; }
}
msg=$(validate_doc "$(parse check)") || die "$msg"

mtime() { stat -c %Y "$1" 2>/dev/null || stat -f %m "$1" 2>/dev/null || echo 0; }

open_doc() {
  local os pv winpath cmdline=()
  os=${ASK_USER_OS:-$(uname -s)}
  case "$os" in
    Darwin) command -v open >/dev/null 2>&1 && cmdline=(open "$doc") ;;
    MINGW*|MSYS*|CYGWIN*)
      winpath=$doc; command -v cygpath >/dev/null 2>&1 && winpath=$(cygpath -w "$doc")
      if command -v cmd >/dev/null 2>&1; then cmdline=(cmd //c start "" "$winpath")
      elif command -v cmd.exe >/dev/null 2>&1; then cmdline=(cmd.exe //c start "" "$winpath"); fi ;;
    *)
      pv=${ASK_USER_PROC_VERSION-$(cat /proc/version 2>/dev/null || true)}
      case "$pv" in
        *[Mm]icrosoft*|*WSL*)
          if command -v wslview >/dev/null 2>&1; then cmdline=(wslview "$doc")
          elif command -v cmd.exe >/dev/null 2>&1 && command -v wslpath >/dev/null 2>&1; then
            cmdline=(cmd.exe /c start "" "$(wslpath -w "$doc")")
          fi ;;
        *) command -v xdg-open >/dev/null 2>&1 && cmdline=(xdg-open "$doc") ;;
      esac ;;
  esac
  if [ ${#cmdline[@]} -gt 0 ]; then ( "${cmdline[@]}" >/dev/null 2>&1 & ); return 0; fi
  echo "ask-user: could not open the doc automatically; please open: $doc" >&2
}

echo "ask-user: waiting for decisions in $doc" >&2
[ "$no_open" = 1 ] || open_doc
start=$SECONDS last_open=$SECONDS gap_start="" future_since=""
while :; do
  if [ ! -f "$doc" ] || ! summary=$(parse check 2>/dev/null); then
    [ -n "$gap_start" ] || gap_start=$SECONDS
    if [ $((SECONDS - gap_start)) -ge "$settle" ]; then echo "error: $doc vanished" >&2; exit 1; fi
    sleep "$poll"; continue
  fi
  gap_start=""
  quiet=$(( $(date +%s) - $(mtime "$doc") ))
  if [ "$quiet" -lt 0 ]; then
    [ -n "$future_since" ] || future_since=$SECONDS
    quiet=$((SECONDS - future_since))
  else
    future_since=""
  fi
  if [ "$quiet" -ge "$settle" ]; then
    msg=$(validate_doc "$summary") || { echo "error: $msg" >&2; exit 64; }
    if [ "$(field openq "$summary")" -gt 0 ]; then
      parse openq
      echo "ask-user: the user asked questions; add an 'A: ...' line under each Q: in $doc and re-run with --no-open" >&2
      exit 4
    fi
    if [ "$(field unanswered "$summary")" -eq 0 ]; then
      parse report
      exit 0
    fi
  fi
  if [ $((SECONDS - start)) -ge "$timeout" ]; then
    parse report
    exit 2
  fi
  if [ $((SECONDS - last_open)) -ge "$reopen" ]; then open_doc; last_open=$SECONDS; fi
  sleep "$poll"
done
