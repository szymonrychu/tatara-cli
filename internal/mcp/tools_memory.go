package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// MemoryTools returns the 5 memory tools (contract D.4). They fold the
// 13 pre-contract memory tools: memory_query/memory_describe/memory_write are
// straight renames of query/describe/create_memory (the LightRAG entry
// points and the memory-insert path); memory_entity folds get_entity,
// search_entities and patch_entity behind an op enum; memory_edges folds
// list_edges, create_edge and delete_edge behind an op enum. get_memory,
// delete_memory, bulk_create_memories and get_ingest_job are dropped: point
// get/delete by opaque track_id has no caller in any skill, and bulk ingest
// is tatara-memory-repo-ingester's job, not an agent's.
func MemoryTools() []Tool {
	return []Tool{
		{
			Name:        "memory_query",
			Description: "Retrieve memory references for the given query.",
			Target:      TargetMemory,
			Schema:      json.RawMessage(`{"type":"object","properties":{"mode":{"type":"string","enum":["local","global","hybrid","naive"]},"text":{"type":"string"},"top_k":{"type":"integer"}},"required":["mode","text"],"additionalProperties":false}`),
			Build: func(a map[string]any) (string, string, any, error) {
				return http.MethodPost, "/queries", a, nil
			},
		},
		{
			Name:        "memory_describe",
			Description: "Generative answer plus source paths for the given query.",
			Target:      TargetMemory,
			Schema:      json.RawMessage(`{"type":"object","properties":{"mode":{"type":"string","enum":["local","global","hybrid","naive"]},"text":{"type":"string"},"top_k":{"type":"integer"}},"required":["mode","text"],"additionalProperties":false}`),
			Build: func(a map[string]any) (string, string, any, error) {
				return http.MethodPost, "/queries:describe", a, nil
			},
		},
		{
			Name:        "memory_write",
			Description: "Insert a new text memory. Returns the track_id.",
			Target:      TargetMemory,
			Schema:      json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"},"metadata":{"type":"object","additionalProperties":{"type":"string"}}},"required":["text"],"additionalProperties":false}`),
			Build: func(a map[string]any) (string, string, any, error) {
				return http.MethodPost, "/memories", a, nil
			},
		},
		{
			Name:        "memory_entity",
			Description: "Read, search, or patch one entity in the project's knowledge graph.",
			Target:      TargetMemory,
			Schema: json.RawMessage(`{"type":"object","properties":{
  "op":{"type":"string","enum":["get","search","patch"],"description":"REQUIRED."},
  "id":{"type":"string","description":"Required for op=get and op=patch."},
  "q":{"type":"string","description":"Required for op=search."},
  "patch":{"type":"object","description":"Required for op=patch: the fields to change."},
  "limit":{"type":"integer"}},
 "required":["op"],"additionalProperties":false}`),
			Build: func(a map[string]any) (string, string, any, error) {
				op, err := requiredString(a, "op")
				if err != nil {
					return "", "", nil, err
				}
				switch op {
				case "get":
					id, err := requiredString(a, "id")
					if err != nil {
						return "", "", nil, err
					}
					return http.MethodGet, "/entities/" + url.PathEscape(id), nil, nil
				case "search":
					q, err := requiredString(a, "q")
					if err != nil {
						return "", "", nil, err
					}
					v := url.Values{"q": {q}}
					addOptional(v, a, "limit")
					return http.MethodGet, "/entities?" + v.Encode(), nil, nil
				case "patch":
					id, err := requiredString(a, "id")
					if err != nil {
						return "", "", nil, err
					}
					patch, ok := a["patch"]
					if !ok || patch == nil {
						return "", "", nil, fmt.Errorf("patch required")
					}
					return http.MethodPatch, "/entities/" + url.PathEscape(id), patch, nil
				}
				return "", "", nil, fmt.Errorf("memory_entity: unknown op %q", op)
			},
		},
		{
			Name:        "memory_edges",
			Description: "List, create, or delete edges between entities in the project's knowledge graph.",
			Target:      TargetMemory,
			Schema: json.RawMessage(`{"type":"object","properties":{
  "op":{"type":"string","enum":["list","create","delete"],"description":"REQUIRED."},
  "from":{"type":"string"},"to":{"type":"string"},
  "relation":{"type":"string"},
  "id":{"type":"string","description":"Required for op=delete."},
  "limit":{"type":"integer"}},
 "required":["op"],"additionalProperties":false}`),
			Build: func(a map[string]any) (string, string, any, error) {
				op, err := requiredString(a, "op")
				if err != nil {
					return "", "", nil, err
				}
				switch op {
				case "list":
					v := url.Values{}
					addOptional(v, a, "from", "to", "relation", "limit")
					return http.MethodGet, "/edges?" + v.Encode(), nil, nil
				case "create":
					return http.MethodPost, "/edges", a, nil
				case "delete":
					id, err := requiredString(a, "id")
					if err != nil {
						return "", "", nil, err
					}
					return http.MethodDelete, "/edges/" + url.PathEscape(id), nil, nil
				}
				return "", "", nil, fmt.Errorf("memory_edges: unknown op %q", op)
			},
		},
	}
}
