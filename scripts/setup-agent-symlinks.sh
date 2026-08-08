#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: setup-agent-symlinks.sh [--home DIR] [--claude-dir DIR]

Expose the shared Claude guidance to Codex CLI and Gemini CLI.

Two mechanisms:

  1. Skills -> HOME/.agents/skills/<name>, one symlink per skill directory.
     Codex reads $HOME/.agents/skills (user scope) and Gemini reads
     ~/.agents/skills as an alias of ~/.gemini/skills, so a single tree serves
     both. Claude Code reads CLAUDE_DIR/skills directly - the canonical copy.

  2. Root config + the legacy topic-doc paths -> HOME/.codex and HOME/.gemini.
     The topic docs are now pointer stubs naming their successor skills; the
     links are kept so any reference to the old paths still resolves.

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

  if [ ! -e "$target" ]; then
    echo "error: target does not exist: $target" >&2
    return 1
  fi

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

link_skills() {
  agents_skills_dir="$home_dir/.agents/skills"
  mkdir -p "$agents_skills_dir"

  found=0
  for skill_path in "$claude_dir"/skills/*/; do
    [ -f "$skill_path/SKILL.md" ] || continue
    skill_name="$(basename "$skill_path")"
    link_one "${skill_path%/}" "$agents_skills_dir/$skill_name"
    found=$((found + 1))
  done

  if [ "$found" -eq 0 ]; then
    echo "error: no skills found under $claude_dir/skills" >&2
    return 1
  fi

  # Prune links this script made for skills that have since been renamed or merged away. Scoped
  # deliberately: only a SYMLINK, only one whose target is under CLAUDE_DIR/skills, and only when
  # that target no longer resolves. Anything else is left alone.
  for link in "$agents_skills_dir"/*; do
    [ -L "$link" ] || continue
    [ -e "$link" ] && continue
    case "$(readlink "$link")" in
      "$claude_dir"/skills/*)
        rm "$link"
        echo "pruned: $link (target no longer exists)"
        ;;
      *)
        echo "warn: leaving broken symlink not owned by this script: $link" >&2
        ;;
    esac
  done

  echo "linked $found skill(s) into $agents_skills_dir"
}

link_shared_docs "$codex_dir"
link_shared_docs "$gemini_dir"
link_one "$claude_dir/CLAUDE.md" "$gemini_dir/GEMINI.md"
link_skills
