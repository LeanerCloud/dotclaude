# Tool Usage — Native Tools, Bash, and Scripts

Guidance for picking the right tool for each action in Claude Code. The goal is to keep the iteration loop fast and the user uninterrupted: every avoidable approval prompt breaks flow.

## Prefer native tools over Bash for file operations

Native tools are faster, safer, and do not trigger permission prompts:

- **Read files**: use the `Read` tool, not `cat`/`head`/`tail`/`less`
- **Edit files**: use `Edit` (or `Write` for new files), not `sed`/`awk`/heredoc-to-file
- **Find files**: use `Glob` (e.g. `**/*.tsx`), not `find` or `ls`
- **Search content**: use `Grep` (ripgrep-backed), not `grep -r` or `rg` via Bash
- **Edit notebooks**: use `NotebookEdit`, not direct file manipulation

## When you must use Bash, write commands that don't need approval

- **Avoid composed commands (`&&`, `||`, `;`) whenever possible**: composition is one of the most common approval triggers — the approval system evaluates the whole line, and the combined form is often flagged even when each individual command would pass on its own. Prefer separate Bash calls for independent commands; they can be issued in parallel from a single assistant turn when there are no dependencies between them, which is usually faster *and* cheaper than one chained call. Reach for `&&` only when the commands are tightly coupled (e.g. "only run Y if X succeeded" and you genuinely need that guard in the shell rather than in your control flow) and the chained form is materially simpler than two calls.
- **Be aware of the current working directory and avoid `cd` whenever possible**: the Bash tool persists `cwd` between commands within a session — it is almost never necessary to `cd` at all. Before issuing a Bash call, check the environment block for the current working directory and the project's primary/additional working directories; if the command's target is already reachable from there, just use a relative or absolute path. Reach for `cd` only when you genuinely need to switch contexts (e.g., running `npm` in a sub-package that resolves config from its own directory), and prefer absolute paths in command arguments over `cd` to that directory first.
- **No compound `cd && ...`**: shells with `cd` followed by other commands almost always require approval (e.g. `cd && git ...` triggers bare-repository-attack prevention). When you do need to operate from a different directory, use absolute paths in the command itself rather than chaining `cd` — e.g. `git -C /abs/path status` instead of `cd /abs/path && git status`, `npm --prefix /abs/path test` instead of `cd /abs/path && npm test`. Most CLIs (`git`, `npm`, `make`, `terraform`, `cargo`, `go`) accept a directory flag that removes the need for `cd` entirely.
- **No shell expansions on untrusted paths**: glob expansions (`*`, `?`, `{a,b}`), command substitution (`$(...)`, backticks), and process substitution (`<(...)`) often trigger approval. Prefer literal arguments, or use `Glob`/`Grep` to enumerate files first and then operate on the explicit list.
- **No `sudo`, `rm -rf`, `chmod`, `chown`** unless absolutely necessary — these always require approval and have wide blast radius.
- **No piping into `bash`/`sh`** (`curl ... | sh`) — always requires approval and is a common attack pattern.
- **No `eval` / `source` / `.` of dynamic content** — always requires approval.

## ⚠️ Multiline shell MUST be a script file — NO EXCEPTIONS

If a shell snippet would span more than one line — multi-line heredocs, `for`/`while` loops, multi-statement `bash -c` blocks, complex pipelines with line continuations, anything with embedded newlines — do NOT inline it in a `Bash` call. Write it to a script file with the `Write` tool and execute via a plain `bash <path>` call.

This rule has no exemptions — even "just this once" multiline commands count. Inline multiline shell is fragile, hard to debug, almost always triggers approval prompts, and quoting/escaping bugs are nearly guaranteed.

### Two script locations by lifetime

- **Temporary scripts** (single-task, throw-away within the current session) → `/tmp/claude/`
  - Examples: a one-off jq pipeline to inspect a JSON file, a curl loop to test an endpoint, a debugging helper that won't be needed again.
  - Use a descriptive, unique filename per script (e.g. `/tmp/claude/inspect-<topic>.sh`) so concurrent tasks don't collide.
  - **Why `/tmp/claude/` and not inside the project**: an in-project scratch path (e.g. `.claude/scripts/tmp/`) sits under every repo's sensitive-file radar — writing to it triggers per-project approval prompts. `/tmp/claude/` is outside any project, so writes don't trip that gating. Create the directory lazily with `mkdir -p /tmp/claude` before the first write if it doesn't exist. The OS clears `/tmp/` on reboot, so no long-term cleanup is needed.
  - **Clean up after each use**: `rm` the script with `Bash` on the explicit path once the task is done — single-file deletes are safe and keep `/tmp/claude/` tidy within the session.
  - **Commit-message files also live here** — see `git-workflow.md` for the full protocol. The same cleanup rule applies: delete after `git commit -F` succeeds.
- **Persistent scripts** (reusable across sessions, but still session-tooling not project code) → `.claude/scripts/`
  - Examples: a recurring environment diagnostic, a helper that wraps a long `aws cli` command you keep needing, a deployment sanity check.
  - **Don't auto-delete** — these accumulate intentionally as a personal toolbox. Review periodically and prune unused ones.

### How to create and run scripts

- **Use the `Write` tool** to create scripts in either location (not heredoc-to-file via Bash, which itself triggers approval).
- **`.claude/scripts/` should be gitignored** at the project level — session artefacts, not committed code. Propose adding `.claude/` to `.gitignore` if missing. `/tmp/claude/` is outside any repo so no gitignore entry is needed.
- **Promotion path**: if a `.claude/scripts/` script becomes broadly useful (others on the team would want it), propose moving it to a proper committed location (`scripts/`, `tools/`) with the user's approval. The `.claude/scripts/` tier is "useful to me, not yet promoted to project asset".

### ⚠️ Mandatory script review loop (3 clean passes)

Before *executing* any script written to `.claude/scripts/` or `/tmp/claude/`, enter a review loop and iterate until **3 consecutive review passes find zero issues**. This applies even to throw-away tmp scripts — a buggy one-off script can still `rm -rf` the wrong directory or leak a token.

Each pass checks the same four dimensions:

- **Completeness**: does it do what you intended? All required args/env handled?
- **Correctness**: quoting, escaping, exit-code propagation, `set -euo pipefail`, path handling, off-by-ones in loops.
- **Security**: no shell-injection from unquoted variables, no `eval` of untrusted input, no destructive ops on unvalidated paths, no leaked secrets in `set -x` output.
- **Bugs**: race conditions, missing error handling, resource leaks, broken assumptions about cwd/env.

Fix every issue found and reset the clean-pass counter — you need 3 clean passes *after* the last fix. The cost of a 30-second review is far less than the cost of a destructive bug.

## Why this matters

The cost of approval prompts is real: each one breaks flow, requires user attention, and slows the loop. Choosing the right tool the first time keeps iteration fast and the user uninterrupted.

**When in doubt**: read the Bash tool's "When not to use" section in its description — it lists the exact dedicated tools to prefer.
