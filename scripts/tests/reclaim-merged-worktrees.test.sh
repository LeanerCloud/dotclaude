#!/usr/bin/env bash
# Fixture suite for scripts/reclaim-merged-worktrees.sh. Run it with /bin/bash (3.2).
#
#   /bin/bash scripts/tests/reclaim-merged-worktrees.test.sh [case-name ...]
#
# Every case builds a scratch repo with a bare "origin" and a fake `gh` on PATH, all under
# /private/tmp/claude-501/reclaim-tests/. The script under test gets a fake HOME and plans
# glob, so real repos and ~/.claude/projects are never touched. SCRIPT may be overridden to
# test a mutated copy.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="${SCRIPT:-$HERE/../reclaim-merged-worktrees.sh}"
BASE=/private/tmp/claude-501/reclaim-tests
mkdir -p "$BASE" || exit 1
ROOT="$(mktemp -d "$BASE/run.XXXXXX")" || exit 1
mkdir -p "$ROOT/bin"
trap 'pkill -P $$ sleep 2>/dev/null; true' EXIT

export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@example.invalid
export GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@example.invalid

cat > "$ROOT/bin/gh" <<'EOF'
#!/bin/bash
d="${FAKE_GH_DIR:?}"
echo "$*" >> "$d/calls.log"
[ -f "$d/fail" ] && { echo "gh: simulated failure" >&2; exit 1; }
if [ "$1" = api ]; then
  [ -f "$d/api_fail" ] && { echo "gh: simulated api failure" >&2; exit 1; }
  [ "$2" = user ] && [ "$3" = --jq ] && [ "$4" = .login ] || { echo "gh: bad api args" >&2; exit 1; }
  if [ -f "$d/user" ]; then cat "$d/user"; else echo me; fi
  exit 0
fi
if [ "$1" = repo ]; then
  # real gh: `repo view` takes the repo positionally and has no -R flag
  [ "$2" = view ] && [ "$3" = test/repo ] && ! printf '%s\n' "$*" | grep -q -- ' -R ' || { echo "gh: bad repo view args" >&2; exit 1; }
  echo main; exit 0
fi
state= head= base=
while [ $# -gt 0 ]; do
  case "$1" in
    --state) state="$2"; shift ;;
    --head) head="$2"; shift ;;
    --base) base="$2"; shift ;;
  esac
  shift
done
case "$state" in merged) src=merged.json ;; open) src=open.json ;; *) exit 1 ;; esac
jq --arg h "$head" --arg b "$base" \
  '[.[] | select(($h == "" or .headRefName == $h) and ($b == "" or .baseRefName == $b))]' "$d/$src"
EOF
chmod +x "$ROOT/bin/gh"

SEQ=0
EXTRA_PATH=
CASE_FAILS=0
TOTAL_FAILS=0
PASSED=0
FAILED_NAMES=

# ---------------------------------------------------------------- fixtures

mkenv() { # name
  C="$ROOT/$1"
  mkdir -p "$C"/{wt,plans,gh,builds,home,tmp}
  git init -q --bare -b main "$C/origin.git"
  git clone -q "$C/origin.git" "$C/main" 2>/dev/null
  git -C "$C/main" checkout -q -b main 2>/dev/null
  echo base > "$C/main/README"
  git -C "$C/main" add README
  git -C "$C/main" commit -q -m init
  git -C "$C/main" push -q origin main
  git -C "$C/main" remote set-url origin https://github.com/test/repo.git
  git -C "$C/main" config "url.$C/origin.git.insteadOf" https://github.com/test/repo.git
  printf '[]\n' > "$C/gh/merged.json"
  printf '[]\n' > "$C/gh/open.json"
  CASE_FAILS=0
}

mkwt() { # name -> branch b-<name>, one commit, pushed
  git -C "$C/main" worktree add -q "$C/wt/$1" -b "b-$1" main
  echo "$1" > "$C/wt/$1/$1.txt"
  git -C "$C/wt/$1" add "$1.txt"
  git -C "$C/wt/$1" commit -q -m "work $1"
  git -C "$C/wt/$1" push -q origin "b-$1"
}

mkbranch() { # name: branch without worktree, pushed
  mkwt "$1"
  git -C "$C/main" worktree remove "$C/wt/$1"
}

tip() { git -C "$C/main" rev-parse "refs/heads/b-$1"; }

add_merged() { # branch oid [base] [cross] [number] [head-owner] [head-repo]
  SEQ=$((SEQ + 1))
  jq --arg h "$1" --arg o "$2" --arg b "${3:-main}" --argjson c "${4:-false}" --argjson n "${5:-$SEQ}" \
    --arg ro "${6:-}" --arg rn "${7:-}" \
    '. + [{headRefName:$h,headRefOid:$o,baseRefName:$b,isCrossRepository:$c,number:$n,
      headRepositoryOwner:(if $ro == "" then null else {login:$ro} end),
      headRepository:(if $rn == "" then null else {name:$rn} end)}]' \
    "$C/gh/merged.json" > "$C/gh/m.tmp" && mv "$C/gh/m.tmp" "$C/gh/merged.json"
}

mkfork() { # bare fork repo me/repo plus a local remote named fork
  mkdir -p "$C/forks/me"
  git init -q --bare -b main "$C/forks/me/repo.git"
  git -C "$C/main" remote add fork "$C/forks/me/repo.git"
}

mkwtf() { # like mkwt, but pushed to the fork instead of origin
  git -C "$C/main" worktree add -q "$C/wt/$1" -b "b-$1" main
  echo "$1" > "$C/wt/$1/$1.txt"
  git -C "$C/wt/$1" add "$1.txt"
  git -C "$C/wt/$1" commit -q -m "work $1"
  git -C "$C/wt/$1" push -q fork "b-$1"
}

fadd() { # branch oid [base] [owner] [repo]: a merged fork PR row
  add_merged "$1" "$2" "${3:-main}" true "" "${4-me}" "${5-repo}"
}

add_open() { # head base
  jq --arg h "$1" --arg b "$2" '. + [{headRefName:$h,baseRefName:$b,number:9000}]' \
    "$C/gh/open.json" > "$C/gh/o.tmp" && mv "$C/gh/o.tmp" "$C/gh/open.json"
}

merged_wt() { # name: worktree + merged PR whose head is the tip
  mkwt "$1"
  add_merged "b-$1" "$(tip "$1")"
}

write_plan() { # name status pid host updated build_dirs_text [worktree]
  local wt="${7:-$C/wt/$1}"
  {
    printf -- '---\nworktree: %s\nbase_branch: main\nfeature_branch: b-%s\nstatus: %s   # note\n' "$wt" "$1" "$2"
    printf 'pid: %s\nhost: %s\npid_updated: %s\n%s\n---\n\nbody\n' "$3" "$4" "$5" "$6"
  } > "$C/plans/$1.md"
}

NOW() { date -u +%Y-%m-%dT%H:%M:%SZ; }
OLD=2020-01-01T00:00:00Z
HOST="$(hostname)"
sleep 0 & DEAD_PID=$!
wait "$DEAD_PID"

run_s() { # args...
  OUT="$C/out.txt"
  PATH="${EXTRA_PATH:+$EXTRA_PATH:}$ROOT/bin:$PATH" FAKE_GH_DIR="$C/gh" RECLAIM_PLANS_GLOB="$C/plans/*.md" \
    HOME="$C/home" TMPDIR="$C/tmp" /bin/bash "$SCRIPT" --repo "$C/main" "$@" > "$OUT" 2>&1
  RC=$?
}

snapshot() {
  git -C "$C/main" worktree list --porcelain
  git -C "$C/main" for-each-ref
  git -C "$C/origin.git" for-each-ref
  ls -R "$C/wt" "$C/plans" "$C/builds" 2>&1
}

# gitwrap <trigger-arg> <command...>: put a git wrapper on PATH that runs the command once,
# just before the first git call whose args contain the trigger, then execs the real git.
gitwrap() {
  local trigger="$1"
  shift
  mkdir -p "$C/gitwrap"
  # shellcheck disable=SC2016 # the wrapper body is literal shell text
  printf '#!/bin/bash\nfor a in "$@"; do\n  if [ "$a" = %s ] && [ ! -f "%s/moved" ]; then\n    touch "%s/moved"; %s\n  fi\ndone\nexec %s "$@"\n' \
    "$trigger" "$C" "$C" "$*" "$(command -v git)" > "$C/gitwrap/git"
  chmod +x "$C/gitwrap/git"
  EXTRA_PATH="$C/gitwrap"
}

# ---------------------------------------------------------------- assertions

bad() { printf '    FAIL: %s\n' "$1"; [ -f "${OUT:-/nonexistent}" ] && sed 's/^/      | /' "$OUT"; CASE_FAILS=$((CASE_FAILS + 1)); }
a_out() { grep -Eq -- "$1" "$OUT" || bad "output lacks /$1/"; }
a_no_out() { grep -Eq -- "$1" "$OUT" && bad "output has /$1/"; true; }
a_rc() { [ "$RC" = "$1" ] || bad "exit code $RC, want $1"; }
a_rc_nonzero() { [ "$RC" != 0 ] || bad "exit code 0, want non-zero"; }
a_wt() { # name present|absent
  if [ -d "$C/wt/$1" ]; then [ "$2" = present ] || bad "worktree $1 still exists"
  else [ "$2" = absent ] || bad "worktree $1 is gone"; fi
}
a_local() { # branch present|absent
  if git -C "$C/main" show-ref --verify --quiet "refs/heads/$1"; then [ "$2" = present ] || bad "local $1 still exists"
  else [ "$2" = absent ] || bad "local $1 is gone"; fi
}
a_remote() { # branch present|absent
  if git -C "$C/origin.git" show-ref --verify --quiet "refs/heads/$1"; then [ "$2" = present ] || bad "remote $1 still exists"
  else [ "$2" = absent ] || bad "remote $1 is gone"; fi
}
a_fremote() { # branch present|absent
  if git -C "$C/forks/me/repo.git" show-ref --verify --quiet "refs/heads/$1"; then [ "$2" = present ] || bad "fork $1 still exists"
  else [ "$2" = absent ] || bad "fork $1 is gone"; fi
}
a_exists() { [ -e "$1" ] || bad "$1 is gone"; }
a_missing() { [ ! -e "$1" ] || bad "$1 still exists"; }

# ---------------------------------------------------------------- cases

c01_merged_clean() {
  mkenv c01; merged_wt a
  git -C "$C/main" merge -q --no-ff b-a -m merge && git -C "$C/main" push -q origin main
  run_s --apply
  a_rc 0; a_out '^reclaimed a '; a_wt a absent; a_local b-a absent; a_remote b-a absent
  a_no_out '^total .*blocked=[1-9]'
  if grep '^pr list' "$C/gh/calls.log" | grep -qv -- '-R test/repo'; then bad "a gh pr list call lacked -R"; fi
  true
}
c02_squash_tip_not_in_main() {
  mkenv c02; merged_wt a
  run_s --apply
  a_out '^reclaimed a '; a_wt a absent; a_local b-a absent; a_remote b-a absent
}
c02b_tip_behind_merged_head() {
  mkenv c02b; mkwt a
  echo more > "$C/wt/a/more.txt"; git -C "$C/wt/a" add more.txt; git -C "$C/wt/a" commit -q -m more
  head="$(git -C "$C/wt/a" rev-parse HEAD)"
  git -C "$C/wt/a" push -q origin b-a
  git -C "$C/wt/a" reset -q --hard HEAD~1
  add_merged b-a "$head"
  run_s --apply
  a_out '^reclaimed a '; a_wt a absent; a_local b-a absent
}
c03_dirty() {
  mkenv c03; merged_wt a
  echo changed >> "$C/wt/a/a.txt"
  run_s --apply
  a_out '^blocked a .*uncommitted'; a_wt a present; a_local b-a present
}
c04e_node_modules_fixtures_and_more_secret_names() {
  mkenv c04e; merged_wt a; merged_wt b; merged_wt c
  printf 'node_modules/\nbuild/\n' >> "$C/main/.git/info/exclude"
  mkdir -p "$C/wt/a/node_modules/pkg" "$C/wt/b/build" "$C/wt/c/build"
  echo fixture > "$C/wt/a/node_modules/pkg/.env"
  echo state > "$C/wt/b/build/terraform.tfstate.backup"; echo tok > "$C/wt/c/build/.npmrc"
  mkdir -p "$C/outside/x/node_modules/q" "$C/wt/j/node_modules" "$C/wt/k/node_modules"
  printf 'pnpm-top\n' >> "$C/main/.git/info/exclude"
  ln -s "$C/outside/x/node_modules/q" "$C/wt/j/pnpm-top"
  ln -s node_modules/q "$C/wt/k/pnpm-top"
  ln -s node_modules/.. "$C/wt/l/.claude/skills/pnpm-x"
  ln -s ../../../../../../../../node_modules/q "$C/wt/m/.claude/skills/pnpm-x"
  run_s --apply
  a_out '^reclaimed a '; a_wt a absent
  a_out '^blocked b .*tfstate'; a_out '^blocked c .*npmrc'
}
c03b_local_branch_moves_before_delete() {
  mkenv c03b; merged_wt a
  gitwrap update-ref "$(command -v git) -C \"$C/main\" update-ref refs/heads/b-a \$($(command -v git) -C \"$C/main\" commit-tree b-a^{tree} -p b-a -m late)"
  run_s --apply
  EXTRA_PATH=
  a_out '^blocked a .*deleting local'; a_local b-a present; a_remote b-a present
}
c03c_branch_held_by_second_worktree() {
  mkenv c03c; merged_wt a
  git -C "$C/main" worktree add -q -f "$C/wt/a2" b-a
  run_s --apply
  a_out '^blocked a .*checked out in another worktree'; a_out '^reclaimed a2 '; a_local b-a absent
}
c04_ignored_env() {
  mkenv c04; merged_wt a
  echo .env >> "$C/main/.git/info/exclude"; echo secret > "$C/wt/a/.env"
  run_s --apply
  a_out '^blocked a .*\.env'; a_wt a present
}
c04b_nested_build_output_allowed_nested_env_blocked() {
  mkenv c04b; merged_wt a; merged_wt b; merged_wt c
  printf 'build/\nnode_modules/\ndist/\ntest-results/\n.env.local\n' >> "$C/main/.git/info/exclude"
  mkdir -p "$C/wt/a/src/build" "$C/wt/a/frontend/dist" "$C/wt/a/frontend/test-results" "$C/wt/b/node_modules" "$C/wt/c/frontend"
  echo k > "$C/wt/a/src/keep.txt"; echo k > "$C/wt/a/frontend/keep.txt"; echo k > "$C/wt/c/frontend/keep.txt"
  git -C "$C/wt/a" add -A; git -C "$C/wt/a" commit -q -m keep; git -C "$C/wt/a" push -q origin b-a
  git -C "$C/wt/c" add -A; git -C "$C/wt/c" commit -q -m keep; git -C "$C/wt/c" push -q origin b-c
  add_merged b-a "$(tip a)"; add_merged b-c "$(tip c)"
  echo x > "$C/wt/a/src/build/o"; echo x > "$C/wt/a/frontend/dist/o"; echo x > "$C/wt/a/frontend/test-results/o"; echo x > "$C/wt/b/node_modules/o"
  echo secret > "$C/wt/c/frontend/.env.local"
  run_s --apply
  a_out '^reclaimed a '; a_wt a absent; a_out '^reclaimed b '; a_wt b absent
  a_out '^blocked c .*frontend/\.env\.local'; a_wt c present
}
c04c_tracked_build_dir_and_secret_names_blocked() {
  mkenv c04c; merged_wt a; merged_wt b; merged_wt c
  printf 'build/\ndist/\n.env\n' >> "$C/main/.git/info/exclude"
  mkdir -p "$C/wt/a/build" "$C/wt/b/dist"
  echo run > "$C/wt/a/build/run.sh"; git -C "$C/wt/a" add -f build/run.sh; git -C "$C/wt/a" commit -q -m run
  git -C "$C/wt/a" push -q origin b-a; add_merged b-a "$(tip a)"
  echo secret > "$C/wt/a/build/.env"; echo secret > "$C/wt/b/dist/secret.pem"
  mkdir -p "$C/wt/c/frontend/dist"; echo k > "$C/wt/c/frontend/keep.txt"
  git -C "$C/wt/c" add -A; git -C "$C/wt/c" commit -q -m keep; git -C "$C/wt/c" push -q origin b-c; add_merged b-c "$(tip c)"
  echo x > "$C/wt/c/frontend/dist/o"
  run_s --apply
  a_out '^blocked a .*build'; a_wt a present
  a_out '^blocked b .*secret.pem'; a_wt b present
  a_out '^reclaimed c '; a_wt c absent
}
c04d_tracked_build_dir_with_ignored_output_blocked() {
  mkenv c04d; merged_wt a
  echo 'build/*.o' >> "$C/main/.git/info/exclude"
  mkdir -p "$C/wt/a/build"; echo run > "$C/wt/a/build/run.sh"
  git -C "$C/wt/a" add -f build/run.sh; git -C "$C/wt/a" commit -q -m run
  git -C "$C/wt/a" push -q origin b-a; add_merged b-a "$(tip a)"
  echo obj > "$C/wt/a/build/out.o"
  run_s --apply
  a_out '^blocked a .*build/out.o'; a_wt a present
}
# quoted_fixture <name> <dir> <files...>: a merged worktree with the files ignored under <dir>;
# TRACK=1 tracks <dir>/keep.txt, otherwise only a sibling src.txt is tracked.
quoted_fixture() {
  local n="$1" d="$2"; shift 2
  mkenv "$n"; merged_wt a
  printf 'build/\n' >> "$C/main/.git/info/exclude"
  mkdir -p "$C/wt/a/$d"
  local k="${d%/build}/src.txt"
  [ -z "${TRACK:-}" ] || k="$d/keep.txt"
  echo k > "$C/wt/a/$k"; git -C "$C/wt/a" add -f -- "$k"; git -C "$C/wt/a" commit -q -m keep
  git -C "$C/wt/a" push -q origin b-a; add_merged b-a "$(tip a)"
  local f; for f in "$@"; do echo x > "$C/wt/a/$d/$f"; done
}
c04f_spaced_path_secret_blocked() {
  TRACK=1 quoted_fixture c04f "a b/build" prod.pem
  run_s --apply
  a_out '^blocked a .*prod.pem'; a_wt a present
  [ -f "$C/wt/a/a b/build/prod.pem" ] || bad "secret file was deleted"
}
c04g_spaced_path_secret_names_blocked() {
  TRACK=1 quoted_fixture c04g "a b/build" x.tfvars
  run_s --apply
  a_out '^blocked a .*x.tfvars'; a_wt a present
  [ -f "$C/wt/a/a b/build/x.tfvars" ] || bad "x.tfvars was deleted"
}
c04h_non_ascii_path_secret_blocked() {
  TRACK=1 quoted_fixture c04h "café/build" k.key
  run_s --apply
  a_out '^blocked a .*k.key'; a_wt a present
}
c04i_spaced_build_dir_harmless_output_reclaimed() {
  quoted_fixture c04i "a b/build" out.js
  run_s --apply
  a_out '^reclaimed a '; a_wt a absent
}
c04j_tab_and_newline_paths_no_misparse() {
  TRACK=1 quoted_fixture c04j "t	b/build" prod.pem
  mkdir -p "$C/wt/a/n
l/build"; echo k > "$C/wt/a/n
l/src.txt"; echo x > "$C/wt/a/n
l/build/out.js"
  git -C "$C/wt/a" add -- "n
l/src.txt"; git -C "$C/wt/a" commit -q -m nl
  git -C "$C/wt/a" push -q origin b-a; add_merged b-a "$(tip a)"
  run_s --apply
  a_out '^blocked a .*prod.pem'; a_wt a present
  [ -f "$C/wt/a/t	b/build/prod.pem" ] || bad "tab-path secret was deleted"
}
c04k_ignored_links_into_build_dirs() {
  mkenv c04k
  mkdir -p "$C/main/.claude/skills"; echo 'pnpm-*' > "$C/main/.claude/skills/.gitignore"
  git -C "$C/main" add -A; git -C "$C/main" commit -q -m skills; git -C "$C/main" push -q origin main
  local w
  for w in a b c d e f g h i j k l m; do merged_wt "$w"; done
  printf 'node_modules/\nbuild/\n' >> "$C/main/.git/info/exclude"
  mkdir -p "$C/outside/skill" "$C/wt/g-evil/node_modules/x"
  for w in a b c d e f g h i j k l m; do mkdir -p "$C/wt/$w/.claude/skills"; done
  mkdir -p "$C/wt/a/node_modules/.pnpm/p/skills/x"
  ln -s ../../node_modules/.pnpm/p/skills/x "$C/wt/a/.claude/skills/pnpm-x"
  ln -s "$C/outside/skill" "$C/wt/b/.claude/skills/pnpm-x"
  ln -s ../../node_modules/missing "$C/wt/c/.claude/skills/pnpm-x"
  mkdir -p "$C/wt/d/build"; echo run > "$C/wt/d/build/run.sh"
  git -C "$C/wt/d" add -f build/run.sh; git -C "$C/wt/d" commit -q -m run; git -C "$C/wt/d" push -q origin b-d
  add_merged b-d "$(tip d)"
  ln -s ../../build/run.sh "$C/wt/d/.claude/skills/pnpm-x"
  echo regular > "$C/wt/e/.claude/skills/pnpm-x"
  mkdir -p "$C/wt/f/node_modules/.pnpm"; echo f > "$C/wt/f/node_modules/.pnpm/file"
  ln -s ../../node_modules/.pnpm/file "$C/wt/f/.claude/skills/pnpm-x"
  ln -s "$C/wt/g-evil/node_modules/x" "$C/wt/g/.claude/skills/pnpm-x"
  ln -s ../../../b/node_modules/x "$C/wt/h/.claude/skills/pnpm-x"
  mkdir -p "$C/wt/i/node_modules"
  ln -s "$C/outside/skill" "$C/wt/i/node_modules/x"
  ln -s ../../node_modules/x "$C/wt/i/.claude/skills/pnpm-x"
  printf 'pnpm-top\n' >> "$C/main/.git/info/exclude"
  mkdir -p "$C/outside/x/node_modules/q" "$C/wt/j/node_modules" "$C/wt/k/node_modules"
  ln -s "$C/outside/x/node_modules/q" "$C/wt/j/pnpm-top"
  ln -s node_modules/q "$C/wt/k/pnpm-top"
  ln -s node_modules/.. "$C/wt/l/.claude/skills/pnpm-x"
  ln -s ../../../../../../../../node_modules/q "$C/wt/m/.claude/skills/pnpm-x"
  run_s --apply
  a_out '^reclaimed a '; a_wt a absent
  a_out '^blocked b .*pnpm-x'; a_wt b present
  a_out '^reclaimed c '; a_wt c absent
  a_out '^blocked d .*pnpm-x'; a_wt d present
  a_out '^blocked e .*pnpm-x'; a_wt e present
  a_out '^reclaimed f '; a_wt f absent
  a_out '^blocked g .*pnpm-x'; a_wt g present
  a_out '^blocked h .*pnpm-x'; a_wt h present
  a_out '^reclaimed i '; a_wt i absent
  a_out '^blocked j .*pnpm-top'; a_wt j present
  a_out '^reclaimed k '; a_wt k absent
  a_out '^blocked l .*pnpm-x'; a_wt l present
  a_out '^blocked m .*pnpm-x'; a_wt m present
  a_exists "$C/outside/skill"; a_exists "$C/wt/g-evil/node_modules/x"
}
c04l_ignored_link_secret_name() {
  mkenv c04l; merged_wt a
  printf 'node_modules/\n.env*\n' >> "$C/main/.git/info/exclude"
  mkdir -p "$C/wt/a/node_modules/d"
  ln -s node_modules/d "$C/wt/a/.env.link"
  run_s --apply
  a_out '^blocked a .*\.env\.link'; a_wt a present
}
c04m_link_to_root_blocked() {
  mkenv c04m
  mkdir -p "$C/main/.claude/skills"; echo 'pnpm-*' > "$C/main/.claude/skills/.gitignore"
  git -C "$C/main" add -A; git -C "$C/main" commit -q -m skills; git -C "$C/main" push -q origin main
  merged_wt a; merged_wt b
  echo pnpm-top >> "$C/main/.git/info/exclude"
  ln -s / "$C/wt/a/pnpm-top"
  mkdir -p "$C/wt/b/.claude/skills"
  ln -s ../../../../../../../../.. "$C/wt/b/.claude/skills/pnpm-x"
  run_s --apply
  a_out '^blocked a .*ignored file not on the build allowlist: pnpm-top'; a_wt a present
  a_out '^blocked b .*ignored file not on the build allowlist: .*pnpm-x'; a_wt b present
}
c05b_plan_md_not_a_plan_symlink() {
  mkenv c05b; merged_wt a; merged_wt b
  echo plan.md >> "$C/main/.git/info/exclude"
  echo regular > "$C/wt/a/plan.md"
  echo x > "$C/elsewhere.md"; ln -s "$C/elsewhere.md" "$C/wt/b/plan.md"
  run_s --apply
  a_out '^blocked a .*plan.md'; a_wt a present; a_out '^blocked b .*plan.md'; a_wt b present
}
c08b_remote_moves_before_push() {
  mkenv c08b; merged_wt a
  git clone -q "$C/origin.git" "$C/other" 2>/dev/null
  git -C "$C/other" checkout -q b-a
  echo late > "$C/other/late.txt"; git -C "$C/other" add late.txt; git -C "$C/other" commit -q -m late
  # The remote must move after the script's ls-remote/fetch but before its push connects, so a
  # git wrapper advances it just before the first push. Server-side compare-and-swap alone
  # would hide a missing --force-with-lease.
  gitwrap push "$(command -v git) -C \"$C/other\" push -q origin b-a"
  run_s --apply
  EXTRA_PATH=
  a_out '^ask a .*remote delete failed'; a_remote b-a present
}
c13d_build_dir_canonical_git_and_cwd() {
  mkenv c13d; merged_wt a
  mkdir -p "$C/builds/real/good" "$C/builds/ok" "$C/builds/repo/.git" "$C/builds/live" "$C/builds/dots"
  ln -s "$C/builds/real" "$C/builds/lnk"
  write_plan a in-progress "$DEAD_PID" "$HOST" "$OLD" "build_dirs:
  - $C/builds/ok/
  - $C/builds/dots/../dots
  - $C/builds/lnk/good
  - $C/builds/repo
  - $C/builds/live"
  (cd "$C/builds/live" && exec sleep 120) & p1=$!
  sleep 0.5
  run_s
  a_out 'build_dir .*/ok/ \(not canonical'; a_out 'dots/\.\./dots \(not canonical'; a_out 'lnk/good \(not canonical'
  a_out 'builds/repo \(contains a git checkout'; a_out 'builds/live \(a live process'
  run_s --apply
  kill "$p1" 2>/dev/null
  a_exists "$C/builds/ok"; a_exists "$C/builds/dots"; a_exists "$C/builds/real/good"; a_exists "$C/builds/repo"; a_exists "$C/builds/live"
}
c05_plan_symlink_only() {
  mkenv c05; merged_wt a
  write_plan a merged "" "$HOST" "$OLD" 'build_dirs: []'
  ln -s "$C/plans/a.md" "$C/wt/a/plan.md"; echo plan.md >> "$C/main/.git/info/exclude"
  run_s --apply
  a_out '^reclaimed a '; a_wt a absent; a_exists "$C/plans/archive/a.md"; a_missing "$C/plans/a.md"
  grep -q '^status: merged' "$C/plans/archive/a.md" || bad "archived plan status not merged"
}
c06_extra_commits() {
  mkenv c06; mkwt a
  add_merged b-a "$(tip a)"
  echo more > "$C/wt/a/more.txt"; git -C "$C/wt/a" add more.txt; git -C "$C/wt/a" commit -q -m more
  run_s --apply
  a_out '^ask a '; a_wt a present; a_local b-a present; a_remote b-a present
}
c07_fork_pr_same_name() {
  mkenv c07; mkwt a
  add_merged b-a "$(tip a)" main true
  run_s --apply
  a_out '^ask a .*fork'; a_wt a present; a_local b-a present
}
c08_remote_ahead() {
  mkenv c08; merged_wt a
  git clone -q "$C/origin.git" "$C/other" 2>/dev/null
  git -C "$C/other" checkout -q b-a
  echo extra > "$C/other/extra.txt"; git -C "$C/other" add extra.txt; git -C "$C/other" commit -q -m extra
  git -C "$C/other" push -q origin b-a
  run_s --apply
  a_out '^ask a .*remote ahead'; a_remote b-a present
}
c09_open_pr_head() {
  mkenv c09; merged_wt a; add_open b-a main
  run_s --apply
  a_out '^skip a '; a_wt a present; a_local b-a present; a_remote b-a present
  run_s --apply --branch b-a
  a_out '^skip a '; a_wt a present; a_remote b-a present
}
c10_stacked_open_base() {
  mkenv c10; merged_wt a; add_open other b-a
  run_s --apply
  a_out '^skip a '; a_wt a present; a_remote b-a present
  run_s --apply --branch b-a
  a_out '^skip a '; a_remote b-a present
}
c11_live_cwd_prefix_and_symlink() {
  mkenv c11; merged_wt foo; merged_wt foo-bar
  ln -s "$C/wt" "$C/link"
  (cd "$C/link/foo" && exec sleep 120) & p1=$!
  sleep 0.5
  run_s
  kill "$p1" 2>/dev/null
  a_out '^blocked foo .*cwd'; a_out '^reclaimable foo-bar '
  (cd "$C/link/foo-bar" && exec sleep 120) & p2=$!
  sleep 0.5
  run_s
  kill "$p2" 2>/dev/null
  a_out '^reclaimable foo '; a_out '^blocked foo-bar .*cwd'
}
c12_plan_liveness() {
  mkenv c12; merged_wt live; merged_wt dead; merged_wt farold; merged_wt farnew
  write_plan live in-progress "$$" "$HOST" "$OLD" 'build_dirs: []'
  write_plan dead in-progress "$DEAD_PID" "$HOST" "$(NOW)" 'build_dirs: []'
  write_plan farold in-progress 1 otherhost.invalid "$OLD" 'build_dirs: []'
  write_plan farnew in-progress 1 otherhost.invalid "$(NOW)" 'build_dirs: []'
  run_s
  a_out '^blocked live .*live owner'; a_out '^reclaimable dead '
  a_out '^reclaimable farold '; a_out '^blocked farnew '
}
c13_build_dirs_report_refusals() {
  mkenv c13; merged_wt a
  mkdir -p "$C/builds/real" "$C/builds/good"; ln -s "$C/builds/real" "$C/builds/link"
  write_plan a in-progress "$DEAD_PID" "$HOST" "$OLD" "build_dirs:
  - /
  - $C/home
  - $C/builds/link
  - $C/main
  - $C
  - $C/wt
  - $C/builds/good"
  run_s
  a_out 'refused build_dir / '; a_out "refused build_dir $C/home "; a_out "build_dir $C/builds/link \\(is a symlink"
  a_out "build_dir $C/main "; a_out "build_dir $C \\("; a_out "build_dir $C/wt "
  a_no_out "refused build_dir $C/builds/good"
  a_exists "$C/builds/good"
}
c13b_build_dirs_apply() {
  mkenv c13b; merged_wt a
  mkdir -p "$C/builds/real" "$C/builds/good" "$C/builds/shared" "$C/outside"; ln -s "$C/builds/real" "$C/builds/link"
  write_plan a in-progress "$DEAD_PID" "$HOST" "$OLD" "build_dirs:
  - $C/home
  - $C/builds/link
  - $C/main
  - $C
  - $C/wt
  - $C/builds/shared
  - $C/builds/good"
  printf -- '---\nworktree: %s/wt/other\nstatus: in-progress\npid: %s\nhost: %s\npid_updated: %s\nbuild_dirs: [%s/builds/shared]\n---\n' \
    "$C" "$$" "$HOST" "$(NOW)" "$C" > "$C/plans/other.md"
  run_s --apply
  a_out '^reclaimed a '; a_missing "$C/builds/good"
  a_exists "$C/home"; a_exists "$C/builds/link"; a_exists "$C/builds/real"; a_exists "$C/main/README"
  a_exists "$C/wt"; a_exists "$C/builds/shared"; a_exists "$C/outside"
}
c13c_build_dir_outside_allowed_roots() {
  mkenv c13c; merged_wt a
  outside="$(mktemp -d /var/tmp/reclaim-outside.XXXXXX)" || { bad "cannot create outside dir"; return; }
  outside="$(cd "$outside" && pwd -P)"
  write_plan a in-progress "$DEAD_PID" "$HOST" "$OLD" "build_dirs:
  - $outside"
  run_s --apply
  a_out '^reclaimed a .*refused build_dir .*allowed root'; a_exists "$outside"
  rmdir "$outside" 2>/dev/null
}
c14_gh_failure_aborts() {
  mkenv c14; merged_wt a
  touch "$C/gh/fail"
  run_s --apply
  a_rc_nonzero; a_wt a present; a_local b-a present; a_remote b-a present
  rm "$C/gh/fail"
  jq -n '[range(1000) | {number: ., headRefName: "x\(.)", baseRefName: "main"}]' > "$C/gh/open.json"
  run_s --apply
  a_rc_nonzero; a_out 'equal to the limit'; a_wt a present; a_local b-a present; a_remote b-a present
}
c15_submodule_refused_and_c17_isolation() {
  mkenv c15
  git init -q -b main "$C/subsrc"; echo s > "$C/subsrc/s"; git -C "$C/subsrc" add s; git -C "$C/subsrc" commit -q -m s
  git -C "$C/main" -c protocol.file.allow=always submodule add -q "$C/subsrc" sub 2>/dev/null
  git -C "$C/main" commit -q -m sub; git -C "$C/main" push -q origin main
  mkwt asub
  git -C "$C/wt/asub" -c protocol.file.allow=always submodule update -q --init 2>/dev/null
  add_merged b-asub "$(tip asub)"
  merged_wt bclean
  run_s --apply
  a_out '^blocked asub .*worktree remove refused'; a_wt asub present
  a_out '^reclaimed bclean '; a_wt bclean absent
}
c16_prunable() {
  mkenv c16; mkwt a; add_merged b-a "$(tip a)"
  rm -rf "$C/wt/a"
  run_s --apply
  a_out '^prunable a '; a_local b-a present
}
c18_branch_mode_worktree_gone() {
  mkenv c18; mkbranch a; add_merged b-a "$(tip a)"
  run_s --apply --branch b-a
  a_out '^reclaimed b-a '; a_local b-a absent; a_remote b-a absent
}
c19_report_mode_changes_nothing() {
  mkenv c19; merged_wt a; merged_wt b; mkbranch c; add_merged b-c "$(tip c)"; echo x >> "$C/wt/b/b.txt"
  write_plan a merged "" "$HOST" "$OLD" 'build_dirs: []'
  snapshot > "$C/before.txt" 2>&1
  run_s
  snapshot > "$C/after.txt" 2>&1
  a_rc 0; a_out '^reclaimable a '; a_out '^blocked b '; a_out '^reclaimable b-c '; a_out 'mode=report'
  cmp -s "$C/before.txt" "$C/after.txt" || bad "report mode changed state"
}
c20_locked() {
  mkenv c20; merged_wt a; git -C "$C/main" worktree lock "$C/wt/a"
  run_s --apply
  a_out '^blocked a .*locked'; a_wt a present
  run_s
  a_out '^blocked a .*locked'; a_no_out '^reclaimable a '
}
c21_no_pr_and_detached() {
  mkenv c21; mkwt a; mkwt d; git -C "$C/wt/d" checkout -q --detach
  run_s --apply
  a_out '^skip a .*no merged PR'; a_out '^skip d .*detached'; a_wt a present
}
c22_non_default_base() {
  mkenv c22; mkwt a; add_merged b-a "$(tip a)" release
  run_s --apply
  a_out '^ask a .*release'; a_wt a present; a_remote b-a present
}
c23_pr_head_not_fetchable() {
  mkenv c23; mkwt a
  add_merged b-a 1111111111111111111111111111111111111111
  run_s --apply
  a_out '^ask a .*fetch'; a_wt a present
}
c24_local_branch_pass() {
  mkenv c24; mkbranch a; add_merged b-a "$(tip a)"
  run_s --apply
  a_out '^reclaimed b-a '; a_local b-a absent; a_remote b-a absent
}

c25_fork_pr_without_flag() {
  mkenv c25; mkfork; mkwtf a; fadd b-a "$(tip a)"
  touch "$C/gh/api_fail"
  run_s --apply
  a_rc 0; a_out '^ask a .*only fork PRs merged'; a_wt a present; a_local b-a present; a_fremote b-a present
  if grep -q 'api user' "$C/gh/calls.log"; then bad "a run without the flag called gh api user"; fi
  true
}
c26_fork_tip_is_head() {
  mkenv c26; mkfork; mkwtf a; fadd b-a "$(tip a)"
  before="$(git -C "$C/origin.git" for-each-ref)"
  run_s --apply --allow-fork
  a_rc 0; a_out '^reclaimed a .*deleted fork branch'; a_wt a absent; a_local b-a absent; a_fremote b-a absent
  [ "$(git -C "$C/origin.git" for-each-ref)" = "$before" ] || bad "origin refs changed"
}
c27_fork_tip_behind_head() {
  mkenv c27; mkfork; mkwtf a
  echo more > "$C/wt/a/more.txt"; git -C "$C/wt/a" add more.txt; git -C "$C/wt/a" commit -q -m more
  head="$(git -C "$C/wt/a" rev-parse HEAD)"
  git -C "$C/wt/a" push -q fork b-a
  git -C "$C/wt/a" reset -q --hard HEAD~1
  fadd b-a "$head"
  run_s --apply --allow-fork
  a_out '^reclaimed a '; a_wt a absent; a_local b-a absent; a_fremote b-a absent
}
c28_fork_extra_commit() {
  mkenv c28; mkfork; mkwtf a; fadd b-a "$(tip a)"
  echo more > "$C/wt/a/more.txt"; git -C "$C/wt/a" add more.txt; git -C "$C/wt/a" commit -q -m more
  run_s --apply --allow-fork
  a_out '^ask a .*not at a merged PR head'; a_wt a present; a_local b-a present; a_fremote b-a present
}
c29_fork_non_default_base() {
  mkenv c29; mkfork; mkwtf a; fadd b-a "$(tip a)" release
  run_s --apply --allow-fork
  a_out '^ask a .*release'; a_wt a present; a_fremote b-a present
}
c30_fork_stash_blocks() {
  mkenv c30; mkfork; mkwtf 'x+1'; mkwtf y
  fadd 'b-x+1' "$(tip 'x+1')"; fadd b-y "$(tip y)"
  echo d >> "$C/wt/x+1/x+1.txt"; git -C "$C/wt/x+1" stash push -q -m m
  echo d >> "$C/wt/y/y.txt"; git -C "$C/wt/y" stash push -q
  run_s --apply --allow-fork
  a_out '^blocked x\+1 .*stash entry on b-x\+1'; a_out '^blocked y .*stash entry on b-y'
  a_wt 'x+1' present; a_wt y present; a_fremote 'b-x+1' present; a_fremote b-y present
}
c30b_fork_stash_on_other_branch_does_not_block() {
  mkenv c30b; mkfork; mkwtf x.1; mkwtf x.1-2; mkwtf xy1
  fadd b-x.1 "$(tip x.1)"
  echo d >> "$C/wt/x.1-2/x.1-2.txt"; git -C "$C/wt/x.1-2" stash push -q -m m
  echo d >> "$C/wt/xy1/xy1.txt"; git -C "$C/wt/xy1" stash push -q -m m
  run_s --apply --allow-fork
  a_out '^reclaimed x\.1 '; a_wt x.1 absent
}
c31_fork_keeps_same_named_origin_branch() {
  mkenv c31; mkfork; mkwtf a; git -C "$C/wt/a" push -q origin b-a; fadd b-a "$(tip a)"
  run_s --apply --allow-fork
  a_out '^reclaimed a .*origin b-a kept \(fork PR\)'; a_remote b-a present; a_fremote b-a absent; a_local b-a absent
}
c32_fork_third_party_owner() {
  mkenv c32; mkfork; mkwtf a; fadd b-a "$(tip a)" main bob
  mkdir -p "$C/forks/bob"; git init -q --bare -b main "$C/forks/bob/repo.git"
  git -C "$C/main" remote add bobfork "$C/forks/bob/repo.git"
  run_s --apply --allow-fork
  a_out '^ask a .*from bob, not me'; a_wt a present; a_local b-a present; a_fremote b-a present
}
c33_fork_null_head_repository() {
  mkenv c33; mkfork; mkwtf a; fadd b-a "$(tip a)" main me ""
  run_s --apply --allow-fork
  a_out '^ask a .*no head repository'; a_wt a present; a_fremote b-a present
}
c34_fork_without_local_remote() {
  mkenv c34; mkfork; mkwtf a; fadd b-a "$(tip a)" main me other
  run_s --apply --allow-fork
  a_out '^ask a .*no local remote'; a_wt a present; a_fremote b-a present
}
c35_fork_remote_ahead() {
  mkenv c35; mkfork; mkwtf a; fadd b-a "$(tip a)"
  git clone -q "$C/forks/me/repo.git" "$C/other" 2>/dev/null
  git -C "$C/other" checkout -q b-a
  echo extra > "$C/other/extra.txt"; git -C "$C/other" add extra.txt; git -C "$C/other" commit -q -m extra
  git -C "$C/other" push -q origin b-a
  run_s --apply --allow-fork
  a_out '^ask a .*fork branch ahead'; a_fremote b-a present
}
c36_fork_match_beside_same_repo_row() {
  mkenv c36; mkfork; mkwtf a; git -C "$C/wt/a" push -q origin b-a; same="$(tip a)"
  echo more > "$C/wt/a/more.txt"; git -C "$C/wt/a" add more.txt; git -C "$C/wt/a" commit -q -m more
  git -C "$C/wt/a" push -q fork b-a
  add_merged b-a "$same"; fadd b-a "$(tip a)"
  run_s --apply --allow-fork
  a_out '^reclaimed a .*origin b-a kept \(fork PR\)'; a_remote b-a present; a_fremote b-a absent; a_local b-a absent
}
c37_fork_https_remote_uppercase_owner() {
  mkenv c37; mkwt a; fadd b-a "$(tip a)" main Me Repo
  git -C "$C/main" config --unset-all "url.$C/origin.git.insteadOf"
  git -C "$C/main" remote add fork https://github.com/Me/Repo.git
  run_s --allow-fork
  a_out '^reclaimable a '
  git -C "$C/main" remote set-url fork https://evil.example/me/repo.git
  run_s --allow-fork
  a_out '^ask a .*no local remote'
}
c38_fork_local_branch_without_worktree() {
  mkenv c38; mkfork; mkwtf a; git -C "$C/main" worktree remove "$C/wt/a"; fadd b-a "$(tip a)"
  run_s --apply --allow-fork --branch b-a
  a_out '^reclaimed b-a '; a_local b-a absent; a_fremote b-a absent
  mkenv c38b; mkfork; mkwtf a; git -C "$C/main" worktree remove "$C/wt/a"; fadd b-a "$(tip a)"
  run_s --apply --allow-fork
  a_out '^reclaimed b-a '; a_local b-a absent; a_fremote b-a absent
}
c39_fork_open_pr_same_head_name() {
  mkenv c39; mkfork; mkwtf a; fadd b-a "$(tip a)"; add_open b-a main
  run_s --apply --allow-fork
  a_out '^skip a '; a_wt a present; a_fremote b-a present
}
c40_fork_pr_head_not_fetchable() {
  mkenv c40; mkfork; mkwtf a; fadd b-a 1111111111111111111111111111111111111111
  run_s --apply --allow-fork
  a_out '^ask a .*fetch'; a_wt a present; a_fremote b-a present
}
c41_unknown_flag() {
  mkenv c41
  run_s --allow-forks
  a_rc 2
}
c42_gh_user_failure_aborts_only_with_flag() {
  mkenv c42; mkfork; mkwtf a; fadd b-a "$(tip a)"
  touch "$C/gh/api_fail"
  run_s --apply --allow-fork
  a_rc 3; a_out 'gh api user'; a_wt a present; a_local b-a present; a_fremote b-a present
}

c43_fork_remote_moves_before_push() {
  mkenv c43; mkfork; mkwtf a; fadd b-a "$(tip a)"
  git clone -q "$C/forks/me/repo.git" "$C/other" 2>/dev/null
  git -C "$C/other" checkout -q b-a
  echo late > "$C/other/late.txt"; git -C "$C/other" add late.txt; git -C "$C/other" commit -q -m late
  gitwrap push "$(command -v git) -C \"$C/other\" push -q origin b-a"
  run_s --apply --allow-fork
  EXTRA_PATH=
  a_out '^ask a .*fork branch delete failed'; a_fremote b-a present
  [ "$(git -C "$C/forks/me/repo.git" rev-parse refs/heads/b-a)" = "$(git -C "$C/other" rev-parse HEAD)" ] || bad "fork branch not at the moved sha"
}

c44_fork_oids_bound_to_selected_repo() {
  mkenv c44; mkfork; mkwtf a
  git init -q --bare -b main "$C/forks/me/repo2.git"
  git -C "$C/main" remote add fork2 "$C/forks/me/repo2.git"
  fadd b-a "$(tip a)"
  echo more > "$C/wt/a/more.txt"; git -C "$C/wt/a" add more.txt; git -C "$C/wt/a" commit -q -m more
  git -C "$C/wt/a" push -q fork b-a
  fadd b-a "$(git -C "$C/wt/a" rev-parse HEAD)" main me repo2
  git -C "$C/wt/a" reset -q --hard HEAD~1
  run_s --apply --allow-fork
  a_out '^ask a .*fork branch ahead'; a_fremote b-a present
}

c45_fork_base_bound_to_selected_repo() {
  mkenv c45; mkfork; mkwtf a
  git init -q --bare -b main "$C/forks/me/repo2.git"
  git -C "$C/main" remote add fork2 "$C/forks/me/repo2.git"
  fadd b-a "$(tip a)" release
  fadd b-a "$(tip a)" main me repo2
  run_s --apply --allow-fork
  a_out '^ask a .*release'; a_wt a present; a_local b-a present; a_fremote b-a present
}
declare_on_main() { # content (printf format, no trailing newline added)
  printf '%b' "$1" > "$C/main/.reclaim-build-outputs"
  git -C "$C/main" add .reclaim-build-outputs
  git -C "$C/main" commit -q -m declare
  git -C "$C/main" push -q origin main
}

gen_wt() { # name path: merged worktree holding one ignored file at path
  local d
  mkwt "$1"
  d="$(dirname "$2")"
  if [ "$d" != . ]; then
    mkdir -p "$C/wt/$1/$d"; echo k > "$C/wt/$1/$d/keep.txt"
    git -C "$C/wt/$1" add -A; git -C "$C/wt/$1" commit -q -m keep; git -C "$C/wt/$1" push -q origin "b-$1"
  fi
  add_merged "b-$1" "$(tip "$1")"
  printf '%s\n' "$2" | sed 's/[][*?]/\\&/g' >> "$C/main/.git/info/exclude"
  echo gen > "$C/wt/$1/$2"
}

d01_declared_on_default_reclaims() {
  mkenv d01; gen_wt a src/route.gen.ts
  declare_on_main '# generated *.ts [x]\n\nsrc/route.gen.ts\n'
  run_s --apply
  a_out '^reclaimed a '; a_wt a absent; a_no_out 'warning'
}
d01b_last_line_without_newline() {
  mkenv d01b; gen_wt a src/route.gen.ts
  declare_on_main '# c\nsrc/route.gen.ts'
  run_s --apply
  a_out '^reclaimed a '; a_wt a absent
}
d02_declared_only_on_branch_blocked() {
  mkenv d02; gen_wt a src/route.gen.ts
  printf 'src/route.gen.ts\n' > "$C/wt/a/.reclaim-build-outputs"
  git -C "$C/wt/a" add .reclaim-build-outputs; git -C "$C/wt/a" commit -q -m decl
  git -C "$C/wt/a" push -q origin b-a; add_merged b-a "$(tip a)"
  run_s --apply
  a_out '^blocked a .*route.gen.ts'; a_wt a present
}
d03_no_declaration_file_unchanged() {
  mkenv d03; gen_wt a src/route.gen.ts
  run_s --apply
  a_out '^blocked a .*route.gen.ts'; a_no_out 'warning'; a_wt a present
}
d04_stale_ref_refreshed() {
  mkenv d04; gen_wt a src/route.gen.ts
  declare_on_main 'src/route.gen.ts\n'
  git clone -q "$C/origin.git" "$C/other" 2>/dev/null
  git -C "$C/other" rm -q .reclaim-build-outputs; git -C "$C/other" commit -q -m undeclare
  git -C "$C/other" push -q origin main
  run_s --apply
  a_out '^blocked a .*route.gen.ts'; a_wt a present
}
d05_fetch_fails_means_no_declarations() {
  mkenv d05; gen_wt a src/route.gen.ts
  declare_on_main 'src/route.gen.ts\n'
  mkdir -p "$C/gitwrap"
  # shellcheck disable=SC2016 # the wrapper body is literal shell text
  printf '#!/bin/bash\nfor a in "$@"; do\n  [ "$a" = "+refs/heads/main:refs/remotes/origin/main" ] && exit 1\ndone\nexec %s "$@"\n' "$(command -v git)" > "$C/gitwrap/git"
  chmod +x "$C/gitwrap/git"; EXTRA_PATH="$C/gitwrap"
  run_s --apply
  EXTRA_PATH=
  a_out '^blocked a .*route.gen.ts'; a_out 'warning.*declared'; a_rc 0; a_wt a present
}
d05c_fetch_fails_and_ref_missing() {
  mkenv d05c; gen_wt a src/route.gen.ts
  declare_on_main 'src/route.gen.ts\n'
  mkdir -p "$C/gitwrap"
  # shellcheck disable=SC2016 # the wrapper body is literal shell text
  printf '#!/bin/bash\nfor a in "$@"; do\n  [ "$a" = "+refs/heads/main:refs/remotes/origin/main" ] && exit 1\ndone\nexec %s "$@"\n' "$(command -v git)" > "$C/gitwrap/git"
  chmod +x "$C/gitwrap/git"; EXTRA_PATH="$C/gitwrap"
  git -C "$C/main" update-ref -d refs/remotes/origin/main
  run_s --apply
  EXTRA_PATH=
  a_out '^blocked a .*route.gen.ts'; a_out 'warning.*declared'; a_rc 0; a_wt a present
}
d05b_narrow_refspec_clone() {
  mkenv d05b; gen_wt a src/route.gen.ts
  declare_on_main 'src/route.gen.ts\n'
  git -C "$C/main" config remote.origin.fetch '+refs/heads/other:refs/remotes/origin/other'
  git -C "$C/main" update-ref -d refs/remotes/origin/main
  run_s --apply
  a_out '^reclaimed a '; a_wt a absent
}
d06_symlink_blob_ignored() {
  mkenv d06; gen_wt a src/route.gen.ts
  ln -s elsewhere "$C/main/.reclaim-build-outputs"
  git -C "$C/main" add .reclaim-build-outputs; git -C "$C/main" commit -q -m link; git -C "$C/main" push -q origin main
  run_s --apply
  a_out '^blocked a .*route.gen.ts'; a_out 'warning.*not a regular file'; a_wt a present
}
d07_invalid_lines_skipped_with_warning() {
  mkenv d07; gen_wt a 'src/*.ts'; gen_wt b 'src/a[x].ts'
  declare_on_main '*.ts\nsrc/*.ts\n.*\n[a-z]*\n?*\n./x\nx/\na//b\n.git/x\nsrc/../x\nsrc/a[x].ts\nsrc/y\r\n'
  run_s --apply
  a_out '^blocked a .*src/\*\.ts'; a_out '^blocked b .*a\[x\]\.ts'
  a_out 'warning.*line 1[^0-9]'; a_out 'warning.*line 6[^0-9]'; a_out 'warning.*line 9[^0-9]'; a_out 'warning.*line 12'
  a_wt a present; a_wt b present; a_rc 0
}
d08_secret_name_still_blocks() {
  mkenv d08; gen_wt a src/.env
  declare_on_main 'src/.env\n'
  run_s --apply
  a_out '^blocked a .*\.env'; a_wt a present
}
d09_declared_dir_blocks() {
  mkenv d09; mkwt a; add_merged b-a "$(tip a)"
  echo gen/ >> "$C/main/.git/info/exclude"; mkdir "$C/wt/a/gen"; echo x > "$C/wt/a/gen/o"
  declare_on_main 'gen\ngen/o\n'
  run_s --apply
  a_out '^blocked a .*gen'; a_wt a present
}
d10_declared_file_in_ignored_dir_blocks() {
  mkenv d10; mkwt a; add_merged b-a "$(tip a)"
  echo out/ >> "$C/main/.git/info/exclude"; mkdir "$C/wt/a/out"; echo x > "$C/wt/a/out/route.gen.ts"
  declare_on_main 'out/route.gen.ts\n'
  run_s --apply
  a_out '^blocked a .*out'; a_wt a present
}
d11_no_prefix_suffix_or_depth_match() {
  mkenv d11; gen_wt a src/route.gen.ts; gen_wt b src/route.gen.ts.bak; gen_wt c route.gen.ts; gen_wt d x/y/z.gen.ts
  declare_on_main 'src/route.gen\nsrc/route.gen.ts.b\nroute.gen.ts.bak\nz.gen.ts\nx/z.gen.ts\n'
  run_s --apply
  a_out '^blocked a '; a_out '^blocked b '; a_out '^blocked c '; a_out '^blocked d '
  a_wt a present; a_wt b present; a_wt c present; a_wt d present
}
d12_bracket_entry_matched_literally() {
  mkenv d12; gen_wt a 'src/ax.ts'; gen_wt b 'src/a[x].ts'
  declare_on_main 'src/ax.ts\n'
  run_s --apply
  a_out '^reclaimed a '; a_out '^blocked b .*a\[x\]\.ts'; a_wt b present
}
d13_declared_symlink_entry_blocks() {
  mkenv d13; mkwt a; add_merged b-a "$(tip a)"
  echo link.gen >> "$C/main/.git/info/exclude"; ln -s a.txt "$C/wt/a/link.gen"
  declare_on_main 'link.gen\n'
  run_s --apply
  a_out '^blocked a .*link.gen'; a_wt a present
}

x1_quoted_name_declaration_rejected() {
  mkenv x1; mkwt a
  mkdir "$C/wt/a/a b"; echo k > "$C/wt/a/a b/keep.txt"
  git -C "$C/wt/a" add -A; git -C "$C/wt/a" commit -q -m keep; git -C "$C/wt/a" push -q origin b-a
  add_merged b-a "$(tip a)"
  echo prod.pem >> "$C/main/.git/info/exclude"; echo k > "$C/wt/a/a b/prod.pem"
  declare_on_main '"a b/prod.pem"\n'
  run_s --apply
  a_out '^blocked a '; a_out 'warning.*line 1'; a_wt a present
}
x2_quoted_dir_declaration_rejected() {
  mkenv x2; mkwt a; add_merged b-a "$(tip a)"
  echo 'my dir/' >> "$C/main/.git/info/exclude"; mkdir "$C/wt/a/my dir"; echo x > "$C/wt/a/my dir/o"
  declare_on_main '"my dir/"\n"my dir"\n'
  run_s --apply
  a_out '^blocked a '; a_wt a present
}
x3_git_component_case_insensitive() {
  mkenv x3; mkwt a; add_merged b-a "$(tip a)"
  declare_on_main '.GIT/x\nsrc/.Git/y\n'
  run_s --apply
  a_out 'warning.*line 1'; a_out 'warning.*line 2'
}
x4_plain_git_dir_declarable() {
  mkenv x4; gen_wt a src/git/x.gen.ts
  declare_on_main 'src/git/x.gen.ts\n'
  run_s --apply
  a_out '^reclaimed a '; a_wt a absent; a_no_out 'warning'
}
c46_path_with_tab_or_newline_blocked() {
  mkenv c46; local sep n d
  for n in tab nl; do
    if [ "$n" = tab ]; then sep=$'\t'; else sep=$'\n'; fi
    d="$C/wt/x${sep}y"
    git -C "$C/main" worktree add -q "$d" -b "b-$n" main 2>/dev/null || continue
    echo "$n" > "$d/f.txt"; git -C "$d" add f.txt; git -C "$d" commit -q -m "work $n"
    git -C "$d" push -q origin "b-$n"
    add_merged "b-$n" "$(git -C "$C/main" rev-parse "refs/heads/b-$n")"
  done
  mkwt ok; add_merged b-ok "$(tip ok)"
  run_s --apply
  a_out '^reclaimed ok '; a_wt ok absent
  a_no_out 'cannot resolve'
  [ "$(grep -c '^blocked x y unsupported worktree path (tab or newline)$' "$OUT")" = 2 ] || bad "want 2 exact unsupported-path lines"
  [ "$(grep -c '^blocked ' "$OUT")" = 4 ] || bad "want exactly 4 blocked lines"
  a_out '^total reclaimed=1 reclaimable=0 blocked=4 ask=0 skip=0 prunable=0 mode=apply$'
  a_local b-tab present; a_remote b-tab present
  if [ -d "$C/wt/x"$'\n'"y" ]; then a_local b-nl present; a_remote b-nl present; fi
  [ -d "$C/wt/x"$'\t'"y" ] || bad "tab worktree was removed"
}

c47_build_dir_inside_bad_path_worktree_refused() {
  mkenv c47; local d="$C/wt/x"$'\t'"y"
  git -C "$C/main" worktree add -q "$d" -b b-tab main
  mkdir -p "$d/outdir"; echo k > "$d/outdir/f"
  merged_wt a
  write_plan a in-progress "$DEAD_PID" "$HOST" "$OLD" "build_dirs:
  - $d/outdir"
  run_s --apply
  a_out '^reclaimed a '; a_exists "$d/outdir/f"
  a_out 'overlaps worktree'
}
c48_main_path_with_tab_keeps_main_skip() {
  mkenv c48; local m="$C/ma"$'\t'"in"
  mv "$C/main" "$m"; ln -s "$m" "$C/main"
  merged_wt a; merged_wt b
  run_s --apply
  a_out '^blocked ma in unsupported worktree path'
  a_out '^reclaimed a '; a_wt a absent; a_out '^reclaimed b '; a_wt b absent
}
c49_locked_with_reason() {
  mkenv c49; merged_wt a; git -C "$C/main" worktree lock --reason "x y" "$C/wt/a"
  run_s --apply
  a_out '^blocked a worktree is locked'; a_wt a present
}

CASES="c01_merged_clean c02_squash_tip_not_in_main c02b_tip_behind_merged_head c03_dirty c04_ignored_env
c04b_nested_build_output_allowed_nested_env_blocked c04e_node_modules_fixtures_and_more_secret_names c03b_local_branch_moves_before_delete c03c_branch_held_by_second_worktree c04c_tracked_build_dir_and_secret_names_blocked c04d_tracked_build_dir_with_ignored_output_blocked c04f_spaced_path_secret_blocked c04g_spaced_path_secret_names_blocked c04h_non_ascii_path_secret_blocked c04i_spaced_build_dir_harmless_output_reclaimed c04j_tab_and_newline_paths_no_misparse c04k_ignored_links_into_build_dirs c04l_ignored_link_secret_name c04m_link_to_root_blocked c05_plan_symlink_only c05b_plan_md_not_a_plan_symlink c06_extra_commits c07_fork_pr_same_name
c08_remote_ahead c08b_remote_moves_before_push c09_open_pr_head c10_stacked_open_base c11_live_cwd_prefix_and_symlink c12_plan_liveness
c13_build_dirs_report_refusals c13b_build_dirs_apply c13c_build_dir_outside_allowed_roots c13d_build_dir_canonical_git_and_cwd c14_gh_failure_aborts c15_submodule_refused_and_c17_isolation
c16_prunable c18_branch_mode_worktree_gone c19_report_mode_changes_nothing c20_locked c21_no_pr_and_detached
c22_non_default_base c23_pr_head_not_fetchable c24_local_branch_pass
c25_fork_pr_without_flag c26_fork_tip_is_head c27_fork_tip_behind_head c28_fork_extra_commit c29_fork_non_default_base
c30_fork_stash_blocks c30b_fork_stash_on_other_branch_does_not_block c31_fork_keeps_same_named_origin_branch
c32_fork_third_party_owner c33_fork_null_head_repository c34_fork_without_local_remote c35_fork_remote_ahead
c36_fork_match_beside_same_repo_row c37_fork_https_remote_uppercase_owner c38_fork_local_branch_without_worktree
c39_fork_open_pr_same_head_name c40_fork_pr_head_not_fetchable c41_unknown_flag c42_gh_user_failure_aborts_only_with_flag c43_fork_remote_moves_before_push c44_fork_oids_bound_to_selected_repo c45_fork_base_bound_to_selected_repo
d01_declared_on_default_reclaims d01b_last_line_without_newline d02_declared_only_on_branch_blocked d03_no_declaration_file_unchanged d04_stale_ref_refreshed d05_fetch_fails_means_no_declarations d05c_fetch_fails_and_ref_missing d05b_narrow_refspec_clone d06_symlink_blob_ignored d07_invalid_lines_skipped_with_warning d08_secret_name_still_blocks d09_declared_dir_blocks d10_declared_file_in_ignored_dir_blocks d11_no_prefix_suffix_or_depth_match d12_bracket_entry_matched_literally d13_declared_symlink_entry_blocks x1_quoted_name_declaration_rejected x2_quoted_dir_declaration_rejected x3_git_component_case_insensitive x4_plain_git_dir_declarable c46_path_with_tab_or_newline_blocked c47_build_dir_inside_bad_path_worktree_refused c48_main_path_with_tab_keeps_main_skip c49_locked_with_reason"

for t in $CASES; do
  if [ $# -gt 0 ]; then
    case " $* " in *" $t "*) ;; *) continue ;; esac
  fi
  OUT=
  CASE_FAILS=0
  "$t"
  if [ -f "${OUT:-/nonexistent}" ] && grep -q 'internal error' "$OUT"; then bad "an item hit an internal error"; fi
  if [ "$CASE_FAILS" -eq 0 ]; then
    PASSED=$((PASSED + 1))
    printf 'ok   %s\n' "$t"
  else
    TOTAL_FAILS=$((TOTAL_FAILS + 1))
    FAILED_NAMES="$FAILED_NAMES $t"
    printf 'FAIL %s\n' "$t"
  fi
done

printf '%d passed, %d failed%s\n' "$PASSED" "$TOTAL_FAILS" "${FAILED_NAMES:+ ($FAILED_NAMES )}"
[ "$TOTAL_FAILS" -eq 0 ]
