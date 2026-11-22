#!/usr/bin/env bash
# Fixture suite for scripts/storage-cleanup.sh (Linux).
#
#   bash scripts/tests/storage-cleanup.test.sh
#
# Every case builds scratch git repos under a throwaway root and points the script at it with
# STORAGE_CLEANUP_ROOT, so nothing outside the scratch dir is read or deleted. The dedupe case needs a
# btrfs directory: set STORAGE_TEST_BTRFS_DIR to one, otherwise it is skipped. SCRIPT may be overridden to
# test a mutated copy.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="${SCRIPT:-$HERE/../storage-cleanup.sh}"
BASE="${TMPDIR:-/tmp}/storage-cleanup-tests"
mkdir -p "$BASE" || exit 1
find "$BASE" -maxdepth 1 -name 'run.*' -mmin +120 -exec rm -rf {} + 2>/dev/null   # leftovers of killed runs
T="$(mktemp -d "$BASE/run.XXXXXX")" || exit 1
trap 'pkill -P $$ sleep 2>/dev/null; chmod -R u+rwx "$T" 2>/dev/null; rm -rf "$T"' EXIT

export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@example.invalid GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@example.invalid
export STORAGE_CLEANUP_ROOT="$T/home" STORAGE_CLEANUP_LOGDIR="$T/home/log" STORAGE_CLEANUP_INDEX_TTL=0
export STORAGE_CLEANUP_TRUST_LOCAL_REMOTES=1   # fixture origins are local bare repos
mkdir -p "$T/home"

PASS=0; FAIL=0
ok() { PASS=$((PASS+1)); printf 'ok   %s\n' "$1"; }
no() { FAIL=$((FAIL+1)); printf 'FAIL %s\n' "$1"; }
expect() { # name expected-rc command...
  local name="$1" want="$2"; shift 2
  local out; out="$("$@" 2>&1)"; local rc=$?
  if [ "$rc" = "$want" ]; then ok "$name"; else no "$name (rc=$rc, wanted $want)"; printf '%s\n' "$out" | sed 's/^/     /'; fi
}
S() { bash "$SCRIPT" "$@"; }
mkrepo() { # mkrepo DIR : a clone of a fresh bare origin (kept outside the tree under test) with one pushed commit
  local d="$1"; git init -q --bare "$T/origins/$(basename "$d").git" 2>/dev/null || { mkdir -p "$T/origins"; git init -q --bare "$T/origins/$(basename "$d").git"; }
  git clone -q "$T/origins/$(basename "$d").git" "$d" 2>/dev/null
  git -C "$d" commit -q --allow-empty -m init; git -C "$d" branch -M main; git -C "$d" push -q origin main 2>/dev/null
}

# --- path gates
mkdir -p "$T/home/.ssh" "$T/home/plain"; touch "$T/home/.ssh/k" "$T/home/plain/f"
expect "protected path is refused" 1 S gates "$T/home/.ssh"
ln -s "$T/home/plain" "$T/home/link"
expect "symlink is refused" 1 S gates "$T/home/link"
expect "symlink with a trailing slash is refused" 1 S gates "$T/home/link/"
mkdir -p "$T/outside"
expect "path outside the root is refused" 1 S gates "$T/outside"
expect "plain directory passes" 0 S gates "$T/home/plain"
mkdir -p "$T/home/pr/.claude/memory" "$T/home/pr/.claude/projects/p/memory"
expect "ancestor of a protected path is refused" 1 S gates "$T/home/pr/.claude"
expect "memory under projects is protected" 1 S gates "$T/home/pr/.claude/projects/p/memory"

# --- a large tree: grep -q under pipefail would read find's SIGPIPE as "no protected path"
mkdir -p "$T/home/big/.ssh" "$T/home/big/d"; touch "$T/home/big/.ssh/k"
seq 1 20000 | sed "s#^#$T/home/big/d/f#" | xargs touch
expect "a protected dir inside a 20000-file tree is still found" 1 S gates "$T/home/big"
mkdir -p "$T/home/big2/.ssh"; seq 1 20000 | sed "s#^#$T/home/big2/.ssh/f#" | xargs touch   # the match is find's first line, so grep -q would exit while find still writes
expect "a protected directory holding 20000 files is refused" 1 S gates "$T/home/big2/.ssh"

# --- live process (cwd inside the target)
mkdir -p "$T/home/busy"; ( cd "$T/home/busy" && exec sleep 60 ) >/dev/null 2>&1 & sleep 0.3
expect "directory used as a cwd is refused" 1 S gates "$T/home/busy"
pkill -P $$ sleep 2>/dev/null; sleep 0.2
expect "same directory passes once the process is gone" 0 S gates "$T/home/busy"

# --- mounts (needs unprivileged user namespaces)
if unshare -Urm true 2>/dev/null; then
  mkdir -p "$T/precious" "$T/home/mnt/inner"; echo keep > "$T/precious/x"
  out="$(unshare -Urm bash -c "mount --bind '$T/precious' '$T/home/mnt/inner' && bash '$SCRIPT' delete '$T/home/mnt' --class t --evidence e --offline --apply 2>&1; echo rc=\$?")"
  case "$out" in *"rc=1"*) ok "mount point below the target is refused";; *) no "mount point not refused: $out";; esac
  [ -f "$T/precious/x" ] && ok "bind-mounted data survived" || no "bind-mounted data was deleted"
else printf 'skip mount case (no unprivileged mount namespaces)\n'; fi

# --- git states
mkrepo "$T/home/clean"
expect "clean pushed repo passes" 0 S gates "$T/home/clean"
expect "a local-path remote is not upstream without the fixture flag" 1 env -u STORAGE_CLEANUP_TRUST_LOCAL_REMOTES bash "$SCRIPT" gates "$T/home/clean"
mkrepo "$T/home/dirty"; echo x > "$T/home/dirty/f"
expect "untracked file refuses" 1 S gates "$T/home/dirty"
mkrepo "$T/home/stash"; echo x > "$T/home/stash/f"; git -C "$T/home/stash" add f; git -C "$T/home/stash" stash -q
expect "stash refuses" 1 S gates "$T/home/stash"
mkrepo "$T/home/ahead"; git -C "$T/home/ahead" commit -q --allow-empty -m local
expect "unpushed commit refuses" 1 S gates "$T/home/ahead"
mkrepo "$T/home/wt"; git worktree --help >/dev/null 2>&1; git -C "$T/home/wt" worktree add -q -b side "$T/outside/wt-side"
expect "repo hosting a worktree outside the root refuses" 1 S gates "$T/home/wt"
mkrepo "$T/home/sub"; mkdir -p "$T/home/sub/src"; echo 'int x;' > "$T/home/sub/src/new.c"
expect "subdirectory of a checkout with untracked work refuses" 1 S gates "$T/home/sub/src"
printf 'build/\n' > "$T/home/sub/.gitignore"; git -C "$T/home/sub" add .gitignore; git -C "$T/home/sub" commit -q -m ignore; git -C "$T/home/sub" push -q origin main 2>/dev/null
rm -rf "$T/home/sub/src"; mkdir -p "$T/home/sub/build"; echo o > "$T/home/sub/build/a.o"
expect "ignored build output inside a checkout may go" 0 S gates "$T/home/sub/build"
mkdir -p "$T/home/sub/notes"; echo plan > "$T/home/sub/notes/plan.md"; printf 'notes/\n' >> "$T/home/sub/.gitignore"
git -C "$T/home/sub" add .gitignore; git -C "$T/home/sub" commit -q -m ign2; git -C "$T/home/sub" push -q origin main 2>/dev/null
expect "an ignored directory that is not a build dir refuses (it may be the only copy)" 1 S gates "$T/home/sub/notes"
echo more > "$T/home/sub/untracked.txt"
expect "a single untracked file inside a checkout refuses" 1 S gates "$T/home/sub/untracked.txt"
rm -f "$T/home/sub/untracked.txt"
expect "env GIT_CEILING_DIRECTORIES cannot hide the enclosing checkout" 1 env GIT_CEILING_DIRECTORIES="$T/home/sub" bash "$SCRIPT" gates "$T/home/sub/notes"
expect "inside a .git directory is refused" 1 S gates "$T/home/sub/.git/objects"
mkdir -p "$T/home/proj"; git init -q --bare "$T/home/proj/only.git"; git clone -q "$T/home/proj/only.git" "$T/home/proj/co" 2>/dev/null
git -C "$T/home/proj/co" commit -q --allow-empty -m only-copy; git -C "$T/home/proj/co" push -q origin HEAD 2>/dev/null
expect "a bare repo holding the only copy refuses" 1 S gates "$T/home/proj"
mkdir -p "$T/home/deep/a/b/c/d/e"; mkrepo "$T/home/deep/a/b/c/d/e/r"; git -C "$T/home/deep/a/b/c/d/e/r" commit -q --allow-empty -m deep-local
expect "a repo more than 4 levels down is still gated" 1 S gates "$T/home/deep"
mkdir -p "$T/home/locked/sub"; chmod 000 "$T/home/locked/sub"
if [ "$(id -u)" != 0 ]; then expect "unreadable directory refuses" 1 S gates "$T/home/locked"; else printf 'skip unreadable case (root)\n'; fi
chmod 755 "$T/home/locked/sub"

mkrepo "$T/home/broken"; mkdir -p "$T/home/broken/src"; echo wip > "$T/home/broken/src/wip.c"; printf '[core\n' >> "$T/home/broken/.git/config"
expect "a checkout git cannot read refuses its subdirectories" 1 S gates "$T/home/broken/src"
mkrepo "$T/home/netfail"; git -C "$T/home/netfail" remote set-url origin ssh://127.0.0.1:9/none.git
expect "--network fails closed when the fetch fails" 1 env -u STORAGE_CLEANUP_TRUST_LOCAL_REMOTES bash "$SCRIPT" gates "$T/home/netfail" --network

# --- filters are neutralised, not executed (direct config and include.path)
mkrepo "$T/home/filt"; echo data > "$T/home/filt/f"; git -C "$T/home/filt" add f; git -C "$T/home/filt" commit -q -m f
git -C "$T/home/filt" push -q origin main 2>/dev/null
echo '* filter=evil' > "$T/home/filt/.gitattributes"; git -C "$T/home/filt" config filter.evil.clean "touch $T/PWNED"
git -C "$T/home/filt" add .gitattributes 2>/dev/null; rm -f "$T/PWNED"   # the setup itself ran the filter once
git -C "$T/home/filt" -c filter.evil.clean= commit -q -m attr; git -C "$T/home/filt" push -q origin main 2>/dev/null
touch "$T/home/filt/f"; git -C "$T/home/filt" status >/dev/null 2>&1
[ -e "$T/PWNED" ] && ok "fixture: an unprotected git status does run the filter" || no "fixture cannot detect filter execution"
rm -f "$T/PWNED"; touch "$T/home/filt/f"; S gates "$T/home/filt" >/dev/null 2>&1
[ ! -e "$T/PWNED" ] && ok "configured filter command is never run by the gates" || no "filter command ran"
git -C "$T/home/filt" config --unset filter.evil.clean; printf '[filter "evil"]\n\tclean = touch %s\n' "$T/PWNED" > "$T/home/filt/.git/evil.inc"
git -C "$T/home/filt" config include.path evil.inc; rm -f "$T/PWNED"; touch "$T/home/filt/f"; S gates "$T/home/filt" >/dev/null 2>&1
[ ! -e "$T/PWNED" ] && ok "a filter arriving through include.path is never run" || no "include.path filter ran"

git -C "$T/home/filt" config --unset include.path; git -C "$T/home/filt" config extensions.worktreeConfig true
git -C "$T/home/filt" config --worktree filter.evil.clean "touch $T/PWNED"; rm -f "$T/PWNED"; touch "$T/home/filt/f"; S gates "$T/home/filt" >/dev/null 2>&1
[ ! -e "$T/PWNED" ] && ok "a filter in the per-worktree config is never run" || no "worktree-config filter ran"

# --- alternates dependents
mkrepo "$T/home/base"; git clone -q --shared "$T/home/base" "$T/home/dep" 2>/dev/null
expect "repo used as another repo's alternates source refuses" 1 S gates "$T/home/base"
expect "the dependent itself can go" 0 S gates "$T/home/dep"
mkrepo "$T/home/r1"; git clone -q "$T/origins/r1.git" "$T/home/r2" 2>/dev/null   # r2 does not name r1 as a remote
printf '../../../r1/.git/objects\n' > "$T/home/r2/.git/objects/info/alternates"
expect "a relative alternates line (resolved from the objects dir) refuses" 1 S gates "$T/home/r1"
mkrepo "$T/home/r3"; ln -s "$T/home/r3/.git/objects" "$T/home/objlink"; git clone -q "$T/origins/r3.git" "$T/home/r4" 2>/dev/null
printf '%s\n' "$T/home/objlink" > "$T/home/r4/.git/objects/info/alternates"
expect "an alternates line through a symlink refuses" 1 S gates "$T/home/r3"
mkrepo "$T/home/e"; STORAGE_CLEANUP_INDEX_TTL=600 bash "$SCRIPT" gates "$T/home/e" >/dev/null 2>&1; git clone -q --shared "$T/home/e" "$T/home/edep" 2>/dev/null
expect "delete rebuilds a stale alternates index" 1 env STORAGE_CLEANUP_INDEX_TTL=600 bash "$SCRIPT" delete "$T/home/e" --class t --evidence e --offline --apply
mkdir -p "$T/home/pair"; git init -q --bare "$T/home/pair/o.git"; git clone -q "$T/home/pair/o.git" "$T/home/pair/c" 2>/dev/null
expect "a local remote pointing into the target refuses its dependents" 1 S gates "$T/home/pair/o.git"

# --- delete: dry run, apply, log row
mkrepo "$T/home/victim"
expect "delete is a dry run without --apply" 0 S delete "$T/home/victim" --class test --evidence "fixture"
[ -d "$T/home/victim" ] && ok "dry run kept the directory" || no "dry run deleted it"
expect "delete --apply needs an explicit network choice" 1 S delete "$T/home/victim" --class test --evidence x --apply
expect "delete --apply succeeds" 0 S delete "$T/home/victim" --class test --evidence "$(printf 'a\tb\nc')" --offline --apply
[ ! -e "$T/home/victim" ] && ok "directory is gone" || no "directory still there"
[ "$(awk -F'\t' 'END{print NF}' "$T/home/log/ACTIONS.tsv" 2>/dev/null)" = 6 ] && grep -q "$(printf 'test\ta b c\tok')" "$T/home/log/ACTIONS.tsv" && ok "ACTIONS.tsv row has six columns with the evidence sanitised" || no "bad ACTIONS.tsv row"
expect "delete refuses a dirty repo even with --apply" 1 S delete "$T/home/dirty" --class test --evidence fixture --offline --apply
[ -d "$T/home/dirty" ] && ok "refused delete kept the repo" || no "dirty repo was deleted"

# --- audit and dedupe argument handling
mkdir -p "$T/home/dir with space/child"; head -c 100000 /dev/zero > "$T/home/dir with space/child/f"
out="$(S audit "$T/home/dir with space" 2>&1)"; case "$out" in *"child"*) ok "audit handles a path with spaces and prints sizes";; *) no "audit lost the child: $out";; esac

# --- repair-alternates
mkrepo "$T/home/src"; git clone -q --shared "$T/home/src" "$T/home/child" 2>/dev/null
git -C "$T/home/child" commit -q --allow-empty -m child-only
mv "$T/home/src" "$T/gone-src"                                   # the alternates source disappears
git -C "$T/home/child" rev-list --objects --all >/dev/null 2>&1 && no "child should be broken" || ok "child is broken without its source"
before="$(cat "$T/home/child/.git/objects/info/alternates")"
expect "repair refuses when no store has the objects" 1 S repair-alternates "$T/home/child" "$T/home/plain"
[ "$(cat "$T/home/child/.git/objects/info/alternates")" = "$before" ] && ok "failed repair restored the alternates file" || no "alternates file changed by a failed repair"
expect "repair-alternates restores a self-contained repo" 0 S repair-alternates "$T/home/child" "$T/gone-src"
git -C "$T/home/child" rev-list --objects --all >/dev/null 2>&1 && ok "child complete after repair" || no "child still broken"
[ ! -f "$T/home/child/.git/objects/info/alternates" ] && ok "alternates file removed after the proof" || no "alternates file left behind"

# --- dedupe (btrfs only)
if [ -n "${STORAGE_TEST_BTRFS_DIR:-}" ]; then
  D="$STORAGE_TEST_BTRFS_DIR/dd.$$"; mkdir -p "$D"; head -c 4194304 /dev/urandom > "$D/a"; cp --reflink=never "$D/a" "$D/b"; touch -d '2 hours ago' "$D/a" "$D/b"
  STORAGE_CLEANUP_ROOT="$STORAGE_TEST_BTRFS_DIR" S dedupe --apply "$D" >/dev/null 2>&1; sync
  [ "$(btrfs filesystem du "$D/b" | tail -1 | awk '{print $2}')" = "0.00B" ] && ok "dedupe shared identical files" || no "dedupe did not share the copy"
  rm -rf "$D"
else printf 'skip dedupe case (set STORAGE_TEST_BTRFS_DIR)\n'; fi

printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"; [ "$FAIL" -eq 0 ]
