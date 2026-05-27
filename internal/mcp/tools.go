package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/szymonrychu/tatara-cli/internal/client"
)

// Tool describes one tatara-memory REST operation exposed as an MCP tool.
type Tool struct {
	Name        string
	Description string
	Schema      json.RawMessage
	Build       func(args map[string]any) (method, path string, body any, err error)
}

// AllTools returns the 13-entry tool registry mapping tatara-memory REST endpoints.
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
