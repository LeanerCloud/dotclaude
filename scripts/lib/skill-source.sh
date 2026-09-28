#!/usr/bin/env bash
# Shared predicate: was skills/<name> imported from an upstream suite, or written for this repo?
#
# Three scripts have to agree on the answer, and disagreeing fails silently: validate-skills.sh keeps
# imported skills out of the Codex list budget, setup-agent-symlinks.sh keeps them out of
# ~/.agents/skills, and import-upstream-skills.sh uses the negation to recognise a local fork it must
# not overwrite. So the rule lives here once.
#
# An imported skill has one of two shapes, both created by import-upstream-skills.sh: a symlink onto
# upstreams/, or a generated trampoline (needed where upstream sets `disable-model-invocation`, which
# stops Claude Code's Skill tool from reaching the skill at all). See skills/UPSTREAM.md.

skill_is_imported() {
  local dir="${1%/}"
  [ -L "$dir" ] && return 0
  grep -q 'GENERATED-TRAMPOLINE' "$dir/SKILL.md" 2>/dev/null
}
