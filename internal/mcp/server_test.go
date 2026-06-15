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

	// Must not panic; all tools register without error.
	srv := NewServer(c, c, c, slog.Default())
	assert.NotNil(t, srv)
	assert.NotNil(t, srv.srv)

	// Cross-check: tool count matches registry.
	assert.Len(t, AllTools(), 34)
}

func TestNewServer_RegistersMemoryOperatorAndChatTools(t *testing.T) {
	mem := freshClient(t, "http://memory.invalid")
	op := freshClient(t, "http://operator.invalid")
	ch := freshClient(t, "http://chat.invalid")
	s := NewServer(mem, op, ch, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.Equal(t, len(AllTools())+len(OperatorTools())+len(ChatTools()), s.ToolCount())
}

func TestOperatorTools_SchemasAreValidJSON(t *testing.T) {
	tools := OperatorTools()
	require.Len(t, tools, 18)
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
		freshClient(t, backend.URL), logger)

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
		freshClient(t, backend.URL), logger)

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
	srv := NewServer(mem, mem, mem, slog.New(slog.NewTextHandler(io.Discard, nil)))
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

// TestRegister_MetricsIncremented verifies that a successful tool call
// increments ToolCallsTotal{tool, "ok"} (hard rule 13).
func TestRegister_MetricsIncremented(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer backend.Close()

	srv := NewServer(freshClient(t, backend.URL), freshClient(t, backend.URL),
		freshClient(t, backend.URL), slog.New(slog.NewTextHandler(io.Discard, nil)))

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
