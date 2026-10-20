#!/usr/bin/env bash
# Report (default) or reclaim worktrees, branches and build dirs whose PRs have merged.
# This is the implementation of the AGENTS.md tenet 9 post-merge exception; the
# `worktrees` skill describes when to run it.
#
#   reclaim-merged-worktrees.sh [--apply] [--allow-fork] [--repo PATH] [--branch NAME]
#
# --allow-fork also reclaims a branch whose merged PR came from the authenticated user's own fork
# (a local remote must point at it); the fork branch is deleted, origin is never touched.
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
ALLOW_FORK=0
REPO_ARG=.
ONLY_BRANCH=
PLANS_GLOB="${RECLAIM_PLANS_GLOB:-$HOME/.claude/projects/*/plans/*.md}"
PR_MERGED_LIMIT=3000
PR_OPEN_LIMIT=1000
DECLARED_FILE=.reclaim-build-outputs
IGNORE_ALLOW=" node_modules target dist build .terraform __pycache__ coverage .next test-results playwright-report "

usage() {
  printf 'usage: %s [--apply] [--allow-fork] [--repo PATH] [--branch NAME]\n' "$0" >&2
  exit 2
}

die() {
  printf 'abort: %s (nothing was deleted by this check)\n' "$1" >&2
  exit 3
}

while [ $# -gt 0 ]; do
  case "$1" in
    --apply) APPLY=1 ;;
    --allow-fork) ALLOW_FORK=1 ;;
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

# url_slug <url>: sets URL_HOST and URL_SLUG (owner/name) lowercase; host is empty for a local path.
url_slug() {
  local u="$1" rest path name owner
  case "$u" in
    *://*)
      rest="${u#*://}"
      URL_HOST="${rest%%/*}"; URL_HOST="${URL_HOST##*@}"
      path="${rest#*/}"
      [ "$path" != "$rest" ] || return 1
      ;;
    /*) URL_HOST=; path="$u" ;;
    *:*) URL_HOST="${u%%:*}"; URL_HOST="${URL_HOST##*@}"; path="${u#*:}" ;;
    *) return 1 ;;
  esac
  URL_HOST="${URL_HOST%%:*}"
  path="${path%/}"; path="${path%.git}"
  name="${path##*/}"
  case "$path" in */*) owner="${path%/*}"; owner="${owner##*/}" ;; *) return 1 ;; esac
  [ -n "$owner" ] && [ -n "$name" ] || return 1
  URL_SLUG="$(printf '%s/%s' "$owner" "$name" | tr '[:upper:]' '[:lower:]')"
  URL_HOST="$(printf '%s' "$URL_HOST" | tr '[:upper:]' '[:lower:]')"
}

# load_remotes: $TMP/remotes gets "remote TAB owner/name" for every non-origin remote on origin's host.
load_remotes() {
  local r u host
  u="$(G remote get-url origin 2>/dev/null)" || die "cannot read the origin url"
  url_slug "$u" || die "cannot parse the origin url"
  host="$URL_HOST"
  : > "$TMP/remotes"
  for r in $(G remote); do
    [ "$r" != origin ] || continue
    u="$(G remote get-url "$r" 2>/dev/null)" || continue
    url_slug "$u" || continue
    [ "$URL_HOST" = "$host" ] && printf '%s\t%s\n' "$r" "$URL_SLUG" >> "$TMP/remotes"
  done
}

load_pr_data() {
  local def b cols
  def="$(gh repo view "$GH_REPO" --json defaultBranchRef --jq .defaultBranchRef.name 2>"$TMP/gh.err")" ||
    die "gh repo view failed: $(head -c 300 "$TMP/gh.err")"
  [ -n "$def" ] || die "empty default branch"
  DEFAULT_BRANCH="$def"
  local mfields=headRefName,headRefOid,baseRefName,isCrossRepository,number
  cols='.headRefName, .headRefOid, .baseRefName, (.number|tostring), (.isCrossRepository|tostring)'
  if [ "$ALLOW_FORK" -eq 1 ]; then
    mfields="$mfields,headRepository,headRepositoryOwner"
    cols="$cols"', ((.headRepositoryOwner.login // "-")|ascii_downcase), ((.headRepository.name // "-")|ascii_downcase)'
    GH_USER="$(gh api user --jq .login 2>"$TMP/gh.err")" || die "gh api user failed: $(head -c 300 "$TMP/gh.err")"
    [ -n "$GH_USER" ] || die "empty gh user"
    GH_USER="$(printf '%s' "$GH_USER" | tr '[:upper:]' '[:lower:]')"
    load_remotes
  fi
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
  # head TAB oid TAB base TAB number TAB cross [TAB head-owner TAB head-repo, only with --allow-fork]
  jq -r ".[] | [$cols] | @tsv" \
    "$TMP/merged.json" > "$TMP/merged.tsv" || die "cannot parse merged PRs"
}

is_protected() { grep -Fxq -- "$1" "$TMP/protected"; }

# ---------------------------------------------------------------- classification

# have_pr_head <oid> <number>: true when the object is local or fetched from origin's pull ref.
have_pr_head() {
  G cat-file -e "$1^{commit}" 2>/dev/null || G fetch -q origin "pull/$2/head" 2>/dev/null
}

# stash_on <branch>: true when a stash entry was made on that branch (fixed-prefix match).
stash_on() {
  local s
  while IFS= read -r s; do
    case "$s" in "WIP on $1: "* | "On $1: "*) return 0 ;; esac
  done <<EOF
$(G stash list --format=%gs 2>/dev/null)
EOF
  return 1
}

# classify <name> <branch> <tip>: 0 when merged and safe, else emits skip/ask and returns 1.
# Sets MATCH_OIDS (same-repo heads); with --allow-fork also IS_FORK, FORK_OIDS, FORK_REMOTE.
classify() {
  local name="$1" br="$2" tip="$3" head oid base num cross fowner fname matched=0 fetch_failed=0
  local any=0 same=0 base_ok=0 fork_cand=0 fork_matched=0 fork_base_ok=0
  local frem='' fork_why='' fork_other_base='' other_base='' fork_rows='' fork_hits='' fork_slug='' fs fo fb
  MATCH_OIDS=
  FORK_OIDS=
  FORK_REMOTE=
  IS_FORK=0
  if is_protected "$br"; then
    emit skip "$name" "branch $br has an open PR or is protected"
    return 1
  fi
  while IFS="$(printf '\t')" read -r head oid base num cross fowner fname; do
    [ "$head" = "$br" ] || continue
    any=1
    if [ "$cross" != false ]; then
      [ "$ALLOW_FORK" -eq 1 ] || continue
      if [ "$fname" = - ]; then fork_why="has no head repository (deleted fork)"; continue; fi
      if [ "$fowner" != "$GH_USER" ]; then fork_why="is from $fowner, not $GH_USER"; continue; fi
      frem="$(awk -F'\t' -v s="$fowner/$fname" '$2 == s { print $1; exit }' "$TMP/remotes")"
      if [ -z "$frem" ]; then fork_why="has no local remote for $fowner/$fname"; continue; fi
      fork_cand=1
      if ! have_pr_head "$oid" "$num"; then
        fetch_failed=1
        continue
      fi
      fork_rows="$fork_rows$fowner/$fname $oid
"
      if [ "$tip" = "$oid" ] || G merge-base --is-ancestor "$tip" "$oid" 2>/dev/null; then
        fork_matched=1
        if [ -z "$FORK_REMOTE" ]; then FORK_REMOTE="$frem"; fork_slug="$fowner/$fname"; fi
        fork_hits="$fork_hits$fowner/$fname $base
"
      fi
      continue
    fi
    same=1
    if ! have_pr_head "$oid" "$num"; then
      fetch_failed=1
      continue
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
  if [ "$same" -eq 0 ] && [ "$fork_cand" -eq 0 ]; then
    if [ -n "$fork_why" ]; then
      emit ask "$name" "fork PR for $br $fork_why"
    else
      emit ask "$name" "only fork PRs merged for $br (reused name)"
    fi
    return 1
  fi
  if [ "$matched" -eq 0 ] && [ "$fork_matched" -eq 1 ]; then
    IS_FORK=1
    while read -r fs fo; do
      [ "$fs" != "$fork_slug" ] || FORK_OIDS="$FORK_OIDS $fo"
    done <<EOF
$fork_rows
EOF
    while read -r fs fb; do
      [ "$fs" = "$fork_slug" ] || continue
      if [ "$fb" = "$DEFAULT_BRANCH" ]; then fork_base_ok=1; else fork_other_base="$fb"; fi
    done <<EOF
$fork_hits
EOF
    matched=1
    base_ok="$fork_base_ok"
    other_base="$fork_other_base"
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
  if [ "$IS_FORK" -eq 1 ] && stash_on "$br"; then
    emit blocked "$name" "stash entry on $br"
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

# decl_valid <line>: an exact repo-relative path of safe characters only; git quotes other names in status output, which would defeat the exact match.
decl_valid() {
  local IFS=/ c LC_ALL=C
  case "$1" in
    *[!A-Za-z0-9._@+/-]* | /* | ./* | */ | *//*) return 1 ;;
  esac
  set -f
  for c in $1; do
    case "$c" in
      . | .. | .[Gg][Ii][Tt]) set +f; return 1 ;;
    esac
  done
  set +f
  return 0
}

# load_declared_outputs: read DECLARED_FILE from origin's default branch (never the worktree, since
# it authorizes deletion). Any failure leaves $TMP/declared empty, which can only block more.
load_declared_outputs() {
  local ref="refs/remotes/origin/$DEFAULT_BRANCH" meta mode sha n=0 line
  : > "$TMP/declared"
  if ! G fetch -q origin "+refs/heads/$DEFAULT_BRANCH:$ref" 2>"$TMP/decl.err"; then
    echo "warning: cannot fetch origin/$DEFAULT_BRANCH, ignoring declared outputs: $(head -c 200 "$TMP/decl.err")" >&2
    return
  fi
  meta="$(G ls-tree "$ref" -- "$DECLARED_FILE" 2>"$TMP/decl.err")" || {
    echo "warning: cannot read $DECLARED_FILE, ignoring declared outputs" >&2
    return
  }
  [ -n "$meta" ] || return
  mode="${meta%% *}"
  sha="${meta#* blob }"
  sha="${sha%%$'\t'*}"
  case "$mode" in
    100644 | 100755) ;;
    *)
      echo "warning: $DECLARED_FILE is not a regular file, ignoring declared outputs" >&2
      return
      ;;
  esac
  G cat-file blob "$sha" > "$TMP/decl.raw" 2>"$TMP/decl.err" || {
    echo "warning: cannot read $DECLARED_FILE, ignoring declared outputs" >&2
    return
  }
  while IFS= read -r line || [ -n "$line" ]; do
    n=$((n + 1))
    case "$line" in '' | '#'*) continue ;; esac
    if decl_valid "$line"; then
      printf '%s\n' "$line" >> "$TMP/declared"
    else
      echo "warning: $DECLARED_FILE line $n is not an exact file path, skipped" >&2
    fi
  done < "$TMP/decl.raw"
}

# is_declared <dir> <entry>: a plain file named exactly in the declarations.
is_declared() {
  [ ! -d "$1/$2" ] && [ ! -L "$1/$2" ] && grep -Fxq -- "$2" "$TMP/declared"
}

# secret_file_under <path>: prints the first secret-named file below path. Git reports an ignored dir as one entry, so look inside for secret
# names (node_modules is skipped: packages ship fixtures with such names).
secret_file_under() {
  find "$1" -name node_modules -prune -o \( -name '.env*' -o -name '*.pem' -o -name '*.key' \
    -o -name '*.tfvars' -o -name '*.tfstate*' -o -name 'credentials*' -o -name '*.p12' \
    -o -name 'id_rsa*' -o -name '.npmrc' -o -name 'secrets.*' \) -print -quit 2>/dev/null
}

# link_build_ok <dir> <entry>: true when an ignored symlink's target, normalized as a string and never followed,
# lies strictly inside <dir> on a build path. Worktree removal unlinks the symlink, so the target need not exist.
link_build_ok() {
  local t path c rel d IFS=/
  t="$(readlink "$1/$2")" || return 1
  case "$t" in
    /*) path="$t" ;;
    *)
      d="${2%/*}"
      if [ "$d" = "$2" ]; then path="$1/$t"; else path="$1/$d/$t"; fi
      ;;
  esac
  set -f
  local -a out=()
  for c in $path; do
    case "$c" in
      '' | .) ;;
      ..) [ "${#out[@]}" -eq 0 ] || unset "out[$((${#out[@]} - 1))]" ;;
      *) out+=("$c") ;;
    esac
  done
  path="/${out[*]-}"
  set +f
  case "$path" in "$1"/?*) rel="${path#"$1"/}" ;; *) return 1 ;; esac
  is_build_path "$1" "$rel"
}

# gate <name> <canon> <locked 0|1> <plansfile>: emits blocked and returns 1 when not safe.
gate() {
  local name="$1" dir="$2" locked="$3" found="$4" rec entry p why hit skip=0
  if [ "$locked" -eq 1 ]; then
    emit blocked "$name" "worktree is locked"
    return 1
  fi
  if [ -n "$(git -C "$dir" status --porcelain 2>&1)" ]; then
    emit blocked "$name" "uncommitted or untracked changes"
    return 1
  fi
  # -z gives unquoted paths; a rename/copy record is followed by its source path as a separate field.
  if ! git -C "$dir" status --porcelain -z --ignored > "$TMP/status.z" 2> "$TMP/status.err"; then
    emit blocked "$name" "git status failed: $(head -c 200 "$TMP/status.err")"
    return 1
  fi
  while IFS= read -r -d '' rec; do
    if [ "$skip" -eq 1 ]; then skip=0; continue; fi
    case "$rec" in [RC]?* | ?[RC]*) skip=1 ;; esac
    case "$rec" in '!! '*) ;; *) continue ;; esac
    entry="${rec#!! }"
    entry="${entry%/}"
    if secret_name "$entry"; then
      :
    elif [ "$entry" = plan.md ]; then
      plan_symlink_ok "$dir" && continue
    elif is_declared "$dir" "$entry"; then
      continue
    elif [ -L "$dir/$entry" ] && link_build_ok "$dir" "$entry"; then
      continue
    elif is_build_path "$dir" "$entry"; then
      hit="$(secret_file_under "$dir/$entry")"
      [ -n "$hit" ] || continue
      emit blocked "$name" "secret-named file in ignored dir: ${hit#"$dir"/}"
      return 1
    fi
    emit blocked "$name" "ignored file not on the build allowlist: $entry"
    return 1
  done < "$TMP/status.z"
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

# delete_fork_remote <br>: after a fork match, keep origin and delete the branch on the fork remote
# only when it is at or behind a merged fork head. Sets REF_STATE and REF_NOTE.
delete_fork_remote() {
  local br="$1" line sha oid ok=0 msg
  [ -z "$(G ls-remote origin "refs/heads/$br" 2>/dev/null)" ] || REF_NOTE="$REF_NOTE; origin $br kept (fork PR)"
  line="$(G ls-remote "$FORK_REMOTE" "refs/heads/$br" 2>/dev/null)" || {
    REF_STATE=ask; REF_NOTE="$REF_NOTE; fork branch kept (ls-remote failed)"; return
  }
  sha="${line%%[[:space:]]*}"
  [ -n "$sha" ] || { REF_NOTE="$REF_NOTE; fork branch already gone"; return; }
  G fetch -q "$FORK_REMOTE" "refs/heads/$br" 2>/dev/null || {
    REF_STATE=ask; REF_NOTE="$REF_NOTE; fork branch kept (cannot fetch it)"; return
  }
  for oid in $FORK_OIDS; do
    if [ "$sha" = "$oid" ] || G merge-base --is-ancestor "$sha" "$oid" 2>/dev/null; then ok=1; break; fi
  done
  if [ "$ok" -eq 0 ]; then
    REF_STATE=ask; REF_NOTE="$REF_NOTE; fork branch ahead of merged head, kept"
  elif msg="$(G push --force-with-lease="refs/heads/$br:$sha" "$FORK_REMOTE" ":refs/heads/$br" 2>&1)"; then
    REF_NOTE="$REF_NOTE; deleted fork branch"
  else
    REF_STATE=ask
    REF_NOTE="$REF_NOTE; fork branch delete failed: $(printf '%s' "$msg" | head -1)"
  fi
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
  if [ "$IS_FORK" -eq 1 ]; then
    delete_fork_remote "$br"
    return
  fi
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
  if [ -f "$TMP/badbr" ] && grep -Fxq -- "$br" "$TMP/badbr"; then
    emit blocked "$name" "branch $br is also checked out at a refused worktree path"; return
  fi
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
load_declared_outputs

# -z keeps tab and newline in a path intact; such a path cannot ride the TSV, so it is only reported.
# A bad main path keeps a '/-' row so the first-row main skip stays positional.
parse_worktrees() {
  local f path='' br=- fl='' have=0 n=0 tab nl name
  tab="$(printf '\t')"; nl="$(printf '\n_')"; nl="${nl%_}"
  while IFS= read -r -d '' f; do
    if [ -z "$f" ]; then
      if [ "$have" -eq 1 ]; then
        n=$((n + 1))
        case "$path" in
          *"$tab"*|*"$nl"*)
            name="$(basename "$path")"
            printf '%s\n' "${name//[$tab$nl]/ }" >> "$TMP/badwt"
            printf '%s\n' "$path" >> "$TMP/badpath"
            [ "$br" = - ] || printf '%s\n' "$br" >> "$TMP/badbr"
            [ "$n" -ne 1 ] || printf '%s\t%s\t%s\n' /- "$br" "$fl" ;;
          *) printf '%s\t%s\t%s\n' "$path" "$br" "$fl" ;;
        esac
      fi
      path=; br=-; fl=; have=0
      continue
    fi
    have=1
    case "$f" in
      "worktree "*) path="${f#worktree }" ;;
      "branch refs/heads/"*) br="${f#branch refs/heads/}" ;;
      locked*) fl="$fl,locked" ;;
      prunable*) fl="$fl,prunable" ;;
    esac
  done
}

G worktree list --porcelain -z | parse_worktrees > "$TMP/wts" || die "cannot list worktrees"
if [ -f "$TMP/badwt" ]; then
  while IFS= read -r bad; do
    emit blocked "$bad" "unsupported worktree path (tab or newline)"
  done < "$TMP/badwt"
fi

: > "$TMP/wt_paths"
: > "$TMP/wt_branches"
while IFS="$(printf '\t')" read -r p br fl; do
  c="$(canon_dir "$p")" || c="$p"
  printf '%s\n' "$c" >> "$TMP/wt_paths"
  [ "$br" = - ] || printf '%s\n' "$br" >> "$TMP/wt_branches"
done < "$TMP/wts"
[ ! -f "$TMP/badbr" ] || cat "$TMP/badbr" >> "$TMP/wt_branches"
if [ -f "$TMP/badpath" ]; then
  while IFS= read -r p; do
    c="$(canon_dir "$p")" || c="$p"
    printf '%s\n' "$c" >> "$TMP/wt_paths"
  done < "$TMP/badpath"
fi

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
