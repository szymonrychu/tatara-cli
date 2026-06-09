package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/szymonrychu/tatara-cli/internal/client"
)

// Target identifies which backend client a Tool is dispatched against.
type Target int

const (
	TargetMemory   Target = iota // default: existing tools hit tatara-memory
	TargetOperator               // operator tools hit tatara-operator
)

// Tool describes a tatara REST operation exposed as an MCP tool.
type Tool struct {
	Name        string
	Description string
	Schema      json.RawMessage
	Target      Target
	Build       func(args map[string]any) (method, path string, body any, err error)
}

// AllTools returns the 28-entry tool registry mapping tatara-memory REST endpoints.
func AllTools() []Tool {
	return []Tool{
		{
			Name:        "create_memory",
			Description: "Insert a new text memory. Returns the track_id.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"},"metadata":{"type":"object","additionalProperties":{"type":"string"}}},"required":["text"]}`),
			Build: func(a map[string]any) (string, string, any, error) {
				return http.MethodPost, "/memories", a, nil
			},
		},
		{
			Name:        "get_memory",
			Description: "Retrieve a memory by track_id.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`),
			Build: func(a map[string]any) (string, string, any, error) {
				id, _ := a["id"].(string)
				if id == "" {
					return "", "", nil, fmt.Errorf("id required")
				}
				return http.MethodGet, "/memories/" + url.PathEscape(id), nil, nil
			},
		},
		{
			Name:        "delete_memory",
			Description: "Delete a memory by track_id.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`),
			Build: func(a map[string]any) (string, string, any, error) {
				id, _ := a["id"].(string)
				if id == "" {
					return "", "", nil, fmt.Errorf("id required")
				}
				return http.MethodDelete, "/memories/" + url.PathEscape(id), nil, nil
			},
		},
		{
			Name:        "bulk_create_memories",
			Description: "Submit a batch of memories for async ingest.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"items":{"type":"array","items":{"type":"object","properties":{"text":{"type":"string"},"metadata":{"type":"object"}},"required":["text"]}}},"required":["items"]}`),
			Build: func(a map[string]any) (string, string, any, error) {
				return http.MethodPost, "/memories:bulk", a, nil
			},
		},
		{
			Name:        "get_ingest_job",
			Description: "Poll the status of a bulk ingest job.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`),
			Build: func(a map[string]any) (string, string, any, error) {
				id, _ := a["id"].(string)
				if id == "" {
					return "", "", nil, fmt.Errorf("id required")
				}
				return http.MethodGet, "/ingest-jobs/" + url.PathEscape(id), nil, nil
			},
		},
		{
			Name:        "query",
			Description: "Retrieve memory references for the given query.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"mode":{"type":"string","enum":["local","global","hybrid","naive","mix","bypass"]},"text":{"type":"string"},"top_k":{"type":"integer"}},"required":["mode","text"]}`),
			Build: func(a map[string]any) (string, string, any, error) {
				return http.MethodPost, "/queries", a, nil
			},
		},
		{
			Name:        "describe",
			Description: "Generative answer plus source paths for the given query.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"mode":{"type":"string"},"text":{"type":"string"},"top_k":{"type":"integer"}},"required":["mode","text"]}`),
			Build: func(a map[string]any) (string, string, any, error) {
				return http.MethodPost, "/queries:describe", a, nil
			},
		},
		{
			Name:        "get_entity",
			Description: "Retrieve an entity by name.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`),
			Build: func(a map[string]any) (string, string, any, error) {
				id, _ := a["id"].(string)
				if id == "" {
					return "", "", nil, fmt.Errorf("id required")
				}
				return http.MethodGet, "/entities/" + url.PathEscape(id), nil, nil
			},
		},
		{
			Name:        "search_entities",
			Description: "Search entities by query string.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`),
			Build: func(a map[string]any) (string, string, any, error) {
				q, _ := a["q"].(string)
				path := "/entities"
				if q != "" {
					path += "?q=" + url.QueryEscape(q)
				}
				return http.MethodGet, path, nil, nil
			},
		},
		{
			Name:        "patch_entity",
			Description: "Apply a partial update to an entity.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"patch":{"type":"object"}},"required":["id","patch"]}`),
			Build: func(a map[string]any) (string, string, any, error) {
				id, _ := a["id"].(string)
				if id == "" {
					return "", "", nil, fmt.Errorf("id required")
				}
				return http.MethodPatch, "/entities/" + url.PathEscape(id), a["patch"], nil
			},
		},
		{
			Name:        "list_edges",
			Description: "List all edges in the knowledge graph.",
			Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
			Build: func(a map[string]any) (string, string, any, error) {
				return http.MethodGet, "/edges", nil, nil
			},
		},
		{
			Name:        "create_edge",
			Description: "Create a new edge between two existing entities.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"from_entity":{"type":"string"},"to_entity":{"type":"string"},"relation":{"type":"string"},"properties":{"type":"object"}},"required":["from_entity","to_entity","relation"]}`),
			Build: func(a map[string]any) (string, string, any, error) {
				return http.MethodPost, "/edges", a, nil
			},
		},
		{
			Name:        "delete_edge",
			Description: "Delete an edge by its opaque ID. Use the id returned by list_edges. Do not parse or construct the ID - the encoding is internal.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"Opaque edge identifier returned by list_edges. Do not parse - the encoding is internal."}},"required":["id"]}`),
			Build: func(a map[string]any) (string, string, any, error) {
				id, _ := a["id"].(string)
				if id == "" {
					return "", "", nil, fmt.Errorf("id required")
				}
				return http.MethodDelete, "/edges/" + url.PathEscape(id), nil, nil
			},
		},
		{Name: "code_search", Description: "Search code-graph entities by name/description, optional type filter.",
			Schema: json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string"},"q":{"type":"string"},"type":{"type":"string"},"limit":{"type":"integer"}},"required":["repo"]}`),
			Build:  codeGet("/code/entities", []string{"repo"}, []string{"q", "type", "limit"})},
		{Name: "code_entity", Description: "Get a single code entity and its immediate edges.",
			Schema: json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string"},"id":{"type":"string"}},"required":["repo","id"]}`),
			Build:  codeGet("/code/entity", []string{"repo", "id"}, nil)},
		{Name: "code_neighbors", Description: "Traverse the code graph from an entity along a relation.",
			Schema: json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string"},"id":{"type":"string"},"relation":{"type":"string"},"direction":{"type":"string","enum":["out","in"]},"depth":{"type":"integer"},"min_confidence":{"type":"number"},"tier":{"type":"string"}},"required":["repo","id","relation"]}`),
			Build:  codeGet("/code/neighbors", []string{"repo", "id", "relation"}, []string{"direction", "depth", "min_confidence", "tier"})},
		{Name: "code_callers", Description: "Who calls this function/method (reverse calls), to depth N.",
			Schema: json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string"},"id":{"type":"string"},"depth":{"type":"integer"},"min_confidence":{"type":"number"},"tier":{"type":"string"}},"required":["repo","id"]}`),
			Build:  codeGet("/code/callers", []string{"repo", "id"}, []string{"depth", "min_confidence", "tier"})},
		{Name: "code_callees", Description: "What this function/method calls (forward calls), to depth N.",
			Schema: json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string"},"id":{"type":"string"},"depth":{"type":"integer"},"min_confidence":{"type":"number"},"tier":{"type":"string"}},"required":["repo","id"]}`),
			Build:  codeGet("/code/callees", []string{"repo", "id"}, []string{"depth", "min_confidence", "tier"})},
		{Name: "code_dependents", Description: "What depends on this entity (reverse imports/references/depends_on).",
			Schema: json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string"},"id":{"type":"string"},"depth":{"type":"integer"},"min_confidence":{"type":"number"},"tier":{"type":"string"}},"required":["repo","id"]}`),
			Build:  codeGet("/code/dependents", []string{"repo", "id"}, []string{"depth", "min_confidence", "tier"})},
		{Name: "code_dependencies", Description: "What this entity depends on (forward imports/references/depends_on).",
			Schema: json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string"},"id":{"type":"string"},"depth":{"type":"integer"},"min_confidence":{"type":"number"},"tier":{"type":"string"}},"required":["repo","id"]}`),
			Build:  codeGet("/code/dependencies", []string{"repo", "id"}, []string{"depth", "min_confidence", "tier"})},
		{Name: "code_file_imports", Description: "Imports out of a file's package.",
			Schema: json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string"},"path":{"type":"string"}},"required":["repo","path"]}`),
			Build:  codeGet("/code/file-imports", []string{"repo", "path"}, nil)},
		{Name: "code_resource_graph", Description: "Terraform/Helm dependency subgraph for a resource.",
			Schema: json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string"},"id":{"type":"string"},"depth":{"type":"integer"}},"required":["repo","id"]}`),
			Build:  codeGet("/code/resource-graph", []string{"repo", "id"}, []string{"depth"})},
		{Name: "code_cross_repo", Description: "Cross-repo symbol links for an entity: who consumes what it provides and who provides what it requires.",
			Schema: json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string"},"id":{"type":"string"}},"required":["repo","id"]}`),
			Build:  codeGet("/code/cross-repo", []string{"repo", "id"}, nil)},
		{Name: "code_path", Description: "Shortest path between two code entities in the code graph.",
			Schema: json.RawMessage(`{"type":"object","properties":{"from":{"type":"string"},"to":{"type":"string"},"relations":{"type":"string"},"max_depth":{"type":"integer"}},"required":["from","to"]}`),
			Build:  codeGet("/code-graph/path", []string{"from", "to"}, []string{"relations", "max_depth"})},
		{Name: "code_important", Description: "Most important (highest-degree) entities in the code graph, optionally filtered by repo.",
			Schema: json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string"},"limit":{"type":"integer"}}}`),
			Build:  codeGet("/code-graph/important", nil, []string{"repo", "limit"})},
		{Name: "code_stats", Description: "Graph statistics: entity/edge counts, types, tiers, isolated entities, import cycles.",
			Schema: json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string"}}}`),
			Build:  codeGet("/code-graph/stats", nil, []string{"repo"})},
		{Name: "code_ambiguous_edges", Description: "Edges with low confidence score or AMBIGUOUS tier, ordered by score ascending.",
			Schema: json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string"},"limit":{"type":"integer"}}}`),
			Build:  codeGet("/code-graph/ambiguous", nil, []string{"repo", "limit"})},
		{Name: "code_explain", Description: "Full context for a code entity: detail, in/out neighbors with file locations.",
			Schema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"repo":{"type":"string"}},"required":["id"]}`),
			Build:  codeGet("/code-graph/explain", []string{"id"}, []string{"repo"})},
	}
}

// codeGet builds a GET tool path with an encoded query, requiring the given keys.
func codeGet(path string, required []string, optional []string) func(map[string]any) (string, string, any, error) {
	return func(a map[string]any) (string, string, any, error) {
		q := url.Values{}
		for _, k := range required {
			v := argString(a, k)
			if v == "" {
				return "", "", nil, fmt.Errorf("%s required", k)
			}
			q.Set(k, v)
		}
		for _, k := range optional {
			if v := argString(a, k); v != "" {
				q.Set(k, v)
			}
		}
		return http.MethodGet, path + "?" + q.Encode(), nil, nil
	}
}

// argString coerces string or JSON number args to a string.
func argString(a map[string]any, k string) string {
	switch v := a[k].(type) {
	case string:
		return v
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	default:
		return ""
	}
}

// argOrEnv returns the arg value if non-empty, otherwise falls back to the
// named environment variable. This lets operator tool calls omit task= and
// project= when the wrapper Pod has injected TATARA_TASK / TATARA_PROJECT.
func argOrEnv(a map[string]any, key, envKey string) string {
	if v := argString(a, key); v != "" {
		return v
	}
	return os.Getenv(envKey)
}

// OperatorTools returns the 9 tatara-operator REST tools (Target=TargetOperator).
func OperatorTools() []Tool {
	op := func(name, desc, schema string, build func(map[string]any) (string, string, any, error)) Tool {
		return Tool{Name: name, Description: desc, Schema: json.RawMessage(schema), Target: TargetOperator, Build: build}
	}
	return []Tool{
		op("project_list", "List all Projects.",
			`{"type":"object","properties":{}}`,
			func(a map[string]any) (string, string, any, error) {
				return http.MethodGet, "/projects", nil, nil
			}),
		op("project_get", "Get a Project by name.",
			`{"type":"object","properties":{"project":{"type":"string"}},"required":["project"]}`,
			func(a map[string]any) (string, string, any, error) {
				p := argOrEnv(a, "project", "TATARA_PROJECT")
				if p == "" {
					return "", "", nil, fmt.Errorf("project required")
				}
				return http.MethodGet, "/projects/" + url.PathEscape(p), nil, nil
			}),
		op("repo_list", "List Repositories in a Project.",
			`{"type":"object","properties":{"project":{"type":"string"}},"required":["project"]}`,
			func(a map[string]any) (string, string, any, error) {
				p := argOrEnv(a, "project", "TATARA_PROJECT")
				if p == "" {
					return "", "", nil, fmt.Errorf("project required")
				}
				return http.MethodGet, "/projects/" + url.PathEscape(p) + "/repositories", nil, nil
			}),
		op("task_list", "List Tasks in a Project.",
			`{"type":"object","properties":{"project":{"type":"string"}},"required":["project"]}`,
			func(a map[string]any) (string, string, any, error) {
				p := argOrEnv(a, "project", "TATARA_PROJECT")
				if p == "" {
					return "", "", nil, fmt.Errorf("project required")
				}
				return http.MethodGet, "/projects/" + url.PathEscape(p) + "/tasks", nil, nil
			}),
		op("task_get", "Get a Task by name.",
			`{"type":"object","properties":{"task":{"type":"string"}},"required":["task"]}`,
			func(a map[string]any) (string, string, any, error) {
				tk := argOrEnv(a, "task", "TATARA_TASK")
				if tk == "" {
					return "", "", nil, fmt.Errorf("task required")
				}
				return http.MethodGet, "/tasks/" + url.PathEscape(tk), nil, nil
			}),
		op("task_update", "Record agent status notes on a Task (resultSummary, note).",
			`{"type":"object","properties":{"task":{"type":"string"},"resultSummary":{"type":"string"},"note":{"type":"string"}},"required":["task"]}`,
			func(a map[string]any) (string, string, any, error) {
				tk := argOrEnv(a, "task", "TATARA_TASK")
				if tk == "" {
					return "", "", nil, fmt.Errorf("task required")
				}
				body := map[string]any{}
				if v, ok := a["resultSummary"]; ok {
					body["resultSummary"] = v
				}
				if v, ok := a["note"]; ok {
					body["note"] = v
				}
				return http.MethodPatch, "/tasks/" + url.PathEscape(tk), body, nil
			}),
		op("subtask_list", "List Subtasks of a Task (sorted by order).",
			`{"type":"object","properties":{"task":{"type":"string"}},"required":["task"]}`,
			func(a map[string]any) (string, string, any, error) {
				tk := argOrEnv(a, "task", "TATARA_TASK")
				if tk == "" {
					return "", "", nil, fmt.Errorf("task required")
				}
				return http.MethodGet, "/tasks/" + url.PathEscape(tk) + "/subtasks", nil, nil
			}),
		op("subtask_create", "Create a Subtask under a Task (agent self-planning).",
			`{"type":"object","properties":{"task":{"type":"string"},"title":{"type":"string"},"detail":{"type":"string"},"order":{"type":"integer"}},"required":["task","title"]}`,
			func(a map[string]any) (string, string, any, error) {
				tk := argOrEnv(a, "task", "TATARA_TASK")
				if tk == "" {
					return "", "", nil, fmt.Errorf("task required")
				}
				if argString(a, "title") == "" {
					return "", "", nil, fmt.Errorf("title required")
				}
				body := map[string]any{"title": a["title"]}
				if v, ok := a["detail"]; ok {
					body["detail"] = v
				}
				if v, ok := a["order"]; ok {
					body["order"] = v
				}
				return http.MethodPost, "/tasks/" + url.PathEscape(tk) + "/subtasks", body, nil
			}),
		op("subtask_update", "Update a Subtask status (phase, result, turnId).",
			`{"type":"object","properties":{"subtask":{"type":"string"},"phase":{"type":"string"},"result":{"type":"string"},"turnId":{"type":"string"}},"required":["subtask"]}`,
			func(a map[string]any) (string, string, any, error) {
				st := argString(a, "subtask")
				if st == "" {
					return "", "", nil, fmt.Errorf("subtask required")
				}
				body := map[string]any{}
				for _, k := range []string{"phase", "result", "turnId"} {
					if v, ok := a[k]; ok {
						body[k] = v
					}
				}
				return http.MethodPatch, "/subtasks/" + url.PathEscape(st), body, nil
			}),
	}
}

// Invoke executes a tool against the given client and returns the raw response body.
func Invoke(ctx context.Context, c *client.Client, t Tool, args map[string]any) ([]byte, error) {
	method, path, body, err := t.Build(args)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	buf, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("tatara: %s %s -> %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(buf)))
	}
	return buf, nil
}
