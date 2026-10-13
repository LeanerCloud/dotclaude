---
name: playwright-verify
description: >-
  Verify web-app changes in a browser via Playwright on the prod-parity local stack. Use after any
  web-app change before declaring done, and when setting up a web harness.
---

# Playwright Verification of Web-App Changes

After ANY change to a web application (frontend, or a backend change affecting a user-facing web flow), verify it in a real browser with Playwright before declaring it done (automates the AGENTS.md §4 use-the-feature-in-a-browser rule). Also invoke when setting up a new web project: installing this harness is project setup, not polish.

## Prod-parity local runs (mandatory per app repo)

Every app repo needs a documented one-command local run mirroring production: same URL contract, headers/proxy topology and build artifacts. A dev server alone doesn't qualify (it passes while the production serving path is broken). Patterns:
- docker-compose with a local gateway (Caddy/nginx) standing in for the CDN with identical paths and cache headers, and MinIO for S3.
- Static sites: build the real artifact (e.g. `vite build`) and serve the output; never verify only against the dev server.

Convention: `make up` runs the stack, `make e2e` runs the suite against it. If the project lacks a prod-parity entrypoint, **creating it is part of the current task**; don't skip verification or fall back to dev-server-only checks.

## The verification loop

1. Start the app via its prod-parity entrypoint.
2. Run Playwright against that serving path: `make e2e` if present, else `npx playwright test`.
3. Enforce as tests (add missing items):
   - App boots with **zero console errors**.
   - Core flows pass: navigation, search, primary interactions.
   - A deep link / shared URL **cold-loads** and restores state.
   - Back/forward behaves correctly.
   - Responsive spot-check at a phone viewport.
   - Only expected network requests; no 404s on app-computed URLs.
4. Fix every finding and re-run until clean. A deliberately deferred finding goes to `known-issues.md`.

## Selectors and test hooks

Prefer stable selectors (roles, labels, `data-testid`) over CSS chains. Where app state is hard to reach from the DOM, expose dev-only hooks (`window.__appState`, `window.__appReady`) gated on the bundler's DEV flag (e.g. `import.meta.env.DEV`) so prod builds strip them; document them next to the tests.
