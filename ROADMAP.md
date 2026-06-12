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

## v0.6.0 - chat MCP tool group

**Status:** code-complete 2026-06-12 (tatara-cli#24), pending tag + release.

10 chat tools (chat_create_room, chat_list_rooms, chat_get_room,
chat_close_room, chat_add_participant, chat_list_participants,
chat_remove_participant, chat_send_message, chat_poll_messages,
chat_get_log) mapping the tatara-chat REST API 1:1. Third client target
(TargetChat); chat URL wiring via flag(--chat-base-url)/env(TATARA_CHAT_URL)/
file(chatBaseUrl)/default. Single-token audience assumption as for operator.

## v0.1.1 - follow-ups

**Status:** planned

- [shipped] `.golangci.yml` is strict-v2-clean (no `disable-all` /
  `exclude-rules` / string `local-prefixes` leftovers); `make lint` and
  pre-commit now run `golangci-lint config verify` so schema drift fails
  fast. Remaining (external, `tatara-argo-workflows`): drop the
  `verify: false` workaround on the CI golangci-lint step once it pins a
  version recent enough to recognise the configured gosec rules (G117).
- [shipped] Drive end-to-end MCP smoke from a test harness.
  `internal/mcp/e2e_test.go` runs the registered server over the real
  JSON-RPC protocol via mcp-go's in-process client: initialize +
  tools/list (asserts the full tool set) + tools/call against faked
  tatara-memory and tatara-operator backends (success, target dispatch,
  and backend-error result paths). Guards the 0.4.x tools/list
  marshalling class of regression that registration-only tests miss.
