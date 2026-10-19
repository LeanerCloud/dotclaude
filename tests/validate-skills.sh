#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
validator="${VALIDATOR:-$repo_root/scripts/validate-skills.sh}"
fixture="$(mktemp -d "${TMPDIR:-/tmp}/validate-skills.XXXXXX")"
trap 'rm -rf "$fixture"' EXIT
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE

failures=0

# make_skill NAME FRONTMATTER_BODY: writes skills/NAME/SKILL.md in a fresh skills dir per case.
make_skill() {
  mkdir -p "$fixture/skills/$1"
  printf -- '---\n%s\n---\nbody\n' "$2" > "$fixture/skills/$1/SKILL.md"
}

# run_case LABEL EXPECT(pass|fail) PATTERN: PATTERN must appear in the combined output.
run_case() {
  local label="$1" expect="$2" pattern="$3" out status=0
  out="$(bash "$validator" "$fixture/skills" 2>&1)" || status=$?
  if { [ "$expect" = pass ] && [ "$status" -ne 0 ]; } || { [ "$expect" = fail ] && [ "$status" -eq 0 ]; }; then
    printf 'FAIL %s: expected %s, exit %s\n%s\n' "$label" "$expect" "$status" "$out" >&2
    failures=$((failures + 1))
  elif ! printf '%s\n' "$out" | grep -qF -- "$pattern"; then
    printf 'FAIL %s: output lacks "%s"\n%s\n' "$label" "$pattern" "$out" >&2
    failures=$((failures + 1))
  else
    printf 'ok   %s\n' "$label"
  fi
  rm -rf "$fixture/skills"
}

make_skill demo $'name: demo\ndescription: A plain valid description'
run_case "valid skill passes" pass "ok: all skills"

make_skill demo $'name: demo\ndescription: Prepare taxes from the country: collect statements'
run_case "unquoted colon-space description fails as YAML" fail "invalid YAML frontmatter"

# Folded value: "first part" + " " + "second part" = 22 chars; name 4 + 4 overhead = 30 in the total.
make_skill demo $'name: demo\ndescription: >-\n  first part\n  second part'
run_case "folded >- description passes, counted from parsed value" pass "exported skill list: 30 / "

make_skill demo $'description: no name here'
run_case "missing name fails" fail "no 'name:' field"

make_skill demo $'- a'
run_case "non-mapping frontmatter fails" fail "invalid YAML frontmatter"

make_skill demo $'just a scalar'
run_case "scalar frontmatter fails" fail "invalid YAML frontmatter"

make_skill demo $'name: 2024\ndescription: numeric name'
run_case "non-string name fails" fail "invalid YAML frontmatter"

make_skill demo $'name: demo\ndescription: bad bytes \xff\xfe here'
run_case "invalid UTF-8 fails per skill" fail "invalid YAML frontmatter"

if [ "$failures" -gt 0 ]; then
  printf '%s case(s) failed\n' "$failures" >&2
  exit 1
fi
echo "all validate-skills cases passed"
