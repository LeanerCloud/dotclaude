#!/usr/bin/env bash
# Validate that every skill under skills/ is discoverable by Claude Code, Codex CLI and Gemini CLI.
#
# Gemini silently skips a SKILL.md whose frontmatter is missing, malformed, or lacks name/description
# — no warning, the skill simply never exists. This hook turns that silent skip into a loud failure.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/skill-source.sh
. "$script_dir/lib/skill-source.sh"

skills_dir="${1:-$(dirname "$script_dir")/skills}"

# Codex renders the whole skill list into at most ~8000 characters and silently shortens
# descriptions past that, degrading selection with no warning. The total is the constraint that
# actually bites; the per-skill cap is a sanity bound so one skill cannot eat the budget.
#
# Only skills this repo authors count toward the total, because only those are exported to Codex and
# Gemini: the imported third-party skills (skills/UPSTREAM.md) are Claude Code only, and
# setup-agent-symlinks.sh applies the same rule. Structural checks still run over every skill: a
# malformed imported SKILL.md is invisible to Claude Code too. An over-long description on a
# symlinked skill is upstream's to fix, so it warns rather than fails.
max_description=400
max_total=7000
total=0
failures=0

fail() {
  echo "error: $1" >&2
  failures=$((failures + 1))
}

warn() {
  echo "warn: $1" >&2
}

if [ ! -d "$skills_dir" ]; then
  echo "error: skills directory not found: $skills_dir" >&2
  exit 1
fi

# Real YAML parsing, one process for every skill: the text tools below cannot tell that
# "description: Prepare taxes from the country: collect" is invalid YAML, but every agent's parser can.
# Output: one line per skill, fields joined by US (0x1f): dir, ok|err, name, description (or message).
if ! command -v ruby >/dev/null 2>&1; then
  echo "error: ruby is required to parse SKILL.md frontmatter (Psych ships with it); install ruby" >&2
  exit 1
fi
us=$'\x1f'
skill_files=()
for f in "$skills_dir"/*/SKILL.md; do
  [ -f "$f" ] && skill_files+=("$f")
done
parsed=""
if [ "${#skill_files[@]}" -gt 0 ]; then
  parsed="$(ruby -ryaml -e '
    one = ->(v) { v.to_s.scrub.gsub(/[\r\n]/, " ") }
    ARGV.each do |f|
      dir = File.basename(File.dirname(f))
      begin
        lines = File.readlines(f, chomp: true)
        next unless lines[0] == "---"
        fm = lines[1..-1].take_while { |l| l != "---" }.join("\n")
        next if fm.strip.empty?
        d = YAML.safe_load(fm, aliases: false)
        raise "frontmatter is not a YAML mapping" unless d.is_a?(Hash)
        %w[name description].each do |k|
          raise "#{k} must be a string" unless d[k].nil? || d[k].is_a?(String)
        end
        puts [dir, "ok", one.(d["name"]), one.(d["description"].to_s.strip)].join("\x1f")
      rescue Exception => e
        puts [dir, "err", one.(e.message)].join("\x1f")
      end
    end
  ' "${skill_files[@]}")" || { echo "error: ruby YAML helper failed" >&2; exit 1; }
fi

for skill_path in "$skills_dir"/*/; do
  [ -d "$skill_path" ] || continue
  # Gitignored trees (the runtime-synced skills/synced/) are not ours to validate.
  git -C "$skills_dir" check-ignore -q "$skill_path" 2>/dev/null && continue
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

  record="$(printf '%s\n' "$parsed" | awk -F "$us" -v n="$skill_name" '$1 == n { print; exit }')"
  IFS="$us" read -r _ parse_status declared_name parsed_description <<EOF
$record
EOF
  if [ "$parse_status" != "ok" ]; then
    fail "$skill_name: invalid YAML frontmatter: ${declared_name:-unparsed}"
    continue
  fi
  if [ -z "$declared_name" ]; then
    fail "$skill_name: frontmatter has no 'name:' field"
  elif [ "$declared_name" != "$skill_name" ]; then
    fail "$skill_name: frontmatter name '$declared_name' does not match directory name"
  fi

  # Raw text, used only for the plain-scalar comment check below; the value itself is parsed.
  description="$(printf '%s\n' "$frontmatter" |
    awk '/^description:[[:space:]]*/ { found=1; sub(/^description:[[:space:]]*/, ""); print; next }
         found && /^[[:space:]]+[^[:space:]]/ { sub(/^[[:space:]]+/, " "); print; next }
         found { exit }' | tr -d '\n')"

  # In a PLAIN (unquoted) YAML scalar, " #" starts a comment — so a description mentioning "#NNN"
  # or a "#123" issue ref is silently truncated at that point, and the skill ends up advertising
  # half of what it does. Block scalars (>- / |-) and quoted scalars are immune.
  case "$description" in
    '>'*|'|'*|'"'*|"'"*) ;;
    *' #'*)
      fail "$skill_name: description is a plain scalar containing ' #', which YAML truncates as a comment — use a '>-' block scalar"
      ;;
  esac

  description="$parsed_description"

  imported=false
  skill_is_imported "$skill_path" && imported=true

  if [ -z "$description" ]; then
    fail "$skill_name: frontmatter has no 'description:' field"
  elif [ "${#description}" -gt "$max_description" ]; then
    if [ "$imported" = true ]; then
      warn "$skill_name: description is ${#description} chars, over the $max_description-char cap - upstream's to fix"
    else
      fail "$skill_name: description is ${#description} chars, over the $max_description-char cap"
    fi
  fi

  [ "$imported" = true ] && continue
  total=$((total + ${#description} + ${#skill_name} + 4))
done

if [ "$total" -gt "$max_total" ]; then
  fail "skill list renders to $total chars, over the $max_total budget — Codex truncates near 8000"
fi

echo "exported skill list: $total / $max_total chars"

if [ "$failures" -gt 0 ]; then
  echo "$failures skill validation error(s)" >&2
  exit 1
fi

echo "ok: all skills in $skills_dir are portable"
