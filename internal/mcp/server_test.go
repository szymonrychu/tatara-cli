package mcp

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/auth"
	"github.com/szymonrychu/tatara-cli/internal/client"
)

func TestNewServer_RegistersThirteenTools(t *testing.T) {
	tok := &auth.Token{
		AccessToken: "test",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		TokenType:   "Bearer",
	}
	c, err := client.New(client.Config{BaseURL: "http://localhost:9999", Token: tok})
	require.NoError(t, err)

	// Must not panic; all 13 tools register without error.
	srv := NewServer(c, slog.Default())
	assert.NotNil(t, srv)
	assert.NotNil(t, srv.srv)

	// Cross-check: tool count matches registry.
	assert.Len(t, AllTools(), 13)
}
