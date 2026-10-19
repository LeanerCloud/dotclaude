---
name: draft-pr-sweep
description: Take every open draft PR the user authored, across several GitHub owners, to merge-ready one at a time - challenge, rebase, adversarial review, local verification, CI and CodeRabbit clean - without merging. Use for "audit and prepare all my draft PRs", "sweep my drafts", or a multi-owner draft cleanup.
---

# Draft PR sweep

Default owners: `vibedarling`, `LeanerCloud`, `cristim`. Override from the request.

## Scope

All open draft PRs authored by the user in the given owners:
`gh search prs --owner <owner> --state open --draft --author @me --limit 1000` per owner; if an
owner returns 1000, the inventory is truncated, so split with `--repo`. Search results lack head
and base, so read each PR:
`gh pr view <n> --repo <owner>/<repo> --json headRefName,headRefOid,baseRefName,headRepository,isCrossRepository`.
Its `commits` field lists only the first 100, so read the head commit's author directly and require
its `oid` to equal `headRefOid`:
`gh api graphql -F owner=<owner> -F name=<repo> -F n=<n> -f query='query($owner:String!,$name:String!,$n:Int!){repository(owner:$owner,name:$name){pullRequest(number:$n){commits(last:1){nodes{commit{oid author{name user{login}}}}}}}}'`.
Record the list in the plan file: PR number, head SHA, base, stack parent (the PR whose head branch
is this one's base), local checkout path (`~/.claude/projects.md`, `local-paths.md`). Skip PRs the
user did not author and report them. Defer, and report, a PR whose latest commit is by someone
other than the user or a bot (stricter than the `pr-iterate` active-human guard); a null `user`
(an email linked to no account) counts as someone else. Report any owner whose search
errored (private repos or orgs the `gh` account cannot reach) instead of treating it as empty.
Confirm the list first only if it is over 15 PRs.

## Process

One PR at a time, finished before the next. Order: stack parents before children, then oldest first.

1. **Challenge.** Read the repo's own `AGENTS.md`/`CLAUDE.md` and `known-issues.md` first, then
   check the PR against the current base (`AGENTS.md` §1). Already fixed, superseded
   or built on a wrong assumption: comment and propose closing or rephrasing; never close unprompted.
   If the PR stays open, write its task-and-proof plan in the plan file and get it through two
   adversarial passes by the same reviewer (`review-and-implement`) before any worktree or code
   change; re-plan if reality diverges.
2. **Sync.** Worktree under `~/.claude/worktrees/` (`worktrees`). Rebase on the current base, never
   merge. After a parent changes, rebase each child onto it (`git rebase --onto`) and re-run steps
   3 to 6 for the child. A parent that was merged or closed: `gh pr edit --base <new-base>`, then
   `git rebase --onto <new-base> <old-parent-tip>`; a closed-unmerged parent needs the user's call.
3. **Audit.** A fresh non-author agent (Sonnet 5.5 high, cold brief, no author transcript) reviews
   the diff on the six dimensions plus `blast-radius`. The PR body is a claim, not evidence. Check
   against the source of truth with `file:line`; run deletion and mutation probes in a scratch copy.
   Two passes with the same reviewer, then close. Verify each finding before acting.
4. **Fix.** An implementer (Sonnet 5.5 medium) applies verified findings as atomic conventional
   commits; fixes go back through the same reviewer. Add regression tests that fail before and pass
   after. PR stays at 400 lines or fewer and one concern; otherwise propose a split.
5. **Verify locally.** The project's real build and test commands, `act` for workflows,
   `playwright-verify` for web apps. Trace the real user path with real data shapes. Print only
   failures. Take the per-repo lock before shared builds (`multi-agent-comms`). Record exact
   commands and results.
6. **Push and watch.** `--force-with-lease` only on the head branch of the user's own PR; never on
   the base or `main`, never when the head is a fork or a branch others push to. Then `ci-watch` and
   `cr-loop` until CI is green and CodeRabbit is clean for the current HEAD.
7. **Prepare.** Make the title and body accurate; post the verification evidence and reviewed SHA
   as a PR comment (redacted per the Git rules); apply `triage-labels` where the repo uses it. Mark
   ready for review only if steps 3 to 6 are clean for the current HEAD and the repo's
   `CONTRIBUTING` does not say otherwise. Otherwise leave it draft with a comment naming the blocker.

## Hard limits

- Never merge, never `--admin`; merging stays human. Never self-merge `LeanerCloud/dotclaude`.
- Never delete data, branches, worktrees or caches except under the post-merge exception
  (`AGENTS.md` tenet 9). Do not touch a dirty main checkout; use worktrees.
- Everything posted is public: no secrets, account IDs, home paths or emails.
- Ask only for irreversible actions, spending money or scope and product choices, via the decision
  doc flow, and keep working on other PRs meanwhile.

## Done when

Every listed PR is in exactly one terminal state: ready for review with a clean current-HEAD review,
CI green and CR clean; still draft with a posted blocker; or proposed for closing with the decision
pending. Leave a summary table (PR, owner/repo, state, reviewed SHA, evidence, blocker), a decision
log via `show-me-your-work`, and a session handoff.
