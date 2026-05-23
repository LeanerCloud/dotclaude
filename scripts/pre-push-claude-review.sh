#!/usr/bin/env bash
# pre-push-claude-review.sh
#
# Opt-in git pre-push hook that runs a Sonnet-tier Claude review pass over the
# diff being pushed before the push proceeds. Catches the same class of issues
# CodeRabbit would catch on the resulting PR, locally, ~30s.
#
# Install:
#   ln -s ~/.claude/scripts/pre-push-claude-review.sh \
#         <project>/.git/hooks/pre-push
#   chmod +x ~/.claude/scripts/pre-push-claude-review.sh
#
# Bypass (single push):
#   SKIP_CLAUDE_REVIEW=1 git push ...
#
# Bypass (always for one repo): just remove the symlink.
#
# Cost note: this adds ~30s and one Sonnet call per push. Set
# CLAUDE_REVIEW_MODEL=haiku for a cheaper/faster pass; set
# CLAUDE_REVIEW_MODEL=opus for the rare diff that warrants the top tier.

set -euo pipefail

if [[ "${SKIP_CLAUDE_REVIEW:-0}" == "1" ]]; then
  echo "[pre-push-claude-review] SKIP_CLAUDE_REVIEW=1 -> skipping local review pass."
  exit 0
fi

if ! command -v claude >/dev/null 2>&1; then
  echo "[pre-push-claude-review] 'claude' CLI not found on PATH -> skipping local review pass."
  exit 0
fi

REMOTE_NAME="${1:-origin}"
MODEL="${CLAUDE_REVIEW_MODEL:-sonnet}"

# stdin lines from git: <local_ref> <local_sha> <remote_ref> <remote_sha>
# We diff each (remote_sha..local_sha) range and concatenate. If remote_sha is
# all-zeros (new branch), fall back to merge-base against the remote default
# branch so the diff isn't the whole history.
remote_default="$(git remote show "$REMOTE_NAME" 2>/dev/null \
                  | awk '/HEAD branch/ {print $NF}')"
remote_default="${remote_default:-main}"

DIFF_FILE="$(mktemp -t claude-pre-push-diff.XXXXXX)"
trap 'rm -f "$DIFF_FILE"' EXIT

found_any=0
while read -r local_ref local_sha remote_ref remote_sha; do
  [[ -z "${local_sha:-}" ]] && continue
  if [[ "$local_sha" == "0000000000000000000000000000000000000000" ]]; then
    continue  # deletion -- nothing to review
  fi
  if [[ "$remote_sha" == "0000000000000000000000000000000000000000" ]]; then
    base="$(git merge-base "$local_sha" "$REMOTE_NAME/$remote_default" 2>/dev/null || echo "")"
    [[ -z "$base" ]] && base="$(git rev-list --max-parents=0 "$local_sha" | head -1)"
  else
    base="$remote_sha"
  fi
  {
    printf '\n=== %s..%s (%s) ===\n' "$base" "$local_sha" "$local_ref"
    git diff "$base..$local_sha"
  } >> "$DIFF_FILE"
  found_any=1
done

if [[ "$found_any" -eq 0 ]]; then
  echo "[pre-push-claude-review] No diff to review -> skipping."
  exit 0
fi

diff_bytes="$(wc -c < "$DIFF_FILE" | tr -d ' ')"
if [[ "$diff_bytes" -lt 50 ]]; then
  echo "[pre-push-claude-review] Diff under 50 bytes -> skipping."
  exit 0
fi

echo "[pre-push-claude-review] Running $MODEL review on ${diff_bytes} bytes of diff..."

# Find the per-project memory garden if it exists.
project_slug="$(pwd | sed 's|/|-|g')"
memory_dir="$HOME/.claude/projects/$project_slug/memory"
memory_listing=""
if [[ -d "$memory_dir" ]]; then
  memory_listing="$(ls "$memory_dir"/feedback_*.md "$memory_dir"/project_*.md 2>/dev/null \
                    | sed "s|^|- |")"
fi

PROMPT_FILE="$(mktemp -t claude-pre-push-prompt.XXXXXX)"
trap 'rm -f "$DIFF_FILE" "$PROMPT_FILE"' EXIT

{
  cat <<'EOF'
Run the /review-staged-diff command's logic over the diff below. Apply
git-workflow.md §1's six dimensions: Completeness, Correctness, Security,
Bugs, Duplication, Memory-garden match. The memory entries to scan are
listed below; read the ones whose names match the diff's language/topic
and report any violation by entry name.

Output ONLY the findings table. If nothing is found, output the literal
string "REVIEW CLEAN" on a single line. Do NOT summarise or commentate.
EOF
  if [[ -n "$memory_listing" ]]; then
    printf '\nMemory entries for this project:\n%s\n' "$memory_listing"
  fi
  printf '\nDiff under review:\n\n'
  cat "$DIFF_FILE"
} > "$PROMPT_FILE"

REVIEW_OUTPUT="$(claude -p --model "$MODEL" < "$PROMPT_FILE" 2>&1 || true)"

if [[ -z "$(printf '%s' "$REVIEW_OUTPUT" | tr -d '[:space:]')" ]]; then
  echo "[pre-push-claude-review] Claude returned empty output -> letting push through."
  exit 0
fi

if grep -qx 'REVIEW CLEAN' <<< "$REVIEW_OUTPUT"; then
  echo "[pre-push-claude-review] REVIEW CLEAN -> push proceeding."
  exit 0
fi

echo "[pre-push-claude-review] Findings (resolve or set SKIP_CLAUDE_REVIEW=1 to bypass):"
echo "----------------------------------------------------------------------"
echo "$REVIEW_OUTPUT"
echo "----------------------------------------------------------------------"
exit 1
