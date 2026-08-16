package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/auth"
	"github.com/szymonrychu/tatara-cli/internal/client"
)

// profileToolCounts is contract D.6's gating TABLE, counted (the always-on six,
// plus the profile's granted tools, plus submit_outcome). D.6's prose SUMMARY
// line under the table says 17/20/14/17/16/13/19; it is arithmetically wrong and
// contradicts the table it summarises (incident is denied issue_write and
// mr_write, so it cannot be 20 out of a 20-tool surface). The table is
// normative; these are its counts. The clarify row is deleted at contract 4;
// implement is 18, not the summary's 17, because clarify's issue_write folded
// into it. upgrade (16) is new at 2026-08-13, not part of the original D.6
// table: implement's code/memory grants plus mr_write, minus issue_write and
// task_list.
var profileToolCounts = map[string]int{
	"brainstorm":    17,
	"incident":      18,
	"implement":     18,
	"review":        16,
	"refine":        13,
	"documentation": 18,
	"upgrade":       16,
}

// readLines reads a golden file into a sorted slice, skipping blanks/comments.
func readLines(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	sort.Strings(out)
	return out
}

// Every registered tool must marshal cleanly. mcp-go's NewTool seeds a default
// object InputSchema; combined with a raw schema the tool has both InputSchema
// and RawInputSchema set, which fails tools/list marshalling and leaves the
// agent with zero tatara tools.
func TestBuildTool_AllToolsMarshal(t *testing.T) {
	all := append(MemoryTools(), CodeTools()...)
	all = append(all, PlatformTools()...)
	all = append(all, SCMTools()...)
	for _, p := range outcomeProfiles {
		tl, ok := OutcomeTool(p)
		require.Truef(t, ok, "profile %q must have a submit_outcome", p)
		all = append(all, tl)
	}
	for _, tl := range all {
		_, err := json.Marshal(buildTool(tl))
		require.NoErrorf(t, err, "tool %s must marshal for tools/list", tl.Name)
	}
}

// staticToolCount is the whole candidate surface minus submit_outcome: the 20
// tools a profile may draw from. It is no longer what any pod registers.
func staticToolCount() int {
	return len(MemoryTools()) + len(CodeTools()) + len(PlatformTools()) + len(SCMTools())
}

func TestToolGroups_AreTheTwentyStaticTools(t *testing.T) {
	tok := &auth.Token{
		AccessToken: "test",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		TokenType:   "Bearer",
	}
	c, err := client.New(client.Config{BaseURL: "http://localhost:9999", Token: tok})
	require.NoError(t, err)

	srv := NewServer(c, c, slog.Default(), "")
	assert.NotNil(t, srv)
	assert.NotNil(t, srv.srv)

	// MemoryTools(5) + CodeTools(4) + PlatformTools(7) + SCMTools(4) = 20 static
	// tools; the 21st is the profile's submit_outcome.
	assert.Len(t, MemoryTools(), 5, "MemoryTools must be 5 after the memory fold")
	assert.Equal(t, 20, staticToolCount(), "the static tool set is exactly 20")
}

func TestNewServer_RegistersOnlyTheProfilesTools(t *testing.T) {
	mem := freshClient(t, "http://memory.invalid")
	op := freshClient(t, "http://operator.invalid")
	for _, kind := range AgentKinds() {
		s := NewServer(mem, op, discardLogger(), kind)
		require.Equal(t, profileToolCounts[kind], s.ToolCount(), "profile %q registers exactly its allow-set", kind)
		require.Len(t, s.RegisteredNames(), profileToolCounts[kind])
		require.Contains(t, s.RegisteredNames(), "submit_outcome", "every agent kind must be able to terminate its Task")
	}
}

func TestNewServer_EmptyProfileRegistersExactlySixTools(t *testing.T) {
	s := NewServer(freshClient(t, "http://memory.invalid"), freshClient(t, "http://operator.invalid"), discardLogger(), "")
	require.Equal(t, 6, s.ToolCount(),
		"fail closed: the always-on six, and NO submit_outcome")
	require.NotContains(t, s.RegisteredNames(), "submit_outcome")
}

func TestNewServer_UnknownProfileRegistersExactlySixTools(t *testing.T) {
	s := NewServer(freshClient(t, "http://memory.invalid"), freshClient(t, "http://operator.invalid"), discardLogger(), "totally-bogus-unknown-profile")
	require.Equal(t, 6, s.ToolCount(), "an unknown profile fails closed to the always-on six (G15)")
	require.NotContains(t, s.RegisteredNames(), "submit_outcome")
}

func TestNewServer_ToolsListIsNoLongerIdenticalAcrossProfiles(t *testing.T) {
	// This asserts the DELIBERATE loss of the byte-identical-tools/list
	// prompt-cache optimisation (design accepted risk 3). If this test ever
	// starts failing because someone restored it, they have also restored the
	// 74-tool list, and every agent is back to choosing between 68 tools it is
	// not allowed to call.
	mem := freshClient(t, "http://memory.invalid")
	op := freshClient(t, "http://operator.invalid")
	a := NewServer(mem, op, discardLogger(), "refine").RegisteredNames()
	b := NewServer(mem, op, discardLogger(), "implement").RegisteredNames()
	require.NotEqual(t, a, b)
}

func TestTotalToolSurfaceIsTwentyOne(t *testing.T) {
	mem := freshClient(t, "http://memory.invalid")
	op := freshClient(t, "http://operator.invalid")
	names := map[string]bool{}
	for _, kind := range AgentKinds() {
		for _, n := range NewServer(mem, op, discardLogger(), kind).RegisteredNames() {
			names[n] = true
		}
	}
	require.Len(t, names, 21, "the union of every profile is exactly the 21-tool contract surface (contract D, +mr_takeover_request)")
}

func TestRegisteredToolGoldens(t *testing.T) {
	mem := freshClient(t, "http://memory.invalid")
	op := freshClient(t, "http://operator.invalid")
	for _, kind := range append(AgentKinds(), "") {
		f := "empty"
		if kind != "" {
			f = kind
		}
		want := readLines(t, filepath.Join("testdata", "profile-tools-"+f+".txt"))
		got := NewServer(mem, op, discardLogger(), kind).RegisteredNames()
		sort.Strings(got)
		require.Equal(t, want, got, "registered tools for profile %q", kind)
	}
}

func TestNewServer_RefineAllowSetIsThirteen(t *testing.T) {
	mem := freshClient(t, "http://memory.invalid")
	op := freshClient(t, "http://operator.invalid")

	sRefine := NewServer(mem, op, discardLogger(), "refine")
	sEmpty := NewServer(mem, op, discardLogger(), "")
	assert.Len(t, sRefine.allow, 13, "refine's resolved allow-set is 13 tools (contract D.6)")
	assert.Equal(t, 13, sRefine.ToolCount(), "and it registers exactly those 13")
	assert.Len(t, sEmpty.allow, len(alwaysOn), "empty profile's resolved allow-set must be exactly the alwaysOn set (fail-closed)")
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
	srv := NewServer(freshClient(t, backend.URL), freshClient(t, backend.URL), logger, "brainstorm")

	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	res := callTool(ctx, t, cli, "memory_write", map[string]any{"text": "hello"})
	require.False(t, res.IsError)

	logged := buf.String()
	assert.Contains(t, logged, "tool call", "INFO log must say 'tool call'")
	assert.Contains(t, logged, "memory_write", "INFO log must carry tool name")
	assert.Contains(t, logged, "duration_ms", "INFO log must carry duration_ms")
	assert.Contains(t, logged, `"ok"`, "INFO log must carry status ok")
}

// TestNewServer_LogsResolvedSurface: the resolved tool surface is a business
// action and is logged at INFO with the profile and the registered count.
func TestNewServer_LogsResolvedSurface(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	NewServer(freshClient(t, "http://memory.invalid"), freshClient(t, "http://operator.invalid"), logger, "implement")

	logged := buf.String()
	assert.Contains(t, logged, "tools_registered", "startup must log the resolved surface")
	assert.Contains(t, logged, "implement")
	assert.Contains(t, logged, `"count":18`)
}

// TestRegister_LogsErrorOnFailure verifies that a backend error produces an
// ERROR log (not just a silent MCP error result). It uses an operator-target
// tool: a 5xx from the MEMORY backend is a degraded subsystem, not a tool
// error, and takes the MEMORY_DEGRADED path instead (see degraded_test.go).
func TestRegister_LogsErrorOnFailure(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer backend.Close()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	srv := NewServer(freshClient(t, backend.URL), freshClient(t, backend.URL), logger, "brainstorm")

	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	res := callTool(ctx, t, cli, "task_get", map[string]any{"task": "task-x"})
	require.True(t, res.IsError)

	logged := buf.String()
	assert.Contains(t, logged, "tool error", "ERROR log must say 'tool error'")
	assert.Contains(t, logged, "task_get", "ERROR log must carry tool name")
}

// TestRun_HonorsContext verifies that Server.Run accepts and wires the context
// (finding #8: Run previously discarded ctx, preventing signal-cancellation).
func TestRun_HonorsContext(t *testing.T) {
	mem := freshClient(t, "http://memory.invalid")
	srv := NewServer(mem, mem, discardLogger(), "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
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

	// incident, not brainstorm: memory_edges is only registered for incident and
	// documentation (contract D.6).
	srv := NewServer(freshClient(t, backend.URL), freshClient(t, backend.URL), discardLogger(), "incident")

	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	res := callTool(ctx, t, cli, "memory_edges", map[string]any{"op": "delete", "id": "mem-123"})
	require.False(t, res.IsError)
	require.NotEmpty(t, res.Content, "tool result must not be empty for 204")
	text := res.Content[0].(mcplib.TextContent).Text
	require.NotEmpty(t, text, "tool result text must not be empty for 204")
	require.Contains(t, text, "ok", "204 response must contain ok marker")
}

// TestRegister_LogsResourceID verifies that the INFO log carries resource_id
// extracted from the tool args (hard rule 12).
func TestRegister_LogsResourceID(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"mem-42"}`))
	}))
	defer backend.Close()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	srv := NewServer(freshClient(t, backend.URL), freshClient(t, backend.URL), logger, "brainstorm")

	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	// memory_entity op=get has an "id" arg - the canonical resource_id.
	res := callTool(ctx, t, cli, "memory_entity", map[string]any{"op": "get", "id": "mem-42"})
	require.False(t, res.IsError)

	logged := buf.String()
	assert.Contains(t, logged, "resource_id", "INFO log must carry resource_id")
	assert.Contains(t, logged, "mem-42", "resource_id must equal the id arg")
}

const testAuthNote = "auth: MCP server started UNAUTHENTICATED (stage=token_status)"

// The tool result text is the only channel out of this process that provably
// reaches Loki (the wrapper tails the transcript). A pod that started without
// credentials must say so where an operator can read it - on the error results
// that are already anomalous, and nowhere else.
func TestRegister_AuthNoteRidesErrorResults(t *testing.T) {
	// 400 is a client error: it does not latch the memory degraded state, so it
	// stays an ordinary error tool result.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"nope"}`))
	}))
	defer backend.Close()

	srv := NewServer(freshClient(t, backend.URL), freshClient(t, backend.URL), discardLogger(), "brainstorm",
		WithAuthNote(testAuthNote))

	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	res := callTool(ctx, t, cli, "memory_write", map[string]any{"text": "hello"})
	require.True(t, res.IsError, "a 400 from the backend must surface as an error tool result")
	assert.Contains(t, resultText(t, res), testAuthNote, "the auth note must ride the error result")
}

// Healthy turns pay no context tax: a successful result is byte-identical to
// what an authenticated server returns.
func TestRegister_AuthNoteAbsentFromSuccessfulResults(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer backend.Close()

	srv := NewServer(freshClient(t, backend.URL), freshClient(t, backend.URL), discardLogger(), "brainstorm",
		WithAuthNote(testAuthNote))

	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	res := callTool(ctx, t, cli, "memory_write", map[string]any{"text": "hello"})
	require.False(t, res.IsError)
	assert.NotContains(t, resultText(t, res), testAuthNote, "a successful result must not carry the auth note")
}

// With credentials resolved there is no note at all, so an error result is
// exactly the backend's message.
func TestRegister_NoAuthNoteLeavesErrorResultUnchanged(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"nope"}`))
	}))
	defer backend.Close()

	srv := NewServer(freshClient(t, backend.URL), freshClient(t, backend.URL), discardLogger(), "brainstorm")

	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	res := callTool(ctx, t, cli, "memory_write", map[string]any{"text": "hello"})
	require.True(t, res.IsError)
	assert.NotContains(t, resultText(t, res), "UNAUTHENTICATED")
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

// requireNotCallable asserts a tool the profile does not hold cannot be invoked:
// it is not registered, so the protocol layer rejects it (or, if a future change
// ever registers it, the call-time allow check errors it).
func requireNotCallable(ctx context.Context, t *testing.T, cli *mcpclient.Client, name string, args map[string]any) {
	t.Helper()
	var req mcplib.CallToolRequest
	req.Params.Name = name
	req.Params.Arguments = args
	res, err := cli.CallTool(ctx, req)
	if err == nil {
		require.NotNil(t, res)
		require.True(t, res.IsError, "tool %q is outside the profile and must not succeed", name)
	}
}

// TestCallTool_NonRegisteredToolIsNotCallable: a tool outside the profile is not
// registered at all, and the call-time allow check in register() stays as belt
// and braces. All agents share one OIDC identity; this is the authz boundary.
func TestCallTool_NonRegisteredToolIsNotCallable(t *testing.T) {
	var opHit bool
	op := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		opHit = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer op.Close()

	srv := NewServer(freshClient(t, "http://memory.invalid"), freshClient(t, op.URL), discardLogger(), "brainstorm")
	require.False(t, srv.allow["mr_write"], "brainstorm must not hold mr_write")
	require.NotContains(t, srv.RegisteredNames(), "mr_write")

	ctx := context.Background()
	cli := startClient(ctx, t, srv)
	requireNotCallable(ctx, t, cli, "mr_write", map[string]any{"repo": "tatara-cli", "number": 80, "action": "comment", "body": "x"})
	require.False(t, opHit, "the backend must never be reached for a tool outside the profile")
}

// TestCallTool_AlwaysOnToolCallableUnderAnyProfile verifies alwaysOn tools
// (e.g. task_get) remain callable regardless of the resolved profile.
func TestCallTool_AlwaysOnToolCallableUnderAnyProfile(t *testing.T) {
	operator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"task-x","state":"Running"}`))
	}))
	defer operator.Close()

	srv := NewServer(freshClient(t, "http://memory.invalid"), freshClient(t, operator.URL), discardLogger(), "refine")

	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	res := callTool(ctx, t, cli, "task_get", map[string]any{"task": "task-x"})
	require.False(t, res.IsError, "an alwaysOn tool must be callable under any profile")
}

// TestCallTool_UnknownProfileOnlyAlwaysOnCallable preserves G15: an unknown,
// non-empty profile fails closed to the alwaysOn set - nothing else is even
// registered, and submit_outcome is absent, so the pod cannot terminate a Task.
func TestCallTool_UnknownProfileOnlyAlwaysOnCallable(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer backend.Close()

	srv := NewServer(freshClient(t, backend.URL), freshClient(t, backend.URL), discardLogger(), "totally-bogus-unknown-profile")
	require.Equal(t, 6, srv.ToolCount(), "unknown profile registers only the always-on six")

	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	okRes := callTool(ctx, t, cli, "task_get", map[string]any{"task": "task-x"})
	require.False(t, okRes.IsError, "alwaysOn tools must remain callable under an unknown profile")

	requireNotCallable(ctx, t, cli, "memory_write", map[string]any{"text": "hello"})
	requireNotCallable(ctx, t, cli, "submit_outcome", map[string]any{"task": "task-x"})
}

// TestCallTool_EmptyProfileOnlyAlwaysOnCallable: an empty profile fails closed
// exactly like an unknown one.
func TestCallTool_EmptyProfileOnlyAlwaysOnCallable(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer backend.Close()

	srv := NewServer(freshClient(t, backend.URL), freshClient(t, backend.URL), discardLogger(), "")

	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	okRes := callTool(ctx, t, cli, "task_get", map[string]any{"task": "task-x"})
	require.False(t, okRes.IsError, "alwaysOn tools must remain callable with an empty profile")

	requireNotCallable(ctx, t, cli, "mr_write", map[string]any{"repo": "tatara-cli", "number": 80, "action": "comment", "body": "x"})
}

// TestCallTool_RefineMRWriteIsCommentOnly is contract J, RESIDUE 1: refine holds
// mr_write (so it IS registered and callable), but only for action=comment. The
// gate is code, not schema, and it runs before Invoke - the backend is never
// reached for a denied action.
func TestCallTool_RefineMRWriteIsCommentOnly(t *testing.T) {
	var opHits int
	op := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		opHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer op.Close()

	srv := NewServer(freshClient(t, "http://memory.invalid"), freshClient(t, op.URL), discardLogger(), "refine")
	require.Contains(t, srv.RegisteredNames(), "mr_write", "refine holds mr_write (comment-only)")

	ctx := context.Background()
	cli := startClient(ctx, t, srv)

	openRes := callTool(ctx, t, cli, "mr_write", map[string]any{"repo": "tatara-cli", "action": "open", "title": "t", "body": "b", "project": "tatara"})
	require.True(t, openRes.IsError, "refine may not open an MR")
	require.Equal(t, 0, opHits, "the refine gate must run before Invoke")

	commentRes := callTool(ctx, t, cli, "mr_write", map[string]any{"repo": "tatara-cli", "number": 80, "action": "comment", "body": "x", "project": "tatara"})
	require.False(t, commentRes.IsError, "refine may comment on an MR")
	require.Equal(t, 1, opHits)

	// The gate is refine-only: implement opens MRs.
	impl := NewServer(freshClient(t, "http://memory.invalid"), freshClient(t, op.URL), discardLogger(), "implement")
	implCli := startClient(ctx, t, impl)
	implRes := callTool(ctx, t, implCli, "mr_write", map[string]any{"repo": "tatara-cli", "action": "open", "title": "t", "body": "b", "project": "tatara"})
	require.False(t, implRes.IsError, "implement may open an MR")
}
