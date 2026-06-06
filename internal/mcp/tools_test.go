package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/auth"
	"github.com/szymonrychu/tatara-cli/internal/client"
)

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
	assert.Len(t, AllTools(), 22)
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

func TestInvoke_DeleteEdge_CompositeID(t *testing.T) {
	var gotMethod string
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		// net/http decodes %7C -> | in r.URL.Path; the encoding is verified by
		// TestDeleteEdge_PathEscaping which tests Build() directly.
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := freshClient(t, srv.URL)
	tool := toolByName(t, "delete_edge")
	_, err := Invoke(context.Background(), c, tool, map[string]any{"id": "a||b"})
	require.NoError(t, err)
	assert.Equal(t, http.MethodDelete, gotMethod)
	assert.Equal(t, "/edges/a||b", gotPath)
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

// TestDeleteEdge_PathEscaping verifies url.PathEscape encodes pipes correctly
// without needing a running server.
func TestDeleteEdge_PathEscaping(t *testing.T) {
	tool := toolByName(t, "delete_edge")
	_, path, _, err := tool.Build(map[string]any{"id": "a||b"})
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(path, "a%7C%7Cb"), "path=%s", path)
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
}
