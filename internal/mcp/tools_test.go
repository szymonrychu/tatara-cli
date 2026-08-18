package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/auth"
	"github.com/szymonrychu/tatara-cli/internal/client"
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

func TestPlatformTools_MovedToolsBuildPaths(t *testing.T) {
	cases := []struct {
		tool   string
		args   map[string]any
		method string
		path   string
	}{
		{"project_get", map[string]any{"project": "alpha"}, http.MethodGet, "/projects/alpha"},
		{"repo_list", map[string]any{"project": "alpha"}, http.MethodGet, "/projects/alpha/repositories"},
		{"task_list", map[string]any{"project": "alpha"}, http.MethodGet, "/projects/alpha/tasks"},
		{"task_get", map[string]any{"task": "t1"}, http.MethodGet, "/tasks/t1"},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			m, p, _, err := platformToolByName(t, c.tool).Build(c.args)
			require.NoError(t, err)
			require.Equal(t, c.method, m)
			require.Equal(t, c.path, p)
		})
	}
}

func TestPlatformTools_MovedToolsRequireArgs(t *testing.T) {
	// Hermetic: these assert the no-arg/no-env error path, so clear any ambient
	// TATARA_TASK / TATARA_PROJECT the runtime may have injected.
	t.Setenv("TATARA_TASK", "")
	t.Setenv("TATARA_PROJECT", "")
	_, _, _, err := platformToolByName(t, "project_get").Build(map[string]any{})
	require.Error(t, err) // project required
	_, _, _, err = platformToolByName(t, "task_get").Build(map[string]any{})
	require.Error(t, err) // task required
}

func TestPlatformTools_Invoke(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"t1","state":"Running"}`))
	}))
	defer srv.Close()
	c := freshClient(t, srv.URL)
	body, err := Invoke(context.Background(), c, platformToolByName(t, "task_note"),
		map[string]any{"task": "t1", "kind": "note", "body": "step"})
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "/tasks/t1/notes", gotPath)
	require.Equal(t, "note", gotBody["kind"])
	require.Contains(t, string(body), "state")
}

func TestPlatformTools_MovedToolsEnvFallback(t *testing.T) {
	t.Setenv("TATARA_TASK", "task-from-env")
	t.Setenv("TATARA_PROJECT", "proj-from-env")

	cases := []struct {
		tool string
		args map[string]any
		path string
	}{
		{"task_get", map[string]any{}, "/tasks/task-from-env"},
		{"task_list", map[string]any{}, "/projects/proj-from-env/tasks"},
		{"repo_list", map[string]any{}, "/projects/proj-from-env/repositories"},
		{"project_get", map[string]any{}, "/projects/proj-from-env"},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			_, p, _, err := platformToolByName(t, c.tool).Build(c.args)
			require.NoError(t, err)
			require.Equal(t, c.path, p)
		})
	}
}

func TestPlatformTools_MovedToolExplicitArgOverridesEnv(t *testing.T) {
	t.Setenv("TATARA_TASK", "env-task")
	_, p, _, err := platformToolByName(t, "task_get").Build(map[string]any{"task": "explicit-task"})
	require.NoError(t, err)
	require.Equal(t, "/tasks/explicit-task", p)
}

// TestReapedToolsAreGone checks the tool names that lived directly in the
// (now deleted) OperatorTools() and so could regress silently (a stray
// re-added string entry, unlike the deleted chat- and handoff-tool
// constructors, which no longer exist as functions at all - their removal is
// enforced at compile time, not by a name lookup here). The last 8 are the
// legacy outcome tools that one profile-shaped submit_outcome replaces
// (contract D.1).
func TestReapedToolsAreGone(t *testing.T) {
	reaped := []string{
		"subtask_list", "subtask_create", "subtask_update",
		"harness_state_get", "harness_state_cas",
		"project_list", "create_issue", "pr_outcome", "comment",
		"list_issues", "list_commits", "close_issue", "edit_issue", "comment_on_issue",
		"propose_issue", "review_verdict", "change_summary", "submit_handover",
		"issue_outcome", "decline_implementation", "already_done", "skip_research",
	}
	live := map[string]bool{}
	for _, tl := range allNewTools() {
		live[tl.Name] = true
	}
	for _, name := range reaped {
		require.False(t, live[name], "tool %q must not exist: it is reaped by the task-centric contract", name)
	}
}

func TestServer_ToolCountAfterOutcomeFold(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := NewServer(freshClient(t, "http://memory.invalid"), freshClient(t, "http://operator.invalid"), log, "implement")
	require.Equal(t, 18, s.ToolCount(), "the implement profile registers its allow-set only: the always-on six, its 11 granted tools (10 plus the issue_write it absorbed from the deleted clarify profile), and its submit_outcome (contract D.6)")

	none := NewServer(freshClient(t, "http://memory.invalid"), freshClient(t, "http://operator.invalid"), log, "")
	require.Equal(t, 6, none.ToolCount(), "an empty profile fails closed to the always-on six and gets NO submit_outcome: a pod with no recognised profile must not be able to terminate a Task")
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

// allNewTools is the surface one agent pod sees (contract-final): the 19 static
// tools plus its profile's submit_outcome. The seven submit_outcome variants
// all share one name, so exactly one of them is ever registered.
func allNewTools() []Tool {
	all := append(append(append(CodeTools(), MemoryTools()...), SCMTools()...), PlatformTools()...)
	outcome, ok := OutcomeTool("implement")
	if ok {
		all = append(all, outcome)
	}
	return all
}

func TestAllTools_SchemasAreValidJSON(t *testing.T) {
	for _, tool := range allNewTools() {
		var v any
		err := json.Unmarshal(tool.Schema, &v)
		assert.NoError(t, err, "tool %q has invalid JSON schema", tool.Name)
	}
}

func TestAllTools_NamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, tool := range allNewTools() {
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
	tool := toolByName(t, MemoryTools(), "memory_write")
	args := map[string]any{"text": "hello world"}
	body, err := Invoke(context.Background(), c, tool, args)
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/memories", gotPath)
	assert.Equal(t, "hello world", gotBody["text"])
	assert.Contains(t, string(body), "track_id")
}

func TestInvoke_MemoryEntity_Search_WithQ(t *testing.T) {
	var gotRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := freshClient(t, srv.URL)
	tool := toolByName(t, MemoryTools(), "memory_entity")
	_, err := Invoke(context.Background(), c, tool, map[string]any{"op": "search", "q": "foo"})
	require.NoError(t, err)
	assert.Equal(t, "q=foo", gotRawQuery)
}

func TestInvoke_MemoryEdges_Delete_PassesOpaqueIDThrough(t *testing.T) {
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
	tool := toolByName(t, MemoryTools(), "memory_edges")
	_, err := Invoke(context.Background(), c, tool, map[string]any{"op": "delete", "id": opaqueID})
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
	tool := toolByName(t, MemoryTools(), "memory_write")
	_, err := Invoke(context.Background(), c, tool, map[string]any{"text": "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestInvoke_MissingRequiredArg(t *testing.T) {
	// Build is called before any HTTP; no server needed.
	tool := toolByName(t, MemoryTools(), "memory_entity")
	_, _, _, err := tool.Build(map[string]any{"op": "get"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `missing required argument "id"`)
}

// toolByName is a test helper that retrieves a tool from a tool slice by name.
func toolByName(t *testing.T, tools []Tool, name string) Tool {
	t.Helper()
	for _, tool := range tools {
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
	for _, name := range []string{"memory_query", "memory_describe"} {
		schema := string(toolByName(t, MemoryTools(), name).Schema)
		for _, valid := range []string{"local", "global", "hybrid", "naive"} {
			require.Contains(t, schema, `"`+valid+`"`, "%s schema must advertise %q", name, valid)
		}
		for _, invalid := range []string{"mix", "bypass"} {
			require.NotContains(t, schema, `"`+invalid+`"`, "%s schema must not advertise backend-rejected mode %q", name, invalid)
		}
	}
}

// TestMemoryEdges_DeleteOpaqueIDInPath verifies that an opaque base64url ID is
// placed verbatim in the URL path (url.PathEscape does not alter base64url
// characters).
func TestMemoryEdges_DeleteOpaqueIDInPath(t *testing.T) {
	tool := toolByName(t, MemoryTools(), "memory_edges")
	_, path, _, err := tool.Build(map[string]any{"op": "delete", "id": "YQBi"})
	require.NoError(t, err)
	assert.Equal(t, "/edges/YQBi", path)
}

// TestMemoryEntity_Search_RequiresQ verifies op=search fails fast without q,
// rather than silently hitting an unfiltered /entities.
func TestMemoryEntity_Search_RequiresQ(t *testing.T) {
	tool := toolByName(t, MemoryTools(), "memory_entity")
	_, _, _, err := tool.Build(map[string]any{"op": "search"})
	require.Error(t, err)
}

// Finding 3: memory_entity op=patch must validate that patch is present before building the request.
func TestMemoryEntity_Patch_RequiresPatch(t *testing.T) {
	_, _, _, err := toolByName(t, MemoryTools(), "memory_entity").Build(map[string]any{"op": "patch", "id": "ent-1"})
	require.Error(t, err, "memory_entity op=patch must return error when patch is omitted")
	require.Contains(t, err.Error(), "patch required")
}

func TestMemoryEntity_Patch_NilPatchRejected(t *testing.T) {
	_, _, _, err := toolByName(t, MemoryTools(), "memory_entity").Build(map[string]any{"op": "patch", "id": "ent-1", "patch": nil})
	require.Error(t, err, "memory_entity op=patch must reject nil patch value")
}

// Finding 5: passthrough tools must set additionalProperties:false so unknown keys are
// rejected before they are forwarded to backends that use DisallowUnknownFields.
func TestPassthroughTools_AdditionalPropertiesFalse(t *testing.T) {
	passthroughTools := []string{"memory_write", "memory_query", "memory_describe", "memory_entity", "memory_edges"}
	for _, name := range passthroughTools {
		name := name
		t.Run(name, func(t *testing.T) {
			tl := toolByName(t, MemoryTools(), name)
			var schema map[string]any
			require.NoError(t, json.Unmarshal(tl.Schema, &schema))
			v, ok := schema["additionalProperties"]
			require.True(t, ok, "tool %s schema must have additionalProperties", name)
			require.Equal(t, false, v, "tool %s schema must have additionalProperties:false", name)
		})
	}
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
	tool := toolByName(t, MemoryTools(), "memory_write")
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
			tool := toolByName(t, MemoryTools(), "memory_write")
			_, err := Invoke(context.Background(), c, tool, map[string]any{"text": "x"})
			require.Error(t, err)
			// Must not contain the raw body (which could include a token).
			assert.NotContains(t, err.Error(), "secret.token", "auth error must not echo raw backend body")
			assert.Contains(t, err.Error(), "authentication/authorization failed")
		})
	}
}

// TestInvoke_HeadMovedIsNotAToolError verifies the operator's 409
// reason=="head-moved" body is rendered as a normal (non-error) tool result
// carrying the guidance message and liveSHA, since the operator already
// refreshed the mirror and the agent just needs to re-review and resubmit.
func TestInvoke_HeadMovedIsNotAToolError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"reason":"head-moved","repo":"org/repo","number":123,"reviewedSHA":"aaa","liveSHA":"bbb","mirrorRefreshed":true,"message":"The head of PR 123 moved. Re-sync your workspace (git fetch && git checkout bbb), re-review the new diff, and submit again."}`))
	}))
	defer srv.Close()
	c := freshClient(t, srv.URL)
	tool := toolByName(t, MemoryTools(), "memory_write")
	body, err := Invoke(context.Background(), c, tool, map[string]any{"text": "x"})
	require.NoError(t, err, "head-moved 409 must not surface as a tool error")
	assert.Contains(t, string(body), "Re-sync your workspace")
	assert.Contains(t, string(body), "bbb", "guidance must carry the liveSHA")
}

// TestInvoke_UnknownReasonConflictStaysAToolError verifies a 409 whose reason
// is not one of the closed list of recognised refusals (currently head-moved
// and pr-not-ready) is unaffected by the guidance carve-out and still surfaces
// as a tool error. This is the closed-list pin: refusalGuidance is a lookup
// table, not a catch-all, and an unrecognised reason must not silently start
// swallowing tool errors.
func TestInvoke_UnknownReasonConflictStaysAToolError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"reason":"some-other-reason","error":"some other conflict"}`))
	}))
	defer srv.Close()
	c := freshClient(t, srv.URL)
	tool := toolByName(t, MemoryTools(), "memory_write")
	body, err := Invoke(context.Background(), c, tool, map[string]any{"text": "x"})
	require.Error(t, err)
	assert.Nil(t, body)
	assert.Contains(t, err.Error(), "409")
}

// TestInvoke_GenericConflictWithNoReasonStaysAToolError verifies a 409 body
// carrying no "reason" field at all (the shape any non-tatara backend, or an
// older operator, might return) is still a hard tool error rather than being
// treated as a recognised, guidance-worthy refusal.
func TestInvoke_GenericConflictWithNoReasonStaysAToolError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"some other conflict"}`))
	}))
	defer srv.Close()
	c := freshClient(t, srv.URL)
	tool := toolByName(t, MemoryTools(), "memory_write")
	body, err := Invoke(context.Background(), c, tool, map[string]any{"text": "x"})
	require.Error(t, err)
	assert.Nil(t, body)
	assert.Contains(t, err.Error(), "409")
}

// TestInvoke_PRNotReadyRendersAsGuidanceNotAToolError verifies the operator's
// 409 reason=="pr-not-ready" body (internal/restapi/readiness.go:
// prNotReadyResponse) is rendered as a normal (non-error) tool result carrying
// the message plus one line per blocked MR naming repo, number and blockers -
// the operator's readiness.go:87-92 comment has claimed since pr-not-ready
// existed that tatara-cli does this; it did not, until this change.
func TestInvoke_PRNotReadyRendersAsGuidanceNotAToolError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"reason":"pr-not-ready","error":"not ready","message":"This submission was NOT accepted: the merge request(s) it names are not ready to be handed on (ci-red, conflict). Fix every item below, push, and submit again.","blocked":[
			{"repo":"tatara-cli","number":123,"headSHA":"aaa111","blockers":["ci-red"],"detail":"CI is RED at the pushed head aaa111"},
			{"repo":"tatara-operator","number":456,"headSHA":"bbb222","blockers":["conflict"],"detail":"the branch CONFLICTS with its base"}
		]}`))
	}))
	defer srv.Close()
	c := freshClient(t, srv.URL)
	tool := toolByName(t, MemoryTools(), "memory_write")
	body, err := Invoke(context.Background(), c, tool, map[string]any{"text": "x"})
	require.NoError(t, err, "pr-not-ready 409 must not surface as a tool error")
	assert.Contains(t, string(body), "not ready to be handed on")
	assert.Contains(t, string(body), "tatara-cli!123", "guidance must name the blocked repo and number")
	assert.Contains(t, string(body), "ci-red", "guidance must name the blocking axis")
	assert.Contains(t, string(body), "tatara-operator!456")
	assert.Contains(t, string(body), "conflict")
}

// TestInvoke_PRNotReadyGuidanceCoversEachBlockerReason exercises the three
// pr-not-ready blocker axes (ci-red, conflict, unresolved-review) individually
// - the operator's blocker vocabulary (readiness.go blockerCIRed,
// blockerConflict, blockerUnresolvedReview) - and asserts every one renders as
// guidance, not just the head-moved special case that existed before this
// change.
func TestInvoke_PRNotReadyGuidanceCoversEachBlockerReason(t *testing.T) {
	for _, reason := range []string{"ci-red", "conflict", "unresolved-review"} {
		reason := reason
		t.Run(reason, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusConflict)
				_, _ = fmt.Fprintf(w, `{"reason":"pr-not-ready","error":"not ready","message":"not ready","blocked":[{"repo":"r","number":1,"headSHA":"h","blockers":[%q],"detail":"d"}]}`, reason)
			}))
			defer srv.Close()
			c := freshClient(t, srv.URL)
			tool := toolByName(t, MemoryTools(), "memory_write")
			body, err := Invoke(context.Background(), c, tool, map[string]any{"text": "x"})
			require.NoError(t, err, "reason=%s must render as guidance, not a tool error", reason)
			assert.Contains(t, string(body), reason)
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
	tool := toolByName(t, MemoryTools(), "memory_write")
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
	tool := toolByName(t, MemoryTools(), "memory_write")
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
		{"project_get", "project"},
		{"repo_list", "project"},
		{"task_list", "project"},
	}
	for _, tc := range envFallbackTools {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tl := platformToolByName(t, tc.name)
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

// The description is the agent's contract with the tool. This process emits no
// metrics, so promising one is a lie the agent cannot check.
func TestReportInternalIssue_DescriptionPromisesNoMetric(t *testing.T) {
	tl := platformToolByName(t, "report_internal_issue")
	require.NotContains(t, tl.Description, "metric",
		"report_internal_issue emits a log, not a metric: nothing gathers this registry")
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

// TestInvoke_PRNotReadyGuidanceNamesTheJudgedHead pins the head SHA into the
// rendered guidance. The operator judges each blocked MR at its LIVE head and
// puts that head on the wire (readiness.go blockedMR.HeadSHA); without it in
// the render, an agent with several pushes in flight cannot tell which head was
// judged - only the ci-red detail happened to embed one.
func TestInvoke_PRNotReadyGuidanceNamesTheJudgedHead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"reason":"pr-not-ready","error":"not ready","message":"not ready","blocked":[
			{"repo":"tatara-cli","number":123,"headSHA":"aaa111","blockers":["conflict"],"detail":"the branch CONFLICTS with its base"}
		]}`))
	}))
	defer srv.Close()
	c := freshClient(t, srv.URL)
	tool := toolByName(t, MemoryTools(), "memory_write")
	body, err := Invoke(context.Background(), c, tool, map[string]any{"text": "x"})
	require.NoError(t, err)
	assert.Contains(t, string(body), "aaa111", "guidance must name the head the operator judged")
}
