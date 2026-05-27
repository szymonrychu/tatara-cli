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

2026-05-27 - GoReleaser uses `brews` key (deprecated warning but functional in v2.16); `homebrew` key does not exist in v2; `homebrew_casks` (new) requires code signing for macOS - not suitable for unsigned CLI binaries. Staying on `brews` until goreleaser adds an unsigned formula equivalent.
2026-05-27 - Wave 6.1 preconditions: (1) create `szymonrychu/tap` GitHub repo (`gh repo create szymonrychu/tap --public`) before pushing first `v*` tag; (2) set `HOMEBREW_TAP_GITHUB_TOKEN` secret (PAT with `repo` scope for tap repo) in tatara-cli GitHub settings.
2026-05-27 - Release preconditions before pushing v0.1.0 tag: (1) gh repo create szymonrychu/tap --public; (2) set repo secrets HOMEBREW_TAP_GITHUB_TOKEN (PAT with repo scope on tap), HARBOR_USERNAME, HARBOR_PASSWORD (robot account with push to /containers/* on harbor.szymonrichert.pl).

## Dead-ends / things tried that did not work

*(nothing yet)*

## Open questions

*(nothing yet)*
