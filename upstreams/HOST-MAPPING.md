# Host mapping for imported skills

The pstack and cursor-team-kit skills under `upstreams/cursor-plugins/` were written for Cursor.
Their prose is otherwise host-neutral, so nothing is forked; instead every Cursor primitive they name
resolves through this table. Skills imported from `superpowers`, `anthropic-skills`,
`terraform-skill` and `owasp-security` are already Claude-native and need nothing here.

| Upstream names | Use instead |
|---|---|
| the `Task` tool | the `Agent` tool, with `model` set explicitly on every call (`subagent-strategy`) |
| `~/.cursor/rules/pstack-models.mdc` | `~/.claude/pstack-models.md` |
| Cursor model slugs in upstream prose | the tiers in `~/.claude/pstack-models.md` |
| `.cursor/skills/<name>/` | `.claude/skills/<name>/` (project scope) or `~/.claude/skills/<name>/` (user scope) |
| `~/.cursor/projects/*/`, the workspace `agent-transcripts/` directory | `~/.claude/projects/<project-slug>/` - same rule applies: never glob across projects |
| Cursor's built-in `create-skill` skill | the `writing-skills` skill |
| "list the MCPs Cursor exposes" / the `mcps/` directory | the `mcp__*` tools in this session, discovered via `ToolSearch` |
| `/skill-name` slash commands for sibling pstack skills | invoke the skill of that name |

Where an upstream instruction collides with `~/.claude/CLAUDE.md`, CLAUDE.md wins. The two known
collisions: upstream `no-comments` strips all comments, while CLAUDE.md keeps comments that carry a
*why*; and upstream PR/CI playbooks are not imported at all (see `skills/UPSTREAM.md`).
