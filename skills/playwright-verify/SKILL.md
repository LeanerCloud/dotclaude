---
name: playwright-verify
description: Automated browser verification of web-app changes - run the prod-parity local stack,
  drive Playwright through the standard checklist, iterate until clean. Invoke after any web-app
  change before declaring it done, and when setting up a new web project's run/verification harness.
---

# Playwright Verification of Web-App Changes

After ANY change to a web application - frontend, or a backend change that affects a user-facing
web flow - verify the change in a real browser with Playwright before declaring it done. This
operationalizes the CLAUDE.md §4 use-the-feature-in-a-browser rule with automation instead of a
one-off manual click-through. Also invoke when setting up a new web project: installing the
harness below is part of project setup, not later polish.

## Prod-parity local runs (mandatory per app repo)

Every application repo must have a documented, one-command way to run the app locally that mirrors
production as closely as possible: same URL contract, same headers/proxy topology, same build
artifacts. The dev server alone does not qualify - it routinely passes while the production serving
path is broken. Examples of the pattern:

- A docker-compose stack where a local gateway (Caddy/nginx) stands in for the CDN with identical
  paths and cache headers, and MinIO stands in for S3.
- For static sites: build the real artifact (e.g. `vite build`) and serve the output statically -
  never verify only against the dev server.

Preferred convention: `make up` runs the stack, `make e2e` runs the verification suite against it.
If the project lacks a prod-parity entrypoint, **creating it is part of the current task** - do not
skip verification or fall back to dev-server-only checks because the harness is missing.

## The verification loop

1. Start the app via its prod-parity entrypoint (`make up` or equivalent).
2. Run Playwright against that serving path: the project's `make e2e` if present, else
   `npx playwright test`.
3. Enforce this checklist as tests (add any missing item to the suite):
   - App boots with **zero console errors**.
   - Core user flows pass: navigation, search, primary interactions.
   - A deep link / shared URL **cold-loads** and restores state.
   - Browser back/forward behaves correctly.
   - Responsive spot-check at a phone viewport.
   - Network tab contains only expected requests - no 404s on app-computed URLs.
4. Iterate: fix every finding and re-run the suite until it passes clean. A finding you
   deliberately defer goes to `known-issues.md`, not into silence.

## Selectors and test hooks

Prefer stable selectors (roles, labels, `data-testid`) over CSS chains that break on refactors.
Where the app's state is hard to reach from the DOM, expose dev-only hooks such as
`window.__appState` or `window.__appReady`, gated on the bundler's DEV flag (e.g.
`import.meta.env.DEV`) so they are stripped from prod builds. Document the hooks next to the tests.
