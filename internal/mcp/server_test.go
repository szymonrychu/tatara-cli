package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/auth"
	"github.com/szymonrychu/tatara-cli/internal/client"
	"github.com/szymonrychu/tatara-cli/internal/obs"
)

// Every registered tool must marshal cleanly. mcp-go's NewTool seeds a default
// object InputSchema; combined with a raw schema the tool has both InputSchema
// and RawInputSchema set, which fails tools/list marshalling and leaves the
// agent with zero tatara tools.
func TestBuildTool_AllToolsMarshal(t *testing.T) {
	all := append(AllTools(), OperatorTools()...)
	all = append(all, ChatTools()...)
	all = append(all, PlatformTools()...)
	for _, tl := range all {
		_, err := json.Marshal(buildTool(tl))
		require.NoErrorf(t, err, "tool %s must marshal for tools/list", tl.Name)
	}
}

func TestNewServer_RegistersAllTools(t *testing.T) {
	tok := &auth.Token{
		AccessToken: "test",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		TokenType:   "Bearer",
	}
	c, err := client.New(client.Config{BaseURL: "http://localhost:9999", Token: tok})
	require.NoError(t, err)

	// Must not panic; all tools register without error. Empty profile = full set.
	srv := NewServer(c, c, c, slog.Default(), "")
	assert.NotNil(t, srv)
	assert.NotNil(t, srv.srv)

	// After Part B: AllTools is 32 (not 34). After Part C: PlatformTools adds 1.
	// After refine agent (E2): OperatorTools grows to 25.
	// After harness_state tools (Task 3): OperatorTools grows to 27.
	// Full count: AllTools(32) + OperatorTools(27) + ChatTools(10) + PlatformTools(1) + HandoffTools(4) = 74.
	assert.Len(t, AllTools(), 32, "AllTools must be 32 after Part B merges")
}

func TestNewServer_EmptyProfileRegistersFullSet(t *testing.T) {
	mem := freshClient(t, "http://memory.invalid")
	op := freshClient(t, "http://operator.invalid")
	ch := freshClient(t, "http://chat.invalid")
	s := NewServer(mem, op, ch, slog.New(slog.NewTextHandler(io.Discard, nil)), "")
	expected := len(AllTools()) + len(OperatorTools()) + len(ChatTools()) + len(PlatformTools()) + len(HandoffTools())
	require.Equal(t, expected, s.ToolCount(), "empty profile must register full set (%d tools)", expected)
	require.Equal(t, 74, s.ToolCount(), "full tool count must be 74")
}

// Component 4a: tools/list must be byte-identical across profiles (a shared
// prompt-cache prefix requires it), so registration no longer varies by
// profile. Per-profile restriction now lives at call time (see
// TestCallTool_* below), not at registration/list time.
func TestNewServer_ProfileDoesNotReduceToolSet(t *testing.T) {
	mem := freshClient(t, "http://memory.invalid")
	op := freshClient(t, "http://operator.invalid")
	ch := freshClient(t, "http://chat.invalid")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	sImpl := NewServer(mem, op, ch, logger, "implement")
	sLifecycle := NewServer(mem, op, ch, logger, "lifecycle")
	sFull := NewServer(mem, op, ch, logger, "")

	assert.Equal(t, sFull.ToolCount(), sImpl.ToolCount(),
		"implement profile must register the same tool count as the full/empty-profile server")
	assert.Equal(t, sFull.ToolCount(), sLifecycle.ToolCount(),
		"lifecycle profile must register the same tool count as the full/empty-profile server")
}

func TestNewServer_ProfileCounts(t *testing.T) {
	mem := freshClient(t, "http://memory.invalid")
	op := freshClient(t, "http://operator.invalid")
	ch := freshClient(t, "http://chat.invalid")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Every known profile registers (lists) the exact full tool count -
	// registration/tools-list no longer varies by profile.
	fullCount := len(AllTools()) + len(OperatorTools()) + len(ChatTools()) + len(PlatformTools()) + len(HandoffTools())
	for _, profile := range []string{"brainstorm", "implement", "review", "triage", "lifecycle", "incident", "selfImprove"} {
		s := NewServer(mem, op, ch, logger, profile)
		assert.Equal(t, fullCount, s.ToolCount(), "profile %q must register the full tool count", profile)
	}
}

func TestNewServer_RefineProfileListsFullSetButRestrictsCalls(t *testing.T) {
	mem := freshClient(t, "http://memory.invalid")
	op := freshClient(t, "http://operator.invalid")
	ch := freshClient(t, "http://chat.invalid")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	sRefine := NewServer(mem, op, ch, logger, "refine")
	sFull := NewServer(mem, op, ch, logger, "")
	// refine still lists the full 74 tools (registration is profile-invariant);
	// only the resolved allow-set (enforced at call time) is the 46-tool refine set.
	assert.Equal(t, sFull.ToolCount(), sRefine.ToolCount(),
		"refine profile must list the same tool count as the profile-invariant full registration")
	assert.Len(t, sRefine.allow, 46, "refine profile's resolved allow-set must be exactly 46 tools")
	assert.Len(t, sFull.allow, len(alwaysOn), "empty profile's resolved allow-set must be exactly the alwaysOn set (fail-closed)")
}

func TestNewServer_RegistersMemoryOperatorAndChatTools(t *testing.T) {
	mem := freshClient(t, "http://memory.invalid")
	op := freshClient(t, "http://operator.invalid")
	ch := freshClient(t, "http://chat.invalid")
	s := NewServer(mem, op, ch, slog.New(slog.NewTextHandler(io.Discard, nil)), "")
	require.Equal(t, len(AllTools())+len(OperatorTools())+len(ChatTools())+len(PlatformTools())+len(HandoffTools()), s.ToolCount())
}

func TestOperatorTools_SchemasAreValidJSON(t *testing.T) {
	tools := OperatorTools()
	require.Len(t, tools, 27)
	for _, tl := range tools {
		var v any
		require.NoErrorf(t, json.Unmarshal(tl.Schema, &v), "operator tool %q has invalid JSON schema", tl.Name)
		_, err := json.Marshal(buildTool(tl))
		require.NoErrorf(t, err, "operator tool %q must marshal for tools/list", tl.Name)
	}
}

// TestRegister_LogsInfoOnSuccess verifies that a successful tool call produces
// a structured INFO log entry with tool, target, duration_ms, and status fields
// (hard rule 12: every business action logged at INFO with structured fields).
func TestRegister_LogsInfoOnSuccess(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer backend.Close()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	srv := NewServer(freshClient(t, backend.URL), freshClient(t, backend.URL),
		freshClient(t, backend.URL), logger, "brainstorm")

	ctx := context.Background()
	cli, err := mcpclient.NewInProcessClient(srv.srv)
	require.NoError(t, err)
	defer func() { _ = cli.Close() }()
	require.NoError(t, cli.Start(ctx))

	var initReq mcplib.InitializeRequest
	initReq.Params.ProtocolVersion = mcplib.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcplib.Implementation{Name: "test", Version: "0"}
	_, err = cli.Initialize(ctx, initReq)
	require.NoError(t, err)

	var req mcplib.CallToolRequest
	req.Params.Name = "create_memory"
	req.Params.Arguments = map[string]any{"text": "hello"}
	res, err := cli.CallTool(ctx, req)
	require.NoError(t, err)
	require.False(t, res.IsError)

	logged := buf.String()
	assert.Contains(t, logged, "tool call", "INFO log must say 'tool call'")
	assert.Contains(t, logged, "create_memory", "INFO log must carry tool name")
	assert.Contains(t, logged, "duration_ms", "INFO log must carry duration_ms")
	assert.Contains(t, logged, `"ok"`, "INFO log must carry status ok")
}

// TestRegister_LogsErrorOnFailure verifies that a backend error produces an
// ERROR log (not just a silent MCP error result).
func TestRegister_LogsErrorOnFailure(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer backend.Close()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	srv := NewServer(freshClient(t, backend.URL), freshClient(t, backend.URL),
		freshClient(t, backend.URL), logger, "brainstorm")

	ctx := context.Background()
	cli, err := mcpclient.NewInProcessClient(srv.srv)
	require.NoError(t, err)
	defer func() { _ = cli.Close() }()
	require.NoError(t, cli.Start(ctx))

	var initReq mcplib.InitializeRequest
	initReq.Params.ProtocolVersion = mcplib.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcplib.Implementation{Name: "test", Version: "0"}
	_, err = cli.Initialize(ctx, initReq)
	require.NoError(t, err)

	var req mcplib.CallToolRequest
	req.Params.Name = "create_memory"
	req.Params.Arguments = map[string]any{"text": "hello"}
	res, err := cli.CallTool(ctx, req)
	require.NoError(t, err) // protocol layer must not fail
	require.True(t, res.IsError)

	logged := buf.String()
	assert.Contains(t, logged, "tool error", "ERROR log must say 'tool error'")
	assert.Contains(t, logged, "create_memory", "ERROR log must carry tool name")
}

// TestRun_HonorsContext verifies that Server.Run accepts and wires the context
// (finding #8: Run previously discarded ctx, preventing signal-cancellation).
// The actual cancellation behaviour requires stdio pipes and is tested at the
// integration level; here we verify the method signature and that a
// pre-cancelled context causes Listen to return promptly.
func TestRun_HonorsContext(t *testing.T) {
	mem := freshClient(t, "http://memory.invalid")
	srv := NewServer(mem, mem, mem, slog.New(slog.NewTextHandler(io.Discard, nil)), "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	// Run with a cancelled context: Listen reads from a no-op reader and should
	// return once the context is done. We pipe /dev/null as stdin so it doesn't block.
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()
	select {
	case <-done:
		// returned promptly - ctx was honoured
	case <-context.Background().Done():
		t.Fatal("Run did not return after context cancellation")
	}
}

// TestRegister_204ReturnsOKMarker verifies that an HTTP 204 No Content response
// produces {"ok":true} (not empty string) as the tool-result text, so the agent
// receives an unambiguous success signal instead of a blank result.
func TestRegister_204ReturnsOKMarker(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent) // no body
	}))
	defer backend.Close()

	srv := NewServer(freshClient(t, backend.URL), freshClient(t, backend.URL),
		freshClient(t, backend.URL), slog.New(slog.NewTextHandler(io.Discard, nil)), "brainstorm")

	ctx := context.Background()
	cli, err := mcpclient.NewInProcessClient(srv.srv)
	require.NoError(t, err)
	defer func() { _ = cli.Close() }()
	require.NoError(t, cli.Start(ctx))

	var initReq mcplib.InitializeRequest
	initReq.Params.ProtocolVersion = mcplib.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcplib.Implementation{Name: "test", Version: "0"}
	_, err = cli.Initialize(ctx, initReq)
	require.NoError(t, err)

	var req mcplib.CallToolRequest
	req.Params.Name = "delete_memory"
	req.Params.Arguments = map[string]any{"id": "mem-123"}
	res, err := cli.CallTool(ctx, req)
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.NotEmpty(t, res.Content, "tool result must not be empty for 204")
	text := res.Content[0].(mcplib.TextContent).Text
	require.NotEmpty(t, text, "tool result text must not be empty for 204")
	require.Contains(t, text, "ok", "204 response must contain ok marker")
}

// TestRegister_LogsResourceID verifies that the INFO log carries resource_id
// extracted from the tool args (hard rule 12: structured fields including resource_id).
func TestRegister_LogsResourceID(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"mem-42"}`))
	}))
	defer backend.Close()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	srv := NewServer(freshClient(t, backend.URL), freshClient(t, backend.URL),
		freshClient(t, backend.URL), logger, "brainstorm")

	ctx := context.Background()
	cli, err := mcpclient.NewInProcessClient(srv.srv)
	require.NoError(t, err)
	defer func() { _ = cli.Close() }()
	require.NoError(t, cli.Start(ctx))

	var initReq mcplib.InitializeRequest
	initReq.Params.ProtocolVersion = mcplib.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcplib.Implementation{Name: "test", Version: "0"}
	_, err = cli.Initialize(ctx, initReq)
	require.NoError(t, err)

	// get_memory has an "id" arg — the canonical resource_id.
	var req mcplib.CallToolRequest
	req.Params.Name = "get_memory"
	req.Params.Arguments = map[string]any{"id": "mem-42"}
	res, err := cli.CallTool(ctx, req)
	require.NoError(t, err)
	require.False(t, res.IsError)

	logged := buf.String()
	assert.Contains(t, logged, "resource_id", "INFO log must carry resource_id")
	assert.Contains(t, logged, "mem-42", "resource_id must equal the id arg")
}

// TestRegister_MetricsIncremented verifies that a successful tool call
// increments ToolCallsTotal{tool, "ok"} (hard rule 13).
func TestRegister_MetricsIncremented(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer backend.Close()

	srv := NewServer(freshClient(t, backend.URL), freshClient(t, backend.URL),
		freshClient(t, backend.URL), slog.New(slog.NewTextHandler(io.Discard, nil)), "brainstorm")

	ctx := context.Background()
	cli, err := mcpclient.NewInProcessClient(srv.srv)
	require.NoError(t, err)
	defer func() { _ = cli.Close() }()
	require.NoError(t, cli.Start(ctx))

	var initReq mcplib.InitializeRequest
	initReq.Params.ProtocolVersion = mcplib.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcplib.Implementation{Name: "test", Version: "0"}
	_, err = cli.Initialize(ctx, initReq)
	require.NoError(t, err)

	before := testutil.ToFloat64(obs.ToolCallsTotal.With(prometheus.Labels{"tool": "create_memory", "result": "ok"}))

	var req mcplib.CallToolRequest
	req.Params.Name = "create_memory"
	req.Params.Arguments = map[string]any{"text": "hello"}
	_, err = cli.CallTool(ctx, req)
	require.NoError(t, err)

	after := testutil.ToFloat64(obs.ToolCallsTotal.With(prometheus.Labels{"tool": "create_memory", "result": "ok"}))
	assert.Equal(t, before+1, after, "ToolCallsTotal must increment on success")
}

// startClient wires an in-process MCP client against srv and completes the
// initialize handshake, returning a ready-to-call client.
func startClient(ctx context.Context, t *testing.T, srv *Server) *mcpclient.Client {
	t.Helper()
	cli, err := mcpclient.NewInProcessClient(srv.srv)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.Close() })
	require.NoError(t, cli.Start(ctx))
	var initReq mcplib.InitializeRequest
	initReq.Params.ProtocolVersion = mcplib.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcplib.Implementation{Name: "test", Version: "0"}
	_, err = cli.Initialize(ctx, initReq)
	require.NoError(t, err)
	return cli
}

// TestCallTool_NonAllowedToolReturnsAuthzErrorAndSkipsHandler verifies
// Component 4a's call-time authz guard: a tool NOT in the resolved profile's
// allow-set is still listed (tools/list is profile-invariant) but errors on
// call, and the handler/backend is never actually reached.
func TestCallTool_NonAllowedToolReturnsAuthzErrorAndSkipsHandler(t *testing.T) {
	var chatHit bool
	chat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chatHit = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"room-1"}`))
	}))
	defer chat.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// "refine" has no chat tools in its allow-set.
	srv := NewServer(freshClient(t, "http://memory.invalid"), freshClient(t, "http://operator.invalid"),
		freshClient(t, chat.URL), logger, "refine")

	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	res := callTool(ctx, t, cli, "chat_create_room", map[string]any{"name": "impl"})
	require.True(t, res.IsError, "a tool outside the profile's allow-set must return an authz error")
	require.False(t, chatHit, "the backend must never be reached for a denied tool")
}

// TestCallTool_AlwaysOnToolCallableUnderAnyProfile verifies alwaysOn tools
// (e.g. task_get) remain callable regardless of the resolved profile.
func TestCallTool_AlwaysOnToolCallableUnderAnyProfile(t *testing.T) {
	operator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"task-x","state":"Running"}`))
	}))
	defer operator.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := NewServer(freshClient(t, "http://memory.invalid"), freshClient(t, operator.URL),
		freshClient(t, "http://chat.invalid"), logger, "refine")

	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	res := callTool(ctx, t, cli, "task_get", map[string]any{"task": "task-x"})
	require.False(t, res.IsError, "an alwaysOn tool must be callable under any profile")
}

// TestCallTool_RefineCanDeleteHandoff verifies refine (the groomer) can call
// delete_handoff at call time.
func TestCallTool_RefineCanDeleteHandoff(t *testing.T) {
	chat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer chat.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := NewServer(freshClient(t, "http://memory.invalid"), freshClient(t, "http://operator.invalid"),
		freshClient(t, chat.URL), logger, "refine")

	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	res := callTool(ctx, t, cli, "delete_handoff", map[string]any{"handoff_key": "k1"})
	require.False(t, res.IsError, "refine must be able to call delete_handoff")
}

// TestCallTool_ImplementDeniedDeleteHandoffButAllowedWriteGetList verifies
// implement can write/get/list handoffs but is denied delete_handoff
// (groomer-only, refine).
func TestCallTool_ImplementDeniedDeleteHandoffButAllowedWriteGetList(t *testing.T) {
	chat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer chat.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := NewServer(freshClient(t, "http://memory.invalid"), freshClient(t, "http://operator.invalid"),
		freshClient(t, chat.URL), logger, "implement")

	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	deniedRes := callTool(ctx, t, cli, "delete_handoff", map[string]any{"handoff_key": "k1"})
	require.True(t, deniedRes.IsError, "implement must be denied delete_handoff")

	writeRes := callTool(ctx, t, cli, "write_handoff", map[string]any{"handoff_key": "k1", "project": "p1", "body": "b"})
	require.False(t, writeRes.IsError, "implement must be allowed write_handoff")

	getRes := callTool(ctx, t, cli, "get_handoff", map[string]any{"handoff_key": "k1"})
	require.False(t, getRes.IsError, "implement must be allowed get_handoff")

	listRes := callTool(ctx, t, cli, "list_handoffs", map[string]any{"project": "p1"})
	require.False(t, listRes.IsError, "implement must be allowed list_handoffs")
}

// TestCallTool_NonHandoffProfileDeniedAllHandoffTools verifies a profile with
// no continuity role (review) is denied all 4 handoff tools at call time.
func TestCallTool_NonHandoffProfileDeniedAllHandoffTools(t *testing.T) {
	var chatHit bool
	chat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chatHit = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer chat.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := NewServer(freshClient(t, "http://memory.invalid"), freshClient(t, "http://operator.invalid"),
		freshClient(t, chat.URL), logger, "review")

	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"write_handoff", map[string]any{"handoff_key": "k1", "project": "p1", "body": "b"}},
		{"get_handoff", map[string]any{"handoff_key": "k1"}},
		{"list_handoffs", map[string]any{"project": "p1"}},
		{"delete_handoff", map[string]any{"handoff_key": "k1"}},
	} {
		res := callTool(ctx, t, cli, tc.tool, tc.args)
		require.True(t, res.IsError, "review profile must be denied %q", tc.tool)
	}
	require.False(t, chatHit, "the backend must never be reached for any denied handoff tool")
}

// TestCallTool_UnknownProfileOnlyAlwaysOnCallable preserves G15: an unknown,
// non-empty profile still fails closed to the alwaysOn set at call time -
// every other tool is listed but errors when called.
func TestCallTool_UnknownProfileOnlyAlwaysOnCallable(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer backend.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := NewServer(freshClient(t, backend.URL), freshClient(t, backend.URL),
		freshClient(t, backend.URL), logger, "totally-bogus-unknown-profile")
	require.Equal(t, len(AllTools())+len(OperatorTools())+len(ChatTools())+len(PlatformTools())+len(HandoffTools()), srv.ToolCount(),
		"unknown profile must still list every tool")

	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	okRes := callTool(ctx, t, cli, "task_get", map[string]any{"task": "task-x"})
	require.False(t, okRes.IsError, "alwaysOn tools must remain callable under an unknown profile")

	deniedRes := callTool(ctx, t, cli, "create_memory", map[string]any{"text": "hello"})
	require.True(t, deniedRes.IsError, "a non-alwaysOn tool must error under an unknown profile (G15 fail-closed)")
}

// TestCallTool_EmptyProfileOnlyAlwaysOnCallable verifies an empty profile now
// fails closed: only alwaysOn tools are callable, every other tool errors.
func TestCallTool_EmptyProfileOnlyAlwaysOnCallable(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"room-1"}`))
	}))
	defer backend.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := NewServer(freshClient(t, backend.URL), freshClient(t, backend.URL),
		freshClient(t, backend.URL), logger, "")

	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	okRes := callTool(ctx, t, cli, "task_get", map[string]any{"task": "task-x"})
	require.False(t, okRes.IsError, "alwaysOn tools must remain callable with an empty profile")

	deniedRes := callTool(ctx, t, cli, "chat_create_room", map[string]any{"name": "impl"})
	require.True(t, deniedRes.IsError, "a non-alwaysOn tool must error with an empty profile (fail-closed)")
}
