package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	assert.Len(t, AllTools(), 13)
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
