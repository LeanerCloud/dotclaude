#!/usr/bin/env bash
# Sync the curated third-party skills into skills/.
#
# Upstreams are pinned as git submodules under upstreams/ so every imported skill traces to a commit
# and re-syncs with `git submodule update --remote`. Nothing is copy-pasted: a skill is either a
# relative symlink onto its submodule, or a trampoline that re-exports the upstream body under
# model-invocable frontmatter (needed for the upstream skills that carry
# `disable-model-invocation: true`, which Claude Code's Skill tool otherwise refuses to invoke).
#
# The curation rationale - what is imported, what is deliberately not, and why - lives in
# skills/UPSTREAM.md. This script is the executable half of that document.
#
# Idempotent, and it never overwrites a local fork: a path that exists as a real directory rather
# than a symlink or a trampoline is left untouched.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/skill-source.sh
. "$SCRIPT_DIR/lib/skill-source.sh"

cd "$(dirname "$SCRIPT_DIR")"

# name|url
UPSTREAMS=(
  "cursor-plugins|https://github.com/cursor/plugins.git"
  "superpowers|https://github.com/obra/superpowers.git"
  "gstack|https://github.com/garrytan/gstack.git"
  "anthropic-skills|https://github.com/anthropics/skills.git"
  "terraform-skill|https://github.com/antonbabenko/terraform-skill.git"
  "owasp-security|https://github.com/agamm/claude-code-owasp.git"
)

PSTACK=upstreams/cursor-plugins/pstack/skills
TEAMKIT=upstreams/cursor-plugins/cursor-team-kit/skills

# skills/<name> -> upstreams/<path>, as a plain symlink.
SYMLINKS=(
  "how|$PSTACK/how"
  "why|$PSTACK/why"
  "unslop|$PSTACK/unslop"
  "typescript-best-practices|$PSTACK/typescript-best-practices"
  "deslop|$TEAMKIT/deslop"
  "verify-this|$TEAMKIT/verify-this"
  "brainstorming|upstreams/superpowers/skills/brainstorming"
  "systematic-debugging|upstreams/superpowers/skills/systematic-debugging"
  "verification-before-completion|upstreams/superpowers/skills/verification-before-completion"
  "writing-skills|upstreams/superpowers/skills/writing-skills"
  "office-hours|upstreams/gstack/office-hours"
  "cso|upstreams/gstack/cso"
  "health|upstreams/gstack/health"
  "retro|upstreams/gstack/retro"
  "webapp-testing|upstreams/anthropic-skills/skills/webapp-testing"
  "mcp-builder|upstreams/anthropic-skills/skills/mcp-builder"
  "terraform-skill|upstreams/terraform-skill/skills/terraform-skill"
  "owasp-security|upstreams/owasp-security/.claude/skills/owasp-security"
)

# Upstream carries `disable-model-invocation: true`; re-export it so CLAUDE.md can route to it.
TRAMPOLINES=(
  "teach|$PSTACK/teach"
  "blast-radius|$PSTACK/blast-radius"
  "architect|$PSTACK/architect"
  "interrogate|$PSTACK/interrogate"
  "arena|$PSTACK/arena"
  "tdd|$PSTACK/tdd"
  "reflect|$PSTACK/reflect"
  "figure-it-out|$PSTACK/figure-it-out"
  "create-verification-skill|$PSTACK/create-verification-skill"
  "maintain-verification-skill|$PSTACK/maintain-verification-skill"
  "technical-writing|$PSTACK/technical-writing"
  "show-me-your-work|$PSTACK/show-me-your-work"
  "thermo-nuclear-code-quality-review|$TEAMKIT/thermo-nuclear-code-quality-review"
)

add_upstream() {
  local name="$1" url="$2"
  if [ -e "upstreams/$name/.git" ]; then
    echo "  = upstreams/$name"
    return
  fi
  echo "  + upstreams/$name"
  git submodule add --force --depth 1 "$url" "upstreams/$name" >/dev/null
  git config -f .gitmodules "submodule.upstreams/$name.shallow" true
}

# An existing skill directory this script did not create is a local fork (skills/UPSTREAM.md). Keep it.
is_local_fork() {
  [ -d "skills/$1" ] && ! skill_is_imported "skills/$1"
}

link_skill() {
  local name="$1" target="$2"
  is_local_fork "$name" && { echo "  ~ skills/$name (local fork, untouched)"; return 0; }
  # A skill promoted from the trampoline list leaves a real directory behind, and `ln -sfn` would
  # drop the link inside it rather than replacing it. is_local_fork has cleared the path, so what
  # stands there is a trampoline this script generated.
  [ -d "skills/$name" ] && [ ! -L "skills/$name" ] && rm -rf "skills/$name"
  ln -sfn "../$target" "skills/$name"
  echo "  + skills/$name"
}

trampoline_skill() {
  local name="$1" target="$2" desc folded
  is_local_fork "$name" && { echo "  ~ skills/$name (local fork, untouched)"; return 0; }
  desc=$(awk '/^description:/{sub(/^description: */,""); print; exit}' "$target/SKILL.md")
  [ -n "$desc" ] || { echo "  ! skills/$name: no description in $target/SKILL.md" >&2; return 1; }

  # A YAML description folded across continuation lines would be silently truncated to its first
  # line, and the skill would advertise half of what it does. Fail instead.
  folded=$(awk '/^description:/ { getline nxt; if (nxt ~ /^[[:space:]]+[^[:space:]]/) print "yes"; exit }' "$target/SKILL.md")
  [ -z "$folded" ] || { echo "  ! skills/$name: upstream description folds across lines - teach this script to join them" >&2; return 1; }

  # Safe despite is_local_fork having just cleared this path: what remains is either nothing, a
  # symlink this script made (removed without touching its target), or a trampoline it generated.
  rm -rf "skills/$name" && mkdir -p "skills/$name"
  cat > "skills/$name/SKILL.md" <<EOF
---
name: $name
description: $desc
---

<!-- GENERATED-TRAMPOLINE by scripts/import-upstream-skills.sh - do not edit; see skills/UPSTREAM.md -->

# $name

Read \`~/.claude/$target/SKILL.md\` and follow it exactly, including anything it references from its
own \`references/\` or \`scripts/\` directory (paths there are relative to that directory).

This file exists only because the upstream skill carries \`disable-model-invocation: true\`, which
stops Claude Code's Skill tool from reaching it. The body is deliberately not copied, so
\`git submodule update --remote\` keeps this skill current.

Resolve every Cursor primitive the upstream names through \`~/.claude/upstreams/HOST-MAPPING.md\`.
Where upstream and \`~/.claude/CLAUDE.md\` disagree, CLAUDE.md wins.
EOF
  echo "  + skills/$name (trampoline)"
}

rc=0
echo "Upstreams:"
for u in "${UPSTREAMS[@]}"; do
  IFS='|' read -r n url <<< "$u"
  add_upstream "$n" "$url"
done

echo "Curated skills:"
for l in "${SYMLINKS[@]}"; do
  IFS='|' read -r n t <<< "$l"
  [ -d "$t" ] || { echo "  ! skills/$n: upstream moved, $t is gone" >&2; rc=1; continue; }
  link_skill "$n" "$t"
done
for l in "${TRAMPOLINES[@]}"; do
  IFS='|' read -r n t <<< "$l"
  [ -d "$t" ] || { echo "  ! skills/$n: upstream moved, $t is gone" >&2; rc=1; continue; }
  trampoline_skill "$n" "$t" || rc=1
done

exit "$rc"
