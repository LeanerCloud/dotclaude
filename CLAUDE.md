@AGENTS.md

## Claude Code only

Imported skills (pstack, cursor-team-kit, superpowers, gstack, anthropics/skills, terraform-skill,
claude-code-owasp, darling-skills; provenance in `skills/UPSTREAM.md`) are exported to Claude Code
only. Where one contradicts `AGENTS.md`, `AGENTS.md` wins. Resolve the Cursor primitives they name
through `upstreams/HOST-MAPPING.md`; their multi-model roster is `pstack-models.md`.

Darling skills (`darling-*`): the Darling repo's `AGENTS.md` and the clean-room rules win over them.
Never run `leftovers.sh -k`, `timeout -s KILL`, `rebuild.sh`, `sudo make install` or `wayland.sh stop`
with an overridden runtime path. Run guests on your own DPREFIX with a private runtime and stop them
with `darling shutdown`. Inspect binaries by names only (`skills/UPSTREAM.md`, "Darling skills: where
they yield").
