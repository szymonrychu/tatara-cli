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
	TargetChat                   // chat tools hit tatara-chat
)

// Tool describes a tatara REST operation exposed as an MCP tool.
type Tool struct {
	Name        string
	Description string
	Schema      json.RawMessage
	Target      Target
	Build       func(args map[string]any) (method, path string, body any, err error)
}

// AllTools returns the 34-entry tool registry mapping tatara-memory REST endpoints.
func AllTools() []Tool {
	return []Tool{
		{
			Name:        "create_memory",
			Description: "Insert a new text memory. Returns the track_id.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"},"metadata":{"type":"object","additionalProperties":{"type":"string"}}},"required":["text"],"additionalProperties":false}`),
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
			Schema:      json.RawMessage(`{"type":"object","properties":{"items":{"type":"array","items":{"type":"object","properties":{"text":{"type":"string"},"metadata":{"type":"object"}},"required":["text"]}}},"required":["items"],"additionalProperties":false}`),
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
			Schema:      json.RawMessage(`{"type":"object","properties":{"mode":{"type":"string","enum":["local","global","hybrid","naive"]},"text":{"type":"string"},"top_k":{"type":"integer"}},"required":["mode","text"],"additionalProperties":false}`),
			Build: func(a map[string]any) (string, string, any, error) {
				return http.MethodPost, "/queries", a, nil
			},
		},
		{
			Name:        "describe",
			Description: "Generative answer plus source paths for the given query.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"mode":{"type":"string","enum":["local","global","hybrid","naive"]},"text":{"type":"string"},"top_k":{"type":"integer"}},"required":["mode","text"],"additionalProperties":false}`),
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
				patch, ok := a["patch"]
				if !ok || patch == nil {
					return "", "", nil, fmt.Errorf("patch required")
				}
				return http.MethodPatch, "/entities/" + url.PathEscape(id), patch, nil
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
			Schema:      json.RawMessage(`{"type":"object","properties":{"from_entity":{"type":"string"},"to_entity":{"type":"string"},"relation":{"type":"string"},"properties":{"type":"object"}},"required":["from_entity","to_entity","relation"],"additionalProperties":false}`),
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
			Schema: json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string"},"from":{"type":"string"},"to":{"type":"string"},"relations":{"type":"string"},"max_depth":{"type":"integer"}},"required":["repo","from","to"]}`),
			Build:  codeGet("/code-graph/path", []string{"repo", "from", "to"}, []string{"relations", "max_depth"})},
		{Name: "code_important", Description: "Most important entities in the code graph, ranked by 'by' (degree default, or betweenness from persisted analytics), scoped to repo.",
			Schema: json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string"},"limit":{"type":"integer"},"by":{"type":"string","enum":["degree","betweenness"]}},"required":["repo"]}`),
			Build:  codeGet("/code-graph/important", []string{"repo"}, []string{"limit", "by"})},
		{Name: "code_stats", Description: "Graph statistics: entity/edge counts, types, tiers, isolated entities, import cycles.",
			Schema: json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string"}},"required":["repo"]}`),
			Build:  codeGet("/code-graph/stats", []string{"repo"}, nil)},
		{Name: "code_ambiguous_edges", Description: "Edges with low confidence score or AMBIGUOUS tier, ordered by score ascending.",
			Schema: json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string"},"limit":{"type":"integer"}},"required":["repo"]}`),
			Build:  codeGet("/code-graph/ambiguous", []string{"repo"}, []string{"limit"})},
		{Name: "code_explain", Description: "Full context for a code entity: detail, in/out neighbors with file locations.",
			Schema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"repo":{"type":"string"}},"required":["id","repo"]}`),
			Build:  codeGet("/code-graph/explain", []string{"id", "repo"}, nil)},
		{Name: "code_related", Description: "Semantic neighbors of an entity over semantic edges (conceptually_related_to, semantically_similar_to, rationale_for, shares_data_with, cites), filtered by relation and min confidence.",
			Schema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"relations":{"type":"string"},"min_confidence":{"type":"number"},"repo":{"type":"string"}},"required":["id","repo"]}`),
			Build:  codeGet("/code-graph/related", []string{"id", "repo"}, []string{"relations", "min_confidence"})},
		{Name: "code_hyperedges", Description: "List n-ary hyperedges (group relations) in the code graph, optionally scoped to a member entity.",
			Schema: json.RawMessage(`{"type":"object","properties":{"entity":{"type":"string"},"repo":{"type":"string"}},"required":["repo"]}`),
			Build:  codeGet("/code-graph/hyperedges", []string{"repo"}, []string{"entity"})},
		{Name: "code_hyperedge", Description: "Get a single hyperedge by id with its members.",
			Schema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"repo":{"type":"string"}},"required":["id","repo"]}`),
			Build:  codeGet("/code-graph/hyperedge", []string{"id", "repo"}, nil)},
		{Name: "code_communities", Description: "List detected communities in the code graph (community, label, size, cohesion), scoped to repo.",
			Schema: json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string"}},"required":["repo"]}`),
			Build:  codeGet("/code-graph/communities", []string{"repo"}, nil)},
		{Name: "code_community", Description: "List the member entities of a specific community.",
			Schema: json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string"},"community":{"type":"integer"}},"required":["repo","community"]}`),
			Build:  codeGet("/code-graph/community", []string{"repo", "community"}, nil)},
		{Name: "code_bridges", Description: "High-betweenness entities that connect more than one community (graph bridges), ranked, scoped to repo.",
			Schema: json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string"},"limit":{"type":"integer"}},"required":["repo"]}`),
			Build:  codeGet("/code-graph/bridges", []string{"repo"}, []string{"limit"})},
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

// OperatorTools returns the tatara-operator REST tools (Target=TargetOperator).
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
		op("project_get", "Get a Project by name. Defaults to TATARA_PROJECT env when project is omitted.",
			`{"type":"object","properties":{"project":{"type":"string"}}}`,
			func(a map[string]any) (string, string, any, error) {
				p := argOrEnv(a, "project", "TATARA_PROJECT")
				if p == "" {
					return "", "", nil, fmt.Errorf("project required")
				}
				return http.MethodGet, "/projects/" + url.PathEscape(p), nil, nil
			}),
		op("repo_list", "List Repositories in a Project. Defaults to TATARA_PROJECT env when project is omitted.",
			`{"type":"object","properties":{"project":{"type":"string"}}}`,
			func(a map[string]any) (string, string, any, error) {
				p := argOrEnv(a, "project", "TATARA_PROJECT")
				if p == "" {
					return "", "", nil, fmt.Errorf("project required")
				}
				return http.MethodGet, "/projects/" + url.PathEscape(p) + "/repositories", nil, nil
			}),
		op("task_list", "List Tasks in a Project. Defaults to TATARA_PROJECT env when project is omitted.",
			`{"type":"object","properties":{"project":{"type":"string"}}}`,
			func(a map[string]any) (string, string, any, error) {
				p := argOrEnv(a, "project", "TATARA_PROJECT")
				if p == "" {
					return "", "", nil, fmt.Errorf("project required")
				}
				return http.MethodGet, "/projects/" + url.PathEscape(p) + "/tasks", nil, nil
			}),
		op("task_get", "Get a Task by name. Defaults to TATARA_TASK env when task is omitted.",
			`{"type":"object","properties":{"task":{"type":"string"}}}`,
			func(a map[string]any) (string, string, any, error) {
				tk := argOrEnv(a, "task", "TATARA_TASK")
				if tk == "" {
					return "", "", nil, fmt.Errorf("task required")
				}
				return http.MethodGet, "/tasks/" + url.PathEscape(tk), nil, nil
			}),
		op("task_update", "Record agent status notes on a Task (resultSummary, note). Defaults to TATARA_TASK env when task is omitted.",
			`{"type":"object","properties":{"task":{"type":"string"},"resultSummary":{"type":"string"},"note":{"type":"string"}}}`,
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
		op("subtask_list", "List Subtasks of a Task (sorted by order). Defaults to TATARA_TASK env when task is omitted.",
			`{"type":"object","properties":{"task":{"type":"string"}}}`,
			func(a map[string]any) (string, string, any, error) {
				tk := argOrEnv(a, "task", "TATARA_TASK")
				if tk == "" {
					return "", "", nil, fmt.Errorf("task required")
				}
				return http.MethodGet, "/tasks/" + url.PathEscape(tk) + "/subtasks", nil, nil
			}),
		op("subtask_create", "Create a Subtask under a Task (agent self-planning). Defaults to TATARA_TASK env when task is omitted.",
			`{"type":"object","properties":{"task":{"type":"string"},"title":{"type":"string"},"detail":{"type":"string"},"order":{"type":"integer"}},"required":["title"]}`,
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
				// No TATARA_SUBTASK env exists; subtask IDs are ephemeral and not
				// injected by the wrapper Pod, so argString (no env fallback) is correct.
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
		op("propose_issue", "Propose a new SCM issue (bug or improvement). The operator opens it under the bot identity as an idea-labelled discovery issue; it stays in discussion until a human approves. Embed <!-- tatara-authored --> in the body to keep it in discovery. project defaults to TATARA_PROJECT env when omitted.",
			`{"type":"object","properties":{"project":{"type":"string"},"repo":{"type":"string"},"title":{"type":"string"},"body":{"type":"string"},"kind":{"type":"string","enum":["bug","improvement"]}},"required":["title","body","kind","repo"]}`,
			func(a map[string]any) (string, string, any, error) {
				p := argOrEnv(a, "project", "TATARA_PROJECT")
				if p == "" {
					return "", "", nil, fmt.Errorf("project required")
				}
				repo := argString(a, "repo")
				if repo == "" {
					return "", "", nil, fmt.Errorf("repo required")
				}
				if argString(a, "title") == "" {
					return "", "", nil, fmt.Errorf("title required")
				}
				if argString(a, "body") == "" {
					return "", "", nil, fmt.Errorf("body required")
				}
				if argString(a, "kind") == "" {
					return "", "", nil, fmt.Errorf("kind required")
				}
				body := map[string]any{
					"repositoryRef": repo,
					"title":         a["title"],
					"body":          a["body"],
					"kind":          a["kind"],
				}
				return http.MethodPost, "/projects/" + url.PathEscape(p) + "/issues", body, nil
			}),
		op("review_verdict", "Record a review verdict on a human-authored PR/MR Task (decision approve|request_changes|comment, optional body and inline suggestions). The operator posts it to SCM.",
			`{"type":"object","properties":{"task":{"type":"string"},"decision":{"type":"string","enum":["approve","request_changes","comment"]},"body":{"type":"string"},"suggestions":{"type":"array","items":{"type":"object","properties":{"path":{"type":"string"},"line":{"type":"integer"},"body":{"type":"string"}},"required":["path","line","body"]}}},"required":["decision"]}`,
			func(a map[string]any) (string, string, any, error) {
				tk := argOrEnv(a, "task", "TATARA_TASK")
				if tk == "" {
					return "", "", nil, fmt.Errorf("task required")
				}
				if argString(a, "decision") == "" {
					return "", "", nil, fmt.Errorf("decision required")
				}
				body := map[string]any{"decision": a["decision"]}
				if v, ok := a["body"]; ok {
					body["body"] = v
				}
				if v, ok := a["suggestions"]; ok {
					body["suggestions"] = v
				}
				return http.MethodPost, "/tasks/" + url.PathEscape(tk) + "/review", body, nil
			}),
		op("pr_outcome", "Decide the outcome of a tatara-authored PR/MR Task (action merge|close, optional reason). selfImprove only; the operator enforces merge policy.",
			`{"type":"object","properties":{"task":{"type":"string"},"action":{"type":"string","enum":["merge","close"]},"reason":{"type":"string"}},"required":["action"]}`,
			func(a map[string]any) (string, string, any, error) {
				tk := argOrEnv(a, "task", "TATARA_TASK")
				if tk == "" {
					return "", "", nil, fmt.Errorf("task required")
				}
				if argString(a, "action") == "" {
					return "", "", nil, fmt.Errorf("action required")
				}
				body := map[string]any{"action": a["action"]}
				if v, ok := a["reason"]; ok {
					body["reason"] = v
				}
				return http.MethodPost, "/tasks/" + url.PathEscape(tk) + "/pr-outcome", body, nil
			}),
		op("change_summary", "Post a change summary for the current task: PR title, PR body, delivered scope, optional remaining scope, and optionally what was most problematic.",
			`{"type":"object","properties":{"task":{"type":"string"},"pr_title":{"type":"string"},"pr_body":{"type":"string"},"delivered_scope":{"type":"string"},"remaining_scope":{"type":"string"},"most_problematic":{"type":"string","description":"What was most problematic or surprising during implementation (gotchas, dead-ends, tricky integration points). Surfaced in the MR body and recorded in docs."}},"required":["pr_title","pr_body","delivered_scope"]}`,
			func(a map[string]any) (string, string, any, error) {
				tk := argOrEnv(a, "task", "TATARA_TASK")
				if tk == "" {
					return "", "", nil, fmt.Errorf("task required")
				}
				if argString(a, "pr_title") == "" {
					return "", "", nil, fmt.Errorf("pr_title required")
				}
				if argString(a, "pr_body") == "" {
					return "", "", nil, fmt.Errorf("pr_body required")
				}
				if argString(a, "delivered_scope") == "" {
					return "", "", nil, fmt.Errorf("delivered_scope required")
				}
				// Operator REST DTOs are camelCase; map the snake_case tool args to
				// the changeSummaryReq json keys (it decodes with DisallowUnknownFields).
				body := map[string]any{
					"prTitle":        a["pr_title"],
					"prBody":         a["pr_body"],
					"deliveredScope": a["delivered_scope"],
				}
				if v, ok := a["remaining_scope"]; ok {
					body["remainingScope"] = v
				}
				if v, ok := a["most_problematic"]; ok {
					body["mostProblematic"] = v
				}
				return http.MethodPost, "/tasks/" + url.PathEscape(tk) + "/change-summary", body, nil
			}),
		op("submit_handover", "Submit a handover document for the current task so the next agent has full context.",
			`{"type":"object","properties":{"task":{"type":"string"},"handover":{"type":"string"}},"required":["handover"]}`,
			func(a map[string]any) (string, string, any, error) {
				tk := argOrEnv(a, "task", "TATARA_TASK")
				if tk == "" {
					return "", "", nil, fmt.Errorf("task required")
				}
				if argString(a, "handover") == "" {
					return "", "", nil, fmt.Errorf("handover required")
				}
				body := map[string]any{"handover": a["handover"]}
				return http.MethodPost, "/tasks/" + url.PathEscape(tk) + "/handover", body, nil
			}),
		op("issue_outcome", "Record the outcome of an issue-triage task: implement (open a PR), close (with a comment), or discuss (post questions/design notes as a comment).",
			`{"type":"object","properties":{"action":{"type":"string","enum":["implement","close","discuss"]},"comment":{"type":"string"},"plan":{"type":"string","description":"When action=implement, a short description of WHAT will be implemented and HOW (flow, key ideas, approach). Posted to the issue as the implementation-start message."}},"required":["action"]}`,
			func(a map[string]any) (string, string, any, error) {
				tk := argOrEnv(a, "task", "TATARA_TASK")
				if tk == "" {
					return "", "", nil, fmt.Errorf("task required")
				}
				if argString(a, "action") == "" {
					return "", "", nil, fmt.Errorf("action required")
				}
				body := map[string]any{"action": a["action"]}
				if v, ok := a["comment"]; ok {
					body["comment"] = v
				}
				if v, ok := a["plan"]; ok {
					body["plan"] = v
				}
				return http.MethodPost, "/tasks/" + url.PathEscape(tk) + "/issue-outcome", body, nil
			}),
		op("decline_implementation", "Declare that you will NOT implement this issue. Call this when, after investigation, you have determined the issue should not or need not be implemented. Posts the reason as a comment on the issue and parks the task. A silent finish with no PR and no decline_implementation call is NOT allowed.",
			`{"type":"object","properties":{"task":{"type":"string"},"reason":{"type":"string","description":"Why you are NOT implementing this issue (what you considered, why it should not be done / is already done / is wrong). Posted to the issue."}},"required":["reason"]}`,
			func(a map[string]any) (string, string, any, error) {
				tk := argOrEnv(a, "task", "TATARA_TASK")
				if tk == "" {
					return "", "", nil, fmt.Errorf("task required")
				}
				if argString(a, "reason") == "" {
					return "", "", nil, fmt.Errorf("reason required")
				}
				body := map[string]any{
					"action": "declined",
					"reason": a["reason"],
				}
				return http.MethodPost, "/tasks/" + url.PathEscape(tk) + "/implement-outcome", body, nil
			}),
		op("comment", "Post a free-form comment on the current task's linked issue (answer maintainer questions, post design notes). The operator posts it under the bot identity on the next reconcile and does NOT change the issue's lifecycle state. Use this to keep a discovery conversation alive; use issue_outcome to set the outcome.",
			`{"type":"object","properties":{"task":{"type":"string"},"body":{"type":"string"}},"required":["body"]}`,
			func(a map[string]any) (string, string, any, error) {
				tk := argOrEnv(a, "task", "TATARA_TASK")
				if tk == "" {
					return "", "", nil, fmt.Errorf("task required")
				}
				if argString(a, "body") == "" {
					return "", "", nil, fmt.Errorf("body required")
				}
				body := map[string]any{"body": a["body"]}
				return http.MethodPost, "/tasks/" + url.PathEscape(tk) + "/comment", body, nil
			}),
		op("comment_on_issue", "Post a comment on an EXISTING open issue (identified by repo + number) when your idea duplicates, extends, or is a sub-aspect of it - instead of opening a duplicate issue. The operator posts it under the bot identity. Use propose_issue ONLY for genuinely novel, standalone problems.",
			`{"type":"object","properties":{"project":{"type":"string"},"repo":{"type":"string"},"number":{"type":"integer"},"body":{"type":"string"}},"required":["repo","number","body"]}`,
			func(a map[string]any) (string, string, any, error) {
				p := argOrEnv(a, "project", "TATARA_PROJECT")
				if p == "" {
					return "", "", nil, fmt.Errorf("project required")
				}
				if argString(a, "repo") == "" {
					return "", "", nil, fmt.Errorf("repo required")
				}
				if _, ok := a["number"]; !ok {
					return "", "", nil, fmt.Errorf("number required")
				}
				if argString(a, "body") == "" {
					return "", "", nil, fmt.Errorf("body required")
				}
				body := map[string]any{
					"repo":   a["repo"],
					"number": a["number"],
					"body":   a["body"],
				}
				return http.MethodPost, "/projects/" + url.PathEscape(p) + "/issue-comment", body, nil
			}),
	}
}

// ChatTools returns the 10 tatara-chat REST tools (Target=TargetChat). They
// cover the documented room/participant/message endpoints so an agent driven
// through `tatara mcp` can run a create -> join -> send -> poll loop.
func ChatTools() []Tool {
	chat := func(name, desc, schema string, build func(map[string]any) (string, string, any, error)) Tool {
		return Tool{Name: name, Description: desc, Schema: json.RawMessage(schema), Target: TargetChat, Build: build}
	}
	return []Tool{
		chat("chat_create_room", "Create a chat room. Returns the room (with its id).",
			`{"type":"object","properties":{"name":{"type":"string"},"created_by":{"type":"string"}},"required":["name"]}`,
			func(a map[string]any) (string, string, any, error) {
				if argString(a, "name") == "" {
					return "", "", nil, fmt.Errorf("name required")
				}
				body := map[string]any{"name": a["name"]}
				if v := argString(a, "created_by"); v != "" {
					body["created_by"] = v
				}
				return http.MethodPost, "/rooms", body, nil
			}),
		chat("chat_list_rooms", "List chat rooms, optionally filtered by status (active|archived).",
			`{"type":"object","properties":{"status":{"type":"string","enum":["active","archived"]}}}`,
			func(a map[string]any) (string, string, any, error) {
				path := "/rooms"
				if s := argString(a, "status"); s != "" {
					path += "?status=" + url.QueryEscape(s)
				}
				return http.MethodGet, path, nil, nil
			}),
		chat("chat_get_room", "Get a chat room and its participants by room id.",
			`{"type":"object","properties":{"room_id":{"type":"string"}},"required":["room_id"]}`,
			func(a map[string]any) (string, string, any, error) {
				id := argString(a, "room_id")
				if id == "" {
					return "", "", nil, fmt.Errorf("room_id required")
				}
				return http.MethodGet, "/rooms/" + url.PathEscape(id), nil, nil
			}),
		chat("chat_close_room", "Close (archive) a chat room by id. No further messages can be posted.",
			`{"type":"object","properties":{"room_id":{"type":"string"}},"required":["room_id"]}`,
			func(a map[string]any) (string, string, any, error) {
				id := argString(a, "room_id")
				if id == "" {
					return "", "", nil, fmt.Errorf("room_id required")
				}
				return http.MethodDelete, "/rooms/" + url.PathEscape(id), nil, nil
			}),
		chat("chat_add_participant", "Join a chat room. Returns the participant (with its id); use that id to send and poll.",
			`{"type":"object","properties":{"room_id":{"type":"string"},"name":{"type":"string"},"role":{"type":"string","enum":["orchestrator","implementer","reviewer","human"]}},"required":["room_id","name"]}`,
			func(a map[string]any) (string, string, any, error) {
				id := argString(a, "room_id")
				if id == "" {
					return "", "", nil, fmt.Errorf("room_id required")
				}
				if argString(a, "name") == "" {
					return "", "", nil, fmt.Errorf("name required")
				}
				body := map[string]any{"name": a["name"]}
				if v := argString(a, "role"); v != "" {
					body["role"] = v
				}
				return http.MethodPost, "/rooms/" + url.PathEscape(id) + "/participants", body, nil
			}),
		chat("chat_list_participants", "List the participants of a chat room.",
			`{"type":"object","properties":{"room_id":{"type":"string"}},"required":["room_id"]}`,
			func(a map[string]any) (string, string, any, error) {
				id := argString(a, "room_id")
				if id == "" {
					return "", "", nil, fmt.Errorf("room_id required")
				}
				return http.MethodGet, "/rooms/" + url.PathEscape(id) + "/participants", nil, nil
			}),
		chat("chat_remove_participant", "Remove a participant from a chat room.",
			`{"type":"object","properties":{"room_id":{"type":"string"},"participant_id":{"type":"string"}},"required":["room_id","participant_id"]}`,
			func(a map[string]any) (string, string, any, error) {
				id := argString(a, "room_id")
				if id == "" {
					return "", "", nil, fmt.Errorf("room_id required")
				}
				pid := argString(a, "participant_id")
				if pid == "" {
					return "", "", nil, fmt.Errorf("participant_id required")
				}
				return http.MethodDelete, "/rooms/" + url.PathEscape(id) + "/participants/" + url.PathEscape(pid), nil, nil
			}),
		chat("chat_send_message", "Send a message to a chat room as a participant. Set target to a participant id for a direct message; kind defaults to message.",
			`{"type":"object","properties":{"room_id":{"type":"string"},"participant_id":{"type":"string"},"body":{"type":"string"},"target":{"type":"string"},"kind":{"type":"string","enum":["message","system"]}},"required":["room_id","participant_id","body"]}`,
			func(a map[string]any) (string, string, any, error) {
				id := argString(a, "room_id")
				if id == "" {
					return "", "", nil, fmt.Errorf("room_id required")
				}
				pid := argString(a, "participant_id")
				if pid == "" {
					return "", "", nil, fmt.Errorf("participant_id required")
				}
				if argString(a, "body") == "" {
					return "", "", nil, fmt.Errorf("body required")
				}
				body := map[string]any{"participant_id": pid, "body": a["body"]}
				if v := argString(a, "target"); v != "" {
					body["target"] = v
				}
				if v := argString(a, "kind"); v != "" {
					body["kind"] = v
				}
				return http.MethodPost, "/rooms/" + url.PathEscape(id) + "/messages", body, nil
			}),
		chat("chat_poll_messages", "Poll for messages addressed to a participant since its last poll (advances the participant's cursor). The response carries room_status and has_more so a loop knows when to stop.",
			`{"type":"object","properties":{"room_id":{"type":"string"},"participant_id":{"type":"string"}},"required":["room_id","participant_id"]}`,
			func(a map[string]any) (string, string, any, error) {
				id := argString(a, "room_id")
				if id == "" {
					return "", "", nil, fmt.Errorf("room_id required")
				}
				pid := argString(a, "participant_id")
				if pid == "" {
					return "", "", nil, fmt.Errorf("participant_id required")
				}
				return http.MethodGet, "/rooms/" + url.PathEscape(id) + "/messages?participant=" + url.QueryEscape(pid), nil, nil
			}),
		chat("chat_get_log", "Read a chat room's full message log (cursor-paginated by sequence). after starts after that sequence; limit caps the page; next is the cursor for the following page.",
			`{"type":"object","properties":{"room_id":{"type":"string"},"after":{"type":"integer"},"limit":{"type":"integer"}},"required":["room_id"]}`,
			func(a map[string]any) (string, string, any, error) {
				id := argString(a, "room_id")
				if id == "" {
					return "", "", nil, fmt.Errorf("room_id required")
				}
				q := url.Values{}
				if v := argString(a, "after"); v != "" {
					q.Set("after", v)
				}
				if v := argString(a, "limit"); v != "" {
					q.Set("limit", v)
				}
				path := "/rooms/" + url.PathEscape(id) + "/log"
				if len(q) > 0 {
					path += "?" + q.Encode()
				}
				return http.MethodGet, path, nil, nil
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
	if resp.StatusCode >= 400 {
		// For auth failures, return a generic message to avoid leaking token
		// details or internal proxy headers the backend may echo. Drain (capped)
		// so the connection can be reused.
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, errBodyCap))
			return nil, fmt.Errorf("tatara: %s %s -> %d: authentication/authorization failed", method, path, resp.StatusCode)
		}
		// Cap the error body to keep error strings (and memory) bounded.
		ebuf, err := io.ReadAll(io.LimitReader(resp.Body, errBodyCap))
		if err != nil {
			return nil, fmt.Errorf("tatara: %s %s -> %d: read body: %w", method, path, resp.StatusCode, err)
		}
		return nil, fmt.Errorf("tatara: %s %s -> %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(ebuf)))
	}
	// Success: read the full body. Tool results (graph queries, memory lists) are
	// routinely larger than the error cap and must not be truncated.
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("tatara: %s %s: read body: %w", method, path, err)
	}
	return buf, nil
}

// errBodyCap bounds how many bytes of an error response body we read into an
// error message, preventing a hostile or broken backend from forcing unbounded
// memory use or multi-megabyte error strings.
const errBodyCap = 4096
