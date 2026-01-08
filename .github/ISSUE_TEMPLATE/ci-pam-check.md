---
name: PAM CI check
about: Add notes for maintaining the PAM CI matrix
---

When adding or changing native authentication code that depends on PAM, ensure the CI matrix includes a `pam: true` job to compile and run `go test -tags pam ./...`. This requires the runner to install system PAM headers (e.g., `libpam0g-dev`) before building.

Notes:
- Keep PAM-dependent code gated behind `//go:build pam` files.
- Use `go test -tags pam` for compilation and unit tests that exercise PAM-specific code.
- Avoid relying on system user accounts in CI tests; prefer compilation-time checks or mocks for unit tests.
