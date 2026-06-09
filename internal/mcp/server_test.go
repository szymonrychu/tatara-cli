package mcp

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/auth"
	"github.com/szymonrychu/tatara-cli/internal/client"
)

func TestNewServer_RegistersAllTools(t *testing.T) {
	tok := &auth.Token{
		AccessToken: "test",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		TokenType:   "Bearer",
	}
	c, err := client.New(client.Config{BaseURL: "http://localhost:9999", Token: tok})
	require.NoError(t, err)

	// Must not panic; all tools register without error.
	srv := NewServer(c, c, slog.Default())
	assert.NotNil(t, srv)
	assert.NotNil(t, srv.srv)

	// Cross-check: tool count matches registry.
	assert.Len(t, AllTools(), 23)
}

func TestNewServer_RegistersMemoryAndOperatorTools(t *testing.T) {
	mem := freshClient(t, "http://memory.invalid")
	op := freshClient(t, "http://operator.invalid")
	s := NewServer(mem, op, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.Equal(t, len(AllTools())+len(OperatorTools()), s.ToolCount())
}

// TestBuildTool_Marshals reproduces the tools/list serialization the MCP
// server performs. NewTool seeds a default InputSchema while we also supply a
// RawInputSchema; mcp-go's MarshalJSON rejects having both, which silently
// broke the whole tools/list response (only the first offending tool was
// reported). buildTool must clear the seeded schema so every tool marshals.
func TestBuildTool_Marshals(t *testing.T) {
	for _, tl := range append(AllTools(), OperatorTools()...) {
		_, err := json.Marshal(buildTool(tl))
		require.NoErrorf(t, err, "tool %s must marshal for tools/list", tl.Name)
	}
}
