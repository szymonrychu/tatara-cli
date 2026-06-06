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

2026-05-27 - v0.1.0 code-complete on main: 5 subcommands wired, 13 MCP tools, full TDD coverage, CI green. Release (tag push) deferred until tap repo + secrets are in place.
2026-05-27 - Pre-argo Wave 6.1 release preconditions (Homebrew tap repo, HOMEBREW_TAP_GITHUB_TOKEN, separate HARBOR creds) are obsolete. Superseded by argo migration entry below: tap dropped, tatara-cli release runs in cluster as `tatara-go-release` CWT with creds from tatara ns secrets.
2026-05-27 - CI/release migrated from .github/workflows to argo workflows. Push to main triggers tatara-go-ci CWT in tatara ns. Tag push (tag_pushed event) triggers tatara-go-release CWT. goreleaser config trimmed to amd64-only (linux + darwin); Homebrew tap dropped, user manages binary install manually from github release tarballs. Wave-6 preconditions in earlier MEMORY entries are SUPERSEDED: tap repo and HOMEBREW_TAP_GITHUB_TOKEN no longer required. Harbor robot creds and github status PAT live in tatara ns via tatara-argo-workflows helm release.

## Dead-ends / things tried that did not work

2026-05-27 - golangci-lint-action@v6 cannot install golangci-lint v2 (must use @v7); v2.1.6 was built with go1.24 and refuses go1.25 modules; v2.12.x tightened config schema and rejects our legacy `disable-all` / `exclude-rules` / string `local-prefixes`. Stable config: golangci-lint v2.11.4 via action@v7 with `verify: false`. Fixing the config to be strict-v2-clean is a v0.1.1 boy-scout task.

## Open questions

*(nothing yet)*

2026-06-06 - **Operator MCP tools.** Added 9-tool group (project/repo/task/subtask) targeting tatara-operator REST API. `Tool` now carries `Target` (TargetMemory|TargetOperator); `Server` holds two clients (memory + operator) and dispatches by target. Operator base URL resolves flag > env(TATARA_OPERATOR_URL) > file(operatorBaseUrl yaml key) > default (https://tatara.szymonrichert.pl/api/v1/operator). Single OIDC token assumed to carry both tatara-memory and tatara-operator audiences (Keycloak audience mapper added in operator M6); revisit if a separate audience-scoped token flow is needed. Ships as tatara-cli v0.4.0.
