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

2026-06-07 - Dockerfile had two build blockers fixed before first push: (1) `COPY go.mod ./` missing `go.sum` - deps failed to download; fixed to `COPY go.mod go.sum ./`. (2) binary placed at `/tatara` not `/usr/local/bin/tatara` - wrapper `COPY --from=tatara-cli` couldn't find it; fixed to install at `/usr/local/bin/tatara`. Committed on main (SHA 9cb6fa6) then built/pushed as tatara-cli:0.4.0. GO_VERSION=1.25 from Dockerfile matched go.mod `go 1.25.5` and built successfully.

2026-06-09 - **Headless OIDC auth (A1).** `internal/auth/clientcreds.go` adds `ClientCredentialsToken` (discovery + grant) and `AccessToken(ctx)` which prefers stored login token, falls back to client_credentials when OIDC_ISSUER/CLI_OIDC_CLIENT_ID/CLI_OIDC_CLIENT_SECRET are set (in-memory cache, 30s pre-expiry refresh), returns ErrNoToken otherwise. `tatara raw` and `tatara mcp` now call `AccessToken` on ErrNoToken from LoadToken so both work in agent pods with no `tatara login`. Token path/refresh/save wiring is preserved for stored tokens; skipped for client-creds tokens (re-mint via AccessToken on next call). SHA 3de66ae.

2026-06-06 - **Operator MCP tools.** Added 9-tool group (project/repo/task/subtask) targeting tatara-operator REST API. `Tool` now carries `Target` (TargetMemory|TargetOperator); `Server` holds two clients (memory + operator) and dispatches by target. Operator base URL resolves flag > env(TATARA_OPERATOR_URL) > file(operatorBaseUrl yaml key) > default (https://tatara.szymonrichert.pl/api/v1/operator). Single OIDC token assumed to carry both tatara-memory and tatara-operator audiences (Keycloak audience mapper added in operator M6); revisit if a separate audience-scoped token flow is needed. Ships as tatara-cli v0.4.0.

2026-06-09 - **SCM intent MCP tools (cli T2).** Added 3 operator-targeted tools (9->12): `propose_issue` -> `POST /projects/{TATARA_PROJECT}/issues` body `{repositoryRef,title,body,kind}` (kind bug|improvement); `review_verdict` -> `POST /tasks/{task|TATARA_TASK}/review` body `{decision,body,suggestions}` (decision approve|request_changes|comment); `pr_outcome` -> `POST /tasks/{task|TATARA_TASK}/pr-outcome` body `{action,reason}` (action merge|close). All are CRD-writes via the operator REST API; the operator TaskReconciler is the SOLE SCM egress path (agents emit intent only). project/task resolved via argOrEnv(TATARA_PROJECT/TATARA_TASK) exactly like existing operator tools; propose_issue accepts repo|repositoryRef arg, body key is always repositoryRef. Wire shapes match contract lock 2026-06-09 sections 3+4 byte-for-byte. Ships as tatara-cli v0.5.0.

2026-06-12 - **Protocol-level MCP e2e harness.** Added `internal/mcp/e2e_test.go` (`TestE2E_StdioProtocol`): builds the `Server` against httptest-faked memory+operator REST backends, drives it via mcp-go's in-process client (`client.NewInProcessClient(s.srv)` - same-package test reaches the unexported `srv`), and runs the real protocol path: initialize, tools/list (asserts every AllTools+OperatorTools name present), and tools/call for `create_memory` (memory backend) + `project_list` (operator backend), asserting the right backend was hit and the response text round-trips. Chose in-process over a subprocess: the in-process transport `json.Marshal`s every response, so it still catches the tools/list marshal regression (verified: reverting buildTool to the dual-schema form makes ListTools fail with "both InputSchema and RawInputSchema set"). KISS - no binary build, no stdio plumbing. Note: pre-existing arg-required tests (`TestOperatorTools_RequireArgs`, `TestRaw_NoTokenSurfacesErrNoToken`, SCM require-args) fail in the agent pod because TATARA_PROJECT/TATARA_TASK/TATARA_OPERATOR_URL are injected into the env; they pass under a clean env (`env -u TATARA_PROJECT -u TATARA_TASK ...`). The new e2e test is env-independent.

2026-06-09 - **CRITICAL: tools/list marshal bug broke ALL MCP tools (0.4.x).** `internal/mcp/server.go register()` built every tool with `mcplib.NewTool(name, WithDescription, WithRawInputSchema)`. mcp-go `NewTool` seeds a default object `InputSchema` (Type="object"); `WithRawInputSchema` also sets `RawInputSchema`. mcp-go `Tool.MarshalJSON` errors when BOTH are set, so `tools/list` returned an error and agents loaded ZERO tatara tools - `claude mcp list` still showed "Connected" (initialize handshake only), masking it. This was the real cause of agent runs Succeeding with 0 subtasks / empty comments / "partially working" PR feedback, NOT auth (auth probes were curl-only, bypassing claude). Fix: `buildTool()` uses `mcplib.NewToolWithRawSchema(name, desc, schema)` (RawInputSchema only). Regression test `TestBuildTool_AllToolsMarshal` json.Marshals every tool. Verified at protocol level: tools/list now returns all 32 tools, zero stderr. Ships as tatara-cli 0.4.3. Lesson: "MCP server Connected" != "tools exposed"; always verify tools/list, not just the handshake.
