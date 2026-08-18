# dotclaude

A personal, opinionated configuration for [Claude Code](https://claude.com/claude-code). These are the files that live under `~/.claude/` and shape how Claude Code behaves across every project on the machine — coding standards, git workflow, tool-selection rules, multi-agent coordination, backlog triage, and a curated agent library.

Published as-is in case it's useful as a starting point. Fork it, trim what you don't need, bend the rest to your preferences.

## About

I'm [Cristian Magherusan-Stanciu](https://www.linkedin.com/in/cristim/), founder of [LeanerCloud](https://leanercloud.com). We build cloud cost-optimization tooling — the open-source [AutoSpotting](https://github.com/LeanerCloud/AutoSpotting) (Spot-instance automation), [savings-estimator](https://github.com/LeanerCloud/savings-estimator), and a multi-cloud commitment optimizer (RIs / Savings Plans / GCP CUDs / Azure Reservations) deployed across AWS, Azure, and GCP.

That work shapes the opinions in here: heavy use of multi-cloud Terraform, supply-chain-hardening reflexes, post-push CI watchers, CodeRabbit-loop iteration, and a strong preference for landing security and cost-optimization fixes through small atomic PRs against shared feature branches rather than direct pushes. If your day looks similar — multi-cloud infra, security-first reviews, CR-driven feedback loops — these rules will probably feel natural. If it doesn't, fork freely.

## How it's organised

[`CLAUDE.md`](CLAUDE.md) is the always-on core: the nine core tenets, the principles, the six review
dimensions, the model-tier table, and a routing table. Everything with a specific trigger lives in a
[**skill**](skills/) whose body loads only when that trigger fires, so a session no longer starts by
reading ~130 KB of guidance it mostly won't use.

Skills follow the [Agent Skills](https://agentskills.io) open standard, so **the same files drive
Claude Code, Codex CLI and Gemini CLI**. See [`skills/README.md`](skills/README.md) for the
portability contract and the per-tool discovery paths.

| Skill | Invoke when |
|-------|-------------|
| [`coding-standards`](skills/coding-standards/) | writing or reviewing code; first visit to any project; **before launching a user-facing app** |
| [`conventions`](skills/conventions/) | Go, TypeScript, Python, Shell, Docker, Terraform, databases |
| [`tool-usage`](skills/tool-usage/) | before any Bash call or shell script |
| [`git-commit`](skills/git-commit/) | before staging a commit or writing a message |
| [`ci-watch`](skills/ci-watch/) | immediately after any `git push` |
| [`pr-lifecycle`](skills/pr-lifecycle/) | opening a PR, or driving one to merge |
| [`cr-loop`](skills/cr-loop/) | a CodeRabbit review is pending or has arrived |
| [`pr-iterate`](skills/pr-iterate/) | driving one or many existing PRs to merge-ready |
| [`rate-limit-retry`](skills/rate-limit-retry/) | any 429 / usage limit / "try again later" |
| [`review-staged-diff`](skills/review-staged-diff/) | reviewing a staged changeset before it lands |
| [`review-and-implement`](skills/review-and-implement/) | hardening a plan, then building it |
| [`worktrees`](skills/worktrees/) | starting any non-trivial change |
| [`subagent-strategy`](skills/subagent-strategy/) | deciding how to delegate, or which tier |
| [`multi-agent-comms`](skills/multi-agent-comms/) | several agents share one project |
| [`pr-orchestration`](skills/pr-orchestration/) | orchestrating several PRs/agents at once |
| [`issue-pr-autopilot`](skills/issue-pr-autopilot/) | the scheduled issue→PR autopilot |
| [`triage-labels`](skills/triage-labels/) | any untriaged issue or PR you touch |
| [`triage-pass`](skills/triage-pass/) | "triage", "prioritize the backlog" |
| [`work-selection`](skills/work-selection/) | "what should I work on next?" |
| [`infra-ops`](skills/infra-ops/) | infrastructure, deployments, cloud resources, ops |
| [`project-docs`](skills/project-docs/) | project documentation, ADRs, `known-issues.md` |

The former flat topic docs (`git-workflow.md`, `triage.md`, …) remain as pointer stubs naming their
successor skills, so older references keep resolving.

| Other files | Purpose |
|------|---------|
| [`scripts/setup-agent-symlinks.sh`](scripts/setup-agent-symlinks.sh) | Link each skill into `~/.agents/skills/` (read by Codex and Gemini) and the root config into `~/.codex` and `~/.gemini`. |
| [`scripts/validate-skills.sh`](scripts/validate-skills.sh) | Pre-commit check that every skill stays discoverable by all three tools. |
| [`agents/`](agents/) | Submodule pointing to [`contains-studio/agents`](https://github.com/contains-studio/agents) — a curated agent library. |
| [`local-paths.md.example`](local-paths.md.example) | Template for `local-paths.md`, per-machine paths and tool locations referenced from the rule files (e.g. graphify CLI / venv). |
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
   cp ~/.claude/local-paths.md.example ~/.claude/local-paths.md
   cp ~/.claude/settings.example.json ~/.claude/settings.json
   ```
   All three are gitignored, so edits stay local.

4. **Restore anything you need** from the backup (e.g. `plugins/`, `projects/`, `sessions/`).

5. **Expose these instructions to Codex and Gemini CLI:**
   ```bash
   ~/.claude/scripts/setup-agent-symlinks.sh
   ```
   This symlinks every skill into `~/.agents/skills/`, which Codex reads as its user scope and
   Gemini reads as an alias of `~/.gemini/skills/`. Claude Code reads `~/.claude/skills/` directly,
   so there is one canonical copy and no duplication. Verify with `gemini skills list` and
   `codex debug prompt-input`.

## Customizing

Everything here is opinion, not gospel. The rules in `CLAUDE.md` and its referenced files are instructions to Claude — edit them to match how you work.

- Change the preferred stack in `coding-standards.md`.
- Adjust the commit conventions in `git-workflow.md`.
- Swap out the agent submodule in `.gitmodules` for a different library, or vendor your own agents into `agents/`.
- Add your own skills under `skills/<name>/SKILL.md` — they become `/name` in Claude, `$name` in
  Codex, and implicitly activatable in Gemini. Run `scripts/validate-skills.sh` before committing.

Split a skill when its body serves more than one trigger: a skill you invoke for one reason but which
loads instructions for three is the flat-file problem again, one level down. If a `SKILL.md` grows
past ~300 lines, that is usually the sign.

## Contributing

This is primarily a personal config. PRs that fix clear bugs, typos, or outdated advice are welcome. Feature additions that make sense only for a specific workflow are better kept in a fork.

If you'll be opening PRs, install the pre-commit hooks once:
```bash
brew install pre-commit         # or: pipx install pre-commit
pre-commit install              # registers the git hook in this clone
pre-commit run --all-files      # one-time clean pass before your first commit
```
The hook set is conservative (trailing whitespace, missing final newline, merge-conflict markers, malformed YAML, accidental large-file commits) — see [`.pre-commit-config.yaml`](.pre-commit-config.yaml) for the full list and rationale.

## License

[MIT](LICENSE).
