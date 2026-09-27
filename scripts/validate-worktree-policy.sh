#!/usr/bin/env bash
set -euo pipefail

mode=worktree
case "${1:-}" in
  "") ;;
  --staged) mode=staged ;;
  --worktree) mode=worktree ;;
  *)
    printf 'usage: %s [--staged|--worktree]\n' "$0" >&2
    exit 2
    ;;
esac

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
grep_args=(grep)
if [ "$mode" = staged ]; then
  grep_args+=(--cached)
fi

pathspecs=(CLAUDE.md skills scripts ':(exclude)scripts/validate-worktree-policy.sh')
failures=0

fail() {
  printf 'error: %s\n' "$1" >&2
  failures=$((failures + 1))
}

read_path() {
  local path="$1"

  if [ "$mode" = staged ]; then
    git -C "$repo_root" show ":$path"
  else
    cat "$repo_root/$path"
  fi
}

assert_contains() {
  local path="$1"
  local text="$2"
  local label="$3"
  local status

  if git -C "$repo_root" "${grep_args[@]}" -q -F "$text" -- "$path"; then
    return
  else
    status=$?
  fi

  if [ "$status" -eq 1 ]; then
    fail "$label ($path)"
  else
    fail "could not read $path from the $mode view (git grep exit $status)"
  fi
}

assert_not_contains() {
  local path="$1"
  local text="$2"
  local label="$3"
  local status

  if git -C "$repo_root" "${grep_args[@]}" -q -F "$text" -- "$path"; then
    fail "$label ($path)"
  else
    status=$?
    if [ "$status" -gt 1 ]; then
      fail "could not read $path from the $mode view (git grep exit $status)"
    fi
  fi
}

assert_contains CLAUDE.md '.claude/worktrees/' 'always-on guidance must name the durable worktree root'
assert_contains skills/worktrees/SKILL.md '.claude/worktrees/' 'worktree creation must use the durable root'
assert_contains skills/pr-orchestration/SKILL.md '.claude/worktrees/' 'orchestration must use the durable root'
assert_contains skills/pr-iterate/SKILL.md '.claude/worktrees/' 'PR iteration must use the durable root'
assert_not_contains skills/worktrees/SKILL.md 'worktree add ../<repo>-<slug>' 'worktrees skill still creates sibling worktrees'
assert_not_contains skills/pr-orchestration/SKILL.md "\$TMPDIR/claude/" 'orchestration still routes worktrees through TMPDIR'
assert_not_contains skills/pr-iterate/SKILL.md '.worktrees/<repo>/<slug>' 'PR iteration still uses a repo-local worktree root'

if files="$(git -C "$repo_root" "${grep_args[@]}" -l -F 'worktree' -- "${pathspecs[@]}")"; then
  while IFS= read -r file; do
    # LC_ALL=C: the scanner walks text one byte at a time, and every byte it
    # tests for is ASCII, so a multibyte locale would only make substr split
    # UTF-8 sequences and fail the conversion.
    if violations="$(read_path "$file" | LC_ALL=C awk -v file="$file" '
      BEGIN { squote = sprintf("%c", 39) }

      # Record a word twice: raw, so the destination can be read with its
      # quotes intact, and quote-stripped, so a command word still matches when
      # it is written as 'git' or "worktree".
      function save_word(value) {
        nt++
        tok[nt] = value
        own[nt] = nc
        word[nt] = value
        gsub(squote, "", word[nt])
        gsub(/["`]/, "", word[nt])
      }

      # Split a logical command into shell words. Words are never allowed to
      # span a comment or a command separator, so one worktree add can neither
      # hide nor invent another. tok[i] is the i-th word, own[i] the command
      # it belongs to. A # starting a word comments out the rest of the line,
      # which is what the shell would run.
      function scan(text,   i, ch, cur, q, word_start) {
        nt = 0
        nc = 1
        cur = ""
        q = ""
        word_start = 1
        for (i = 1; i <= length(text); i++) {
          ch = substr(text, i, 1)
          if (q != "") {
            # Inside double quotes a backslash escapes the next character, so
            # an escaped \" must not be taken for the closing quote. Left
            # unhandled, the word closes early, a following # reads as a
            # comment, and a real command after it goes unseen. Inside single
            # quotes a backslash is literal, as the shell treats it.
            if (q == "\"" && ch == "\\" && i < length(text)) {
              cur = cur ch substr(text, i + 1, 1)
              i++
              continue
            }
            if (ch == q) q = ""
            cur = cur ch
            continue
          }
          if (ch == "\"" || ch == squote) {
            q = ch
            cur = cur ch
            word_start = 0
            continue
          }
          if (ch == "#" && word_start) break
          if (ch ~ /[[:space:]]/) {
            if (cur != "") { save_word(cur); cur = "" }
            word_start = 1
            continue
          }
          if (ch ~ /[;&|`]/) {
            if (cur != "") { save_word(cur); cur = "" }
            nc++
            word_start = 1
            continue
          }
          cur = cur ch
          word_start = 0
        }
        if (cur != "") save_word(cur)
      }

      # Report every unsafe destination in one command. The destination is the
      # first word after `worktree add` that is not an option, so options git
      # documents as taking a value (git worktree add --help) are stepped over,
      # and a `--` ends option parsing the way git ends it.
      function check_command(lo, hi,   i, j, t, dest, saw_git, quoted_tilde, past_options, lead) {
        for (i = lo; i + 1 <= hi; i++) {
          if (word[i] != "worktree" || word[i + 1] != "add") continue
          saw_git = 0
          for (j = lo; j < i; j++) if (word[j] == "git") saw_git = 1
          if (!saw_git) continue
          dest = ""
          past_options = 0
          for (j = i + 2; j <= hi; j++) {
            t = tok[j]
            if (!past_options) {
              if (t == "--") {
                past_options = 1
                continue
              }
              if (t ~ /^-/) {
                if (t == "-b" || t == "-B" || t == "--reason") j++
                continue
              }
            }
            dest = t
            break
          }
          if (dest == "") continue
          # Read the quote before stripping it: the shell expands neither a
          # single-quoted nor a double-quoted ~, and a single-quoted $HOME
          # stays literal too, so single quotes are deliberately left on dest
          # rather than stripped.
          lead = substr(dest, 1, 1)
          quoted_tilde = (lead == "\"" || lead == squote || lead == "`") && substr(dest, 2, 1) == "~"
          gsub(/^["`]+/, "", dest)
          gsub(/["`]+$/, "", dest)
          if (quoted_tilde) {
            print file ":" start_line ": quoted tilde is not expanded: " dest
          } else if (dest ~ /(^|\/)\.\.(\/|$)/) {
            print file ":" start_line ": worktree destination escapes the durable root: " dest
          } else if (dest !~ /^\$\{?HOME\}?\/[.]claude\/worktrees\// && dest !~ /^~\/[.]claude\/worktrees\//) {
            print file ":" start_line ": worktree destination is " dest
          }
        }
      }

      function flush(   lo, hi) {
        scan(command)
        lo = 1
        while (lo <= nt) {
          hi = lo
          while (hi + 1 <= nt && own[hi + 1] == own[lo]) hi++
          check_command(lo, hi)
          lo = hi + 1
        }
        command = ""
      }
      {
        raw = $0
        if (command == "") {
          start_line = NR
        }
        continued = raw ~ /\\[[:space:]]*$/
        sub(/[[:space:]]*\\[[:space:]]*$/, "", raw)
        command = command " " raw
        if (!continued) {
          flush()
        }
      }
      END {
        flush()
      }
    ')"; then
      if [ -n "$violations" ]; then
        while IFS= read -r violation; do
          fail "$violation; expected the worktree destination to be under \$HOME/.claude/worktrees/"
        done <<< "$violations"
      fi
    else
      status=$?
      fail "could not parse worktree creation commands in $file (awk exit $status)"
    fi
  done <<< "$files"
else
  status=$?
  if [ "$status" -ne 1 ]; then
    fail "git grep failed while locating worktree creation commands (exit $status)"
  fi
fi

if [ "$failures" -gt 0 ]; then
  printf '%d worktree policy error(s)\n' "$failures" >&2
  exit 1
fi

printf 'ok: persistent worktrees use %s\n' "\$HOME/.claude/worktrees/"
