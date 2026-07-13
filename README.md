# tatara-cli

> Part of the tatara platform - see ARCHITECTURE.md in the tatara repo.

tatara platform CLI: OIDC device flow, REST passthrough, stdio MCP server.

Architecture: [github.com/szymonrychu/tatara/blob/main/ARCHITECTURE.md](https://github.com/szymonrychu/tatara/blob/main/ARCHITECTURE.md)

## Install

```sh
brew tap szymonrychu/tap
brew install tatara
```

## Quickstart

```sh
# authenticate (OIDC device flow)
tatara login

# show local auth state and resolved backend URLs (no network calls)
tatara status

# call any tatara-memory endpoint
tatara raw GET /memories

# call the operator backend with the same auth
tatara raw --target operator GET /tasks/foo

# start the MCP stdio server (for Claude Code integration)
tatara mcp

# write an MCP server config into the given directory
tatara mcp-config ~/.config/claude

# clear stored credentials
tatara logout
```

## MCP tool surface (contract v4)

`tatara mcp` exposes a 20-tool surface as one `submit_outcome` tool per
agent kind. `tools/list` is served **per profile** (`TATARA_TOOL_PROFILE`,
resolved at registration time) - this deliberately gives up a
byte-identical-tools/list prompt-cache prefix across pods, because
`submit_outcome`'s schema is itself profile-shaped, so an identical list
across profiles was never achievable anyway.

| Group | Tools |
|---|---|
| Code (`CodeTools`, 4) | `code_search`, `code_context`, `code_graph`, `code_explain` |
| Memory (`MemoryTools`, 5) | `memory_query`, `memory_describe`, `memory_write`, `memory_entity`, `memory_edges` |
| SCM (`SCMTools`, 3) | `scm_read`, `issue_write`, `mr_write` |
| Platform (`PlatformTools`, 7) | `task_get`, `task_list`, `task_context`, `task_note`, `project_get`, `repo_list`, `report_internal_issue` |
| Outcome (`OutcomeTool`, 1) | `submit_outcome` (schema shaped from `TATARA_TOOL_PROFILE`) |

20 tools total. The always-on set (served under every profile, including
the fail-closed empty one) is `task_get`, `task_context`, `task_note`,
`project_get`, `repo_list`, `report_internal_issue`.

### Per-profile gating (contract D.6)

One row per agent kind (`Task.status.agentKind`), resolved by
`TATARA_TOOL_PROFILE`. An unknown or empty profile fails **closed** to the
always-on six, with no `submit_outcome` - that pod cannot terminate its Task.

| Profile | task_list | scm_read | issue_write | mr_write | code_* (4) | memory_query/describe | memory_write | memory_entity | memory_edges | submit_outcome | **Total** |
|---|---|---|---|---|---|---|---|---|---|---|---|
| brainstorm | x | x | | | x (4) | x | x | x | | x | **17** |
| incident | x | x | | | x (4) | x | x | x | x | x | **18** |
| clarify | | x | x | | x (3: no code_graph) | x | | | | x | **14** |
| implement | | x | | x | x (4) | x | x | | | x | **16** |
| review | | x | | x | x (4) | x | | | | x | **15** |
| refine | x | x | x | x | | x | | | | x | **13** |
| documentation | | x | | x | x (4) | x | x | x | x | x | **18** |

Every profile also gets the always-on six on top of the columns above.
`mr_write` under `refine` is restricted to `action=comment` in code
(contract J, RESIDUE 1) - the schema is shared, so that gate lives in
`checkRefineMRWrite`, enforced identically server-side.

## Build from source

```sh
git clone https://github.com/szymonrychu/tatara-cli
cd tatara-cli
make build          # binary at bin/tatara
make test           # unit tests
make lint           # golangci-lint
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for build, test, and lint instructions.
