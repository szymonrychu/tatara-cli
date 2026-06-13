package mcp

import (
	"context"
	"encoding/json"
	"fmt"
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
	// Hermetic: these assert the no-arg/no-env error path, so clear any ambient
	// TATARA_TASK / TATARA_PROJECT the runtime may have injected.
	t.Setenv("TATARA_TASK", "")
	t.Setenv("TATARA_PROJECT", "")
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

func TestOperatorTools_EnvFallback(t *testing.T) {
	t.Setenv("TATARA_TASK", "task-from-env")
	t.Setenv("TATARA_PROJECT", "proj-from-env")

	cases := []struct {
		tool string
		args map[string]any
		path string
	}{
		{"task_get", map[string]any{}, "/tasks/task-from-env"},
		{"task_update", map[string]any{"resultSummary": "ok"}, "/tasks/task-from-env"},
		{"subtask_list", map[string]any{}, "/tasks/task-from-env/subtasks"},
		{"subtask_create", map[string]any{"title": "step"}, "/tasks/task-from-env/subtasks"},
		{"task_list", map[string]any{}, "/projects/proj-from-env/tasks"},
		{"repo_list", map[string]any{}, "/projects/proj-from-env/repositories"},
		{"project_get", map[string]any{}, "/projects/proj-from-env"},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			_, p, _, err := operatorToolByName(t, c.tool).Build(c.args)
			require.NoError(t, err)
			require.Equal(t, c.path, p)
		})
	}
}

func TestOperatorTools_ExplicitArgOverridesEnv(t *testing.T) {
	t.Setenv("TATARA_TASK", "env-task")
	_, p, _, err := operatorToolByName(t, "task_get").Build(map[string]any{"task": "explicit-task"})
	require.NoError(t, err)
	require.Equal(t, "/tasks/explicit-task", p)
}

func TestAllOperatorTools_Count(t *testing.T) {
	require.Len(t, OperatorTools(), 15)
}

func TestOperatorTools_TargetIsOperator(t *testing.T) {
	for _, tl := range OperatorTools() {
		require.Equal(t, TargetOperator, tl.Target)
	}
}

func chatToolByName(t *testing.T, name string) Tool {
	t.Helper()
	for _, tl := range ChatTools() {
		if tl.Name == name {
			return tl
		}
	}
	t.Fatalf("chat tool %q not found", name)
	return Tool{}
}

func TestChatTools_Count(t *testing.T) {
	require.Len(t, ChatTools(), 10)
}

func TestChatTools_TargetIsChat(t *testing.T) {
	for _, tl := range ChatTools() {
		require.Equal(t, TargetChat, tl.Target)
	}
}

func TestChatTools_SchemasAreValidJSON(t *testing.T) {
	for _, tl := range ChatTools() {
		var v any
		require.NoErrorf(t, json.Unmarshal(tl.Schema, &v), "chat tool %q has invalid JSON schema", tl.Name)
	}
}

func TestChatTools_BuildPaths(t *testing.T) {
	cases := []struct {
		tool   string
		args   map[string]any
		method string
		path   string
	}{
		{"chat_create_room", map[string]any{"name": "impl"}, http.MethodPost, "/rooms"},
		{"chat_list_rooms", map[string]any{}, http.MethodGet, "/rooms"},
		{"chat_list_rooms", map[string]any{"status": "active"}, http.MethodGet, "/rooms?status=active"},
		{"chat_get_room", map[string]any{"room_id": "r1"}, http.MethodGet, "/rooms/r1"},
		{"chat_close_room", map[string]any{"room_id": "r1"}, http.MethodDelete, "/rooms/r1"},
		{"chat_add_participant", map[string]any{"room_id": "r1", "name": "bot"}, http.MethodPost, "/rooms/r1/participants"},
		{"chat_list_participants", map[string]any{"room_id": "r1"}, http.MethodGet, "/rooms/r1/participants"},
		{"chat_remove_participant", map[string]any{"room_id": "r1", "participant_id": "p1"}, http.MethodDelete, "/rooms/r1/participants/p1"},
		{"chat_send_message", map[string]any{"room_id": "r1", "participant_id": "p1", "body": "hi"}, http.MethodPost, "/rooms/r1/messages"},
		{"chat_poll_messages", map[string]any{"room_id": "r1", "participant_id": "p1"}, http.MethodGet, "/rooms/r1/messages?participant=p1"},
		{"chat_get_log", map[string]any{"room_id": "r1"}, http.MethodGet, "/rooms/r1/log"},
		{"chat_get_log", map[string]any{"room_id": "r1", "after": float64(5), "limit": float64(20)}, http.MethodGet, "/rooms/r1/log?after=5&limit=20"},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			m, p, _, err := chatToolByName(t, c.tool).Build(c.args)
			require.NoError(t, err)
			require.Equal(t, c.method, m)
			require.Equal(t, c.path, p)
		})
	}
}

func TestChatTools_RequireArgs(t *testing.T) {
	_, _, _, err := chatToolByName(t, "chat_create_room").Build(map[string]any{})
	require.Error(t, err) // name required
	_, _, _, err = chatToolByName(t, "chat_get_room").Build(map[string]any{})
	require.Error(t, err) // room_id required
	_, _, _, err = chatToolByName(t, "chat_add_participant").Build(map[string]any{"room_id": "r1"})
	require.Error(t, err) // name required
	_, _, _, err = chatToolByName(t, "chat_remove_participant").Build(map[string]any{"room_id": "r1"})
	require.Error(t, err) // participant_id required
	_, _, _, err = chatToolByName(t, "chat_send_message").Build(map[string]any{"room_id": "r1", "participant_id": "p1"})
	require.Error(t, err) // body required
	_, _, _, err = chatToolByName(t, "chat_poll_messages").Build(map[string]any{"room_id": "r1"})
	require.Error(t, err) // participant_id required
}

func TestChatTools_Bodies(t *testing.T) {
	t.Run("create_room", func(t *testing.T) {
		_, _, body, err := chatToolByName(t, "chat_create_room").Build(map[string]any{"name": "impl", "created_by": "orchestrator"})
		require.NoError(t, err)
		m := body.(map[string]any)
		require.Equal(t, "impl", m["name"])
		require.Equal(t, "orchestrator", m["created_by"])
	})
	t.Run("create_room_name_only", func(t *testing.T) {
		_, _, body, err := chatToolByName(t, "chat_create_room").Build(map[string]any{"name": "impl"})
		require.NoError(t, err)
		_, hasCreatedBy := body.(map[string]any)["created_by"]
		require.False(t, hasCreatedBy)
	})
	t.Run("send_message_full", func(t *testing.T) {
		_, _, body, err := chatToolByName(t, "chat_send_message").Build(map[string]any{
			"room_id": "r1", "participant_id": "p1", "body": "hi", "target": "p2", "kind": "system",
		})
		require.NoError(t, err)
		m := body.(map[string]any)
		require.Equal(t, "p1", m["participant_id"])
		require.Equal(t, "hi", m["body"])
		require.Equal(t, "p2", m["target"])
		require.Equal(t, "system", m["kind"])
	})
	t.Run("send_message_minimal", func(t *testing.T) {
		_, _, body, err := chatToolByName(t, "chat_send_message").Build(map[string]any{
			"room_id": "r1", "participant_id": "p1", "body": "hi",
		})
		require.NoError(t, err)
		m := body.(map[string]any)
		_, hasTarget := m["target"]
		require.False(t, hasTarget)
		_, hasKind := m["kind"]
		require.False(t, hasKind)
	})
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

func TestAllTools_ThirtyFourEntries(t *testing.T) {
	assert.Len(t, AllTools(), 34)
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

// TestQueryDescribe_ModeEnumMatchesBackend verifies the query/describe mode
// enums only advertise modes the tatara-memory /queries backend accepts. The
// backend rejects anything outside {local,global,hybrid,naive} with a 400
// "invalid mode", so advertising mix/bypass made the tool unusable for those.
func TestQueryDescribe_ModeEnumMatchesBackend(t *testing.T) {
	for _, name := range []string{"query", "describe"} {
		schema := string(toolByName(t, name).Schema)
		for _, valid := range []string{"local", "global", "hybrid", "naive"} {
			require.Contains(t, schema, `"`+valid+`"`, "%s schema must advertise %q", name, valid)
		}
		for _, invalid := range []string{"mix", "bypass"} {
			require.NotContains(t, schema, `"`+invalid+`"`, "%s schema must not advertise backend-rejected mode %q", name, invalid)
		}
	}
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

// TestAllTools_Count verifies the tool registry grows to 34 after Phase 2 additions.
func TestAllTools_Count(t *testing.T) {
	assert.Len(t, AllTools(), 34)
}

func TestNewCodeGraphTools_BuildQueries(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]any
		path string
		q    map[string]string
	}{
		{
			"code_path",
			map[string]any{"from": "a", "to": "b", "relations": "calls,imports", "max_depth": float64(5)},
			"/code-graph/path",
			map[string]string{"from": "a", "to": "b", "relations": "calls,imports", "max_depth": "5"},
		},
		{
			"code_path",
			map[string]any{"from": "a", "to": "b"},
			"/code-graph/path",
			map[string]string{"from": "a", "to": "b"},
		},
		{
			"code_path",
			map[string]any{"repo": "myrepo", "from": "a", "to": "b"},
			"/code-graph/path",
			map[string]string{"repo": "myrepo", "from": "a", "to": "b"},
		},
		{
			"code_important",
			map[string]any{"repo": "r", "limit": float64(20)},
			"/code-graph/important",
			map[string]string{"repo": "r", "limit": "20"},
		},
		{
			"code_important",
			map[string]any{},
			"/code-graph/important",
			map[string]string{},
		},
		{
			"code_stats",
			map[string]any{"repo": "r"},
			"/code-graph/stats",
			map[string]string{"repo": "r"},
		},
		{
			"code_stats",
			map[string]any{},
			"/code-graph/stats",
			map[string]string{},
		},
		{
			"code_ambiguous_edges",
			map[string]any{"repo": "r", "limit": float64(10)},
			"/code-graph/ambiguous",
			map[string]string{"repo": "r", "limit": "10"},
		},
		{
			"code_explain",
			map[string]any{"id": "go:func:m.F", "repo": "r"},
			"/code-graph/explain",
			map[string]string{"id": "go:func:m.F", "repo": "r"},
		},
		{
			"code_explain",
			map[string]any{"id": "go:func:m.F"},
			"/code-graph/explain",
			map[string]string{"id": "go:func:m.F"},
		},
	}
	for i, c := range cases {
		c := c
		t.Run(fmt.Sprintf("%s/%d", c.tool, i), func(t *testing.T) {
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
				require.Equal(t, v, gotQuery.Get(k), "param %s", k)
			}
		})
	}
}

func TestNewCodeGraphTools_RequireArgs(t *testing.T) {
	_, _, _, err := toolByName(t, "code_path").Build(map[string]any{"from": "a"})
	require.Error(t, err) // to required
	_, _, _, err = toolByName(t, "code_path").Build(map[string]any{"to": "b"})
	require.Error(t, err) // from required
	_, _, _, err = toolByName(t, "code_explain").Build(map[string]any{})
	require.Error(t, err) // id required
}

func TestConfidenceParams_ForwardedAsQueryParams(t *testing.T) {
	tools := []string{"code_neighbors", "code_callers", "code_callees", "code_dependencies", "code_dependents"}
	for _, name := range tools {
		name := name
		t.Run(name, func(t *testing.T) {
			var gotQuery url.Values
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotQuery = r.URL.Query()
				_, _ = w.Write([]byte(`[]`))
			}))
			defer srv.Close()
			cli := freshClient(t, srv.URL)
			args := map[string]any{
				"repo":           "r",
				"id":             "x",
				"min_confidence": float64(0.8),
				"tier":           "HIGH",
			}
			if name == "code_neighbors" {
				args["relation"] = "calls"
			}
			_, err := Invoke(context.Background(), cli, toolByName(t, name), args)
			require.NoError(t, err)
			require.Equal(t, "0.8", gotQuery.Get("min_confidence"), "tool %s: min_confidence", name)
			require.Equal(t, "HIGH", gotQuery.Get("tier"), "tool %s: tier", name)
		})
	}
}

func TestCodeRelated_BuildQuery(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		q    map[string]string
	}{
		{
			"all params",
			map[string]any{"id": "go:func:m.F", "relations": "conceptually_related_to,cites", "min_confidence": float64(0.5), "repo": "r"},
			map[string]string{"id": "go:func:m.F", "relations": "conceptually_related_to,cites", "min_confidence": "0.5", "repo": "r"},
		},
		{
			"id only",
			map[string]any{"id": "go:func:m.F"},
			map[string]string{"id": "go:func:m.F"},
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			var gotPath string
			var gotQuery url.Values
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotQuery = r.URL.Query()
				_, _ = w.Write([]byte(`[]`))
			}))
			defer srv.Close()
			cli := freshClient(t, srv.URL)
			_, err := Invoke(context.Background(), cli, toolByName(t, "code_related"), c.args)
			require.NoError(t, err)
			require.Equal(t, "/code-graph/related", gotPath)
			for k, v := range c.q {
				require.Equal(t, v, gotQuery.Get(k), "param %s", k)
			}
		})
	}
}

func TestCodeRelated_RequireID(t *testing.T) {
	_, _, _, err := toolByName(t, "code_related").Build(map[string]any{"repo": "r"})
	require.Error(t, err) // id required
}

func TestCodeHyperedges_BuildQuery(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		q    map[string]string
	}{
		{"entity and repo", map[string]any{"entity": "go:func:m.F", "repo": "r"}, map[string]string{"entity": "go:func:m.F", "repo": "r"}},
		{"no args", map[string]any{}, map[string]string{}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			var gotPath string
			var gotQuery url.Values
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotQuery = r.URL.Query()
				_, _ = w.Write([]byte(`[]`))
			}))
			defer srv.Close()
			cli := freshClient(t, srv.URL)
			_, err := Invoke(context.Background(), cli, toolByName(t, "code_hyperedges"), c.args)
			require.NoError(t, err)
			require.Equal(t, "/code-graph/hyperedges", gotPath)
			for k, v := range c.q {
				require.Equal(t, v, gotQuery.Get(k), "param %s", k)
			}
		})
	}
}

func TestCodeHyperedge_BuildQuery(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	cli := freshClient(t, srv.URL)
	_, err := Invoke(context.Background(), cli, toolByName(t, "code_hyperedge"), map[string]any{"id": "he:r:file:label", "repo": "r"})
	require.NoError(t, err)
	require.Equal(t, "/code-graph/hyperedge", gotPath)
	require.Equal(t, "he:r:file:label", gotQuery.Get("id"))
	require.Equal(t, "r", gotQuery.Get("repo"))
}

func TestCodeHyperedge_RequireID(t *testing.T) {
	_, _, _, err := toolByName(t, "code_hyperedge").Build(map[string]any{"repo": "r"})
	require.Error(t, err) // id required
}

func TestCodeCommunities_BuildQuery(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		q    map[string]string
	}{
		{"repo", map[string]any{"repo": "r"}, map[string]string{"repo": "r"}},
		{"no args", map[string]any{}, map[string]string{}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			var gotPath string
			var gotQuery url.Values
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotQuery = r.URL.Query()
				_, _ = w.Write([]byte(`[]`))
			}))
			defer srv.Close()
			cli := freshClient(t, srv.URL)
			_, err := Invoke(context.Background(), cli, toolByName(t, "code_communities"), c.args)
			require.NoError(t, err)
			require.Equal(t, "/code-graph/communities", gotPath)
			for k, v := range c.q {
				require.Equal(t, v, gotQuery.Get(k), "param %s", k)
			}
		})
	}
}

func TestCodeCommunity_BuildQuery(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	cli := freshClient(t, srv.URL)
	_, err := Invoke(context.Background(), cli, toolByName(t, "code_community"), map[string]any{"repo": "r", "community": float64(3)})
	require.NoError(t, err)
	require.Equal(t, "/code-graph/community", gotPath)
	require.Equal(t, "r", gotQuery.Get("repo"))
	require.Equal(t, "3", gotQuery.Get("community"))
}

func TestCodeCommunity_RequireArgs(t *testing.T) {
	_, _, _, err := toolByName(t, "code_community").Build(map[string]any{"community": float64(1)})
	require.Error(t, err) // repo required
	_, _, _, err = toolByName(t, "code_community").Build(map[string]any{"repo": "r"})
	require.Error(t, err) // community required
}

func TestCodeBridges_BuildQuery(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		q    map[string]string
	}{
		{"repo and limit", map[string]any{"repo": "r", "limit": float64(5)}, map[string]string{"repo": "r", "limit": "5"}},
		{"no args", map[string]any{}, map[string]string{}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			var gotPath string
			var gotQuery url.Values
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotQuery = r.URL.Query()
				_, _ = w.Write([]byte(`[]`))
			}))
			defer srv.Close()
			cli := freshClient(t, srv.URL)
			_, err := Invoke(context.Background(), cli, toolByName(t, "code_bridges"), c.args)
			require.NoError(t, err)
			require.Equal(t, "/code-graph/bridges", gotPath)
			for k, v := range c.q {
				require.Equal(t, v, gotQuery.Get(k), "param %s", k)
			}
		})
	}
}

func TestCodeImportant_ByParam(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		q    map[string]string
	}{
		{"by betweenness", map[string]any{"repo": "r", "by": "betweenness", "limit": float64(10)}, map[string]string{"repo": "r", "by": "betweenness", "limit": "10"}},
		{"by degree", map[string]any{"by": "degree"}, map[string]string{"by": "degree"}},
		{"no by", map[string]any{"repo": "r"}, map[string]string{"repo": "r"}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			var gotPath string
			var gotQuery url.Values
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotQuery = r.URL.Query()
				_, _ = w.Write([]byte(`[]`))
			}))
			defer srv.Close()
			cli := freshClient(t, srv.URL)
			_, err := Invoke(context.Background(), cli, toolByName(t, "code_important"), c.args)
			require.NoError(t, err)
			require.Equal(t, "/code-graph/important", gotPath)
			for k, v := range c.q {
				require.Equal(t, v, gotQuery.Get(k), "param %s", k)
			}
		})
	}
}

func TestCodeImportant_NoByParam_NotForwarded(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	cli := freshClient(t, srv.URL)
	_, err := Invoke(context.Background(), cli, toolByName(t, "code_important"), map[string]any{"repo": "r"})
	require.NoError(t, err)
	require.False(t, gotQuery.Has("by"), "by must not be forwarded when absent")
}

func TestOperatorTools_SCMBuildPaths(t *testing.T) {
	cases := []struct {
		tool   string
		args   map[string]any
		method string
		path   string
	}{
		{"propose_issue", map[string]any{"project": "alpha", "repositoryRef": "szymonrychu/tatara", "title": "t", "body": "b", "kind": "bug"}, http.MethodPost, "/projects/alpha/issues"},
		{"propose_issue", map[string]any{"project": "alpha", "repo": "szymonrychu/tatara", "title": "t", "body": "b", "kind": "improvement"}, http.MethodPost, "/projects/alpha/issues"},
		{"review_verdict", map[string]any{"task": "t1", "decision": "approve"}, http.MethodPost, "/tasks/t1/review"},
		{"pr_outcome", map[string]any{"task": "t1", "action": "merge"}, http.MethodPost, "/tasks/t1/pr-outcome"},
		{"issue_outcome", map[string]any{"task": "t1", "action": "implement"}, http.MethodPost, "/tasks/t1/issue-outcome"},
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

func TestOperatorTools_SCMRequireArgs(t *testing.T) {
	// Hermetic: clear ambient TATARA_TASK / TATARA_PROJECT so the missing-arg
	// error paths are exercised regardless of the runtime environment.
	t.Setenv("TATARA_TASK", "")
	t.Setenv("TATARA_PROJECT", "")
	_, _, _, err := operatorToolByName(t, "propose_issue").Build(map[string]any{"repo": "r", "title": "t", "body": "b", "kind": "bug"})
	require.Error(t, err) // project required (no env set)
	_, _, _, err = operatorToolByName(t, "propose_issue").Build(map[string]any{"project": "p", "title": "t", "body": "b", "kind": "bug"})
	require.Error(t, err) // repo required
	_, _, _, err = operatorToolByName(t, "propose_issue").Build(map[string]any{"project": "p", "repo": "r", "body": "b", "kind": "bug"})
	require.Error(t, err) // title required
	_, _, _, err = operatorToolByName(t, "propose_issue").Build(map[string]any{"project": "p", "repo": "r", "title": "t", "kind": "bug"})
	require.Error(t, err) // body required
	_, _, _, err = operatorToolByName(t, "propose_issue").Build(map[string]any{"project": "p", "repo": "r", "title": "t", "body": "b"})
	require.Error(t, err) // kind required
	_, _, _, err = operatorToolByName(t, "review_verdict").Build(map[string]any{"decision": "approve"})
	require.Error(t, err) // task required (no env set)
	_, _, _, err = operatorToolByName(t, "review_verdict").Build(map[string]any{"task": "t1"})
	require.Error(t, err) // decision required
	_, _, _, err = operatorToolByName(t, "pr_outcome").Build(map[string]any{"action": "merge"})
	require.Error(t, err) // task required (no env set)
	_, _, _, err = operatorToolByName(t, "pr_outcome").Build(map[string]any{"task": "t1"})
	require.Error(t, err) // action required
	_, _, _, err = operatorToolByName(t, "issue_outcome").Build(map[string]any{"action": "implement"})
	require.Error(t, err) // task required (no env set)
	_, _, _, err = operatorToolByName(t, "issue_outcome").Build(map[string]any{"task": "t1"})
	require.Error(t, err) // action required
}

func TestOperatorTools_SCMEnvFallback(t *testing.T) {
	t.Setenv("TATARA_TASK", "task-from-env")
	t.Setenv("TATARA_PROJECT", "proj-from-env")
	cases := []struct {
		tool string
		args map[string]any
		path string
	}{
		{"propose_issue", map[string]any{"repo": "r", "title": "t", "body": "b", "kind": "bug"}, "/projects/proj-from-env/issues"},
		{"review_verdict", map[string]any{"decision": "comment"}, "/tasks/task-from-env/review"},
		{"pr_outcome", map[string]any{"action": "merge"}, "/tasks/task-from-env/pr-outcome"},
		{"issue_outcome", map[string]any{"action": "close"}, "/tasks/task-from-env/issue-outcome"},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			_, p, _, err := operatorToolByName(t, c.tool).Build(c.args)
			require.NoError(t, err)
			require.Equal(t, c.path, p)
		})
	}
}

func TestOperatorTools_SCMBodies(t *testing.T) {
	t.Run("propose_issue", func(t *testing.T) {
		_, _, body, err := operatorToolByName(t, "propose_issue").Build(map[string]any{
			"project": "alpha", "repo": "szymonrychu/tatara", "title": "t", "body": "b", "kind": "bug",
		})
		require.NoError(t, err)
		m := body.(map[string]any)
		require.Equal(t, "szymonrychu/tatara", m["repositoryRef"])
		require.Equal(t, "t", m["title"])
		require.Equal(t, "b", m["body"])
		require.Equal(t, "bug", m["kind"])
		_, hasRepo := m["repo"]
		require.False(t, hasRepo, "body must use repositoryRef, not repo")
	})
	t.Run("review_verdict", func(t *testing.T) {
		sugg := []any{map[string]any{"path": "a.go", "line": float64(12), "body": "fix"}}
		_, _, body, err := operatorToolByName(t, "review_verdict").Build(map[string]any{
			"task": "t1", "decision": "request_changes", "body": "no", "suggestions": sugg,
		})
		require.NoError(t, err)
		m := body.(map[string]any)
		require.Equal(t, "request_changes", m["decision"])
		require.Equal(t, "no", m["body"])
		require.Equal(t, sugg, m["suggestions"])
	})
	t.Run("review_verdict_decision_only", func(t *testing.T) {
		_, _, body, err := operatorToolByName(t, "review_verdict").Build(map[string]any{
			"task": "t1", "decision": "comment",
		})
		require.NoError(t, err)
		m := body.(map[string]any)
		require.Equal(t, "comment", m["decision"])
		_, hasBody := m["body"]
		require.False(t, hasBody)
		_, hasSugg := m["suggestions"]
		require.False(t, hasSugg)
	})
	t.Run("pr_outcome", func(t *testing.T) {
		_, _, body, err := operatorToolByName(t, "pr_outcome").Build(map[string]any{
			"task": "t1", "action": "close", "reason": "stale",
		})
		require.NoError(t, err)
		m := body.(map[string]any)
		require.Equal(t, "close", m["action"])
		require.Equal(t, "stale", m["reason"])
	})
	t.Run("pr_outcome_action_only", func(t *testing.T) {
		_, _, body, err := operatorToolByName(t, "pr_outcome").Build(map[string]any{
			"task": "t1", "action": "merge",
		})
		require.NoError(t, err)
		m := body.(map[string]any)
		require.Equal(t, "merge", m["action"])
		_, hasReason := m["reason"]
		require.False(t, hasReason)
	})
	t.Run("issue_outcome_close", func(t *testing.T) {
		_, _, body, err := operatorToolByName(t, "issue_outcome").Build(map[string]any{
			"task": "t1", "action": "close", "comment": "out of scope",
		})
		require.NoError(t, err)
		m := body.(map[string]any)
		require.Equal(t, "close", m["action"])
		require.Equal(t, "out of scope", m["comment"])
	})
	t.Run("issue_outcome_action_only", func(t *testing.T) {
		_, _, body, err := operatorToolByName(t, "issue_outcome").Build(map[string]any{
			"task": "t1", "action": "implement",
		})
		require.NoError(t, err)
		m := body.(map[string]any)
		require.Equal(t, "implement", m["action"])
		_, hasComment := m["comment"]
		require.False(t, hasComment)
		_, hasPlan := m["plan"]
		require.False(t, hasPlan)
	})
	t.Run("issue_outcome_implement_with_plan", func(t *testing.T) {
		_, _, body, err := operatorToolByName(t, "issue_outcome").Build(map[string]any{
			"task": "t1", "action": "implement", "plan": "add field, wire handler, test",
		})
		require.NoError(t, err)
		m := body.(map[string]any)
		require.Equal(t, "implement", m["action"])
		require.Equal(t, "add field, wire handler, test", m["plan"])
	})
	t.Run("issue_outcome_discuss", func(t *testing.T) {
		_, _, body, err := operatorToolByName(t, "issue_outcome").Build(map[string]any{
			"task": "t1", "action": "discuss", "comment": "need more info",
		})
		require.NoError(t, err)
		m := body.(map[string]any)
		require.Equal(t, "discuss", m["action"])
		require.Equal(t, "need more info", m["comment"])
	})
}

func TestIssueOutcome_SchemaContainsDiscuss(t *testing.T) {
	tl := operatorToolByName(t, "issue_outcome")
	require.Contains(t, string(tl.Schema), `"discuss"`, "schema enum must include discuss")
}

func TestSubmitHandover_Build(t *testing.T) {
	t.Run("explicit task", func(t *testing.T) {
		m, p, body, err := operatorToolByName(t, "submit_handover").Build(map[string]any{
			"task": "t1", "handover": "all done, next steps: ...",
		})
		require.NoError(t, err)
		require.Equal(t, http.MethodPost, m)
		require.Equal(t, "/tasks/t1/handover", p)
		bm := body.(map[string]any)
		require.Equal(t, "all done, next steps: ...", bm["handover"])
	})
	t.Run("env fallback", func(t *testing.T) {
		t.Setenv("TATARA_TASK", "env-task")
		_, p, _, err := operatorToolByName(t, "submit_handover").Build(map[string]any{
			"handover": "ctx",
		})
		require.NoError(t, err)
		require.Equal(t, "/tasks/env-task/handover", p)
	})
	t.Run("require handover", func(t *testing.T) {
		t.Setenv("TATARA_TASK", "t1")
		_, _, _, err := operatorToolByName(t, "submit_handover").Build(map[string]any{})
		require.Error(t, err)
	})
	t.Run("require task", func(t *testing.T) {
		t.Setenv("TATARA_TASK", "")
		_, _, _, err := operatorToolByName(t, "submit_handover").Build(map[string]any{"handover": "x"})
		require.Error(t, err)
	})
}

func TestChangeSummary_Build(t *testing.T) {
	t.Run("all fields", func(t *testing.T) {
		m, p, body, err := operatorToolByName(t, "change_summary").Build(map[string]any{
			"task":             "t1",
			"pr_title":         "feat: add thing",
			"pr_body":          "## Summary\n- added thing",
			"delivered_scope":  "thing done",
			"remaining_scope":  "cleanup later",
			"most_problematic": "token refresh race",
		})
		require.NoError(t, err)
		require.Equal(t, http.MethodPost, m)
		require.Equal(t, "/tasks/t1/change-summary", p)
		bm := body.(map[string]any)
		// Body keys are the operator's camelCase changeSummaryReq json tags.
		require.Equal(t, "feat: add thing", bm["prTitle"])
		require.Equal(t, "## Summary\n- added thing", bm["prBody"])
		require.Equal(t, "thing done", bm["deliveredScope"])
		require.Equal(t, "cleanup later", bm["remainingScope"])
		require.Equal(t, "token refresh race", bm["mostProblematic"])
	})
	t.Run("optional remaining_scope and most_problematic absent", func(t *testing.T) {
		_, _, body, err := operatorToolByName(t, "change_summary").Build(map[string]any{
			"task":            "t1",
			"pr_title":        "feat: x",
			"pr_body":         "body",
			"delivered_scope": "done",
		})
		require.NoError(t, err)
		bm := body.(map[string]any)
		_, hasRemaining := bm["remainingScope"]
		require.False(t, hasRemaining)
		_, hasProblematic := bm["mostProblematic"]
		require.False(t, hasProblematic)
	})
	t.Run("env fallback", func(t *testing.T) {
		t.Setenv("TATARA_TASK", "env-task")
		_, p, _, err := operatorToolByName(t, "change_summary").Build(map[string]any{
			"pr_title": "t", "pr_body": "b", "delivered_scope": "d",
		})
		require.NoError(t, err)
		require.Equal(t, "/tasks/env-task/change-summary", p)
	})
	t.Run("require pr_title", func(t *testing.T) {
		t.Setenv("TATARA_TASK", "t1")
		_, _, _, err := operatorToolByName(t, "change_summary").Build(map[string]any{
			"pr_body": "b", "delivered_scope": "d",
		})
		require.Error(t, err)
	})
	t.Run("require pr_body", func(t *testing.T) {
		t.Setenv("TATARA_TASK", "t1")
		_, _, _, err := operatorToolByName(t, "change_summary").Build(map[string]any{
			"pr_title": "t", "delivered_scope": "d",
		})
		require.Error(t, err)
	})
	t.Run("require delivered_scope", func(t *testing.T) {
		t.Setenv("TATARA_TASK", "t1")
		_, _, _, err := operatorToolByName(t, "change_summary").Build(map[string]any{
			"pr_title": "t", "pr_body": "b",
		})
		require.Error(t, err)
	})
	t.Run("require task", func(t *testing.T) {
		t.Setenv("TATARA_TASK", "")
		_, _, _, err := operatorToolByName(t, "change_summary").Build(map[string]any{
			"pr_title": "t", "pr_body": "b", "delivered_scope": "d",
		})
		require.Error(t, err)
	})
}
