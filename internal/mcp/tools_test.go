package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/auth"
	"github.com/szymonrychu/tatara-cli/internal/client"
)

func TestOperatorTools_BuildPaths(t *testing.T) {
	cases := []struct {
		tool   string
		args   map[string]any
		method string
		path   string
	}{
		{"project_list", map[string]any{}, http.MethodGet, "/projects"},
		{"project_get", map[string]any{"project": "alpha"}, http.MethodGet, "/projects/alpha"},
		{"repo_list", map[string]any{"project": "alpha"}, http.MethodGet, "/projects/alpha/repositories"},
		{"task_list", map[string]any{"project": "alpha"}, http.MethodGet, "/projects/alpha/tasks"},
		{"task_get", map[string]any{"task": "t1"}, http.MethodGet, "/tasks/t1"},
		{"task_update", map[string]any{"task": "t1", "resultSummary": "x"}, http.MethodPatch, "/tasks/t1"},
		{"subtask_list", map[string]any{"task": "t1"}, http.MethodGet, "/tasks/t1/subtasks"},
		{"subtask_create", map[string]any{"task": "t1", "title": "step"}, http.MethodPost, "/tasks/t1/subtasks"},
		{"subtask_update", map[string]any{"subtask": "s1", "phase": "Done"}, http.MethodPatch, "/subtasks/s1"},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			m, p, _, err := operatorToolByName(t, c.tool).Build(c.args)
			require.NoError(t, err)
			require.Equal(t, c.method, m)
			require.Equal(t, c.path, p)
		})
	}
}

func TestOperatorTools_RequireArgs(t *testing.T) {
	_, _, _, err := operatorToolByName(t, "project_get").Build(map[string]any{})
	require.Error(t, err) // project required
	_, _, _, err = operatorToolByName(t, "task_get").Build(map[string]any{})
	require.Error(t, err) // task required
	_, _, _, err = operatorToolByName(t, "subtask_create").Build(map[string]any{"task": "t1"})
	require.Error(t, err) // title required
	_, _, _, err = operatorToolByName(t, "subtask_update").Build(map[string]any{})
	require.Error(t, err) // subtask required
}

func TestOperatorTools_Invoke(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"s1","taskRef":"t1"}`))
	}))
	defer srv.Close()
	c := freshClient(t, srv.URL)
	body, err := Invoke(context.Background(), c, operatorToolByName(t, "subtask_create"),
		map[string]any{"task": "t1", "title": "step", "detail": "d", "order": float64(1)})
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "/tasks/t1/subtasks", gotPath)
	require.Equal(t, "step", gotBody["title"])
	require.Contains(t, string(body), "taskRef")
}

func TestAllOperatorTools_Count(t *testing.T) {
	require.Len(t, OperatorTools(), 9)
}

func TestOperatorTools_TargetIsOperator(t *testing.T) {
	for _, tl := range OperatorTools() {
		require.Equal(t, TargetOperator, tl.Target)
	}
}

func operatorToolByName(t *testing.T, name string) Tool {
	t.Helper()
	for _, tl := range OperatorTools() {
		if tl.Name == name {
			return tl
		}
	}
	t.Fatalf("operator tool %q not found", name)
	return Tool{}
}

// freshClient returns a Client pointed at the given base URL with a valid token.
func freshClient(t *testing.T, baseURL string) *client.Client {
	t.Helper()
	tok := &auth.Token{
		AccessToken: "test-tok",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		TokenType:   "Bearer",
	}
	c, err := client.New(client.Config{BaseURL: baseURL, Token: tok})
	require.NoError(t, err)
	return c
}

func TestAllTools_ThirteenEntries(t *testing.T) {
	assert.Len(t, AllTools(), 23)
}

func TestAllTools_SchemasAreValidJSON(t *testing.T) {
	for _, tool := range AllTools() {
		var v any
		err := json.Unmarshal(tool.Schema, &v)
		assert.NoError(t, err, "tool %q has invalid JSON schema", tool.Name)
	}
}

func TestAllTools_NamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, tool := range AllTools() {
		assert.False(t, seen[tool.Name], "duplicate tool name: %q", tool.Name)
		seen[tool.Name] = true
	}
}

func TestInvoke_CreateMemory(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"track_id":"abc123"}`))
	}))
	defer srv.Close()

	c := freshClient(t, srv.URL)
	tool := toolByName(t, "create_memory")
	args := map[string]any{"text": "hello world"}
	body, err := Invoke(context.Background(), c, tool, args)
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/memories", gotPath)
	assert.Equal(t, "hello world", gotBody["text"])
	assert.Contains(t, string(body), "track_id")
}

func TestInvoke_GetMemory(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"mem-1"}`))
	}))
	defer srv.Close()

	c := freshClient(t, srv.URL)
	tool := toolByName(t, "get_memory")
	_, err := Invoke(context.Background(), c, tool, map[string]any{"id": "mem-1"})
	require.NoError(t, err)
	assert.Equal(t, "/memories/mem-1", gotPath)
}

func TestInvoke_SearchEntities_WithQ(t *testing.T) {
	var gotRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := freshClient(t, srv.URL)
	tool := toolByName(t, "search_entities")
	_, err := Invoke(context.Background(), c, tool, map[string]any{"q": "foo"})
	require.NoError(t, err)
	assert.Equal(t, "q=foo", gotRawQuery)
}

func TestInvoke_DeleteEdge_PassesOpaqueIDThrough(t *testing.T) {
	var gotMethod string
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		// net/http URL-decodes the path; capture it to verify the opaque ID
		// is passed through verbatim after decoding.
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	// "YQBi" is a base64url-looking opaque ID (as returned by tatara-memory v0.2.0+).
	const opaqueID = "YQBi"
	c := freshClient(t, srv.URL)
	tool := toolByName(t, "delete_edge")
	_, err := Invoke(context.Background(), c, tool, map[string]any{"id": opaqueID})
	require.NoError(t, err)
	assert.Equal(t, http.MethodDelete, gotMethod)
	assert.Equal(t, "/edges/"+opaqueID, gotPath)
}

func TestInvoke_StatusErrorSurfacedAsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal"}`))
	}))
	defer srv.Close()

	c := freshClient(t, srv.URL)
	tool := toolByName(t, "create_memory")
	_, err := Invoke(context.Background(), c, tool, map[string]any{"text": "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestInvoke_MissingRequiredArg(t *testing.T) {
	// Build is called before any HTTP; no server needed.
	tool := toolByName(t, "get_memory")
	_, _, _, err := tool.Build(map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "id required")
}

// toolByName is a test helper that retrieves a tool from AllTools by name.
func toolByName(t *testing.T, name string) Tool {
	t.Helper()
	for _, tool := range AllTools() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found in registry", name)
	return Tool{}
}

// TestDeleteEdge_OpaqueIDInPath verifies that an opaque base64url ID is placed
// verbatim in the URL path (url.PathEscape does not alter base64url characters).
func TestDeleteEdge_OpaqueIDInPath(t *testing.T) {
	tool := toolByName(t, "delete_edge")
	_, path, _, err := tool.Build(map[string]any{"id": "YQBi"})
	require.NoError(t, err)
	assert.Equal(t, "/edges/YQBi", path)
}

// TestSearchEntities_NoQ verifies that omitting q produces a clean /entities path.
func TestSearchEntities_NoQ(t *testing.T) {
	tool := toolByName(t, "search_entities")
	_, path, _, err := tool.Build(map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, "/entities", path)
}

func TestCodeTools_BuildQueries(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]any
		path string            // expected URL.Path
		q    map[string]string // expected query params
	}{
		{"code_search", map[string]any{"repo": "r", "q": "x", "type": "go_func", "limit": float64(10)}, "/code/entities", map[string]string{"repo": "r", "q": "x", "type": "go_func", "limit": "10"}},
		{"code_entity", map[string]any{"repo": "r", "id": "go:func:m.F"}, "/code/entity", map[string]string{"repo": "r", "id": "go:func:m.F"}},
		{"code_neighbors", map[string]any{"repo": "r", "id": "x", "relation": "calls", "direction": "out", "depth": float64(2)}, "/code/neighbors", map[string]string{"repo": "r", "id": "x", "relation": "calls", "direction": "out", "depth": "2"}},
		{"code_callers", map[string]any{"repo": "r", "id": "x", "depth": float64(3)}, "/code/callers", map[string]string{"repo": "r", "id": "x", "depth": "3"}},
		{"code_callees", map[string]any{"repo": "r", "id": "x"}, "/code/callees", map[string]string{"repo": "r", "id": "x"}},
		{"code_dependents", map[string]any{"repo": "r", "id": "x"}, "/code/dependents", map[string]string{"repo": "r", "id": "x"}},
		{"code_dependencies", map[string]any{"repo": "r", "id": "x"}, "/code/dependencies", map[string]string{"repo": "r", "id": "x"}},
		{"code_file_imports", map[string]any{"repo": "r", "path": "a/b.go"}, "/code/file-imports", map[string]string{"repo": "r", "path": "a/b.go"}},
		{"code_resource_graph", map[string]any{"repo": "r", "id": "x", "depth": float64(1)}, "/code/resource-graph", map[string]string{"repo": "r", "id": "x", "depth": "1"}},
		{"code_cross_repo", map[string]any{"repo": "r", "id": "x"}, "/code/cross-repo", map[string]string{"repo": "r", "id": "x"}},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			var gotPath string
			var gotQuery url.Values
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotQuery = r.URL.Query()
				_, _ = w.Write([]byte(`[]`))
			}))
			defer srv.Close()
			cli := freshClient(t, srv.URL)
			_, err := Invoke(context.Background(), cli, toolByName(t, c.tool), c.args)
			require.NoError(t, err)
			require.Equal(t, c.path, gotPath)
			for k, v := range c.q {
				require.Equal(t, v, gotQuery.Get(k))
			}
		})
	}
}

func TestCodeTools_RequireArgs(t *testing.T) {
	_, _, _, err := toolByName(t, "code_entity").Build(map[string]any{"repo": "r"})
	require.Error(t, err) // id required
	_, _, _, err = toolByName(t, "code_search").Build(map[string]any{})
	require.Error(t, err) // repo required
	_, _, _, err = toolByName(t, "code_neighbors").Build(map[string]any{"repo": "r", "id": "x"})
	require.Error(t, err) // relation required
	_, _, _, err = toolByName(t, "code_cross_repo").Build(map[string]any{"repo": "r"})
	require.Error(t, err) // id required
}
