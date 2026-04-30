#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: setup-agent-symlinks.sh [--home DIR] [--claude-dir DIR]

Create or update Codex and Gemini symlinks to the shared Claude guidance files.

Options:
  --home DIR        Home directory containing .codex and .gemini (default: $HOME)
  --claude-dir DIR  Claude config directory to link to (default: HOME/.claude)
  -h, --help        Show this help.

The script is idempotent. It updates existing symlinks, creates missing symlinks,
and refuses to overwrite regular files or directories.
USAGE
}

home_dir="${HOME:?HOME is not set}"
claude_dir=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --home)
      if [ "$#" -lt 2 ]; then
        echo "error: --home requires a directory" >&2
        exit 2
      fi
      home_dir="$2"
      shift 2
      ;;
    --claude-dir)
      if [ "$#" -lt 2 ]; then
        echo "error: --claude-dir requires a directory" >&2
        exit 2
      fi
      claude_dir="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "error: unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [ -z "$claude_dir" ]; then
  claude_dir="$home_dir/.claude"
fi

if [ ! -d "$claude_dir" ]; then
  echo "error: Claude config directory does not exist: $claude_dir" >&2
  exit 1
fi

codex_dir="$home_dir/.codex"
gemini_dir="$home_dir/.gemini"

link_one() {
  target="$1"
  link_path="$2"

  if [ -e "$link_path" ] && [ ! -L "$link_path" ]; then
    echo "error: refusing to overwrite non-symlink: $link_path" >&2
    return 1
  fi

  if [ -L "$link_path" ]; then
    current_target="$(readlink "$link_path")"
    if [ "$current_target" = "$target" ]; then
      echo "ok: $link_path -> $target"
      return 0
    fi
    ln -sfn "$target" "$link_path"
    echo "updated: $link_path -> $target"
    return 0
  fi

  ln -s "$target" "$link_path"
  echo "created: $link_path -> $target"
}

link_shared_docs() {
  agent_dir="$1"
  mkdir -p "$agent_dir"

  link_one "$claude_dir/CLAUDE.md" "$agent_dir/AGENTS.md"
  link_one "$claude_dir/coding-standards.md" "$agent_dir/coding-standards.md"
  link_one "$claude_dir/conventions.md" "$agent_dir/conventions.md"
  link_one "$claude_dir/git-workflow.md" "$agent_dir/git-workflow.md"
  link_one "$claude_dir/infra-ops.md" "$agent_dir/infra-ops.md"
  link_one "$claude_dir/local-paths.md" "$agent_dir/local-paths.md"
  link_one "$claude_dir/multi-agent-comms.md" "$agent_dir/multi-agent-comms.md"
  link_one "$claude_dir/project-docs.md" "$agent_dir/project-docs.md"
  link_one "$claude_dir/projects.md" "$agent_dir/projects.md"
  link_one "$claude_dir/tool-usage.md" "$agent_dir/tool-usage.md"
  link_one "$claude_dir/triage.md" "$agent_dir/triage.md"
}

link_shared_docs "$codex_dir"
link_shared_docs "$gemini_dir"
link_one "$claude_dir/CLAUDE.md" "$gemini_dir/GEMINI.md"
