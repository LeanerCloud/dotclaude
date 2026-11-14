@AGENTS.md

## Claude Code only

Imported skills (pstack, cursor-team-kit, superpowers, gstack, anthropics/skills, terraform-skill,
claude-code-owasp, darling-skills; provenance in `skills/UPSTREAM.md`) are exported to Claude Code
only. Where one contradicts `AGENTS.md`, `AGENTS.md` wins. Resolve the Cursor primitives they name
through `upstreams/HOST-MAPPING.md`; their multi-model roster is `pstack-models.md`.

Darling skills (`darling-*`): the Darling repo's `AGENTS.md` and the clean-room rules win over them;
never use their kill or install steps on a shared prefix (`skills/UPSTREAM.md`, "Darling skills:
where they yield").
