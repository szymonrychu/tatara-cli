# MEMORY.md

Component-local memory for tatara-cli. Cross-repo decisions live in
`~/Documents/tatara/MEMORY.md`.

Format: `YYYY-MM-DD - decision/finding`

---

## Decisions

2026-05-27 - repo bootstrapped; no go.sum yet (no deps); Dockerfile copies only go.mod (not go.sum) to avoid build error on empty module.
2026-05-27 - helmlint pre-commit hook omitted (no chart in this repo); yamllint charts/ exclude omitted for same reason.
2026-05-27 - revive `exported` rule disabled in .golangci.yml; CLAUDE.md hard rule prohibits docstrings on new code.

2026-05-27 - RefreshToken takes *http.Client param (nullable) so tests can inject httptest server without changing public contract; spec's 4-arg form would have required real issuer in tests.
2026-05-27 - DeviceFlow.TickOverride (zero=use device-code interval) lets tests run at 1ms without changing Poll's production behaviour or API surface.
2026-05-27 - gosec G304/G115/G117 nolint'd on intentional file-path-from-variable and uintptr->int casts; rationale in-line.

## Dead-ends / things tried that did not work

*(nothing yet)*

## Open questions

*(nothing yet)*
