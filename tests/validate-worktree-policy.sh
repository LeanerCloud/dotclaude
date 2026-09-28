#!/usr/bin/env bash
# shellcheck disable=SC2016 # case bodies are literal shell text
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
validator="$repo_root/scripts/validate-worktree-policy.sh"
fixture_repo="$(mktemp -d "${TMPDIR:-/tmp}/wt-policy.XXXXXX")"
trap 'rm -rf "$fixture_repo"' EXIT

# Git exports these to hook processes; inheriting them makes fixture commands
# reinitialize the real repository instead of the temporary fixture.
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_COMMON_DIR \
  GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES

if [ ! -f "$validator" ]; then
  printf 'validator not found: %s\n' "$validator" >&2
  exit 2
fi

mkdir -p "$fixture_repo/scripts" "$fixture_repo/skills/worktrees" \
  "$fixture_repo/skills/pr-orchestration" "$fixture_repo/skills/pr-iterate" \
  "$fixture_repo/skills/testdata"
printf 'root ~/.claude/worktrees/\n' > "$fixture_repo/CLAUDE.md"
printf 'root ~/.claude/worktrees/\n' > "$fixture_repo/skills/worktrees/SKILL.md"
printf 'root ~/.claude/worktrees/\n' > "$fixture_repo/skills/pr-orchestration/SKILL.md"
printf 'root ~/.claude/worktrees/\n' > "$fixture_repo/skills/pr-iterate/SKILL.md"
cp "$validator" "$fixture_repo/scripts/validate-worktree-policy.sh"
printf 'fixture\n' > "$fixture_repo/skills/testdata/case.md"
git -C "$fixture_repo" init -q
git -C "$fixture_repo" config user.email test@example.com
git -C "$fixture_repo" config user.name Test
git -C "$fixture_repo" config core.hooksPath /dev/null
git -C "$fixture_repo" add -A
git -C "$fixture_repo" -c commit.gpgsign=false commit -qm init

passed=0
failed=0
xfailed=0
unexpected=0
unknown=0

report_failure() {
  local name="$1"
  local expect="$2"
  local got="$3"
  local out="$4"

  failed=$((failed + 1))
  printf '  FAIL %s: expected %s, got %s\n' "$name" "$expect" "$got" >&2
  printf '%s\n' "$out" >&2
}

run_case() {
  local expect="$1"
  local name="$2"
  local body="$3"
  local got
  local out

  printf '%s\n' "$body" > "$fixture_repo/skills/testdata/case.md"
  if out="$(bash "$fixture_repo/scripts/validate-worktree-policy.sh" --worktree 2>&1)"; then
    got=ok
  else
    got=violation
  fi

  case "$expect" in
    ok)
      if [ "$got" = ok ]; then passed=$((passed + 1)); else report_failure "$name" "$expect" "$got" "$out"; fi
      ;;
    violation)
      if [ "$got" = violation ]; then passed=$((passed + 1)); else report_failure "$name" "$expect" "$got" "$out"; fi
      ;;
    xfail-ok)
      if [ "$got" = ok ]; then
        xfailed=$((xfailed + 1))
      else
        unexpected=$((unexpected + 1))
        printf '  RESOLVED? %s changed from the tracked xfail to %s\n' "$name" "$got" >&2
      fi
      ;;
    xfail-violation)
      if [ "$got" = violation ]; then
        xfailed=$((xfailed + 1))
      else
        unexpected=$((unexpected + 1))
        printf '  RESOLVED? %s changed from the tracked xfail to %s\n' "$name" "$got" >&2
      fi
      ;;
    *)
      unknown=$((unknown + 1))
      printf '  FAIL %s: unknown expectation %s\n' "$name" "$expect" >&2
      ;;
  esac
}

run_case ok 'durable root, canonical' \
  'git worktree add "$HOME/.claude/worktrees/repo-slug" -b feat/x main'
run_case ok 'durable root, tilde form' \
  'git worktree add ~/.claude/worktrees/repo-slug main'
run_case ok 'durable root, braced var' \
  'git worktree add ${HOME}/.claude/worktrees/repo-slug main'
run_case ok 'option -b before the path' \
  'git worktree add -b feature "$HOME/.claude/worktrees/project-feature" main'
run_case ok 'option -B before the path' \
  'git worktree add -B feature "$HOME/.claude/worktrees/project-feature" main'
run_case ok 'global -C before the subcommand' \
  'git -C <repo-root> worktree add "$HOME/.claude/worktrees/<repo-name>-<slug>" <branch>'
run_case ok 'no-value flags before the path' \
  'git worktree add -f --detach --no-checkout "$HOME/.claude/worktrees/repo-slug" main'
run_case ok 'value-bearing flag before the path' \
  'git worktree add --lock --reason "in use" "$HOME/.claude/worktrees/repo-slug" main'
run_case ok 'end-of-options marker' \
  'git worktree add -- "$HOME/.claude/worktrees/repo-slug"'
run_case violation 'dash destination after end-of-options' \
  'git worktree add -- -tmp/foo'
run_case ok 'markdown bullet around inline code' \
  '- `git -C <repo-root> worktree add "$HOME/.claude/worktrees/<repo>-<slug>" <branch>` (do NOT create a fresh branch).'
run_case xfail-violation 'line continuation before the path' \
  'git worktree add \
  "$HOME/.claude/worktrees/repo-slug" main'
run_case ok 'unrelated worktree command' \
  'git worktree prune'
run_case xfail-violation 'comment mentioning worktree add' \
  '# remember to run git worktree add under the durable root'
run_case ok 'trailing comment does not mask a durable root on the same line' \
  'git worktree add "$HOME/.claude/worktrees/safe" main # git worktree add /tmp/job'
run_case ok 'UTF-8 prose around a compliant command' \
  'Per §1b: `git worktree add "$HOME/.claude/worktrees/repo-slug" -b feat/x main` OK'
run_case violation 'UTF-8 prose around an unsafe command' \
  'Per §1b: `git worktree add /tmp/job main` OK'

run_case xfail-ok 'single-quoted git' \
  "'git' worktree add /tmp/job"
run_case xfail-ok 'double-quoted git' \
  '"git" worktree add /tmp/job'
run_case xfail-ok 'quoted worktree and add' \
  'git "worktree" "add" /tmp/job'
run_case ok 'quoted command with a durable destination' \
  "'git' worktree add \"\$HOME/.claude/worktrees/repo-slug\" main"
run_case violation 'single-quoted tilde is not expanded' \
  "git worktree add '~/.claude/worktrees/repo-slug'"
run_case xfail-ok 'single-quoted HOME stays literal' \
  "git worktree add '\$HOME/.claude/worktrees/repo-slug'"
run_case violation 'double-quoted tilde is not expanded' \
  'git worktree add "~/.claude/worktrees/repo-slug"'

run_case violation 'escaped-quote prefix does not mask a later semicolon violation' \
  'echo "ok \" # literal" ; git worktree add /tmp/job'
run_case violation 'escaped-quote prefix does not mask a later && violation' \
  'echo "ok \" # literal" && git worktree add /tmp/job'
run_case ok 'escaped-quote prefix before a compliant command' \
  'echo "ok \" # literal" ; git worktree add "$HOME/.claude/worktrees/repo-slug"'
run_case violation 'escaped-hash prefix does not mask a later violation' \
  'echo a\#b ; git worktree add /tmp/job'
run_case violation 'single-quote backslash prefix does not mask a later violation' \
  "echo 'a\\' ; git worktree add /tmp/job"

run_case violation 'apostrophe before a semicolon does not mask a violation' \
  "Don't use /tmp; git worktree add /tmp/job"
run_case violation 'apostrophe before && does not mask a violation' \
  "Don't use /tmp && git worktree add /tmp/job"
run_case violation 'possessive apostrophe does not mask a violation' \
  "The repo's rule; git worktree add /tmp/job"
run_case violation 'escaped apostrophe does not mask a violation' \
  "don\\'t use /tmp; git worktree add /tmp/job"
run_case violation 'unmatched double quote does not mask a violation' \
  'The "durable root rule; git worktree add /tmp/job'
run_case ok 'apostrophe before a compliant command' \
  "Don't use /tmp; git worktree add \"\$HOME/.claude/worktrees/repo-slug\" main"
run_case xfail-ok 'apostrophe plus a single-quoted HOME' \
  "Don't use /tmp; git worktree add '\$HOME/.claude/worktrees/repo-slug'"

run_case xfail-ok 'escaped git command word' \
  'The worktree rule: g\it worktree add /tmp/job'
run_case xfail-ok 'escaped worktree command word' \
  'The worktree rule: git wor\ktree add /tmp/job'
run_case xfail-ok 'escaped add command word' \
  'The worktree rule: git worktree a\dd /tmp/job'
run_case ok 'escaped command words with a durable destination' \
  'The worktree rule: g\it wor\ktree a\dd "$HOME/.claude/worktrees/repo-slug"'

run_case xfail-ok 'comment conceals a temp destination' \
  'git worktree add /tmp/job # git worktree add "$HOME/.claude/worktrees/safe"'
run_case violation 'plain temp destination' \
  'git worktree add /tmp/job main'
run_case violation 'TMPDIR destination' \
  'git worktree add $TMPDIR/claude/repo-slug'
run_case violation 'sibling relative destination' \
  'git worktree add ../repo-slug -b feat/x main'
run_case violation 'repo-local destination' \
  'git worktree add .worktrees/repo/slug main'
run_case violation 'traversal out of the durable root' \
  'git worktree add "$HOME/.claude/worktrees/../../tmp/job"'
run_case violation 'temp destination behind an option' \
  'git worktree add -f /tmp/job main'
run_case violation 'safe command before an unsafe && command' \
  'git worktree add "$HOME/.claude/worktrees/safe" main && git worktree add /tmp/job main'
run_case violation 'unsafe command before a safe && command' \
  'git worktree add /tmp/job main && git worktree add "$HOME/.claude/worktrees/safe" main'
run_case violation 'unsafe command beside a pipe' \
  'git worktree add /tmp/job main | git worktree add "$HOME/.claude/worktrees/safe" main'

staged_failures=0
printf '%s\n' 'git worktree add /tmp/job main' > "$fixture_repo/skills/testdata/case.md"
git -C "$fixture_repo" add skills/testdata/case.md
if bash "$fixture_repo/scripts/validate-worktree-policy.sh" --staged >/dev/null 2>&1; then
  printf '  FAIL staged mode accepted a temp destination\n' >&2
  staged_failures=$((staged_failures + 1))
fi
printf '%s\n' 'git worktree add "$HOME/.claude/worktrees/repo-slug" main' > "$fixture_repo/skills/testdata/case.md"
git -C "$fixture_repo" add skills/testdata/case.md
if ! bash "$fixture_repo/scripts/validate-worktree-policy.sh" --staged >/dev/null 2>&1; then
  printf '  FAIL staged mode rejected a durable destination\n' >&2
  staged_failures=$((staged_failures + 1))
fi
printf '%s\n' 'git worktree add /tmp/job main' > "$fixture_repo/skills/testdata/case.md"
if ! bash "$fixture_repo/scripts/validate-worktree-policy.sh" --staged >/dev/null 2>&1; then
  printf '  FAIL staged mode did not use the index while the worktree differed\n' >&2
  staged_failures=$((staged_failures + 1))
fi
if bash "$fixture_repo/scripts/validate-worktree-policy.sh" --worktree >/dev/null 2>&1; then
  printf '  FAIL worktree mode did not see the unstaged violation\n' >&2
  staged_failures=$((staged_failures + 1))
fi

total=$((passed + xfailed + failed + unexpected + unknown))
if [ "$total" -ne 50 ]; then
  printf 'expected 50 parser cases, ran %d\n' "$total" >&2
  exit 1
fi
if [ "$failed" -gt 0 ] || [ "$unexpected" -gt 0 ] || [ "$unknown" -gt 0 ] || [ "$staged_failures" -gt 0 ]; then
  printf '%d case(s) failed, %d tracked xfail(s) changed, %d unknown expectation(s), %d staged-mode check(s) failed\n' \
    "$failed" "$unexpected" "$unknown" "$staged_failures" >&2
  exit 1
fi

printf 'ok: %d worktree policy cases passed, %d known gaps tracked in #115, staged/worktree divergence verified\n' \
  "$passed" "$xfailed"
