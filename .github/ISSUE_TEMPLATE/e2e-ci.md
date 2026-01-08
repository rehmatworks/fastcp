---
name: E2E CI notes
about: Notes and checklist for maintaining Playwright-based E2E in CI
---

- CI will start the backend binary with `FASTCP_DEV=1` and run Playwright tests against `https://localhost:8080`.
- The runner must have Playwright dependencies installed. We use `npx playwright install --with-deps` in the workflow.
- To run E2E locally: `make test-e2e-ci` (requires Node + Playwright dependencies and a system with browser support).
- Keep E2E tests hermetic: avoid relying on system user accounts or external services; prefer using development-mode admin credentials or mocked endpoints.
