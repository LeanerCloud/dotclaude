#!/usr/bin/env bash
# Run a lightweight Linux check in the darling-ci-lite container.
# usage: darling-ci-lite.sh [--build] <worktree> <command> [args...]
#   --build     (re)build the image first
#   <worktree>  mounted read-only at /src; /tmp and $HOME are tmpfs scratch
# Serialized with other heavy jobs on this host; not for full Darling builds.
# macOS only (needs lockf) and docker.
set -euo pipefail

IMAGE=darling-ci-lite
if ! DOCKERFILE_DIR=$(cd "$(dirname "$0")/../docker/darling-ci-lite" 2>/dev/null && pwd -P); then
	echo "darling-ci-lite: Dockerfile dir not found next to this script (expected ../docker/darling-ci-lite)" >&2
	exit 1
fi
LOCK=/tmp/claude/heavy.lock

build=0
if [ "${1:-}" = "--build" ]; then
	build=1
	shift
fi
if [ "$#" -lt 2 ]; then
	echo "usage: $0 [--build] <worktree> <command> [args...]" >&2
	exit 2
fi

for tool in docker lockf; do
	command -v "$tool" >/dev/null 2>&1 || { echo "darling-ci-lite: $tool not found in PATH" >&2; exit 1; }
done

if [ ! -d "$1" ]; then
	echo "darling-ci-lite: worktree is not a directory: $1" >&2
	exit 1
fi
worktree=$(cd "$1" && pwd -P)
shift

mkdir -p "$(dirname "$LOCK")"

if [ "$build" -eq 1 ] || ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
	lockf -k -t 600 "$LOCK" nice -n 10 docker build --pull -t "$IMAGE" "$DOCKERFILE_DIR"
fi

# A git worktree's .git file points outside the mount; mount the common git dir read-only too.
extra=()
if [ -f "$worktree/.git" ]; then
	common=$(git -C "$worktree" rev-parse --path-format=absolute --git-common-dir)
	extra+=(--mount "type=bind,source=$common,target=$common,readonly")
fi

exec lockf -k -t 600 "$LOCK" nice -n 10 docker run --rm \
	--memory=4g --cpus=2 \
	--network=none \
	--mount "type=bind,source=$worktree,target=/src,readonly" \
	${extra[@]+"${extra[@]}"} \
	--tmpfs /tmp:rw,size=1g,exec \
	--tmpfs /home/ubuntu:rw,size=256m,uid=1000,gid=1000 \
	-e HOME=/home/ubuntu \
	-e GIT_CONFIG_COUNT=1 -e GIT_CONFIG_KEY_0=safe.directory -e GIT_CONFIG_VALUE_0='*' \
	-w /src \
	"$IMAGE" "$@"
