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

// Every registered tool must marshal cleanly. mcp-go's NewTool seeds a default
// object InputSchema; combined with a raw schema the tool has both InputSchema
// and RawInputSchema set, which fails tools/list marshalling and leaves the
// agent with zero tatara tools.
func TestBuildTool_AllToolsMarshal(t *testing.T) {
	for _, tl := range append(AllTools(), OperatorTools()...) {
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
	srv := NewServer(c, c, slog.Default())
	assert.NotNil(t, srv)
	assert.NotNil(t, srv.srv)

	// Cross-check: tool count matches registry.
	assert.Len(t, AllTools(), 28)
}

func TestNewServer_RegistersMemoryAndOperatorTools(t *testing.T) {
	mem := freshClient(t, "http://memory.invalid")
	op := freshClient(t, "http://operator.invalid")
	s := NewServer(mem, op, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.Equal(t, len(AllTools())+len(OperatorTools()), s.ToolCount())
}
