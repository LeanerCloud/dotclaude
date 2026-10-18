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

echo "== top-level sizes (KiB) =="
printf 'size_kib\tpath\n' >"$TOP"
find "$HOME" -mindepth 1 -maxdepth 1 -print0 \
  | xargs -0 -r du -sxk 2>/dev/null \
  | sort -rn >>"$TOP.tmp"
awk -F'\t' '{print $1"\t"$2}' "$TOP.tmp" >>"$TOP"
rm -f "$TOP.tmp"

echo "== git repos and worktrees =="
printf 'path\tkind\tbranch\tdirty_files\tunpushed_commits\tremotes\tlast_commit\tsize_kib\n' >"$REPOS"
find "$HOME" -xdev \( "${PRUNE_ARGS[@]}" \) -prune -o -name .git -print -prune 2>/dev/null \
  | while IFS= read -r g; do
      r="$(dirname "$g")"
      if [ -d "$g" ]; then kind=main; else kind=worktree; fi
      branch="$(git -C "$r" symbolic-ref --short -q HEAD 2>/dev/null || echo DETACHED)"
      dirty="$(timeout 120 git -C "$r" status --porcelain 2>/dev/null | wc -l)"
      unpushed="$(git -C "$r" rev-list --count HEAD --not --remotes 2>/dev/null || echo '?')"
      remotes="$(git -C "$r" remote 2>/dev/null | paste -sd, -)"
      last="$(git -C "$r" log -1 --format=%cs 2>/dev/null || echo '?')"
      size="$(timeout 300 du -sxk "$r" 2>/dev/null | cut -f1)"
      printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$r" "$kind" "$branch" "$dirty" "$unpushed" "$remotes" "$last" "${size:-?}" >>"$REPOS"
    done

echo "== rebuildable candidates =="
printf 'size_kib\tpath\n' >"$CANDS"
find "$HOME" -xdev \( "${PRUNE_ARGS[@]}" \) -prune -o -type d \
  \( -name 'build*' -o -name target -o -name .venv -o -name venv -o -name __pycache__ \
     -o -name .gradle -o -name .next -o -name dist -o -name .ccache \) -print -prune 2>/dev/null \
  | xargs -d '\n' -r du -sxk 2>/dev/null \
  | sort -rn | head -200 >>"$CANDS"

{
  echo "# Disk inventory"
  echo
  echo "Generated: $(date -Is)"
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
  awk -F'\t' 'NR==1 || ($4>0 || $5>0 || $6=="")' "$REPOS"
  echo '```'
} >"$INV"

echo "done: $INV"
