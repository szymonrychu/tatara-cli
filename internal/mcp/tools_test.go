package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/auth"
	"github.com/szymonrychu/tatara-cli/internal/client"
	"github.com/szymonrychu/tatara-cli/internal/obs"
)

// discardLogger returns a *slog.Logger that discards all output. Use for tests
// that exercise validation or metric behaviour but do not assert log records.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// captureHandler is a minimal slog.Handler that records every log record.
type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}
func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h // test-only; attrs not needed
}
func (h *captureHandler) WithGroup(name string) slog.Handler {
	return h // test-only; groups not needed
}
func (h *captureHandler) Records() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	cp := make([]slog.Record, len(h.records))
	copy(cp, h.records)
	return cp
}

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
	require.Len(t, OperatorTools(), 20)
}

func TestDeclineImplementation_Build(t *testing.T) {
	t.Run("explicit task + reason", func(t *testing.T) {
		t.Setenv("TATARA_TASK", "")
		m, p, body, err := operatorToolByName(t, "decline_implementation").Build(map[string]any{
			"task":   "t1",
			"reason": "already implemented in #42",
		})
		require.NoError(t, err)
		require.Equal(t, http.MethodPost, m)
		require.Equal(t, "/tasks/t1/implement-outcome", p)
		bm := body.(map[string]any)
		require.Equal(t, "declined", bm["action"])
		require.Equal(t, "already implemented in #42", bm["reason"])
	})
	t.Run("env fallback", func(t *testing.T) {
		t.Setenv("TATARA_TASK", "env-task")
		_, p, _, err := operatorToolByName(t, "decline_implementation").Build(map[string]any{
			"reason": "out of scope",
		})
		require.NoError(t, err)
		require.Equal(t, "/tasks/env-task/implement-outcome", p)
	})
	t.Run("require reason", func(t *testing.T) {
		t.Setenv("TATARA_TASK", "t1")
		_, _, _, err := operatorToolByName(t, "decline_implementation").Build(map[string]any{})
		require.Error(t, err)
	})
	t.Run("require task", func(t *testing.T) {
		t.Setenv("TATARA_TASK", "")
		_, _, _, err := operatorToolByName(t, "decline_implementation").Build(map[string]any{"reason": "no"})
		require.Error(t, err)
	})
}

func TestDeclineImplementation_RejectsWhitespaceReason(t *testing.T) {
	t.Setenv("TATARA_TASK", "t1")
	_, _, _, err := operatorToolByName(t, "decline_implementation").Build(map[string]any{
		"reason": "   ",
	})
	require.Error(t, err)
}

func TestAlreadyDone_Build(t *testing.T) {
	t.Run("explicit task + reason", func(t *testing.T) {
		t.Setenv("TATARA_TASK", "")
		m, p, body, err := operatorToolByName(t, "already_done").Build(map[string]any{
			"task":   "t1",
			"reason": "the change is already present on the shared branch",
		})
		require.NoError(t, err)
		require.Equal(t, http.MethodPost, m)
		require.Equal(t, "/tasks/t1/implement-outcome", p)
		bm := body.(map[string]any)
		require.Equal(t, "already_done", bm["action"])
		require.Equal(t, "the change is already present on the shared branch", bm["reason"])
	})
	t.Run("env fallback", func(t *testing.T) {
		t.Setenv("TATARA_TASK", "env-task")
		_, p, _, err := operatorToolByName(t, "already_done").Build(map[string]any{
			"reason": "already committed",
		})
		require.NoError(t, err)
		require.Equal(t, "/tasks/env-task/implement-outcome", p)
	})
	t.Run("require reason", func(t *testing.T) {
		t.Setenv("TATARA_TASK", "t1")
		_, _, _, err := operatorToolByName(t, "already_done").Build(map[string]any{})
		require.Error(t, err)
	})
	t.Run("reject whitespace reason", func(t *testing.T) {
		t.Setenv("TATARA_TASK", "t1")
		_, _, _, err := operatorToolByName(t, "already_done").Build(map[string]any{"reason": "  "})
		require.Error(t, err)
	})
	t.Run("require task", func(t *testing.T) {
		t.Setenv("TATARA_TASK", "")
		_, _, _, err := operatorToolByName(t, "already_done").Build(map[string]any{"reason": "x"})
		require.Error(t, err)
	})
}

func TestAllToolsIncludeAlreadyDone(t *testing.T) {
	// The MCP server registers both AllTools() and OperatorTools().
	// already_done lives in OperatorTools(); confirm it is served tokenless.
	found := false
	for _, tl := range OperatorTools() {
		if tl.Name == "already_done" {
			found = true
			require.Equal(t, TargetOperator, tl.Target)
		}
	}
	require.True(t, found, "already_done must be in OperatorTools() so tatara mcp serves it tokenless")
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

func TestAllTools_ThirtyTwoEntries(t *testing.T) {
	// After Part B: code_community + code_hyperedge merged into their list counterparts.
	// 13 memory + 19 code-graph = 32.
	assert.Len(t, AllTools(), 32)
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

// TestAllTools_Count verifies the tool registry is 32 after Part B merges.
func TestAllTools_Count(t *testing.T) {
	assert.Len(t, AllTools(), 32)
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
			map[string]any{"repo": "myrepo", "from": "a", "to": "b", "relations": "calls,imports", "max_depth": float64(5)},
			"/code-graph/path",
			map[string]string{"repo": "myrepo", "from": "a", "to": "b", "relations": "calls,imports", "max_depth": "5"},
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
			"code_stats",
			map[string]any{"repo": "r"},
			"/code-graph/stats",
			map[string]string{"repo": "r"},
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
	_, _, _, err = toolByName(t, "code_path").Build(map[string]any{"from": "a", "to": "b"})
	require.Error(t, err) // repo required (finding #3: server always requires repo)
	_, _, _, err = toolByName(t, "code_explain").Build(map[string]any{})
	require.Error(t, err) // id required
	_, _, _, err = toolByName(t, "code_explain").Build(map[string]any{"id": "x"})
	require.Error(t, err) // repo required (finding #4: server always requires repo)
	_, _, _, err = toolByName(t, "code_important").Build(map[string]any{})
	require.Error(t, err) // repo required
	_, _, _, err = toolByName(t, "code_stats").Build(map[string]any{})
	require.Error(t, err) // repo required
	_, _, _, err = toolByName(t, "code_ambiguous_edges").Build(map[string]any{})
	require.Error(t, err) // repo required
	_, _, _, err = toolByName(t, "code_related").Build(map[string]any{"id": "x"})
	require.Error(t, err) // repo required
	_, _, _, err = toolByName(t, "code_hyperedges").Build(map[string]any{"id": "he:r:f:l"})
	require.Error(t, err) // repo required (merged tool still requires repo)
	_, _, _, err = toolByName(t, "code_communities").Build(map[string]any{})
	require.Error(t, err) // repo required
	_, _, _, err = toolByName(t, "code_bridges").Build(map[string]any{})
	require.Error(t, err) // repo required
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
			"id and repo only",
			map[string]any{"id": "go:func:m.F", "repo": "r"},
			map[string]string{"id": "go:func:m.F", "repo": "r"},
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
	_, _, _, err = toolByName(t, "code_related").Build(map[string]any{"id": "go:func:m.F"})
	require.Error(t, err) // repo required (finding #4)
}

func TestCodeHyperedges_BuildQuery(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		q    map[string]string
	}{
		{"entity and repo", map[string]any{"entity": "go:func:m.F", "repo": "r"}, map[string]string{"entity": "go:func:m.F", "repo": "r"}},
		{"repo only", map[string]any{"repo": "r"}, map[string]string{"repo": "r"}},
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

// TestCodeHyperedges_MergedBehavior verifies the merged code_hyperedges tool
// (Part B): id set -> single hyperedge; id absent -> list (entity optional).
func TestCodeHyperedges_MergedBehavior(t *testing.T) {
	t.Run("id set -> GET /code-graph/hyperedge", func(t *testing.T) {
		var gotPath string
		var gotQuery url.Values
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotQuery = r.URL.Query()
			_, _ = w.Write([]byte(`{}`))
		}))
		defer srv.Close()
		cli := freshClient(t, srv.URL)
		_, err := Invoke(context.Background(), cli, toolByName(t, "code_hyperedges"), map[string]any{"id": "he:r:file:label", "repo": "r"})
		require.NoError(t, err)
		require.Equal(t, "/code-graph/hyperedge", gotPath)
		require.Equal(t, "he:r:file:label", gotQuery.Get("id"))
		require.Equal(t, "r", gotQuery.Get("repo"))
	})
	t.Run("id absent + entity -> GET /code-graph/hyperedges with entity", func(t *testing.T) {
		var gotPath string
		var gotQuery url.Values
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotQuery = r.URL.Query()
			_, _ = w.Write([]byte(`[]`))
		}))
		defer srv.Close()
		cli := freshClient(t, srv.URL)
		_, err := Invoke(context.Background(), cli, toolByName(t, "code_hyperedges"), map[string]any{"entity": "go:func:m.F", "repo": "r"})
		require.NoError(t, err)
		require.Equal(t, "/code-graph/hyperedges", gotPath)
		require.Equal(t, "go:func:m.F", gotQuery.Get("entity"))
	})
	t.Run("id wins over entity", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			_, _ = w.Write([]byte(`{}`))
		}))
		defer srv.Close()
		cli := freshClient(t, srv.URL)
		_, err := Invoke(context.Background(), cli, toolByName(t, "code_hyperedges"), map[string]any{
			"id": "he:r:f:l", "entity": "go:func:m.F", "repo": "r",
		})
		require.NoError(t, err)
		require.Equal(t, "/code-graph/hyperedge", gotPath, "id must win over entity")
	})
	t.Run("repo required", func(t *testing.T) {
		_, _, _, err := toolByName(t, "code_hyperedges").Build(map[string]any{"id": "he:r:f:l"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "repo required")
	})
	t.Run("repo only -> list", func(t *testing.T) {
		_, p, _, err := toolByName(t, "code_hyperedges").Build(map[string]any{"repo": "r"})
		require.NoError(t, err)
		require.Contains(t, p, "/code-graph/hyperedges")
	})
}

func TestCodeCommunities_BuildQuery(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		q    map[string]string
	}{
		{"repo", map[string]any{"repo": "r"}, map[string]string{"repo": "r"}},
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

// TestCodeCommunities_MergedBehavior verifies the merged code_communities tool
// (Part B): community set -> list members; community absent -> list all communities.
func TestCodeCommunities_MergedBehavior(t *testing.T) {
	t.Run("community set -> GET /code-graph/community", func(t *testing.T) {
		var gotPath string
		var gotQuery url.Values
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotQuery = r.URL.Query()
			_, _ = w.Write([]byte(`{}`))
		}))
		defer srv.Close()
		cli := freshClient(t, srv.URL)
		_, err := Invoke(context.Background(), cli, toolByName(t, "code_communities"), map[string]any{"repo": "r", "community": float64(3)})
		require.NoError(t, err)
		require.Equal(t, "/code-graph/community", gotPath)
		require.Equal(t, "r", gotQuery.Get("repo"))
		require.Equal(t, "3", gotQuery.Get("community"))
	})
	t.Run("community absent -> GET /code-graph/communities", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			_, _ = w.Write([]byte(`[]`))
		}))
		defer srv.Close()
		cli := freshClient(t, srv.URL)
		_, err := Invoke(context.Background(), cli, toolByName(t, "code_communities"), map[string]any{"repo": "r"})
		require.NoError(t, err)
		require.Equal(t, "/code-graph/communities", gotPath)
	})
	t.Run("repo required", func(t *testing.T) {
		_, _, _, err := toolByName(t, "code_communities").Build(map[string]any{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "repo required")
	})
	t.Run("malformed community key silently falls back to list", func(t *testing.T) {
		// Silent fallback to list on malformed key (RESOLVED Q1).
		// A type that argString returns "" for (e.g. bool) falls back to list.
		// JSON-decoded numbers come as float64; booleans remain bool (unknown to argString).
		_, p, _, err := toolByName(t, "code_communities").Build(map[string]any{"repo": "r", "community": true})
		require.NoError(t, err)
		require.Contains(t, p, "/code-graph/communities", "malformed community must silently fall back to list")
	})
}

func TestCodeBridges_BuildQuery(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		q    map[string]string
	}{
		{"repo and limit", map[string]any{"repo": "r", "limit": float64(5)}, map[string]string{"repo": "r", "limit": "5"}},
		{"repo only", map[string]any{"repo": "r"}, map[string]string{"repo": "r"}},
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
		{"by degree", map[string]any{"repo": "r", "by": "degree"}, map[string]string{"repo": "r", "by": "degree"}},
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
		{"propose_issue", map[string]any{"project": "alpha", "repo": "szymonrychu/tatara", "title": "t", "body": "b", "kind": "bug"}, http.MethodPost, "/projects/alpha/issues"},
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

func TestCommentToolBuildsTaskScopedPost(t *testing.T) {
	t.Setenv("TATARA_TASK", "task-xyz")
	var tool Tool
	for _, x := range OperatorTools() {
		if x.Name == "comment" {
			tool = x
		}
	}
	if tool.Name != "comment" {
		t.Fatal("comment tool not registered in OperatorTools")
	}
	if tool.Target != TargetOperator {
		t.Fatalf("comment tool target = %v, want TargetOperator", tool.Target)
	}
	method, path, body, err := tool.Build(map[string]any{"body": "design note"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if method != http.MethodPost {
		t.Fatalf("method = %s, want POST", method)
	}
	if path != "/tasks/task-xyz/comment" {
		t.Fatalf("path = %s, want /tasks/task-xyz/comment", path)
	}
	m, ok := body.(map[string]any)
	if !ok || m["body"] != "design note" {
		t.Fatalf("body = %#v, want {body: design note}", body)
	}
}

func TestCommentToolRequiresBody(t *testing.T) {
	t.Setenv("TATARA_TASK", "task-xyz")
	var tool Tool
	for _, x := range OperatorTools() {
		if x.Name == "comment" {
			tool = x
		}
	}
	if _, _, _, err := tool.Build(map[string]any{}); err == nil {
		t.Fatal("expected error when body missing")
	}
}

// TestCodeHyperedges_RequireRepo verifies the merged code_hyperedges tool
// still requires repo (server handler calls reqRepo first).
func TestCodeHyperedges_RequireRepo(t *testing.T) {
	_, _, _, err := toolByName(t, "code_hyperedges").Build(map[string]any{"id": "he:r:f:l"})
	require.Error(t, err, "code_hyperedges must return error when repo is omitted")
	require.Contains(t, err.Error(), "repo required")
}

func TestCodeHyperedges_SchemaRepoRequired(t *testing.T) {
	tl := toolByName(t, "code_hyperedges")
	var schema map[string]any
	require.NoError(t, json.Unmarshal(tl.Schema, &schema))
	req, _ := schema["required"].([]any)
	var hasRepo bool
	for _, r := range req {
		if r == "repo" {
			hasRepo = true
		}
	}
	require.True(t, hasRepo, "code_hyperedges schema required must contain repo")
	// id is now optional (discriminator), not required
	var hasID bool
	for _, r := range req {
		if r == "id" {
			hasID = true
		}
	}
	require.False(t, hasID, "code_hyperedges schema must NOT have id in required (it is the optional discriminator)")
}

// Finding 3: patch_entity must validate that patch is present before building the request.
func TestPatchEntity_RequiresPatch(t *testing.T) {
	_, _, _, err := toolByName(t, "patch_entity").Build(map[string]any{"id": "ent-1"})
	require.Error(t, err, "patch_entity must return error when patch is omitted")
	require.Contains(t, err.Error(), "patch required")
}

func TestPatchEntity_NilPatchRejected(t *testing.T) {
	_, _, _, err := toolByName(t, "patch_entity").Build(map[string]any{"id": "ent-1", "patch": nil})
	require.Error(t, err, "patch_entity must reject nil patch value")
}

// Finding 5: passthrough tools must set additionalProperties:false so unknown keys are
// rejected before they are forwarded to backends that use DisallowUnknownFields.
func TestPassthroughTools_AdditionalPropertiesFalse(t *testing.T) {
	passthroughTools := []string{"create_memory", "query", "describe", "bulk_create_memories", "create_edge"}
	for _, name := range passthroughTools {
		name := name
		t.Run(name, func(t *testing.T) {
			tl := toolByName(t, name)
			var schema map[string]any
			require.NoError(t, json.Unmarshal(tl.Schema, &schema))
			v, ok := schema["additionalProperties"]
			require.True(t, ok, "tool %s schema must have additionalProperties", name)
			require.Equal(t, false, v, "tool %s schema must have additionalProperties:false", name)
		})
	}
}

func TestCommentOnIssueTool_Registered(t *testing.T) {
	var found bool
	for _, tl := range OperatorTools() {
		if tl.Name == "comment_on_issue" {
			found = true
		}
	}
	require.True(t, found, "comment_on_issue must be registered in OperatorTools")
}

func TestCommentOnIssueTool_BuildPath(t *testing.T) {
	t.Setenv("TATARA_PROJECT", "")
	m, p, body, err := operatorToolByName(t, "comment_on_issue").Build(map[string]any{
		"project": "myproj",
		"repo":    "szymonrychu/tatara",
		"number":  float64(42),
		"body":    "this duplicates #7",
	})
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, m)
	require.Equal(t, "/projects/myproj/issue-comment", p)
	bm := body.(map[string]any)
	require.Equal(t, "szymonrychu/tatara", bm["repo"])
	require.Equal(t, int64(42), bm["number"])
	require.Equal(t, "this duplicates #7", bm["body"])
}

func TestCommentOnIssueTool_EnvFallback(t *testing.T) {
	t.Setenv("TATARA_PROJECT", "proj-from-env")
	_, p, _, err := operatorToolByName(t, "comment_on_issue").Build(map[string]any{
		"repo":   "szymonrychu/tatara",
		"number": float64(1),
		"body":   "comment",
	})
	require.NoError(t, err)
	require.Equal(t, "/projects/proj-from-env/issue-comment", p)
}

func TestCommentOnIssueTool_RequireArgs(t *testing.T) {
	t.Setenv("TATARA_PROJECT", "")
	// missing project
	_, _, _, err := operatorToolByName(t, "comment_on_issue").Build(map[string]any{
		"repo": "r", "number": float64(1), "body": "b",
	})
	require.Error(t, err)
	// missing repo
	_, _, _, err = operatorToolByName(t, "comment_on_issue").Build(map[string]any{
		"project": "p", "number": float64(1), "body": "b",
	})
	require.Error(t, err)
	// missing number
	_, _, _, err = operatorToolByName(t, "comment_on_issue").Build(map[string]any{
		"project": "p", "repo": "r", "body": "b",
	})
	require.Error(t, err)
	// missing body
	_, _, _, err = operatorToolByName(t, "comment_on_issue").Build(map[string]any{
		"project": "p", "repo": "r", "number": float64(1),
	})
	require.Error(t, err)
}

func TestAllOperatorTools_CountAfterCommentOnIssue(t *testing.T) {
	require.Len(t, OperatorTools(), 20)
}

// Finding r3-1: bulk_create_memories schema must expose repo, reconcile_files, and
// items[].idempotency_key so CLI-driven callers can do reconcile-aware, idempotent ingests.
func TestBulkCreateMemories_SchemaExposesReconcileFields(t *testing.T) {
	tl := toolByName(t, "bulk_create_memories")
	var schema map[string]any
	require.NoError(t, json.Unmarshal(tl.Schema, &schema))
	props, _ := schema["properties"].(map[string]any)
	_, hasRepo := props["repo"]
	require.True(t, hasRepo, "bulk_create_memories schema must expose repo")
	_, hasReconcile := props["reconcile_files"]
	require.True(t, hasReconcile, "bulk_create_memories schema must expose reconcile_files")
	// items must allow idempotency_key per item
	itemsProp, _ := props["items"].(map[string]any)
	itemSchema, _ := itemsProp["items"].(map[string]any)
	itemProps, _ := itemSchema["properties"].(map[string]any)
	_, hasKey := itemProps["idempotency_key"]
	require.True(t, hasKey, "bulk_create_memories items schema must expose idempotency_key")
}

// Finding r3-2: argString must not have a dead `case int` branch (JSON numbers are float64).
func TestArgString_NoIntCase(t *testing.T) {
	// JSON-decoded numbers always arrive as float64. Verify float64 works and int
	// is never needed (the `case int` branch was dead code).
	a := map[string]any{"n": float64(7)}
	require.Equal(t, "7", argString(a, "n"))
	a2 := map[string]any{"f": float64(3.14)}
	require.Equal(t, "3.14", argString(a2, "f"))
	// string still works
	a3 := map[string]any{"s": "hello"}
	require.Equal(t, "hello", argString(a3, "s"))
	// missing key
	require.Equal(t, "", argString(map[string]any{}, "x"))
}

// Finding r3-5: comment_on_issue must coerce/validate number before dispatch.
// A non-numeric string must be rejected with a clear error.
func TestCommentOnIssue_NumberStringRejected(t *testing.T) {
	t.Setenv("TATARA_PROJECT", "p")
	_, _, _, err := operatorToolByName(t, "comment_on_issue").Build(map[string]any{
		"repo": "r", "number": "not-a-number", "body": "b",
	})
	require.Error(t, err, "non-numeric string number must be rejected with a clear error")
	require.Contains(t, err.Error(), "number", "error must mention number")
}

// Finding r3-5: number=0 must be rejected (operator enforces Number > 0).
func TestCommentOnIssue_NumberZeroRejected(t *testing.T) {
	t.Setenv("TATARA_PROJECT", "p")
	_, _, _, err := operatorToolByName(t, "comment_on_issue").Build(map[string]any{
		"repo": "r", "number": float64(0), "body": "b",
	})
	require.Error(t, err, "zero number must be rejected")
}

// Finding r3-5: number as float64 integer must coerce to int in body.
func TestCommentOnIssue_NumberCoercedToInt(t *testing.T) {
	t.Setenv("TATARA_PROJECT", "p")
	_, _, body, err := operatorToolByName(t, "comment_on_issue").Build(map[string]any{
		"repo": "r", "number": float64(42), "body": "b",
	})
	require.NoError(t, err)
	bm := body.(map[string]any)
	require.Equal(t, int64(42), bm["number"], "number must be coerced to int64 in body")
}

// Finding #1: Invoke must return error when body read fails (not silently truncate).
func TestInvoke_BodyReadErrorSurfaced(t *testing.T) {
	// Server writes a partial response and then abruptly closes without flushing.
	// Use a custom listener that closes the connection mid-body.
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Signal response started but hijack before body is complete.
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("ResponseWriter does not implement Hijacker")
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		// Write a valid 200 header with Content-Length > actual body to force truncation.
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 1000\r\n\r\n{")
		_ = buf.Flush()
		_ = conn.Close()
		close(done)
	}))
	defer srv.Close()

	c := freshClient(t, srv.URL)
	tool := toolByName(t, "create_memory")
	_, err := Invoke(context.Background(), c, tool, map[string]any{"text": "x"})
	<-done
	// The error may come from the HTTP transport (unexpected EOF) wrapping the
	// body read; either way it must not be nil.
	require.Error(t, err, "truncated body read must surface as error")
}

// Finding #7: 401/403 responses must return a generic auth error, not the raw body.
func TestInvoke_AuthErrorGeneric(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		status := status
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"Bearer eyJhbGciOiJSUzI1NiJ9.secret.token"}`))
			}))
			defer srv.Close()
			c := freshClient(t, srv.URL)
			tool := toolByName(t, "create_memory")
			_, err := Invoke(context.Background(), c, tool, map[string]any{"text": "x"})
			require.Error(t, err)
			// Must not contain the raw body (which could include a token).
			assert.NotContains(t, err.Error(), "secret.token", "auth error must not echo raw backend body")
			assert.Contains(t, err.Error(), "authentication/authorization failed")
		})
	}
}

// Finding #7: Body must be capped (not unlimited) on large error responses.
func TestInvoke_ErrorBodyCapped(t *testing.T) {
	large := strings.Repeat("x", 8192)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(large))
	}))
	defer srv.Close()
	c := freshClient(t, srv.URL)
	tool := toolByName(t, "create_memory")
	_, err := Invoke(context.Background(), c, tool, map[string]any{"text": "x"})
	require.Error(t, err)
	// The error string must not contain the full 8 KB body.
	assert.Less(t, len(err.Error()), 5000, "error string must not contain unbounded backend body")
}

// Regression: the body cap must apply ONLY to error responses. A successful
// response larger than errBodyCap (graph queries, memory lists routinely are)
// must be returned in full and remain valid JSON, not truncated to 4096 bytes.
func TestInvoke_SuccessBodyNotTruncated(t *testing.T) {
	// Build a valid JSON payload well over the 4096-byte error cap.
	items := make([]map[string]string, 0, 400)
	for i := 0; i < 400; i++ {
		items = append(items, map[string]string{"id": fmt.Sprintf("entity-%04d", i)})
	}
	payload, err := json.Marshal(map[string]any{"results": items})
	require.NoError(t, err)
	require.Greater(t, len(payload), 4096, "test payload must exceed errBodyCap to be meaningful")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	c := freshClient(t, srv.URL)
	tool := toolByName(t, "create_memory")
	buf, err := Invoke(context.Background(), c, tool, map[string]any{"text": "x"})
	require.NoError(t, err)
	require.Equal(t, payload, buf, "successful body must be returned in full, not truncated")

	// And it must still parse as valid JSON (truncation would break this).
	var out map[string]any
	require.NoError(t, json.Unmarshal(buf, &out), "full success body must be valid JSON")
	results, ok := out["results"].([]any)
	require.True(t, ok)
	require.Len(t, results, 400, "all items must survive (no truncation)")
}

// Finding #5: Operator tool schemas must NOT list task/project in required when env fallback exists.
func TestOperatorTools_EnvFallbackFieldsNotInSchemaRequired(t *testing.T) {
	envFallbackTools := []struct {
		name      string
		fieldName string
	}{
		{"task_get", "task"},
		{"task_update", "task"},
		{"subtask_list", "task"},
		{"subtask_create", "task"},
		{"project_get", "project"},
		{"repo_list", "project"},
		{"task_list", "project"},
	}
	for _, tc := range envFallbackTools {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tl := operatorToolByName(t, tc.name)
			var schema map[string]any
			require.NoError(t, json.Unmarshal(tl.Schema, &schema))
			req, _ := schema["required"].([]any)
			for _, r := range req {
				assert.NotEqual(t, tc.fieldName, r,
					"tool %s: %q must not be in schema required (it has env fallback via %s)",
					tc.name, tc.fieldName, strings.ToUpper("TATARA_"+tc.fieldName))
			}
		})
	}
}

// Finding #6: propose_issue must NOT advertise repositoryRef; only repo is valid.
func TestProposeIssue_NoRepositoryRefInSchema(t *testing.T) {
	tl := operatorToolByName(t, "propose_issue")
	assert.NotContains(t, string(tl.Schema), "repositoryRef",
		"propose_issue schema must not expose repositoryRef; use only repo")
}

// Finding #6: propose_issue must reject repositoryRef-only arg (no repo fallback).
func TestProposeIssue_RepositoryRefArgIgnored(t *testing.T) {
	t.Setenv("TATARA_PROJECT", "p")
	_, _, _, err := operatorToolByName(t, "propose_issue").Build(map[string]any{
		"repositoryRef": "szymonrychu/tatara", "title": "t", "body": "b", "kind": "bug",
	})
	// repositoryRef is no longer a valid input; without repo the build must fail.
	require.Error(t, err, "propose_issue must fail when only repositoryRef is provided (schema no longer advertises it)")
}

func TestProposeIssue_SchemaHasOptionalSystemicID(t *testing.T) {
	tl := operatorToolByName(t, "propose_issue")
	schema := string(tl.Schema)
	require.Contains(t, schema, `"systemicId"`,
		"propose_issue schema must advertise systemicId property")
	require.Contains(t, schema, `"systemicId":{"type":"string"`,
		"systemicId must be a string property")

	// systemicId must NOT be required: decode the schema and inspect the
	// required list so a future reorder of properties cannot make this pass
	// by accident.
	var parsed struct {
		Required []string `json:"required"`
	}
	require.NoError(t, json.Unmarshal(tl.Schema, &parsed))
	require.NotContains(t, parsed.Required, "systemicId",
		"systemicId must stay optional (not in required)")
	// The existing required set is unchanged.
	require.ElementsMatch(t, []string{"title", "body", "kind", "repo"}, parsed.Required)
}

func TestSkipResearchTool(t *testing.T) {
	t.Run("explicit task + reason", func(t *testing.T) {
		t.Setenv("TATARA_TASK", "")
		m, p, body, err := operatorToolByName(t, "skip_research").Build(map[string]any{
			"task":   "t-123",
			"reason": "nothing worth proposing",
		})
		require.NoError(t, err)
		require.Equal(t, http.MethodPost, m)
		require.Equal(t, "/tasks/t-123/brainstorm-outcome", p)
		bm := body.(map[string]any)
		require.Equal(t, "none", bm["action"])
		require.Equal(t, "nothing worth proposing", bm["reason"])
	})
	t.Run("env fallback", func(t *testing.T) {
		t.Setenv("TATARA_TASK", "env-task")
		_, p, _, err := operatorToolByName(t, "skip_research").Build(map[string]any{
			"reason": "scanned all open issues, nothing novel",
		})
		require.NoError(t, err)
		require.Equal(t, "/tasks/env-task/brainstorm-outcome", p)
	})
	t.Run("require task", func(t *testing.T) {
		t.Setenv("TATARA_TASK", "")
		_, _, _, err := operatorToolByName(t, "skip_research").Build(map[string]any{
			"reason": "nothing",
		})
		require.Error(t, err)
	})
}

func TestSkipResearchRequiresReason(t *testing.T) {
	t.Setenv("TATARA_TASK", "t-1")
	_, _, _, err := operatorToolByName(t, "skip_research").Build(map[string]any{"task": "t-1"})
	require.Error(t, err, "expected error when reason missing")
}

func TestSkipResearch_Build(t *testing.T) {
	t.Run("explicit task + reason", func(t *testing.T) {
		t.Setenv("TATARA_TASK", "")
		m, p, body, err := operatorToolByName(t, "skip_research").Build(map[string]any{
			"task":   "t1",
			"reason": "no novel net-new angle this cycle",
		})
		require.NoError(t, err)
		require.Equal(t, http.MethodPost, m)
		require.Equal(t, "/tasks/t1/brainstorm-outcome", p)
		bm := body.(map[string]any)
		require.Equal(t, "none", bm["action"])
		require.Equal(t, "no novel net-new angle this cycle", bm["reason"])
	})
	t.Run("env fallback", func(t *testing.T) {
		t.Setenv("TATARA_TASK", "env-task")
		_, p, _, err := operatorToolByName(t, "skip_research").Build(map[string]any{
			"reason": "already covered by #42",
		})
		require.NoError(t, err)
		require.Equal(t, "/tasks/env-task/brainstorm-outcome", p)
	})
	t.Run("require reason blank", func(t *testing.T) {
		t.Setenv("TATARA_TASK", "t1")
		_, _, _, err := operatorToolByName(t, "skip_research").Build(map[string]any{})
		require.Error(t, err)
	})
	t.Run("require reason whitespace", func(t *testing.T) {
		t.Setenv("TATARA_TASK", "t1")
		_, _, _, err := operatorToolByName(t, "skip_research").Build(map[string]any{"reason": "   "})
		require.Error(t, err)
	})
	t.Run("require task", func(t *testing.T) {
		t.Setenv("TATARA_TASK", "")
		_, _, _, err := operatorToolByName(t, "skip_research").Build(map[string]any{"reason": "x"})
		require.Error(t, err)
	})
}

// --- PlatformTools tests (Part C) ---

func platformToolByName(t *testing.T, name string) Tool {
	t.Helper()
	for _, tl := range PlatformTools() {
		if tl.Name == name {
			return tl
		}
	}
	t.Fatalf("platform tool %q not found", name)
	return Tool{}
}

func TestPlatformTools_ContainsReportInternalIssue(t *testing.T) {
	found := false
	for _, tl := range PlatformTools() {
		if tl.Name == "report_internal_issue" {
			found = true
		}
	}
	require.True(t, found, "PlatformTools() must include report_internal_issue")
}

func TestPlatformTools_Count(t *testing.T) {
	require.Len(t, PlatformTools(), 1)
}

func TestReportInternalIssue_SchemaValid(t *testing.T) {
	tl := platformToolByName(t, "report_internal_issue")
	var schema map[string]any
	require.NoError(t, json.Unmarshal(tl.Schema, &schema))
	props, _ := schema["properties"].(map[string]any)
	_, hasCat := props["category"]
	_, hasSev := props["severity"]
	_, hasDesc := props["description"]
	_, hasOffTool := props["offending_tool"]
	_, hasResID := props["resource_id"]
	require.True(t, hasCat, "schema must have category")
	require.True(t, hasSev, "schema must have severity")
	require.True(t, hasDesc, "schema must have description")
	require.True(t, hasOffTool, "schema must have offending_tool")
	require.True(t, hasResID, "schema must have resource_id")

	req, _ := schema["required"].([]any)
	var hasCatReq, hasDescReq bool
	for _, r := range req {
		if r == "category" {
			hasCatReq = true
		}
		if r == "description" {
			hasDescReq = true
		}
	}
	require.True(t, hasCatReq, "category must be required")
	require.True(t, hasDescReq, "description must be required")
}

func TestReportInternalIssue_HasHandler(t *testing.T) {
	tl := platformToolByName(t, "report_internal_issue")
	require.NotNil(t, tl.Handler, "report_internal_issue must have a Handler (no HTTP round-trip)")
	require.Nil(t, tl.Build, "report_internal_issue must not have a Build func (uses Handler instead)")
}

func TestReportInternalIssue_HandlerValidation(t *testing.T) {
	tl := platformToolByName(t, "report_internal_issue")
	cases := []struct {
		name    string
		args    map[string]any
		wantErr bool
		errMsg  string
	}{
		{"valid tool_error", map[string]any{"category": "tool_error", "description": "something broke"}, false, ""},
		{"valid with severity warn", map[string]any{"category": "auth", "severity": "warn", "description": "token expired"}, false, ""},
		{"valid with optional fields", map[string]any{"category": "graph_inconsistent", "description": "entity missing", "offending_tool": "code_entity", "resource_id": "go:func:m.F"}, false, ""},
		{"empty description", map[string]any{"category": "tool_error", "description": ""}, true, "description"},
		{"whitespace description", map[string]any{"category": "tool_error", "description": "   "}, true, "description"},
		{"unknown category", map[string]any{"category": "invalid_cat", "description": "desc"}, true, "category"},
		{"empty category", map[string]any{"category": "", "description": "desc"}, true, "category"},
		{"unknown severity", map[string]any{"category": "other", "severity": "critical", "description": "desc"}, true, "severity"},
	}
	log := discardLogger()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result, err := tl.Handler(c.args, log)
			if c.wantErr {
				require.Error(t, err, "expected error for case %q", c.name)
				if c.errMsg != "" {
					require.Contains(t, err.Error(), c.errMsg)
				}
			} else {
				require.NoError(t, err, "unexpected error for case %q", c.name)
				require.NotEmpty(t, result)
			}
		})
	}
}

func TestReportInternalIssue_DefaultSeverityIsError(t *testing.T) {
	tl := platformToolByName(t, "report_internal_issue")
	// omit severity -> should default to "error" (no error returned)
	result, err := tl.Handler(map[string]any{
		"category":    "workspace_broken",
		"description": "the workspace is broken",
	}, discardLogger())
	require.NoError(t, err)
	require.Contains(t, result, "severity=error")
}

func TestReportInternalIssue_IncrementMetric(t *testing.T) {
	tl := platformToolByName(t, "report_internal_issue")
	before := testutil.ToFloat64(obs.InternalIssueTotal.With(prometheus.Labels{"category": "other", "severity": "error"}))
	_, err := tl.Handler(map[string]any{
		"category":    "other",
		"description": "test metric increment",
	}, discardLogger())
	require.NoError(t, err)
	after := testutil.ToFloat64(obs.InternalIssueTotal.With(prometheus.Labels{"category": "other", "severity": "error"}))
	require.Equal(t, before+1, after, "InternalIssueTotal must increment on handler call")
}

func TestReportInternalIssue_LogLevel(t *testing.T) {
	tl := platformToolByName(t, "report_internal_issue")

	t.Run("warn severity emits Warn record", func(t *testing.T) {
		h := &captureHandler{}
		log := slog.New(h)
		_, err := tl.Handler(map[string]any{
			"category":    "auth",
			"severity":    "warn",
			"description": "token about to expire",
		}, log)
		require.NoError(t, err)
		recs := h.Records()
		require.Len(t, recs, 1)
		require.Equal(t, slog.LevelWarn, recs[0].Level)
		var foundCat bool
		recs[0].Attrs(func(a slog.Attr) bool {
			if a.Key == "category" && a.Value.String() == "auth" {
				foundCat = true
			}
			return true
		})
		require.True(t, foundCat, "log record must carry category attr")
	})

	t.Run("error severity emits Error record", func(t *testing.T) {
		h := &captureHandler{}
		log := slog.New(h)
		_, err := tl.Handler(map[string]any{
			"category":    "tool_error",
			"severity":    "error",
			"description": "something went wrong",
		}, log)
		require.NoError(t, err)
		recs := h.Records()
		require.Len(t, recs, 1)
		require.Equal(t, slog.LevelError, recs[0].Level)
	})
}

func TestProposeIssue_SystemicIDForwarded(t *testing.T) {
	t.Run("supplied systemicId is forwarded under the systemicId key", func(t *testing.T) {
		_, _, body, err := operatorToolByName(t, "propose_issue").Build(map[string]any{
			"project":    "alpha",
			"repo":       "szymonrychu/tatara",
			"title":      "feat: add platform-wide /metrics endpoint",
			"body":       "b",
			"kind":       "improvement",
			"systemicId": "sys-7f3a",
		})
		require.NoError(t, err)
		m := body.(map[string]any)
		require.Equal(t, "sys-7f3a", m["systemicId"])
		// existing keys are untouched
		require.Equal(t, "szymonrychu/tatara", m["repositoryRef"])
		require.Equal(t, "improvement", m["kind"])
	})

	t.Run("omitted systemicId leaves the payload unchanged", func(t *testing.T) {
		_, _, body, err := operatorToolByName(t, "propose_issue").Build(map[string]any{
			"project": "alpha",
			"repo":    "szymonrychu/tatara",
			"title":   "t",
			"body":    "b",
			"kind":    "bug",
		})
		require.NoError(t, err)
		m := body.(map[string]any)
		_, has := m["systemicId"]
		require.False(t, has, "systemicId must be absent when the agent did not supply it")
	})
}
