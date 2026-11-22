#!/usr/bin/env bash
# Gated storage cleanup for a $HOME full of git checkouts, build trees and run dirs (btrfs aware, Linux only:
# needs /proc, GNU readlink/stat/find/rm; btrfs-progs for Exclusive sizes and dedupe).
#
#   storage-cleanup.sh audit  [--top N] [PATH...]
#   storage-cleanup.sh gates  PATH [--network]
#   storage-cleanup.sh delete PATH --class CLASS --evidence TEXT (--network|--offline) [--apply]
#   storage-cleanup.sh dedupe [--apply] [--min-size BYTES] [--out FILE] ROOT...
#   storage-cleanup.sh repair-alternates REPO STORE...
#
# Every deletion goes through `gates`; `delete` is a dry run unless --apply is given, and --apply needs an
# explicit --network (fetch --prune every remote first) or --offline (trust the remote-tracking refs).
# repair-alternates edits the repo it is given and is not limited to STORAGE_CLEANUP_ROOT. Exit codes: 0 ok /
# gates passed, 1 refused or failed, 2 usage error. Env: STORAGE_CLEANUP_ROOT (default $HOME; targets must
# be inside it), STORAGE_CLEANUP_PROTECTED (extra ERE of protected paths), STORAGE_CLEANUP_LOGDIR
# (default ~/.claude/reports/disk-cleanup), STORAGE_CLEANUP_INDEX_TTL (seconds `gates` may reuse its
# alternates index, default 300; `delete` always rebuilds it), STORAGE_CLEANUP_ALLOW_UNREADABLE (ERE of
# directories the dependency scan may skip because they are unreadable, e.g. root-owned run dirs; any other
# unreadable directory refuses), STORAGE_CLEANUP_TRUST_LOCAL_REMOTES=1
# (fixtures only: counts a local-path remote as upstream; real runs must not, a mirror in the same
# home is not a backup).
# Unlike disk-audit.sh (apparent sizes, report only) this tool measures Exclusive size and deletes, so it
# carries its own hardened git: repo-local config can run commands, so every configured filter is
# blanked on the command line (include files and --worktree config included) and network commands
# refuse risky config.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
die() { printf 'storage-cleanup: %s\n' "$*" >&2; exit 1; }
usage() { sed -n '2,19p' "$0" | sed 's/^# \{0,1\}//' >&2; exit 2; }
[ -d /proc/self ] || die "needs /proc (Linux only)"
# the caller's git environment must not redirect or limit repository discovery
unset GIT_DIR GIT_WORK_TREE GIT_COMMON_DIR GIT_CEILING_DIRECTORIES GIT_INDEX_FILE GIT_OBJECT_DIRECTORY \
  GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_DISCOVERY_ACROSS_FILESYSTEM GIT_NAMESPACE GIT_CONFIG_PARAMETERS GIT_CONFIG_COUNT

ROOT="$(readlink -f "${STORAGE_CLEANUP_ROOT:-$HOME}")"
LOGDIR="${STORAGE_CLEANUP_LOGDIR:-$HOME/.claude/reports/disk-cleanup}"
ACTIONS="$LOGDIR/ACTIONS.tsv"
INDEX="$LOGDIR/.alternates-index.$(printf '%s' "$ROOT" | cksum | cut -d' ' -f1).tsv"
TTL="${STORAGE_CLEANUP_INDEX_TTL:-300}"
PROTECTED_RE='(^|/)\.ssh(/|$)|(^|/)\.gnupg(/|$)|^/swap(/|$)|\.swapfile|/\.claude/(backups|reports|skills|memory|plans|decisions|agents|hooks|commands|templates|rules)(/|$)|/\.claude/(projects\.md|local-paths\.md|settings(\.local)?\.json|CLAUDE\.md|AGENTS\.md)$|/\.claude/projects/[^/]+/memory(/|$)|/(Downloads|Documents|Pictures|Videos|Music|Desktop)(/|$)|/\.password-store(/|$)|/\.local/share/keyrings(/|$)|/\.mozilla(/|$)|/\.config/(chromium|google-chrome|BraveSoftware|vivaldi|microsoft-edge)(/|$)'
[ -n "${STORAGE_CLEANUP_PROTECTED:-}" ] && PROTECTED_RE="$PROTECTED_RE|$STORAGE_CLEANUP_PROTECTED"
BUILD_DIRS=" target build dist out node_modules .next .cache __pycache__ .venv venv .gradle .terraform coverage ${STORAGE_CLEANUP_BUILD_DIRS:-} "
SAFE_URL_RE='^(https://|ssh://|git@[^/:]+:)'
[ -n "${STORAGE_CLEANUP_TRUST_LOCAL_REMOTES:-}" ] && SAFE_URL_RE="$SAFE_URL_RE|^/|^file://"
RISK_RE='^(core\.sshcommand|core\.gitproxy|core\.askpass|remote\..*\.proxy|url\..*\.(insteadof|pushinsteadof)|credential\..*|protocol\..*)$'

# git in an untrusted repo: no fsmonitor/hooks/auto-gc and every configured filter.* value blanked, including
# keys that arrive through include.path and the per-worktree config.
rgit() {
  local r="$1"; shift
  local -a f=(); local k scope
  while IFS= read -r k; do [ -n "$k" ] && f+=(-c "$k="); done < <(
    for scope in --local --worktree; do
      GIT_CONFIG_NOSYSTEM=1 git -C "$r" config "$scope" --includes --name-only --get-regexp '^filter\.' 2>/dev/null
    done)
  GIT_CONFIG_NOSYSTEM=1 GIT_OPTIONAL_LOCKS=0 git -c core.fsmonitor=false -c core.hooksPath=/dev/null \
    -c gc.auto=0 ${f[@]+"${f[@]}"} -C "$r" "$@"
}
ngit() { # network git: never prompts, refuses to run through proxies or helpers configured by the repo
  local r="$1"; shift
  GIT_TERMINAL_PROMPT=0 GIT_ASKPASS=true SSH_ASKPASS=true rgit "$r" -c 'core.sshCommand=ssh -o BatchMode=yes -o ConnectTimeout=10' -c core.gitProxy= \
    -c protocol.ext.allow=never -c protocol.file.allow=never "$@"
}

norm() { local p="$1"; while [ "${p%/}" != "$p" ] && [ "$p" != / ]; do p="${p%/}"; done; printf '%s' "$p"; }
shown() { case "$1" in "$HOME"/*) printf '~%s' "${1#"$HOME"}";; *) printf '%s' "$1";; esac; }
size_of() { # Exclusive bytes on btrfs, apparent bytes elsewhere
  if [ "$(stat -f -c %T "$1" 2>/dev/null)" = btrfs ]; then
    btrfs filesystem du -s --raw "$1" 2>/dev/null | tail -1 | awk '{print $2}'
  else du -sxb "$1" 2>/dev/null | cut -f1; fi
}

INDEX_NOTE=""
fresh_index() { # kind<TAB>file<TAB>resolved target: alternates lines, gitfiles and local-path remotes under ROOT
  mkdir -p "$LOGDIR"; INDEX_NOTE=""
  if [ -z "${REFRESH:-}" ] && [ -f "$INDEX" ]; then
    local age=$(( $(date +%s) - $(stat -c %Y "$INDEX") ))
    if [ "$age" -lt "$TTL" ]; then INDEX_NOTE=" (index cached ${age}s ago; delete always rebuilds it)"; return; fi
  fi
  local tmp="$INDEX.$$" f line base mp; local -a prune=()
  while IFS= read -r mp; do # real mount points are skipped; nested subvolumes are not mounts and are walked
    mp="$(printf '%b' "$mp")"; case "$mp" in "$ROOT"/*) prune+=(-path "$mp" -o);; esac
  done < <(awk '{print $5}' /proc/self/mountinfo)
  : > "$tmp"
  while IFS= read -r -d '' f; do
    case "$f" in
      */objects/info/alternates)
        base="$(dirname "$(dirname "$f")")"             # git resolves relative lines from the objects dir
        while IFS= read -r line; do
          [ -n "$line" ] || continue
          case "$line" in /*) ;; *) line="$base/$line" ;; esac
          printf 'alternates\t%s\t%s\n' "$f" "$(readlink -m "$line")" >> "$tmp"
        done < "$f" ;;
      */.git/config)
        base="$(dirname "$(dirname "$f")")"             # a relative local url is relative to the checkout
        git config --file "$f" --get-regexp '^remote\..*\.url$' 2>/dev/null | while IFS= read -r line; do
          line="${line#* }"; line="${line#file://}"
          case "$line" in /*) ;; ./*|../*) line="$base/$line" ;; *) continue ;; esac
          printf 'remote\t%s\t%s\n' "$f" "$(readlink -m "$line")" >> "$tmp"
        done ;;
      */.git)
        line="$(sed -n 's/^gitdir: //p' "$f" | head -1)"
        case "$line" in /*|"") ;; *) line="$(dirname "$f")/$line" ;; esac
        [ -n "$line" ] && printf 'gitfile\t%s\t%s\n' "$f" "$(readlink -m "$line")" >> "$tmp" ;;
    esac
  done < <(LC_ALL=C find "$ROOT" \( ${prune[@]+"${prune[@]}"} -false \) -prune -o -type f \( -path '*/objects/info/alternates' -o -path '*/.git/config' -o -name .git \) -print0 2>"$tmp.err")
  sed -nE "s/^find: '(.*)': Permission denied$/\1/p" "$tmp.err" | { if [ -n "${STORAGE_CLEANUP_ALLOW_UNREADABLE:-}" ]; then grep -Ev "$STORAGE_CLEANUP_ALLOW_UNREADABLE" || true; else cat; fi; } > "$INDEX.unreadable"
  rm -f "$tmp.err"; mv "$tmp" "$INDEX"
}

FAILS=0
fail() { printf '  FAIL %s\n' "$*"; FAILS=$((FAILS+1)); }
pass() { printf '  ok   %s\n' "$*"; }

gate_path() { # $1 is the path as given, before any symlink is resolved
  local p="$1" rp n
  [ -e "$p" ] || { fail "path: $p does not exist"; return; }
  [ ! -L "$p" ] || { fail "path: $p is a symlink"; return; }
  rp="$(readlink -f "$p")"
  case "$rp" in "$ROOT"/*) ;; *) fail "path: $rp is outside $ROOT"; return;; esac
  case "$rp" in */.git|*/.git/*) fail "path: $rp is inside a .git directory"; return;; esac
  n="$(find "$rp" -print0 2>/dev/null | grep -zEc "$PROTECTED_RE")"   # -c reads everything: with grep -q, pipefail reads find's SIGPIPE as "no match"
  if [ "${n:-0}" -gt 0 ]; then fail "path: $rp is or contains a protected path"; return; fi
  pass "path inside $ROOT, not a symlink, not protected, holds nothing protected"
}

gate_mounts() { # rm must never reach another filesystem
  local p="$1" mp bad=""
  while IFS= read -r mp; do
    mp="$(printf '%b' "$mp")"
    case "$mp" in "$p"|"$p"/*) bad="$bad $mp";; esac
  done < <(awk '{print $5}' /proc/self/mountinfo)
  if [ -n "$bad" ]; then fail "mounts: mount point(s) at or below the target:$bad"; else pass "no mount point at or below it"; fi
}

gate_live() { # cwd, exe, fds and mappings only (command lines are not evidence: a scan matches itself)
  local p="$1" esc hit m
  esc="$(printf '%s' "$p" | sed 's/[][*?\\]/\\&/g')"   # find -lname treats these as glob characters
  hit="$(find /proc/[0-9]*/fd /proc/[0-9]*/cwd /proc/[0-9]*/exe -maxdepth 1 \( -lname "$esc" -o -lname "$esc/*" \) 2>/dev/null \
    | grep -v "^/proc/$$/" | sed -E 's#^/proc/([0-9]+)/([a-z]+).*#\1(\2)#' | sort -u | tr '\n' ' ')"
  m="$(grep -lF "$p/" /proc/[0-9]*/maps 2>/dev/null | grep -v "^/proc/$$/" | sed -E 's#^/proc/([0-9]+)/.*#\1(map)#' | sort -u | tr '\n' ' ')"
  if [ -n "$hit$m" ]; then fail "live: in use by pid $hit$m"; else pass "no live process of this user uses it (cwd/exe/fd/maps; other users' processes cannot be inspected)"; fi
}

gate_dependents() { # other repos that need objects, gitdirs or a local remote inside the target
  local p="$1" kind file tgt n=0
  fresh_index
  if [ -s "$INDEX.unreadable" ]; then
    fail "dependency scan incomplete: $(wc -l < "$INDEX.unreadable" | tr -d ' ') unreadable director(ies), e.g. $(head -1 "$INDEX.unreadable"); set STORAGE_CLEANUP_ALLOW_UNREADABLE for known-safe ones"; return
  fi
  while IFS=$'\t' read -r kind file tgt; do
    case "$file" in "$p"/*) continue;; esac
    case "$tgt" in "$p"|"$p"/*) fail "$kind: $file points into the target ($tgt)"; n=$((n+1));; esac
  done < "$INDEX"
  [ "$n" -eq 0 ] && pass "no alternates file, gitfile or local remote elsewhere points into it$INDEX_NOTE"
}

gate_repo() { # one checkout: clean, no stash, no linked worktrees, every commit on a remote ref
  local r="$1" out rc n remote url risky tips="" safe=0
  if [ -d "$r/.git/worktrees" ] && [ -n "$(ls -A "$r/.git/worktrees" 2>/dev/null)" ]; then fail "repo $r: hosts linked worktrees"; return; fi
  out="$(rgit "$r" status --porcelain --untracked-files=all --ignore-submodules=all 2>&1)"; rc=$?
  [ $rc -eq 0 ] || { fail "repo $r: git status failed (rc=$rc)"; return; }
  [ -z "$out" ] || { fail "repo $r: $(printf '%s\n' "$out" | wc -l | tr -d ' ') dirty or untracked paths"; return; }
  for remote in $(rgit "$r" remote 2>/dev/null); do
    url="$(rgit "$r" config --get "remote.$remote.url")"
    [[ "$url" =~ $SAFE_URL_RE ]] || continue
    safe=$((safe+1))
    if [ -n "${NETWORK:-}" ]; then
      risky="$( { for sc in --local --worktree; do GIT_CONFIG_NOSYSTEM=1 git -C "$r" config "$sc" --includes --name-only --get-regexp '.*' 2>/dev/null; done; } | grep -Eic "$RISK_RE")"
      if [ "${risky:-0}" -gt 0 ]; then fail "repo $r: risky config, network check refused"; return; fi
      ngit "$r" fetch -q --no-tags --prune "$remote" "+refs/heads/*:refs/remotes/$remote/*" 2>/dev/null \
        || { fail "repo $r: fetch from $remote failed"; return; }
    fi
    tips="$tips$(rgit "$r" for-each-ref --format='%(objectname)' "refs/remotes/$remote/")"$'\n'
  done
  if [ "$safe" -eq 0 ]; then n="$(rgit "$r" rev-list --count --all 2>/dev/null)"; else
    n="$(printf '%s' "$tips" | grep . | sed 's/^/^/' | rgit "$r" rev-list --count --all --stdin 2>/dev/null)"; fi
  if [ "${n:-1}" = 0 ]; then pass "repo $r: clean, no stash, every commit reachable from a remote ref ($safe usable remote(s))"
  else fail "repo $r: ${n:-?} commits on no upstream remote ref${NETWORK:+ (after fetching)}; local-path remotes do not count"; fi
}

gate_git() {
  local p="$1" g top dir anc found=0 rc rc2 lsf sts d="$1" base
  [ -d "$p" ] || d="$(dirname "$p")"
  anc="$d"; while [ "$anc" != "$ROOT" ] && [ "$anc" != / ] && [ ! -e "$anc/.git" ]; do anc="$(dirname "$anc")"; done
  top="$(rgit "$d" rev-parse --show-toplevel 2>/dev/null)"
  case "$top" in "$ROOT"|"$ROOT"/*) ;; *) top="";; esac   # a checkout enclosing the whole root is not this tool's business
  if [ -e "$anc/.git" ] && [ "$top" != "$anc" ]; then
    fail "enclosing checkout $anc is unreadable by git (broken .git or config): its work cannot be checked"
  elif [ -n "$top" ] && [ "$top" != "$p" ]; then # inside a checkout: only a known build directory of ignored output may go
    base="$(basename "$p")"
    lsf="$(rgit "$top" ls-files -- "$p" 2>&1)"; rc=$?
    sts="$(rgit "$top" status --porcelain --untracked-files=all --ignore-submodules=all -- "$p" 2>&1)"; rc2=$?
    if [ "$rc" -ne 0 ] || [ "$rc2" -ne 0 ]; then
      fail "inside checkout $top: git could not list or status the target (corrupt index?)"
    elif [ -n "$lsf$sts" ]; then
      fail "inside checkout $top: tracked or untracked non-ignored files under the target"
    elif [[ "$BUILD_DIRS" != *" $base "* ]]; then
      fail "inside checkout $top: ignored files may be the only copy of notes or config; only build dirs (${BUILD_DIRS# }) may go"
    else pass "inside checkout $top: ignored build output ($base)"; fi
  fi
  while IFS= read -r -d '' g; do
    found=1; gate_repo "$(dirname "$g")"
  done < <(find "$p" -name .git -prune -print0 2>/dev/null)
  while IFS= read -r -d '' dir; do # bare repositories hold refs nothing else has
    [ -f "$dir/HEAD" ] && [ -d "$dir/refs" ] && [ "$(basename "$dir")" != .git ] && { found=1; fail "bare repository inside: $dir"; }
  done < <(find "$p" -type d -name objects -not -path '*/.git/*' -printf '%h\0' 2>/dev/null)
  find "$p" -print0 >/dev/null 2>&1; rc=$?
  [ "$rc" -eq 0 ] || fail "find could not read part of the tree (permissions): repos below cannot be checked"
  [ "$found" -eq 1 ] || [ -n "$top" ] || pass "contains no git checkout"
}

run_gates() { # $1 resolved path, $2 the path as given (a symlink is refused before it is resolved)
  FAILS=0; gate_path "${2:-$1}"; [ "$FAILS" -gt 0 ] && return 1
  gate_mounts "$1"; gate_live "$1"; gate_dependents "$1"; gate_git "$1"; [ "$FAILS" -eq 0 ]
}

cmd_gates() {
  local p="" orig="" NETWORK=""
  while [ $# -gt 0 ]; do case "$1" in --network) NETWORK=1;; -*) usage;; *) p="$1";; esac; shift; done
  [ -n "$p" ] || usage
  orig="$(norm "$p")"; p="$(readlink -f "$orig" 2>/dev/null || printf '%s' "$orig")"; printf 'gates for %s\n' "$p"
  if run_gates "$p" "$orig"; then echo "PASS"; else echo "REFUSED ($FAILS failed)"; return 1; fi
}

cmd_delete() {
  local p="" orig="" cls="" ev="" NETWORK="" offline="" apply="" b res rc
  while [ $# -gt 0 ]; do
    case "$1" in
      --class) cls="${2:-}"; shift;; --evidence) ev="${2:-}"; shift;;
      --network) NETWORK=1;; --offline) offline=1;; --apply) apply=1;; -*) usage;; *) p="$1";;
    esac; shift
  done
  [ -n "$p" ] && [ -n "$cls" ] && [ -n "$ev" ] || usage
  [ -z "$apply" ] || [ -n "$NETWORK$offline" ] || die "--apply needs --network (fetch every remote first) or --offline (trust the remote-tracking refs)"
  orig="$(norm "$p")"; p="$(readlink -f "$orig" 2>/dev/null || printf '%s' "$orig")"; printf 'delete %s\n' "$p"
  REFRESH=1 run_gates "$p" "$orig" || { echo "REFUSED ($FAILS failed); nothing deleted"; return 1; }
  b="$(size_of "$p")"
  [ -n "$apply" ] || { echo "DRY RUN: would delete $p (${b:-?} bytes); pass --apply"; return 0; }
  mkdir -p "$LOGDIR"; ev="$(printf '%s' "$ev" | tr '\t\n' '  ')"
  journal() { printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$(date -Is)" "$(shown "$p")" "${b:-0}" "$cls" "$ev" "$1" >> "$ACTIONS"; }
  { [ -s "$ACTIONS" ] || printf 'time\tpath\tbytes\tclass\tevidence\tresult\n' > "$ACTIONS"; } 2>/dev/null && journal pending 2>/dev/null \
    || die "cannot write the action journal $ACTIONS; nothing deleted"
  # the network fetch and the size measurement took time: re-check the local state right before removing
  local keep="$NETWORK"; NETWORK=""
  REFRESH=1 run_gates "$p" "$orig" >/dev/null; rc=$?; NETWORK="$keep"
  [ "$rc" -eq 0 ] || { journal "aborted: state changed" 2>/dev/null; echo "REFUSED: the target changed while the gates ran; nothing deleted"; return 1; }
  # --one-file-system is defence in depth: gate_mounts already refuses a target with a mount below it
  if rm -rf --one-file-system -- "$p"; then res=ok; else res=fail; fi
  journal "$res" 2>/dev/null || { echo "WARNING: $p was processed ($res) but the journal write failed" >&2; return 1; }
  echo "$res: $p (${b:-0} bytes) logged in $ACTIONS"; [ "$res" = ok ]
}

cmd_audit() {
  local top=20 p; local -a paths=()
  while [ $# -gt 0 ]; do case "$1" in --top) top="${2:-20}"; shift;; -*) usage;; *) paths+=("$1");; esac; shift; done
  [ "${#paths[@]}" -gt 0 ] || paths=("$ROOT")
  df -h "${paths[0]}" | tail -n +2
  if [ "$(stat -f -c %T "${paths[0]}")" = btrfs ]; then
    btrfs filesystem usage "${paths[0]}" 2>/dev/null | grep -E '^(Data|Metadata),|Unallocated|Free \(estimated'
    echo "note: metadata is DUP; deleting many small files frees metadata only after a balance (root)."
  fi
  echo "top $top children by Exclusive size on btrfs (apparent size elsewhere); hardlinks count in every"
  echo "directory that holds them and reflinks once per directory, so a sum overstates what deleting frees:"
  for p in "${paths[@]}"; do
    find "$p" -mindepth 1 -maxdepth 1 -print0 2>/dev/null | { grep -zEv "$PROTECTED_RE" || true; } \
      | xargs -0 -r -P4 -n1 sh -c 'p=$1; if [ "$(stat -f -c %T "$p")" = btrfs ]; then s=$(btrfs filesystem du -s --raw "$p" 2>/dev/null | tail -1 | awk "{print \$2}"); else s=$(du -sxb "$p" 2>/dev/null | cut -f1); fi; printf "%s\t%s\n" "${s:-0}" "$p"' _ \
      | sort -rn | head -n "$top" | awk -F'\t' '{printf "%8.2f GiB  %s\n", $1/1073741824, $2}'
  done
}

cmd_dedupe() {
  local apply="" min=1048576 out="$LOGDIR/dedupe-groups.jsonl"; local -a roots=()
  while [ $# -gt 0 ]; do
    case "$1" in --apply) apply=1;; --min-size) min="${2:-}"; shift;; --out) out="${2:-}"; shift;; -*) usage;; *) roots+=("$1");; esac; shift
  done
  [ "${#roots[@]}" -gt 0 ] || usage; mkdir -p "$LOGDIR"
  nice -n 19 ionice -c3 python3 -I "$HERE/lib/btrfs_dedupe.py" scan "${roots[@]}" --min-size "$min" --protected "$PROTECTED_RE" --out "$out" || return 1
  [ -n "$apply" ] || { echo "estimate only: $out; pass --apply to share extents (kernel byte-compare, nothing deleted)"; return 0; }
  nice -n 19 ionice -c3 python3 -I "$HERE/lib/btrfs_dedupe.py" apply "$out" --fs-path "${roots[0]}" --protected "$PROTECTED_RE" --log "$LOGDIR/dedupe.log"
}

objects_dir() { if [ -d "$1/objects" ]; then echo "$1/objects"; else echo "$1/.git/objects"; fi; }

cmd_repair_alternates() { # restore a repo whose alternates source was deleted, then prove it is self-contained
  local r="${1:-}" s line head alt
  [ -n "$r" ] && [ "$#" -ge 2 ] && [ -d "$r/.git" ] || usage
  shift; alt="$r/.git/objects/info/alternates"; head="$(rgit "$r" rev-parse HEAD 2>/dev/null)"
  [ -f "$alt" ] || die "$r has no alternates file; nothing to repair"
  cp "$alt" "$alt.pre-repair"
  { while IFS= read -r line; do case "$line" in /*) ;; *) line="$r/.git/objects/$line" ;; esac; [ -n "$line" ] && readlink -m "$line"; done < "$alt.pre-repair"
    for s in "$@"; do objects_dir "$(readlink -f "$s")"; done; } | awk 'NF && !seen[$0]++' \
    | while IFS= read -r line; do [ -d "$line" ] && printf '%s\n' "$line"; done > "$alt"
  if ! rgit "$r" repack -a -d -q 2>/dev/null; then cp "$alt.pre-repair" "$alt"; die "repack failed (objects missing from every store); alternates restored, old packs kept"; fi
  mv "$alt" "$alt.hold"
  if rgit "$r" rev-list --objects --all >/dev/null 2>&1; then
    rm -f "$alt.hold"; echo "self-contained: $r (HEAD ${head:0:9}); backup of the old alternates: $alt.pre-repair"
  else mv "$alt.hold" "$alt"; die "still incomplete without alternates; restored the original list"; fi
}

cmd="${1:-}"; [ -n "$cmd" ] || usage; shift
case "$cmd" in
  audit) cmd_audit "$@";; gates) cmd_gates "$@";; delete) cmd_delete "$@";;
  dedupe) cmd_dedupe "$@";; repair-alternates) cmd_repair_alternates "$@";; *) usage;;
esac
