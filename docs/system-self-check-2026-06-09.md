# Tatara system self-check (2026-06-09)

Audit run from inside a tatara task pod (`task-46lwg`, project `tatara`, repo
`tatara-cli`, GitHub issue `szymonrychu/tatara-cli#10`). The goal was to verify,
from within the running system, that the MCP tooling, repository checkouts, and
memory coverage are healthy.

## TL;DR

- **The tatara MCP server is not wired into this Claude session.** `.mcp.json` is
  empty (`{"mcpServers":{}}`) and the `tatara` CLI has no stored token
  (`tatara raw` → `auth: no token`). So none of the `code_*` / `task_*` /
  `subtask_*` / memory MCP tools are callable through the harness here.
- The backends themselves are **up and reachable** (verified out-of-band with a
  client-credentials token minted from `CLI_OIDC_*` env): operator REST ✅,
  memory code-graph ✅.
- The **LightRAG semantic layer is degraded**: `POST /queries` → `502 upstream
  error`; repositories carry a standing `MemoryNotReady / MemoryProvisioning`
  condition.
- All **6 repos are checked out clean** on `tatara/task-task-46lwg` at their
  ingested commits, and the **code graph is populated for every repo**.

## 1. MCP / tool availability

The `tatara mcp` stdio server (defined in `internal/mcp/tools.go`) is designed to
expose three tool groups. State below combines: *registered in this session?* and
*backend reachable?* (probed directly).

| Tool (group) | Routes to | Registered in session | Backend state |
|---|---|---|---|
| `repo_list`, `task_list`, `task_get`, `task_update`, `subtask_list`, `subtask_create`, `subtask_update` (operator) | tatara-operator REST | ❌ not registered | ✅ reachable — `GET /tasks/{name}`, `/projects/{p}/repositories`, `/projects/{p}/tasks` all return 200 |
| `code_search`, `code_entity`, `code_neighbors`, `code_callers`, `code_callees`, `code_dependents`, `code_dependencies`, `code_file_imports`, `code_resource_graph`, `code_cross_repo` (code graph) | tatara-memory `/code/*` | ❌ not registered | ✅ reachable — entity/callers/callees/neighbors return 200 (require `repo` param; `neighbors` also `relation`) |
| `query`, `describe` (semantic) | tatara-memory `/queries` | ❌ not registered | ⚠️ **degraded** — `POST /queries` → `502 upstream error` (LightRAG not Ready) |
| `create_memory`, `get_memory`, `delete_memory`, `bulk_create_memories`, `get_ingest_job`, `get_entity`, `search_entities`, `patch_entity`, `list_edges`, `create_edge`, `delete_edge` (memory store) | tatara-memory `/memories`,`/entities`,`/edges` | ❌ not registered | ◻️ endpoints present (not exercised with writes); creates depend on the same LightRAG stack that is degraded |

Harness/built-in tools (`Task*`, `Bash`, `Read`/`Edit`/`Write`, `Agent`,
`Web*`, etc.) are present and working — that is how this audit was performed.

**Why the tatara tools are absent:** the wrapper that spawned this session did
not run `tatara mcp-config <dir>` to register the server, and the CLI is not
authenticated. The MCP `query`-style tools would also currently fail because of
the LightRAG outage even if registered.

## 2. Repositories & checkout

All six platform repos are cloned under `/workspace/<name>`, clean, and on the
task branch at the exact commit the operator last ingested.

| Repo | Phase | Checked-out HEAD | Ingested commit | Match | Clean |
|---|---|---|---|---|---|
| tatara-cli (primary) | Ingested | 28a463e | 28a463e | ✅ | ✅ |
| tatara-operator | Ingested | adb5f50 | adb5f50 | ✅ | ✅ |
| tatara-chat | Ingested | b1dc4bd | b1dc4bd | ✅ | ✅ |
| tatara-claude-code-wrapper | Ingested | 2c017fb | 2c017fb | ✅ | ✅ |
| tatara-memory | Ingested | 646684d | 646684d | ✅ | ✅ |
| tatara-memory-repo-ingester | Ingested | d389da0 | d389da0 | ✅ | ✅ |

All on branch `tatara/task-task-46lwg`; `git status --porcelain` empty for each.

### Memory coverage per repo

Two senses of "memory": the **tatara-memory code graph** (ingested entities) and
**in-repo docs** (`MEMORY.md` etc.).

| Repo | Code-graph entities | Ingested | `MEMORY.md` (lines) | `CLAUDE.md` | `ROADMAP.md` | Coverage |
|---|---|---|---|---|---|---|
| tatara-memory | ~409 | ✅ | 100 | 107 | 51 | Excellent |
| tatara-operator | ~323 | ✅ | 89 | 109 | 77 | Excellent |
| tatara-memory-repo-ingester | ~214 | ✅ | 25 | 101 | 17 | Good graph, thin docs |
| tatara-claude-code-wrapper | ~166 | ✅ | 77 | 101 | 38 | Good |
| tatara-chat | ~134 | ✅ | 26 | 101 | 20 | Good graph, thin docs |
| tatara-cli | ~43 | ✅ | 32 | 107 | 46 | Adequate (small codebase) |

Code-graph traversal verified working end-to-end (e.g. `code_callers` on
`tatara-cli/internal/auth.AcquireLock` returns its caller `cmd.newRawCmd`).
Semantic `query` coverage could **not** be assessed because `/queries` is down.

## 3. Usability for fixing issues / building features through Tatara

What works well today:

- **Operator CRDs as the task system** — `task_get`/`task_list`/`repo_list` give
  an agent its goal, source issue, repo set, and ingest state in one place, and
  `subtask_create`/`subtask_update` let an agent self-plan and report phase/PR
  back. This is the right backbone for unattended work and it is healthy.
- **Code-graph navigation** is the strongest asset: callers/callees/neighbors/
  cross-repo over a deterministic, freshly-ingested graph is exactly what you
  want for *locating* where a fix belongs and gauging blast radius — and it is
  fully populated for all six repos and in sync with the checkouts.
- **Clean, commit-accurate checkouts** mean local edits + the code graph agree;
  no drift to reconcile.

Gaps that blunt usefulness right now:

1. **MCP not registered in-session.** The single biggest issue: an agent in this
   pod cannot call any tatara tool through the harness — it must shell out to the
   REST APIs. Fix: have the wrapper run `tatara mcp-config` and ensure the CLI
   has a token (the `query`-style tools also need the semantic stack up).
2. **Semantic layer (LightRAG `/queries`) is down (502).** This removes the
   natural-language "explain/where-is" retrieval, leaving only structural graph
   queries. For *understanding intent* (vs. structure) this is a real loss.
3. **Thin in-repo `MEMORY.md`** for `tatara-chat` and `tatara-memory-repo-ingester`
   relative to their code size — fine for now, worth growing.

Net: the **structural half** of Tatara (operator task model + code graph +
checkouts) is solid and genuinely useful for driving fixes and features; the
**semantic half** (LightRAG query / memory notes) is currently unavailable, and
the MCP surface needs to be registered+authenticated in the agent session before
an agent can use any of it natively.

---
*Generated by the agent for task `task-46lwg`. Backend probes used a
client-credentials token from `CLI_OIDC_*`; no data was mutated.*
