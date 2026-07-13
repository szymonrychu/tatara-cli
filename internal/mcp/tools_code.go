package mcp

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// CodeTools returns the 4 code-graph tools (contract D.3). They fold the 19
// pre-contract code_* tools: the relation traversals collapse into
// code_context(rel=...), the whole-graph analyses into code_graph(op=...).
// rel=neighbors is deliberate: it is the only directional-relation traversal
// with no substitute among the other eight rels (contract L.6).
func CodeTools() []Tool {
	return []Tool{
		{
			Name:        "code_search",
			Description: "Search the code graph of one repository for entities (functions, types, files) matching a query.",
			Target:      TargetMemory,
			Schema: json.RawMessage(`{"type":"object","properties":{
  "repo":{"type":"string","description":"Repository CR name, e.g. tatara-operator."},
  "q":{"type":"string","description":"Free-text query."},
  "type":{"type":"string","description":"Optional entity type filter."},
  "limit":{"type":"integer"}},
 "required":["repo","q"],"additionalProperties":false}`),
			Build: func(a map[string]any) (string, string, any, error) {
				repo, err := requiredString(a, "repo")
				if err != nil {
					return "", "", nil, err
				}
				q, err := requiredString(a, "q")
				if err != nil {
					return "", "", nil, err
				}
				v := url.Values{"repo": {repo}, "q": {q}}
				addOptional(v, a, "type", "limit")
				return "GET", "/code/entities?" + v.Encode(), nil, nil
			},
		},
		{
			Name:        "code_context",
			Description: "Read one entity's neighbourhood in the code graph: the entity itself, its neighbours, callers, callees, dependents, dependencies, file imports, related entities, or its cross-repo links.",
			Target:      TargetMemory,
			Schema: json.RawMessage(`{"type":"object","properties":{
  "repo":{"type":"string","description":"Repository CR name. REQUIRED."},
  "rel":{"type":"string","enum":["entity","neighbors","callers","callees","dependents","dependencies","file_imports","related","cross_repo"],
    "description":"Which relation to traverse. REQUIRED."},
  "id":{"type":"string","description":"Entity id from code_search."},
  "depth":{"type":"integer","description":"Traversal depth, default 1, max 4."},
  "relation":{"type":"string","description":"rel=neighbors only: restrict to one relation name."},
  "direction":{"type":"string","enum":["out","in"],"description":"rel=neighbors only."},
  "limit":{"type":"integer"}},
 "required":["repo","rel"],"additionalProperties":false}`),
			Build: func(a map[string]any) (string, string, any, error) {
				repo, err := requiredString(a, "repo")
				if err != nil {
					return "", "", nil, err
				}
				rel, err := requiredString(a, "rel")
				if err != nil {
					return "", "", nil, err
				}
				paths := map[string]string{
					"entity":       "/code/entity",
					"neighbors":    "/code/neighbors",
					"callers":      "/code/callers",
					"callees":      "/code/callees",
					"dependents":   "/code/dependents",
					"dependencies": "/code/dependencies",
					"file_imports": "/code/file-imports",
					"related":      "/code-graph/related",
					"cross_repo":   "/code/cross-repo",
				}
				p, ok := paths[rel]
				if !ok {
					return "", "", nil, fmt.Errorf("code_context: unknown rel %q", rel)
				}
				if d, ok := a["depth"]; ok {
					if n, err := asInt(d); err != nil || n < 1 || n > 4 {
						return "", "", nil, fmt.Errorf("code_context: depth must be 1..4, got %v", d)
					}
				}
				v := url.Values{"repo": {repo}}
				addOptional(v, a, "id", "depth", "relation", "direction", "limit")
				return "GET", p + "?" + v.Encode(), nil, nil
			},
		},
		{
			Name:        "code_graph",
			Description: "Whole-graph analyses over one repository: shortest path between entities, the most important entities, graph statistics, ambiguous edges, communities, hyperedges, bridges, or the Kubernetes resource graph.",
			Target:      TargetMemory,
			Schema: json.RawMessage(`{"type":"object","properties":{
  "repo":{"type":"string","description":"Repository CR name. REQUIRED."},
  "op":{"type":"string","enum":["path","important","stats","ambiguous","communities","hyperedges","bridges","resource_graph"],
    "description":"Which analysis to run. REQUIRED."},
  "from":{"type":"string","description":"op=path only."},
  "to":{"type":"string","description":"op=path only."},
  "community":{"type":"string","description":"op=communities only: read one community instead of the list."},
  "id":{"type":"string","description":"op=hyperedges only: read one hyperedge instead of the list."},
  "limit":{"type":"integer"}},
 "required":["repo","op"],"additionalProperties":false}`),
			Build: func(a map[string]any) (string, string, any, error) {
				repo, err := requiredString(a, "repo")
				if err != nil {
					return "", "", nil, err
				}
				op, err := requiredString(a, "op")
				if err != nil {
					return "", "", nil, err
				}
				paths := map[string]string{
					"path":           "/code-graph/path",
					"important":      "/code-graph/important",
					"stats":          "/code-graph/stats",
					"ambiguous":      "/code-graph/ambiguous",
					"communities":    "/code-graph/communities",
					"hyperedges":     "/code-graph/hyperedges",
					"bridges":        "/code-graph/bridges",
					"resource_graph": "/code/resource-graph",
				}
				p, ok := paths[op]
				if !ok {
					return "", "", nil, fmt.Errorf("code_graph: unknown op %q", op)
				}
				if op == "communities" && argString(a, "community") != "" {
					p = "/code-graph/community"
				}
				if op == "hyperedges" && argString(a, "id") != "" {
					p = "/code-graph/hyperedge"
				}
				v := url.Values{"repo": {repo}}
				addOptional(v, a, "from", "to", "community", "id", "limit")
				return "GET", p + "?" + v.Encode(), nil, nil
			},
		},
		{
			Name:        "code_explain",
			Description: "Explain one code-graph entity: what it is, what it touches, and why it matters.",
			Target:      TargetMemory,
			Schema: json.RawMessage(`{"type":"object","properties":{
  "repo":{"type":"string","description":"Repository CR name. REQUIRED."},
  "id":{"type":"string","description":"Entity id from code_search. REQUIRED."}},
 "required":["repo","id"],"additionalProperties":false}`),
			Build: func(a map[string]any) (string, string, any, error) {
				repo, err := requiredString(a, "repo")
				if err != nil {
					return "", "", nil, err
				}
				id, err := requiredString(a, "id")
				if err != nil {
					return "", "", nil, err
				}
				v := url.Values{"repo": {repo}, "id": {id}}
				return "GET", "/code-graph/explain?" + v.Encode(), nil, nil
			},
		},
	}
}
