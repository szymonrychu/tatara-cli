package mcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/client"
)

// resultText extracts the text content of a tool result.
func resultText(t *testing.T, res *mcplib.CallToolResult) string {
	t.Helper()
	require.NotEmpty(t, res.Content, "tool result must carry content")
	txt, ok := mcplib.AsTextContent(res.Content[0])
	require.True(t, ok, "tool result content must be text")
	return txt.Text
}

// countingBackend serves status/body and counts the requests it received.
func countingBackend(t *testing.T, status int, body string) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// TATARA_MEMORY_DEGRADED=true is the operator telling the pod, at spawn time,
// that tatara-memory is unhealthy but the agent must run anyway. Every memory
// tool then answers with the guidance instead of burning a request timeout on
// a backend the platform already knows is down.
//
// It must NOT ask for a report_internal_issue. The operator already told the
// agent at turn 0, in promptguidance.MemoryDegradedGuidance, not to report this
// exact condition (tatara-operator#523); the tool layer instructing the
// opposite manufactures one duplicate platform-problem alert per turn and
// contradicts the prompt on the way.
func TestCallTool_DegradedEnvReturnsGuidanceWithoutCallingBackend(t *testing.T) {
	t.Setenv(memoryDegradedEnv, "true")
	backend, hits := countingBackend(t, http.StatusOK, `{"ok":true}`)

	srv := NewServer(freshClient(t, backend.URL), freshClient(t, backend.URL), discardLogger(), "brainstorm")
	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	res := callTool(ctx, t, cli, "memory_query", map[string]any{"mode": "hybrid", "text": "anything"})
	require.False(t, res.IsError, "a degraded memory subsystem is a known platform condition, not a tool error")

	text := resultText(t, res)
	require.Contains(t, text, "MEMORY_DEGRADED")
	require.Contains(t, text, "Proceed WITHOUT recall")
	require.NotContains(t, text, "report_internal_issue(",
		"the operator already told the agent at turn 0 not to report a spawn-time verdict")
	require.Contains(t, text, "Do NOT call report_internal_issue")
	require.Equal(t, int32(0), atomic.LoadInt32(hits), "a known-degraded backend must not be called")
}

// TATARA_MEMORY_DISABLED=true is a project configured without memory, not an
// outage. The reason must say so - "the platform flagged it unhealthy" sends an
// incident agent chasing a memory failure that does not exist - and the
// guidance must not ask for a report either, mirroring the operator's
// MemoryDisabledGuidance.
//
// The nil-client case is the one that actually happens: disabling memory clears
// Project.status.memory.endpoint, so the pod gets TATARA_MEMORY_URL="" and cmd
// wires no memory client at all. The disabled arm has to be reached even then,
// or it is dead code and the agent reads "your pod is misconfigured" on a
// project that is working exactly as configured.
func TestCallTool_MemoryDisabledIsNotReportedAsAFault(t *testing.T) {
	cases := []struct {
		name   string
		memory bool // a memory client resolved
	}{
		{"no endpoint, the steady state on a disabled project", false},
		{"endpoint still set, the spec-vs-status skew window", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(memoryDegradedEnv, "true")
			t.Setenv(memoryDisabledEnv, "true")
			backend, hits := countingBackend(t, http.StatusOK, `{"ok":true}`)

			var memory *client.Client
			if tc.memory {
				memory = freshClient(t, backend.URL)
			}
			srv := NewServer(memory, freshClient(t, backend.URL), discardLogger(), "brainstorm")
			ctx := context.Background()
			cli := startClient(ctx, t, srv)

			res := callTool(ctx, t, cli, "memory_query", map[string]any{"mode": "hybrid", "text": "anything"})
			require.False(t, res.IsError)

			text := resultText(t, res)
			require.Contains(t, text, "MEMORY_DEGRADED", "the prefix is load-bearing for skills matching on it")
			require.Contains(t, text, "configured without memory")
			require.NotContains(t, text, "flagged it unhealthy",
				"a deliberately disabled project was never flagged unhealthy")
			require.NotContains(t, text, "TATARA_MEMORY_URL is set but empty",
				"an empty endpoint is the CONSEQUENCE of disabling memory, not a misconfiguration to report")
			require.NotContains(t, text, "report_internal_issue(")
			require.Equal(t, int32(0), atomic.LoadInt32(hits))
		})
	}
}

// The mid-turn latch is the ONE case turn-0 guidance never covers: the operator
// said healthy at spawn and the backend died afterwards, so nothing upstream
// knows yet. That path keeps the report_internal_issue instruction - it is the
// case the instruction was written for.
func TestCallTool_LatchedOutageStillAsksForOneReport(t *testing.T) {
	t.Setenv(memoryDegradedEnv, "false")
	t.Setenv(memoryDisabledEnv, "false")
	backend, _ := countingBackend(t, http.StatusInternalServerError, `{"error":"boom"}`)

	srv := NewServer(freshClient(t, backend.URL), freshClient(t, backend.URL), discardLogger(), "brainstorm")
	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	res := callTool(ctx, t, cli, "memory_query", map[string]any{"mode": "hybrid", "text": "anything"})
	require.False(t, res.IsError)

	text := resultText(t, res)
	require.Contains(t, text, "MEMORY_DEGRADED")
	require.Contains(t, text, `report_internal_issue(category="tool_error", offending_tool="memory_query"`)
	require.Contains(t, text, "ONCE this turn")
}

// Every one of the nine TargetMemory tools must answer the same way; a code_*
// tool reads the same code graph out of the same backend.
func TestCallTool_DegradedEnvCoversCodeTools(t *testing.T) {
	t.Setenv(memoryDegradedEnv, "true")
	backend, hits := countingBackend(t, http.StatusOK, `{"ok":true}`)

	srv := NewServer(freshClient(t, backend.URL), freshClient(t, backend.URL), discardLogger(), "brainstorm")
	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	res := callTool(ctx, t, cli, "code_search", map[string]any{"repo": "tatara-cli", "q": "Invoke"})
	require.False(t, res.IsError)
	require.Contains(t, resultText(t, res), "MEMORY_DEGRADED")
	require.Equal(t, int32(0), atomic.LoadInt32(hits))
}

// A mid-turn outage: the operator said healthy at spawn, then the backend went
// away. The first transport failure latches, and the second call is answered
// from the latch with the SHORT form, so one outage cannot flood the transcript.
func TestCallTool_TransportFailureLatchesThenShortens(t *testing.T) {
	t.Setenv(memoryDegradedEnv, "false")
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close() // nothing is listening there any more

	srv := NewServer(freshClient(t, deadURL), freshClient(t, "http://operator.invalid"), discardLogger(), "brainstorm")
	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	first := callTool(ctx, t, cli, "memory_query", map[string]any{"mode": "hybrid", "text": "anything"})
	require.False(t, first.IsError)
	firstText := resultText(t, first)
	require.Contains(t, firstText, "MEMORY_DEGRADED")
	require.Contains(t, firstText, "Proceed WITHOUT recall", "the first occurrence carries the full guidance")
	require.Contains(t, firstText, "/queries", "the guidance must carry the original error")

	second := callTool(ctx, t, cli, "code_search", map[string]any{"repo": "tatara-cli", "q": "Invoke"})
	require.False(t, second.IsError)
	secondText := resultText(t, second)
	require.Contains(t, secondText, "MEMORY_DEGRADED (see earlier)")
	require.NotContains(t, secondText, "Proceed WITHOUT recall", "later occurrences get the short form")
	require.Less(t, len(secondText), len(firstText))
}

// A 5xx is the backend's own failure and latches exactly like a transport
// failure: the second call is answered from the latch and never leaves the pod.
func TestCallTool_5xxLatchesAndStopsCallingTheBackend(t *testing.T) {
	t.Setenv(memoryDegradedEnv, "false")
	backend, hits := countingBackend(t, http.StatusInternalServerError, `{"error":"boom"}`)

	srv := NewServer(freshClient(t, backend.URL), freshClient(t, backend.URL), discardLogger(), "brainstorm")
	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	first := callTool(ctx, t, cli, "memory_write", map[string]any{"text": "x"})
	require.False(t, first.IsError)
	require.Contains(t, resultText(t, first), "MEMORY_DEGRADED")
	require.Equal(t, int32(1), atomic.LoadInt32(hits))

	second := callTool(ctx, t, cli, "memory_write", map[string]any{"text": "y"})
	require.False(t, second.IsError)
	require.Contains(t, resultText(t, second), "MEMORY_DEGRADED (see earlier)")
	require.Equal(t, int32(1), atomic.LoadInt32(hits), "once latched, memory tools stop calling the backend")
}

// A 4xx is the agent's problem, not the platform's. It must keep its normal
// tool error AND must not latch - otherwise one malformed query would hide a
// perfectly healthy memory behind a MEMORY_DEGRADED banner for the whole turn.
func TestCallTool_4xxIsNotLatchedAndStaysAToolError(t *testing.T) {
	t.Setenv(memoryDegradedEnv, "false")
	backend, hits := countingBackend(t, http.StatusBadRequest, `{"error":"invalid mode"}`)

	srv := NewServer(freshClient(t, backend.URL), freshClient(t, backend.URL), discardLogger(), "brainstorm")
	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	first := callTool(ctx, t, cli, "memory_query", map[string]any{"mode": "hybrid", "text": "x"})
	require.True(t, first.IsError, "a 400 is a bad request, not a degraded subsystem")
	require.Contains(t, resultText(t, first), "400")
	require.NotContains(t, resultText(t, first), "MEMORY_DEGRADED")

	second := callTool(ctx, t, cli, "memory_query", map[string]any{"mode": "hybrid", "text": "y"})
	require.True(t, second.IsError)
	require.NotContains(t, resultText(t, second), "MEMORY_DEGRADED")
	require.Equal(t, int32(2), atomic.LoadInt32(hits), "a 4xx must not stop later memory calls")
}

// The latch is memory-only: operator-target tools are unaffected by the
// degraded flag, and by a latched memory outage.
func TestCallTool_OperatorToolUnaffectedByDegradedMemory(t *testing.T) {
	t.Setenv(memoryDegradedEnv, "true")
	operator, hits := countingBackend(t, http.StatusOK, `{"name":"task-x","state":"Running"}`)

	srv := NewServer(freshClient(t, "http://memory.invalid"), freshClient(t, operator.URL), discardLogger(), "brainstorm")
	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	res := callTool(ctx, t, cli, "task_get", map[string]any{"task": "task-x"})
	require.False(t, res.IsError)
	require.NotContains(t, resultText(t, res), "MEMORY_DEGRADED")
	require.Equal(t, int32(1), atomic.LoadInt32(hits))
}

// The guidance tells the agent to call report_internal_issue, so that tool must
// keep working while memory is degraded. It carries the zero-value Target
// (TargetMemory) but runs a local Handler and never touches a backend.
func TestCallTool_ReportInternalIssueStillWorksWhenDegraded(t *testing.T) {
	t.Setenv(memoryDegradedEnv, "true")
	srv := NewServer(freshClient(t, "http://memory.invalid"), freshClient(t, "http://operator.invalid"), discardLogger(), "brainstorm")
	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	res := callTool(ctx, t, cli, "report_internal_issue", map[string]any{
		"category": "tool_error", "description": "tatara-memory unavailable", "offending_tool": "memory_query",
	})
	require.False(t, res.IsError, "the agent must be able to report the outage the guidance told it to report")
	require.NotContains(t, resultText(t, res), "MEMORY_DEGRADED")
}

// A pod whose TATARA_MEMORY_URL is set but empty has no memory backend at all:
// cmd wires a nil memory client, and every memory tool answers with the
// guidance rather than panicking or silently reaching the public endpoint.
func TestCallTool_NoMemoryClientConfiguredIsDegraded(t *testing.T) {
	t.Setenv(memoryDegradedEnv, "false")
	srv := NewServer(nil, freshClient(t, "http://operator.invalid"), discardLogger(), "brainstorm")
	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	res := callTool(ctx, t, cli, "memory_query", map[string]any{"mode": "hybrid", "text": "x"})
	require.False(t, res.IsError)
	text := resultText(t, res)
	require.Contains(t, text, "MEMORY_DEGRADED")
	require.Contains(t, text, "TATARA_MEMORY_URL")
	// An empty endpoint with memory neither disabled nor flagged degraded is the
	// one spawn-time state nobody upstream has a name for: no turn-0 guidance
	// covers it, and off-pod there may be no operator at all. It keeps the
	// report instruction the other two arms drop.
	require.Contains(t, text, `report_internal_issue(category="tool_error", offending_tool="memory_query"`)
}

// The tool surface stays stable when memory is degraded: the nine memory tools
// are still listed, so the agent can see what it lost and report it.
func TestNewServer_DegradedKeepsTheToolSurface(t *testing.T) {
	t.Setenv(memoryDegradedEnv, "true")
	srv := NewServer(nil, freshClient(t, "http://operator.invalid"), discardLogger(), "brainstorm")
	require.Equal(t, profileToolCounts["brainstorm"], srv.ToolCount(), "degraded memory must not shrink tools/list")
	require.Contains(t, srv.RegisteredNames(), "memory_query")
}

// A degraded answer is a distinct outcome and the transcript is where it has to
// show up: there is no metrics egress from this process.
func TestCallTool_DegradedIsLoggedAsSuchAndTellsTheAgent(t *testing.T) {
	t.Setenv(memoryDegradedEnv, "true")
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	srv := NewServer(nil, freshClient(t, "http://operator.invalid"), logger, "brainstorm")
	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	res := callTool(ctx, t, cli, "memory_describe", map[string]any{"mode": "hybrid", "text": "x"})
	require.False(t, res.IsError, "the degraded path answers with guidance, not an error")
	require.Contains(t, resultText(t, res), "MEMORY_DEGRADED",
		"the agent-visible result is the only thing that reaches Loki")
	require.Contains(t, buf.String(), "memory tool answered from the degraded path")
	require.Contains(t, buf.String(), "memory_describe")
}

func TestMemoryBackendDown_ClassifiesFailures(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"transport", &client.TransportError{Err: errors.New("dial tcp: connection refused")}, true},
		{"500", &statusError{code: http.StatusInternalServerError, msg: "boom"}, true},
		{"503", &statusError{code: http.StatusServiceUnavailable, msg: "boom"}, true},
		{"400", &statusError{code: http.StatusBadRequest, msg: "bad"}, false},
		{"404", &statusError{code: http.StatusNotFound, msg: "nope"}, false},
		{"401", &statusError{code: http.StatusUnauthorized, msg: "auth"}, false},
		{"409", &statusError{code: http.StatusConflict, msg: "conflict"}, false},
		{"local build error", errors.New("missing required argument \"mode\""), false},
		{"wrapped transport", fmt.Errorf("tatara: %w", &client.TransportError{Err: errors.New("eof")}), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, memoryBackendDown(tc.err))
		})
	}
}
