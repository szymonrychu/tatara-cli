package mcp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_StdioProtocol drives the registered MCP server over the real
// JSON-RPC protocol (in-process transport): initialize, tools/list, and
// tools/call against faked tatara-memory and tatara-operator backends.
// This is the regression guard the API-level registration tests cannot give:
// a tools/list marshalling break (see MEMORY.md, the 0.4.x bug) or a
// dispatch/result mistake surfaces here, not just at the registry level.
func TestE2E_StdioProtocol(t *testing.T) {
	var memMethod, memPath string
	memory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		memMethod, memPath = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"track_id":"abc123"}`))
	}))
	defer memory.Close()

	var opPath string
	var opFail bool
	operator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		opPath = r.URL.Path
		if opFail {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"task-x","state":"Running"}`))
	}))
	defer operator.Close()

	srv := NewServer(freshClient(t, memory.URL), freshClient(t, operator.URL),
		slog.New(slog.NewTextHandler(io.Discard, nil)), "brainstorm")

	ctx := context.Background()
	cli, err := mcpclient.NewInProcessClient(srv.srv)
	require.NoError(t, err)
	defer func() { _ = cli.Close() }()
	require.NoError(t, cli.Start(ctx))

	// initialize handshake.
	var initReq mcplib.InitializeRequest
	initReq.Params.ProtocolVersion = mcplib.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcplib.Implementation{Name: "tatara-e2e-test", Version: "0.0.0"}
	initRes, err := cli.Initialize(ctx, initReq)
	require.NoError(t, err)
	require.Equal(t, "tatara", initRes.ServerInfo.Name)

	// tools/list must expose exactly the brainstorm profile's allow-set. This is
	// the call that returned an error (and zero tools) under the 0.4.x
	// marshalling bug, and it is now per-profile (contract D.6).
	listRes, err := cli.ListTools(ctx, mcplib.ListToolsRequest{})
	require.NoError(t, err)
	require.Len(t, listRes.Tools, profileToolCounts["brainstorm"], "brainstorm registers exactly its 17-tool allow-set")

	exposed := map[string]bool{}
	for _, tl := range listRes.Tools {
		exposed[tl.Name] = true
	}
	for _, want := range []string{"memory_write", "memory_query", "code_search", "task_get", "scm_read", "submit_outcome"} {
		assert.Truef(t, exposed[want], "tools/list must expose %q", want)
	}
	for _, denied := range []string{"mr_write", "issue_write", "memory_edges"} {
		assert.Falsef(t, exposed[denied], "brainstorm must NOT be offered %q (contract D.6)", denied)
	}

	// tools/call against the memory backend: routes to tatara-memory and the
	// JSON body is returned as result text (the result-marshalling path).
	memRes := callTool(ctx, t, cli, "memory_write", map[string]any{"text": "hello e2e"})
	require.False(t, memRes.IsError)
	assert.Equal(t, http.MethodPost, memMethod)
	assert.Equal(t, "/memories", memPath)
	require.NotEmpty(t, memRes.Content)
	txt, ok := mcplib.AsTextContent(memRes.Content[0])
	require.True(t, ok, "result content must be text")
	assert.Contains(t, txt.Text, "track_id")

	// tools/call against the operator backend: confirms target dispatch picks
	// the operator client, not memory.
	opRes := callTool(ctx, t, cli, "task_get", map[string]any{"task": "task-x"})
	require.False(t, opRes.IsError)
	assert.Equal(t, "/tasks/task-x", opPath)

	// A backend error surfaces as an MCP error result, not a transport failure.
	opFail = true
	errRes := callTool(ctx, t, cli, "project_get", map[string]any{"project": "alpha"})
	require.True(t, errRes.IsError, "backend 500 must surface as an MCP error result")
}

func callTool(ctx context.Context, t *testing.T, cli *mcpclient.Client, name string, args map[string]any) *mcplib.CallToolResult {
	t.Helper()
	var req mcplib.CallToolRequest
	req.Params.Name = name
	req.Params.Arguments = args
	res, err := cli.CallTool(ctx, req)
	require.NoErrorf(t, err, "tools/call %q must not fail at the protocol layer", name)
	return res
}
