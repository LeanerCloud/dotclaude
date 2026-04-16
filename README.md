# dotclaude

A personal, opinionated configuration for [Claude Code](https://claude.com/claude-code). These are the files that live under `~/.claude/` and shape how Claude Code behaves across every project on the machine — coding standards, git workflow, tool-selection rules, multi-agent coordination, and a curated agent library.

Published as-is in case it's useful as a starting point. Fork it, trim what you don't need, bend the rest to your preferences.

## What's in here

| File | Purpose |
|------|---------|
| [`CLAUDE.md`](CLAUDE.md) | Root config. Loaded at the start of every session. Points at the topic files below. |
| [`coding-standards.md`](coding-standards.md) | Stack preferences, testing philosophy, error handling, security, API design. |
| [`conventions.md`](conventions.md) | Language- and tool-specific conventions: Go, TypeScript, Python, Shell, Docker, Terraform, databases. |
| [`git-workflow.md`](git-workflow.md) | Conventional commits, pre-commit review loop, atomic commits, PR rules, post-push CI watcher. |
| [`tool-usage.md`](tool-usage.md) | When to use native tools vs. Bash, how to avoid approval prompts, script review loop. |
| [`infra-ops.md`](infra-ops.md) | Rollback awareness, secrets, monitoring, timeouts, CI/CD, Terraform ops. |
| [`project-docs.md`](project-docs.md) | Project knowledge-base layout, ADR template, runbook template, `known-issues.md` convention. |
| [`multi-agent-comms.md`](multi-agent-comms.md) | Protocol for multiple Claude instances / agents coordinating on the same project. |
| [`commands/`](commands/) | Custom slash commands. |
| [`agents/`](agents/) | Submodule pointing to [`contains-studio/agents`](https://github.com/contains-studio/agents) — a curated agent library. |
| [`projects.md.example`](projects.md.example) | Template for `projects.md`, the personal index of projects Claude should know about. |
| [`settings.example.json`](settings.example.json) | Template for `settings.json`, listing enabled plugins and other Claude Code options. |

## Using it

> **Heads up**: this installs into `~/.claude/`, which already contains your local Claude Code state (sessions, tasks, plans, caches). The repo's `.gitignore` is designed so cloning on top of an existing `~/.claude` is safe — runtime state won't be tracked — but **back up anything you care about first**.

1. **Back up your existing `~/.claude`:**
   ```bash
   mv ~/.claude ~/.claude.backup
   ```

2. **Clone with submodules:**
   ```bash
   git clone --recurse-submodules git@github.com:LeanerCloud/dotclaude.git ~/.claude
   ```

   If you already cloned without `--recurse-submodules`:
   ```bash
   git -C ~/.claude submodule update --init --recursive
   ```

3. **Create your personal config from the templates:**
   ```bash
   cp ~/.claude/projects.md.example ~/.claude/projects.md
   cp ~/.claude/settings.example.json ~/.claude/settings.json
   ```
   Both are gitignored, so edits stay local.

4. **Restore anything you need** from the backup (e.g. `plugins/`, `projects/`, `sessions/`).

## Customizing

Everything here is opinion, not gospel. The rules in `CLAUDE.md` and its referenced files are instructions to Claude — edit them to match how you work.

- Change the preferred stack in `coding-standards.md`.
- Adjust the commit conventions in `git-workflow.md`.
- Swap out the agent submodule in `.gitmodules` for a different library, or vendor your own agents into `agents/`.
- Add your own slash commands under `commands/`.

The topic-file layout (root `CLAUDE.md` pointing at focused sub-docs) keeps each file small enough to read in full during a session. If a file grows past ~300 lines, consider splitting it further.

## Contributing

This is primarily a personal config. PRs that fix clear bugs, typos, or outdated advice are welcome. Feature additions that make sense only for a specific workflow are better kept in a fork.

## License

[MIT](LICENSE).
