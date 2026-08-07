# ROADMAP.md

Component-local roadmap for tatara-cli. Phase-level platform roadmap
lives in `~/Documents/tatara/ROADMAP.md`.

Statuses: `planned`, `in progress`, `shipped`.

---

## v0.1.0 - phase 2 ship

**Status:** code-complete on main 2026-05-27, awaiting release.

OIDC device flow, REST passthrough, stdio MCP server, full cobra CLI
(`login`, `logout`, `raw`, `mcp`, `mcp-config`). 13 MCP tools mapping
tatara-memory REST 1:1. CI green.

See `~/Documents/tatara/docs/superpowers/specs/2026-05-27-tatara-cli-mvp-design.md`.

**Status:** code-complete on main 2026-05-27; release pipeline migrated to argo workflows.

Pushing a `v*` tag now triggers tatara-go-release in the tatara ns:
goreleaser builds amd64 binary tarballs (linux + darwin), publishes a
github release with checksums, and pushes the linux/amd64 container
to harbor.szymonrichert.pl/containers/tatara-cli. No outstanding
preconditions.

## v0.4.0 - operator MCP tool group

**Status:** code-complete on feat/operator-mcp-tools 2026-06-06, pending v0.4.0 tag + release.

9 MCP tools (project_list, project_get, repo_list, task_list, task_get, task_update,
subtask_list, subtask_create, subtask_update) mapping tatara-operator REST API 1:1.
Dual-client MCP server; operator URL/audience wiring via flag/env/file/default.
OIDC single-token assumption (both audiences carried by one token); Keycloak mapper in M6.

See `~/Documents/tatara/docs/superpowers/plans/2026-06-06-tatara-operator-m3-restapi-cli-tools.md`.

## v0.5.0 - SCM intent MCP tools

**Status:** shipped

3 operator intent tools (propose_issue, review_verdict, pr_outcome) per
the SCM-projects contract lock. See
`~/Documents/tatara/docs/superpowers/plans/2026-06-09-scm-projects-cli.md`
and spec `2026-06-09-scm-projects-pr-reactions-design.md`.

## v0.7.0 - lifecycle notification params

**Status:** code-complete 2026-06-13 (issue tatara-claude-code-wrapper#6).

change_summary gains an optional `most_problematic` param and issue_outcome
gains an optional `plan` param (used when action=implement) so the lifecycle
agent can supply the implementation-start message and the post-mortem note
the operator surfaces in the issue thread + MR body. Also fixed a latent bug:
change_summary POSTed snake_case body keys (pr_title, delivered_scope, ...)
to the operator, whose changeSummaryReq uses camelCase json tags with
DisallowUnknownFields - so those fields were silently rejected and the MR fell
back to the default title/body. Body keys are now camelCase to match the
operator REST contract.

## v1.0.0 - task-centric contract v4 (74 -> 20 tools)

**Status:** shipped 2026-07-12.

The MCP tool surface collapsed from 74 tools to exactly 20: `CodeTools()` 4,
`MemoryTools()` 5, `SCMTools()` 3, `PlatformTools()` 7, `OutcomeTool(profile)`
1 (`submit_outcome`, one name with a profile-shaped schema). `tools/list` went
per-profile (contract D.6); `profiles.go` is rekeyed on the 7 agent kinds,
fixing the live `clarify` P0. `tatara-chat` (chat/handoff MCP tools, the chat
client, `TargetChat`, `--chat-base-url`, `TATARA_CHAT_URL`) is fully
decommissioned - `task_note` is now the platform's only agent-to-agent
channel. `tatara mcp` refuses to start on a `TATARA_CONTRACT_VERSION`
mismatch. See `~/Documents/tatara-new/docs/superpowers/plans/2026-07-12-task-centric-cli.md`.

## v2.0.0 - contract 4: the clarify fold and the implement approval gate

**Status:** code-complete 2026-08-07 (tatara #521, MR4 of seven).

`clarify` is deleted as an agent kind and a tool profile: 7 kinds -> 6.
Its `implement`/`close`/`discuss` decisions became `approved`/`rejected`/
`discuss` actions on the implement `submit_outcome`, alongside the new
`approvingMaintainer` and `planNoteId` gate fields (`approvalCitations`
moved across unchanged). `documentation` stopped aliasing
`implementOutcomeSchema` and got its own, so it cannot emit the gate
actions. The implement profile absorbed clarify's `issue_write` and
memory-recall grants (17 -> 18 tools). `ContractVersion` 3 -> 4, which an
operator or wrapper on 3 must refuse at pod-ready.

Backward-incompatible by definition, and it gates the skills MR: the
released `tool-manifest.json` is what `tatara-agent-skills`'
`validate_tool_calls.py` validates against, so this must RELEASE before
that repo can document the new actions. See
`~/Documents/tatara-new/code/tatara-operator/docs/superpowers/plans/2026-08-07-521-lifecycle-and-agent-merge.md`.

## v0.1.1 - follow-ups

**Status:** planned

- [shipped] `.golangci.yml` is strict-v2-clean (no `disable-all` /
  `exclude-rules` / string `local-prefixes` leftovers); `make lint`,
  pre-commit and the CI `lint` job now run `golangci-lint config verify`
  so schema drift fails fast. Remaining (external,
  `tatara-argo-workflows`): drop the
  `verify: false` workaround on the CI golangci-lint step once it pins a
  version recent enough to recognise the configured gosec rules (G117).
- [shipped] Drive end-to-end MCP smoke from a test harness.
  `internal/mcp/e2e_test.go` runs the registered server over the real
  JSON-RPC protocol via mcp-go's in-process client: initialize +
  tools/list (asserts the full tool set) + tools/call against faked
  tatara-memory and tatara-operator backends (success, target dispatch,
  and backend-error result paths). Guards the 0.4.x tools/list
  marshalling class of regression that registration-only tests miss.
- [open, cross-repo] `tatara-operator` must vendor
  `internal/mcp/testdata/agent-kinds.txt` byte-identically into its own
  `internal/agent/testdata/agent-kinds.txt` and add
  `TestKindProfiles_MatchTheGolden` (or equivalent) checking its
  `kindProfiles` map against that golden. The anti-drift golden in this
  repo only catches drift if BOTH repos check it - the operator's half is
  the half that failed last time (contract L.5: `pod.go` had a `clarify`
  key, this repo's `profiles.go` did not, and `resolveProfile`'s
  correct fail-closed behavior turned the gap into a live P0). Tracked as
  an issue against `tatara-operator`, referencing contract G.9 and L.5.
