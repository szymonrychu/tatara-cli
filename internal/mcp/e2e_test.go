package mcp

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_StdioProtocol drives the MCP server over the real protocol machinery:
// initialize, tools/list and tools/call all flow through the mcp-go request
// handlers and JSON (un)marshalling, against faked tatara-memory and
// tatara-operator REST backends. The in-process transport marshals every
// response, so a tools/list result that fails to marshal (the 0.4.x regression
// where every tool carried both InputSchema and RawInputSchema) surfaces here
// as a ListTools error - exactly what the API-level registration tests miss.
func TestE2E_StdioProtocol(t *testing.T) {
	type call struct {
		method string
		path   string
	}
	var memCalls, opCalls []call

	mem := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		memCalls = append(memCalls, call{r.Method, r.URL.Path})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"track_id":"mem-abc"}`))
	}))
	defer mem.Close()

	op := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		opCalls = append(opCalls, call{r.Method, r.URL.Path})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"name":"alpha"}]`))
	}))
	defer op.Close()

	srv := NewServer(freshClient(t, mem.URL), freshClient(t, op.URL),
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	cli, err := mcpclient.NewInProcessClient(srv.srv)
	require.NoError(t, err)
	defer cli.Close()
	require.NoError(t, cli.Start(t.Context()))

	initReq := mcplib.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcplib.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcplib.Implementation{Name: "e2e-test", Version: "0.0.0"}
	initRes, err := cli.Initialize(t.Context(), initReq)
	require.NoError(t, err)
	assert.Equal(t, "tatara", initRes.ServerInfo.Name)

	// tools/list must expose every registered tool by name.
	listRes, err := cli.ListTools(t.Context(), mcplib.ListToolsRequest{})
	require.NoError(t, err, "tools/list must marshal and return all tools")

	got := map[string]bool{}
	for _, tl := range listRes.Tools {
		got[tl.Name] = true
	}
	want := append(AllTools(), OperatorTools()...)
	assert.Len(t, listRes.Tools, len(want))
	for _, tl := range want {
		assert.Truef(t, got[tl.Name], "tool %q missing from tools/list", tl.Name)
	}

	// tools/call against the memory backend.
	memCall := mcplib.CallToolRequest{}
	memCall.Params.Name = "create_memory"
	memCall.Params.Arguments = map[string]any{"text": "hello"}
	memRes, err := cli.CallTool(t.Context(), memCall)
	require.NoError(t, err)
	assert.False(t, memRes.IsError)
	assert.Contains(t, toolText(t, memRes), "mem-abc")
	require.Len(t, memCalls, 1)
	assert.Equal(t, http.MethodPost, memCalls[0].method)
	assert.Equal(t, "/memories", memCalls[0].path)
	assert.Empty(t, opCalls, "memory tool must not hit the operator backend")

	// tools/call against the operator backend.
	opCall := mcplib.CallToolRequest{}
	opCall.Params.Name = "project_list"
	opRes, err := cli.CallTool(t.Context(), opCall)
	require.NoError(t, err)
	assert.False(t, opRes.IsError)
	assert.Contains(t, toolText(t, opRes), "alpha")
	require.Len(t, opCalls, 1)
	assert.Equal(t, http.MethodGet, opCalls[0].method)
	assert.Equal(t, "/projects", opCalls[0].path)
}

// toolText concatenates the text content blocks of a tool result.
func toolText(t *testing.T, res *mcplib.CallToolResult) string {
	t.Helper()
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcplib.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	// Sanity: the body must be valid JSON the server pretty-printed.
	var v any
	require.NoError(t, json.Unmarshal([]byte(b.String()), &v))
	return b.String()
}
