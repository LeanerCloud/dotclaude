#!/usr/bin/env bash
# Read-only disk audit of $HOME. Writes TSVs and a markdown inventory to
# ~/.claude/reports/disk-cleanup/. Never modifies anything outside that dir.
set -uo pipefail

REPORT_DIR="${REPORT_DIR:-$HOME/.claude/reports/disk-cleanup}"
mkdir -p "$REPORT_DIR"
TOP="$REPORT_DIR/top-level.tsv"
REPOS="$REPORT_DIR/repos.tsv"
CANDS="$REPORT_DIR/rebuildable-candidates.tsv"
INV="$REPORT_DIR/INVENTORY.md"

# Directory names to skip entirely while walking (huge or protected).
# Protected by policy: secrets/keys, browser data, personal media.
PRUNE_ARGS=(
  -name .ssh -o -name .gnupg -o -name .password-store -o -name .mozilla
  -o -name Downloads -o -name Pictures -o -name Videos -o -name Music
  -o -name Documents -o -name node_modules -o -name .cargo -o -name .rustup
  -o -path "$HOME/.config/google-chrome" -o -path "$HOME/.config/chromium"
  -o -path "$HOME/.config/BraveSoftware" -o -path "$HOME/.local/share/Trash"
  -o -path "$HOME/.cache" -o -path "$HOME/.local/share/containers"
  -o -path "$HOME/.local/share/Steam" -o -path "$HOME/.claude/projects"
)

# Scanned repos are untrusted: their config must not run fsmonitor or hooks.
export GIT_CONFIG_NOSYSTEM=1
# GIT_OPTIONAL_LOCKS=0 stops status from rewriting the index of a repo we only read.
export GIT_OPTIONAL_LOCKS=0
GIT=(git -c core.fsmonitor=false -c core.hooksPath=/dev/null)

# Stock macOS has no timeout; fail loud instead of reporting bogus zeros.
if command -v timeout >/dev/null 2>&1; then TMO=timeout
elif command -v gtimeout >/dev/null 2>&1; then TMO=gtimeout
else echo "disk-audit: need timeout or gtimeout (brew install coreutils)" >&2; exit 1
fi

# Pass NUL-delimited paths through, dropping any with control characters
# (newline/tab would forge TSV records); rejects are logged, never reported.
REJECTS="$REPORT_DIR/rejected-paths.log"
: >"$REJECTS"
safe_paths() {
  local p
  while IFS= read -r -d '' p; do
    if [[ $p == *[[:cntrl:]]* ]]; then
      printf '%q\n' "$p" >>"$REJECTS"
    else
      printf '%s\0' "$p"
    fi
  done
}

echo "== top-level sizes (KiB) =="
printf 'size_kib\tpath\n' >"$TOP"
find "$HOME" -mindepth 1 -maxdepth 1 -print0 \
  | safe_paths \
  | xargs -0 -r du -sxk 2>/dev/null \
  | sort -rn >>"$TOP"

echo "== git repos and worktrees =="
printf 'path\tkind\tbranch\tdirty_files\tunpushed_commits\tstashes\tremotes\tlast_commit\tsize_kib\n' >"$REPOS"
find "$HOME" -xdev \( "${PRUNE_ARGS[@]}" \) -prune -o -name .git -print0 -prune 2>/dev/null \
  | safe_paths \
  | while IFS= read -r -d '' g; do
      r="$(dirname "$g")"
      if [ -d "$g" ]; then kind=main; else kind=worktree; fi
      branch="$("${GIT[@]}" -C "$r" symbolic-ref --short -q HEAD 2>/dev/null || echo DETACHED)"
      # status would run clean filters from the untrusted repo config; skip it.
      # Only repo-controlled config is a threat; the global one may define filter.lfs.
      FRE='^filter\..*\.(clean|process)$'
      if "${GIT[@]}" -C "$r" config --local --includes --get-regexp "$FRE" >/dev/null 2>&1 \
        || "${GIT[@]}" -C "$r" config --worktree --includes --get-regexp "$FRE" >/dev/null 2>&1; then
        dirty="skipped: filter config"
      else
        out="$("$TMO" 120 "${GIT[@]}" -C "$r" status --porcelain 2>/dev/null)"
        if [ $? -eq 124 ]; then dirty=timeout
        else dirty="$(printf '%s' "$out" | grep -c '^')"; fi
      fi
      unpushed="$("${GIT[@]}" -C "$r" rev-list --count --branches --tags HEAD --not --remotes 2>/dev/null || echo '?')"
      stashes="$("${GIT[@]}" -C "$r" stash list 2>/dev/null | wc -l | tr -d ' ')"
      remotes="$("${GIT[@]}" -C "$r" remote 2>/dev/null | paste -sd, -)"
      last="$("${GIT[@]}" -C "$r" log -1 --format=%cs 2>/dev/null || echo '?')"
      size="$("$TMO" 300 du -sxk "$r" 2>/dev/null | cut -f1)"
      printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$r" "$kind" "$branch" "$dirty" "$unpushed" "$stashes" "$remotes" "$last" "${size:-?}" >>"$REPOS"
    done

echo "== rebuildable candidates =="
printf 'size_kib\tpath\n' >"$CANDS"
find "$HOME" -xdev \( "${PRUNE_ARGS[@]}" \) -prune -o -type d \
  \( -name 'build*' -o -name target -o -name .venv -o -name venv -o -name __pycache__ \
     -o -name .gradle -o -name .next -o -name dist -o -name .ccache \) -print0 -prune 2>/dev/null \
  | safe_paths \
  | xargs -0 -r du -sxk 2>/dev/null \
  | sort -rn | head -200 >>"$CANDS"

{
  echo "# Disk inventory"
  echo
  echo "Generated: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo
  echo '## Filesystem'
  echo '```'
  df -h "$HOME"
  echo '```'
  echo
  echo '## Top-level entries of ~ (largest 30)'
  echo '```'
  head -31 "$TOP"
  echo '```'
  echo
  echo '## Largest rebuildable candidates (top 30)'
  echo '```'
  head -31 "$CANDS"
  echo '```'
  echo
  echo '## Repos and worktrees'
  echo "Count: $(($(wc -l <"$REPOS") - 1)). Full list: repos.tsv"
  echo
  echo '### Repos with unique work (dirty or unpushed or no remote)'
  echo '```'
  awk -F'\t' 'NR==1 || ($4!=0 || $5!=0 || $6!=0 || $7=="")' "$REPOS"
  echo '```'
} >"$INV"

echo "done: $INV"
