---
name: tool-usage
description: When to use native file tools instead of Bash, which shell patterns trigger approval
  prompts, and the rule that multiline shell must become a reviewed script file. Invoke before any
  Bash call or before writing a shell script.
---

# Tool Usage - Native Tools, Bash, and Scripts

Pick the right tool so every avoidable approval prompt is avoided.

## Prefer native tools over Bash for file operations

Native tools are faster, safer and don't prompt:

- **Read**: `Read`, not `cat`/`head`/`tail`/`less`
- **Edit**: `Edit` (or `Write` for new files), not `sed`/`awk`/heredoc-to-file
- **Find files**: `Glob`, not `find`/`ls`
- **Search content**: `Grep`, not `grep -r`/`rg` via Bash
- **Notebooks**: `NotebookEdit`

## When you must use Bash, write commands that don't need approval

- **Avoid composed commands (`&&`, `||`, `;`)**: the approval system evaluates the whole line and flags the combination even when each part passes. Prefer separate calls, issued in parallel when independent. Use `&&` only when the commands are tightly coupled and the chain is materially simpler.
- **Avoid `cd`**: the Bash tool persists `cwd`, so check the environment block's working directories first and use relative or absolute paths. `cd` only when a tool resolves config from its own directory.
- **No compound `cd && ...`** (e.g. `cd && git ...` triggers bare-repository-attack prevention). Use directory flags: `git -C /abs/path status`, `npm --prefix /abs/path test`; most CLIs (`git`, `npm`, `make`, `terraform`, `cargo`, `go`) have one.
- **No shell expansions on untrusted paths** (globs, `$(...)`, backticks, `<(...)`): use literal arguments, or enumerate with `Glob`/`Grep` first.
- **No `sudo`, `rm -rf`, `chmod`, `chown`** unless absolutely necessary.
- **No piping into `bash`/`sh`** (`curl ... | sh`).
- **No `eval` / `source` / `.` of dynamic content.**

## ⚠️ Multiline shell MUST be a script file - NO EXCEPTIONS

Anything spanning more than one line (heredocs, `for`/`while` loops, multi-statement `bash -c`, pipelines with line continuations, embedded newlines) is never inlined in a `Bash` call. `Write` it to a script file and run `bash <path>`. No "just this once".

### Two script locations by lifetime

- **Temporary** (single-task, throw-away) -> `/tmp/claude/`, with a descriptive unique name (`/tmp/claude/inspect-<topic>.sh`).
  - **Why `/tmp/claude/`, not in the project**: in-project scratch paths (e.g. `.claude/scripts/tmp/`) sit under every repo's sensitive-file radar and trigger per-project prompts; `/tmp/claude/` is outside any project. Create it lazily with `mkdir -p /tmp/claude`; the OS clears it on reboot.
  - **Delete after use** with `rm` on the explicit path.
  - Commit-message files also live here (`git-commit` skill), deleted after `git commit -F` succeeds.
- **Persistent** (reusable across sessions, session tooling not project code) -> `.claude/scripts/`: recurring diagnostics, wrappers for long `aws cli` commands, deployment sanity checks. Don't auto-delete; prune periodically.

### How to create and run scripts

- Create with the `Write` tool (heredoc-to-file via Bash itself prompts).
- **Gitignore `.claude/scripts/`** at project level (propose adding `.claude/` to `.gitignore` if missing). `/tmp/claude/` needs no entry.
- **Promotion**: a broadly useful `.claude/scripts/` script moves to `scripts/` or `tools/` with the user's approval.

### Review scripts before running them

Before *executing* any script in `.claude/scripts/` or `/tmp/claude/`, run **2 review passes**, fix what they find, then run it. Two passes, including scripts that delete, move or overwrite files, push, or touch credentials; on those spend the passes on exactly that risk (a buggy one-off can `rm -rf` the wrong directory or leak a token).

Each pass checks:
- **Completeness**: does it do what you intended; all args/env handled?
- **Correctness**: quoting, escaping, exit-code propagation, `set -euo pipefail`, path handling, loop off-by-ones.
- **Security**: no injection from unquoted variables, no `eval` of untrusted input, no destructive ops on unvalidated paths, no secrets leaked by `set -x`.
- **Bugs**: races, missing error handling, resource leaks, wrong cwd/env assumptions.

No counter resets; after the second pass, run it.

## Auto-fixing formatters/linters: match the CI-pinned version, or don't run them

Auto-fixers (`gofmt`/`gofumpt`, `prettier`, `black`, `ruff format`, `terraform fmt`, `rubocop -a`) produce different output across tool versions. One whose version differs from CI's is harmful: `--all-files` (or an agent doing so) **rewrites CI-clean files repo-wide**, and a check-only hook reports false failures on files CI considers clean.

- Before trusting or committing a local formatter result, confirm your version matches CI's pin (workflow `setup-go`/`setup-node`, `.tool-versions`, `go.mod toolchain`, `.nvmrc`); if not, the result is unreliable.
- Pin the hook to the repo's version source (Go: `gofmt` from the `go.mod` `toolchain` via explicit `GOTOOLCHAIN=go1.xx.y`, recipe in `conventions` "Code Style"; JS: the repo's pinned `prettier`).
- When a CI format check fails on a stale branch, **rebase onto the cleaned-up base** rather than committing a local whole-repo reformat to "make CI green".
- Never stage auto-fixer output with `git add -A`/`git add .`; stage only the files your change owns.

**When in doubt**, read the Bash tool's "When not to use" section for the dedicated tools to prefer.
