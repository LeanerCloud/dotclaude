#!/usr/bin/env bash
# Report (default) or reclaim worktrees, branches and build dirs whose PRs have merged.
# This is the implementation of the AGENTS.md tenet 9 post-merge exception; the
# `worktrees` skill describes when to run it.
#
#   reclaim-merged-worktrees.sh [--apply] [--repo PATH] [--branch NAME]
#
# Output, one line per item: <state> <name> <reason>, then a totals line.
#   state: reclaimed | reclaimable | blocked | ask | skip | prunable
# Exit 0 normally; non-zero only for usage errors (2) and the fail-closed abort (3).
#
# Env: RECLAIM_PLANS_GLOB overrides the plan files searched (default
# ~/.claude/projects/*/plans/*.md); it exists so the fixture suite never reads real plans.
#
# Bash 3.2 compatible (macOS /bin/bash). No `set -e`: every step is checked explicitly so
# one failing item is reported as blocked and the sweep continues.
set -uo pipefail

APPLY=0
REPO_ARG=.
ONLY_BRANCH=
PLANS_GLOB="${RECLAIM_PLANS_GLOB:-$HOME/.claude/projects/*/plans/*.md}"
PR_MERGED_LIMIT=3000
PR_OPEN_LIMIT=1000
IGNORE_ALLOW=" node_modules target dist build .terraform __pycache__ coverage .next test-results playwright-report "

usage() {
  printf 'usage: %s [--apply] [--repo PATH] [--branch NAME]\n' "$0" >&2
  exit 2
}

die() {
  printf 'abort: %s (nothing was deleted by this check)\n' "$1" >&2
  exit 3
}

while [ $# -gt 0 ]; do
  case "$1" in
    --apply) APPLY=1 ;;
    --repo) [ $# -ge 2 ] || usage; REPO_ARG="$2"; shift ;;
    --branch) [ $# -ge 2 ] || usage; ONLY_BRANCH="$2"; shift ;;
    *) usage ;;
  esac
  shift
done

for tool in gh git jq lsof; do
  command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
done

TMP="$(mktemp -d "${TMPDIR:-/tmp}/reclaim.XXXXXX")" || die "cannot create a temp dir"
trap 'rm -rf "$TMP"' EXIT
: > "$TMP/counts"

G() { git -C "$REPO_MAIN" "$@"; }

emit() { # state name reason
  printf '%s %s %s\n' "$1" "$2" "$3"
  printf '%s\n' "$1" >> "$TMP/counts"
}

canon_dir() { (cd "$1" 2>/dev/null && pwd -P); }

# ---------------------------------------------------------------- repo and PR data

setup_repo() {
  local common url
  common="$(git -C "$REPO_ARG" rev-parse --path-format=absolute --git-common-dir 2>/dev/null)" ||
    die "$REPO_ARG is not a git repository"
  [ "$(basename "$common")" = .git ] || die "$REPO_ARG has no main worktree (bare repo?)"
  REPO_MAIN="$(dirname "$common")"
  REPO_MAIN="$(canon_dir "$REPO_MAIN")" || die "cannot resolve the main worktree"
  url="$(git -C "$REPO_MAIN" config --get remote.origin.url)" || die "no origin remote"
  GH_REPO="$(printf '%s' "$url" | sed -E 's#^(https?://[^/]+/|ssh://[^/]+/|[^@/]+@[^:/]+:)##; s#\.git$##')"
  case "$GH_REPO" in
    */*/* | */ | /* | "" | *" "*) die "cannot derive owner/repo from origin url" ;;
    */*) ;;
    *) die "cannot derive owner/repo from origin url" ;;
  esac
}

# gh_list <outfile> <limit> <gh pr list args...>: fail closed on error or a full page.
gh_list() {
  local out="$1" limit="$2" n
  shift 2
  gh pr list -R "$GH_REPO" --limit "$limit" "$@" > "$out" 2>"$TMP/gh.err" ||
    die "gh pr list failed: $(head -c 300 "$TMP/gh.err")"
  n="$(jq 'length' "$out" 2>/dev/null)" || die "gh returned invalid JSON"
  [ "$n" -lt "$limit" ] || die "gh returned $n results, equal to the limit $limit"
}

load_pr_data() {
  local def b
  def="$(gh repo view "$GH_REPO" --json defaultBranchRef --jq .defaultBranchRef.name 2>"$TMP/gh.err")" ||
    die "gh repo view failed: $(head -c 300 "$TMP/gh.err")"
  [ -n "$def" ] || die "empty default branch"
  DEFAULT_BRANCH="$def"
  local mfields=headRefName,headRefOid,baseRefName,isCrossRepository,number
  : > "$TMP/protected"
  if [ -n "$ONLY_BRANCH" ]; then
    b="$ONLY_BRANCH"
    gh_list "$TMP/merged.json" "$PR_OPEN_LIMIT" --head "$b" --state merged --json "$mfields"
    gh_list "$TMP/open_head.json" "$PR_OPEN_LIMIT" --head "$b" --state open --json number
    gh_list "$TMP/open_base.json" "$PR_OPEN_LIMIT" --base "$b" --state open --json number
    [ "$(jq 'length' "$TMP/open_head.json")" -eq 0 ] || printf '%s\n' "$b" >> "$TMP/protected"
    [ "$(jq 'length' "$TMP/open_base.json")" -eq 0 ] || printf '%s\n' "$b" >> "$TMP/protected"
  else
    gh_list "$TMP/merged.json" "$PR_MERGED_LIMIT" --state merged --json "$mfields"
    gh_list "$TMP/open.json" "$PR_OPEN_LIMIT" --state open --json headRefName,baseRefName
    jq -r '.[] | .headRefName, .baseRefName' "$TMP/open.json" >> "$TMP/protected" ||
      die "cannot parse open PRs"
    jq -r '.[] | .headRefName' "$TMP/open.json" > "$TMP/open_heads"
  fi
  printf '%s\n%s\n%s\n' main master "$DEFAULT_BRANCH" >> "$TMP/protected"
  # head TAB oid TAB base TAB number TAB cross
  jq -r '.[] | [.headRefName, .headRefOid, .baseRefName, (.number|tostring), (.isCrossRepository|tostring)] | @tsv' \
    "$TMP/merged.json" > "$TMP/merged.tsv" || die "cannot parse merged PRs"
}

is_protected() { grep -Fxq -- "$1" "$TMP/protected"; }

# ---------------------------------------------------------------- classification

# classify <name> <branch> <tip>: returns 0 when the branch is merged and safe to consider.
# Otherwise emits skip/ask and returns 1. Sets MATCH_OIDS to every same-repo merged head.
classify() {
  local name="$1" br="$2" tip="$3" head oid base num cross matched=0 fetch_failed=0
  local any=0 same=0 base_ok=0 other_base=
  MATCH_OIDS=
  if is_protected "$br"; then
    emit skip "$name" "branch $br has an open PR or is protected"
    return 1
  fi
  while IFS="$(printf '\t')" read -r head oid base num cross; do
    [ "$head" = "$br" ] || continue
    any=1
    [ "$cross" = false ] || continue
    same=1
    if ! G cat-file -e "$oid^{commit}" 2>/dev/null; then
      if ! G fetch -q origin "pull/$num/head" 2>/dev/null; then
        fetch_failed=1
        continue
      fi
    fi
    MATCH_OIDS="$MATCH_OIDS $oid"
    if [ "$tip" = "$oid" ] || G merge-base --is-ancestor "$tip" "$oid" 2>/dev/null; then
      matched=1
      if [ "$base" = "$DEFAULT_BRANCH" ]; then base_ok=1; else other_base="$base"; fi
    fi
  done < "$TMP/merged.tsv"
  if [ "$any" -eq 0 ]; then
    emit skip "$name" "no merged PR for $br"
    return 1
  fi
  if [ "$same" -eq 0 ]; then
    emit ask "$name" "only fork PRs merged for $br (reused name)"
    return 1
  fi
  if [ "$matched" -eq 0 ]; then
    if [ "$fetch_failed" -eq 1 ]; then
      emit ask "$name" "cannot fetch merged PR head for $br"
    else
      emit ask "$name" "tip of $br is not at a merged PR head (reused name or extra commits)"
    fi
    return 1
  fi
  if [ "$base_ok" -eq 0 ]; then
    emit ask "$name" "PR for $br merged into $other_base, not $DEFAULT_BRANCH"
    return 1
  fi
  return 0
}

# ---------------------------------------------------------------- plans

list_plans() {
  local f
  : > "$TMP/plans"
  # shellcheck disable=SC2086 # the glob must expand
  for f in $PLANS_GLOB; do
    [ -f "$f" ] && printf '%s\n' "$f" >> "$TMP/plans"
  done
}

# plan_field <file> <key>: first frontmatter value, comment and quotes stripped.
plan_field() {
  awk -v key="$2" '
    /^---[ \t]*$/ { fm++; if (fm == 2) exit; next }
    fm == 1 && index($0, key ":") == 1 {
      v = substr($0, length(key) + 2)
      sub(/^[ \t]+/, "", v); sub(/[ \t]+#.*$/, "", v); sub(/[ \t]+$/, "", v)
      gsub(/^["\x27]|["\x27]$/, "", v)
      print v; exit
    }' "$1"
}

plan_build_dirs() {
  awk '
    /^---[ \t]*$/ { fm++; if (fm == 2) exit; next }
    fm != 1 { next }
    inlist && /^[ \t]*-[ \t]+/ { v = $0; sub(/^[ \t]*-[ \t]+/, "", v); sub(/[ \t]+#.*$/, "", v); gsub(/["\x27]/, "", v); print v; next }
    inlist && /^[ \t]*$/ { next }
    { inlist = 0 }
    index($0, "build_dirs:") == 1 {
      v = substr($0, 12); sub(/[ \t]+#.*$/, "", v); gsub(/^[ \t]+|[ \t]+$/, "", v)
      if (v == "") { inlist = 1; next }
      gsub(/[\[\]"\x27]/, "", v); n = split(v, a, ",")
      for (i = 1; i <= n; i++) { gsub(/^[ \t]+|[ \t]+$/, "", a[i]); if (a[i] != "") print a[i] }
    }' "$1"
}

# find_plans <raw> <canon>: write matching plan files to $TMP/found.<pid>; echo that path.
find_plans() {
  local out="$TMP/found.$$" f val c cands
  : > "$out"
  cands="$1
$2"
  case "$1" in "$HOME"/*) cands="$cands
~/${1#"$HOME"/}" ;; esac
  case "$2" in "$HOME"/*) cands="$cands
~/${2#"$HOME"/}" ;; esac
  while IFS= read -r f; do
    val="$(plan_field "$f" worktree)"
    [ -n "$val" ] || continue
    while IFS= read -r c; do
      if [ "$val" = "$c" ]; then printf '%s\n' "$f" >> "$out"; break; fi
    done <<EOF
$cands
EOF
  done < "$TMP/plans"
  printf '%s' "$out"
}

iso_epoch() {
  if [ "$(uname)" = Darwin ]; then
    date -j -u -f '%Y-%m-%dT%H:%M:%SZ' "$1" +%s 2>/dev/null
  else
    date -u -d "$1" +%s 2>/dev/null
  fi
}

# plan_blocks <plan>: reason string when the plan shows a live owner, else empty.
plan_blocks() {
  local plan="$1" status pid host upd e now
  status="$(plan_field "$plan" status)"
  case "$status" in in-progress | verifying) ;; *) return 0 ;; esac
  pid="$(plan_field "$plan" pid)"
  host="$(plan_field "$plan" host)"
  case "$pid" in '' | *[!0-9]*) pid= ;; esac
  if [ -n "$pid" ] && [ "$host" = "$(hostname)" ]; then
    if kill -0 "$pid" 2>/dev/null; then
      printf 'plan %s has live owner pid %s' "$(basename "$plan")" "$pid"
    fi
    return 0
  fi
  upd="$(plan_field "$plan" pid_updated)"
  e="$(iso_epoch "$upd")" || e=
  now="$(date +%s)"
  if [ -z "$e" ] || [ $((now - e)) -le 86400 ]; then
    printf 'plan %s is %s with owner unknown or recently updated' "$(basename "$plan")" "$status"
  fi
}

# ---------------------------------------------------------------- gates

cwd_file() {
  if [ ! -f "$TMP/cwds" ]; then
    lsof -a -d cwd -Fn 2>/dev/null | grep '^n/' > "$TMP/cwds.tmp"
    [ -s "$TMP/cwds.tmp" ] || die "lsof reported no process cwds"
    mv "$TMP/cwds.tmp" "$TMP/cwds"
  fi
  printf '%s' "$TMP/cwds"
}

plan_symlink_ok() { # worktree plan.md must be a symlink to a known plan file
  local target
  [ -L "$1/plan.md" ] || return 1
  target="$(readlink "$1/plan.md")" || return 1
  grep -Fxq -- "$target" "$TMP/plans"
}

# is_build_path <dir> <path>: true when a path component is a build-output name (any depth)
# and nothing under the path up to that component is tracked.
is_build_path() {
  local IFS=/ c prefix=
  set -f
  for c in $2; do
    prefix="${prefix:+$prefix/}$c"
    case "$IGNORE_ALLOW" in
      *" $c "*)
        set +f
        [ -z "$(git -C "$1" ls-files -- ":(literal)$prefix" 2>/dev/null)" ]
        return
        ;;
    esac
  done
  set +f
  return 1
}

# secret_name <path>: true for basenames that are never build output, even under a build dir.
secret_name() {
  case "${1##*/}" in
    .env* | *.pem | *.key | *.tfvars | *.tfstate* | credentials* | *.p12 | id_rsa* | .npmrc | secrets.*) return 0 ;;
  esac
  return 1
}

# secret_file_under <path>: prints the first secret-named file below path. Git reports an ignored dir as one entry, so look inside for secret
# names (node_modules is skipped: packages ship fixtures with such names).
secret_file_under() {
  find "$1" -name node_modules -prune -o \( -name '.env*' -o -name '*.pem' -o -name '*.key' \
    -o -name '*.tfvars' -o -name '*.tfstate*' -o -name 'credentials*' -o -name '*.p12' \
    -o -name 'id_rsa*' -o -name '.npmrc' -o -name 'secrets.*' \) -print -quit 2>/dev/null
}

# gate <name> <canon> <locked 0|1> <plansfile>: emits blocked and returns 1 when not safe.
gate() {
  local name="$1" dir="$2" locked="$3" found="$4" line entry p why hit
  if [ "$locked" -eq 1 ]; then
    emit blocked "$name" "worktree is locked"
    return 1
  fi
  if [ -n "$(git -C "$dir" status --porcelain 2>&1)" ]; then
    emit blocked "$name" "uncommitted or untracked changes"
    return 1
  fi
  while IFS= read -r line; do
    case "$line" in '!! '*) ;; *) continue ;; esac
    entry="${line#!! }"
    entry="${entry%/}"
    if secret_name "$entry"; then
      :
    elif [ "$entry" = plan.md ]; then
      plan_symlink_ok "$dir" && continue
    elif is_build_path "$dir" "$entry"; then
      hit="$(secret_file_under "$dir/$entry")"
      [ -n "$hit" ] || continue
      emit blocked "$name" "secret-named file in ignored dir: ${hit#"$dir"/}"
      return 1
    fi
    emit blocked "$name" "ignored file not on the build allowlist: $entry"
    return 1
  done <<EOF
$(git -C "$dir" status --porcelain --ignored 2>&1)
EOF
  if awk -v p="$dir" '{ s = substr($0, 2); if (s == p || index(s, p "/") == 1) { f = 1; exit } } END { exit !f }' "$(cwd_file)"; then
    emit blocked "$name" "a live process has its cwd in the worktree"
    return 1
  fi
  while IFS= read -r p; do
    [ -n "$p" ] || continue
    why="$(plan_blocks "$p")"
    if [ -n "$why" ]; then
      emit blocked "$name" "$why"
      return 1
    fi
  done < "$found"
  return 0
}

# ---------------------------------------------------------------- build dirs

# check_build_dir <dir> <found plans file>: prints the refusal reason, returns 1 if refused.
check_build_dir() {
  local d="$1" found="$2" c root p f other
  case "$d" in /*) ;; *) printf 'not absolute'; return 1 ;; esac
  [ -e "$d" ] || { printf 'does not exist'; return 1; }
  [ ! -L "$d" ] || { printf 'is a symlink'; return 1; }
  [ -d "$d" ] || { printf 'not a directory'; return 1; }
  c="$(canon_dir "$d")" || { printf 'cannot resolve'; return 1; }
  [ "$c" = "$d" ] || { printf 'not canonical (resolves to %s)' "$c"; return 1; }
  [ ! -e "$c/.git" ] || { printf 'contains a git checkout'; return 1; }
  if awk -v p="$c" '{ s = substr($0, 2); if (s == p || index(s, p "/") == 1) { f = 1; exit } } END { exit !f }' "$(cwd_file)"; then
    printf 'a live process has its cwd inside'; return 1
  fi
  if [ "$c" = / ] || [ "$c" = "$(canon_dir "$HOME")" ]; then printf 'is / or home'; return 1; fi
  if [ "$c" = "$REPO_MAIN" ] || [ "${REPO_MAIN#"$c"/}" != "$REPO_MAIN" ]; then
    printf 'is the repo or an ancestor of it'; return 1
  fi
  while IFS= read -r p; do
    [ -n "$p" ] || continue
    if [ "$c" = "$p" ] || [ "${c#"$p"/}" != "$c" ] || [ "${p#"$c"/}" != "$p" ]; then
      printf 'overlaps worktree %s' "$p"; return 1
    fi
  done < "$TMP/wt_paths"
  local ok=0
  for root in "$HOME/.claude/worktrees" "${TMPDIR:-}" /private/tmp; do
    [ -n "$root" ] || continue
    root="$(canon_dir "$root")" || continue
    [ "$root" != / ] && [ "$root" != "$(canon_dir "$HOME")" ] || continue
    [ "${c#"$root"/}" != "$c" ] && ok=1
  done
  [ "$ok" -eq 1 ] || { printf 'not strictly below an allowed root'; return 1; }
  while IFS= read -r f; do
    grep -Fxq -- "$f" "$found" && continue
    while IFS= read -r other; do
      if [ "$other" = "$d" ] || [ "$other" = "$c" ]; then
        printf 'also listed by plan %s' "$(basename "$f")"; return 1
      fi
    done <<EOF
$(plan_build_dirs "$f")
EOF
  done < "$TMP/plans"
  return 0
}

# ---------------------------------------------------------------- actions

# remote_state <branch>: prints "gone", "ok:<sha>", "ahead", or "error:<why>".
remote_state() {
  local br="$1" line sha oid
  line="$(G ls-remote origin "refs/heads/$br" 2>/dev/null)" || { printf 'error:ls-remote failed'; return; }
  sha="${line%%[[:space:]]*}"
  [ -n "$sha" ] || { printf 'gone'; return; }
  G fetch -q origin "refs/heads/$br" 2>/dev/null || { printf 'error:cannot fetch remote branch'; return; }
  for oid in $MATCH_OIDS; do
    if [ "$sha" = "$oid" ] || G merge-base --is-ancestor "$sha" "$oid" 2>/dev/null; then
      printf 'ok:%s' "$sha"
      return
    fi
  done
  printf 'ahead'
}

# delete_refs <br>: sets REF_STATE (reclaimed|blocked|ask) and REF_NOTE.
delete_refs() {
  local br="$1" tip="$2" rs sha msg
  REF_STATE=reclaimed
  if G worktree list --porcelain | grep -Fxq "branch refs/heads/$br"; then
    REF_STATE=blocked
    REF_NOTE="branch $br is checked out in another worktree"
    return
  fi
  if ! msg="$(G update-ref -d "refs/heads/$br" "$tip" 2>&1)"; then
    REF_STATE=blocked
    REF_NOTE="deleting local $br failed: $msg"
    return
  fi
  REF_NOTE="deleted local $br"
  rs="$(remote_state "$br")"
  case "$rs" in
    gone) REF_NOTE="$REF_NOTE; remote already gone" ;;
    ok:*)
      sha="${rs#ok:}"
      if msg="$(G push --force-with-lease="refs/heads/$br:$sha" origin ":refs/heads/$br" 2>&1)"; then
        REF_NOTE="$REF_NOTE; deleted remote"
      else
        REF_STATE=ask
        REF_NOTE="$REF_NOTE; remote delete failed: $(printf '%s' "$msg" | head -1)"
      fi
      ;;
    ahead) REF_STATE=ask; REF_NOTE="$REF_NOTE; remote ahead of merged head, kept" ;;
    *) REF_STATE=ask; REF_NOTE="$REF_NOTE; remote kept (${rs#error:})" ;;
  esac
}

archive_plan() {
  local plan="$1" dir dest
  dir="$(dirname "$plan")/archive"
  mkdir -p "$dir" || return 1
  dest="$dir/$(basename "$plan")"
  [ ! -e "$dest" ] || dest="$dest.$(date +%s)"
  awk '/^---[ \t]*$/ { fm++ } fm == 1 && index($0, "status:") == 1 { sub(/^status:[ \t]*[a-z-]*/, "status: merged") } { print }' \
    "$plan" > "$dest" || return 1
  rm -- "$plan"
}

# ---------------------------------------------------------------- items

process_worktree() { # path branch flags
  local path="$1" br="$2" flags="$3" name canon tip found locked=0 msg d why refused="" p
  name="$(basename "$path")"
  case ",$flags," in *,prunable,*) emit prunable "$name" "worktree directory is missing; run git worktree prune"; return ;; esac
  if [ "$br" = - ]; then emit skip "$name" "detached HEAD"; return; fi
  canon="$(canon_dir "$path")" || { emit blocked "$name" "cannot resolve $path"; return; }
  tip="$(G rev-parse "refs/heads/$br" 2>&1)" || { emit blocked "$name" "cannot resolve $br: $tip"; return; }
  case ",$flags," in *,locked,*) locked=1 ;; esac
  classify "$name" "$br" "$tip" || return 0
  found="$(find_plans "$path" "$canon")"
  gate "$name" "$canon" "$locked" "$found" || return 0

  : > "$TMP/dirs.$$"
  while IFS= read -r p; do
    [ -n "$p" ] || continue
    plan_build_dirs "$p" >> "$TMP/dirs.$$"
  done < "$found"
  while IFS= read -r d; do
    [ -n "$d" ] || continue
    if why="$(check_build_dir "$d" "$found")"; then :; else refused="$refused; refused build_dir $d ($why)"; fi
  done < "$TMP/dirs.$$"

  if [ "$APPLY" -eq 0 ]; then
    emit reclaimable "$name" "branch $br is merged and the worktree is clean$refused"
    return
  fi
  if ! msg="$(G worktree remove "$canon" 2>&1)"; then
    emit blocked "$name" "git worktree remove refused: $(printf '%s' "$msg" | head -1)"
    return
  fi
  msg="removed worktree"
  while IFS= read -r d; do
    [ -n "$d" ] || continue
    if check_build_dir "$d" "$found" >/dev/null; then
      if rm -rf -- "$(canon_dir "$d")"; then msg="$msg; deleted build_dir $d"; else refused="$refused; could not delete build_dir $d"; fi
    fi
  done < "$TMP/dirs.$$"
  delete_refs "$br" "$tip"
  msg="$msg; $REF_NOTE$refused"
  if [ "$REF_STATE" != blocked ]; then
    while IFS= read -r p; do
      [ -n "$p" ] || continue
      archive_plan "$p" && msg="$msg; archived $(basename "$p")" || msg="$msg; could not archive $(basename "$p")"
    done < "$found"
  fi
  emit "$REF_STATE" "$name" "$msg"
}

process_local_branch() { # branch
  local br="$1" tip msg
  tip="$(G rev-parse "refs/heads/$br" 2>&1)" || { emit blocked "$br" "cannot resolve branch: $tip"; return; }
  classify "$br" "$br" "$tip" || return 0
  if [ "$APPLY" -eq 0 ]; then
    emit reclaimable "$br" "local branch without a worktree is merged"
    return
  fi
  delete_refs "$br" "$tip"
  msg="$REF_NOTE"
  emit "$REF_STATE" "$br" "$msg"
}

# ---------------------------------------------------------------- main

setup_repo
list_plans
load_pr_data

G worktree list --porcelain | awk -v RS= -v FS='\n' '
  { path = ""; br = "-"; fl = ""
    for (i = 1; i <= NF; i++) {
      if ($i ~ /^worktree /) path = substr($i, 10)
      else if ($i ~ /^branch refs\/heads\//) br = substr($i, 19)
      else if ($i ~ /^locked/) fl = fl ",locked"
      else if ($i ~ /^prunable/) fl = fl ",prunable"
    }
    printf "%s\t%s\t%s\n", path, br, fl }' > "$TMP/wts" || die "cannot list worktrees"

: > "$TMP/wt_paths"
: > "$TMP/wt_branches"
while IFS="$(printf '\t')" read -r p br fl; do
  c="$(canon_dir "$p")" || c="$p"
  printf '%s\n' "$c" >> "$TMP/wt_paths"
  [ "$br" = - ] || printf '%s\n' "$br" >> "$TMP/wt_branches"
done < "$TMP/wts"

first=1
while IFS="$(printf '\t')" read -r p br fl; do
  if [ "$first" -eq 1 ]; then first=0; continue; fi # the main worktree
  [ -z "$ONLY_BRANCH" ] || [ "$br" = "$ONLY_BRANCH" ] || continue
  ( process_worktree "$p" "$br" "$fl" ) || emit blocked "$(basename "$p")" "internal error while processing"
done < "$TMP/wts"

if [ -n "$ONLY_BRANCH" ]; then
  if [ "$(sed -n 1p "$TMP/wts" | cut -f2)" = "$ONLY_BRANCH" ]; then
    emit skip "$ONLY_BRANCH" "checked out in the main worktree"
  elif ! grep -Fxq -- "$ONLY_BRANCH" "$TMP/wt_branches"; then
    if G show-ref --verify --quiet "refs/heads/$ONLY_BRANCH"; then
      ( process_local_branch "$ONLY_BRANCH" ) || emit blocked "$ONLY_BRANCH" "internal error while processing"
    else
      emit skip "$ONLY_BRANCH" "no local branch"
    fi
  fi
else
  G for-each-ref --format='%(refname:short)' refs/heads > "$TMP/locals"
  while IFS= read -r br; do
    [ -n "$br" ] || continue
    grep -Fxq -- "$br" "$TMP/wt_branches" && continue
    is_protected "$br" && continue
    awk -F'\t' -v b="$br" '$1 == b { f = 1 } END { exit !f }' "$TMP/merged.tsv" || continue
    ( process_local_branch "$br" ) || emit blocked "$br" "internal error while processing"
  done < "$TMP/locals"
fi

count() { grep -cx "$1" "$TMP/counts" || true; }
printf 'total reclaimed=%s reclaimable=%s blocked=%s ask=%s skip=%s prunable=%s mode=%s\n' \
  "$(count reclaimed)" "$(count reclaimable)" "$(count blocked)" "$(count ask)" \
  "$(count skip)" "$(count prunable)" "$([ "$APPLY" -eq 1 ] && echo apply || echo report)"
exit 0
