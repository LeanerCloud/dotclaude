#!/usr/bin/env bash
# Validate that every skill under skills/ is discoverable by Claude Code, Codex CLI and Gemini CLI.
#
# Gemini silently skips a SKILL.md whose frontmatter is missing, malformed, or lacks name/description
# — no warning, the skill simply never exists. This hook turns that silent skip into a loud failure.
set -euo pipefail

skills_dir="${1:-$(cd "$(dirname "$0")/.." && pwd)/skills}"
max_description=300
failures=0

fail() {
  echo "error: $1" >&2
  failures=$((failures + 1))
}

if [ ! -d "$skills_dir" ]; then
  echo "error: skills directory not found: $skills_dir" >&2
  exit 1
fi

for skill_path in "$skills_dir"/*/; do
  [ -d "$skill_path" ] || continue
  skill_name="$(basename "$skill_path")"
  skill_md="$skill_path/SKILL.md"

  if [ ! -f "$skill_md" ]; then
    fail "$skill_name: no SKILL.md (a skill directory without one is invisible to every tool)"
    continue
  fi

  if [ "$(head -n 1 "$skill_md")" != "---" ]; then
    fail "$skill_name: SKILL.md must start with '---' on line 1 (Gemini skips it otherwise)"
    continue
  fi

  # Frontmatter is everything up to the second '---' on its own line.
  frontmatter="$(awk 'NR>1 { if ($0 == "---") exit; print }' "$skill_md")"

  if [ -z "$frontmatter" ]; then
    fail "$skill_name: empty or unterminated YAML frontmatter"
    continue
  fi

  declared_name="$(printf '%s\n' "$frontmatter" | sed -n 's/^name:[[:space:]]*//p' | head -n 1)"
  if [ -z "$declared_name" ]; then
    fail "$skill_name: frontmatter has no 'name:' field"
  elif [ "$declared_name" != "$skill_name" ]; then
    fail "$skill_name: frontmatter name '$declared_name' does not match directory name"
  fi

  # description may be folded across continuation lines (indented, no 'key:' of its own).
  description="$(printf '%s\n' "$frontmatter" |
    awk '/^description:[[:space:]]*/ { found=1; sub(/^description:[[:space:]]*/, ""); print; next }
         found && /^[[:space:]]+[^[:space:]]/ { sub(/^[[:space:]]+/, " "); print; next }
         found { exit }' | tr -d '\n')"

  if [ -z "$description" ]; then
    fail "$skill_name: frontmatter has no 'description:' field"
  elif [ "${#description}" -gt "$max_description" ]; then
    fail "$skill_name: description is ${#description} chars, over the $max_description-char budget"
  fi
done

if [ "$failures" -gt 0 ]; then
  echo "$failures skill validation error(s)" >&2
  exit 1
fi

echo "ok: all skills in $skills_dir are portable"
