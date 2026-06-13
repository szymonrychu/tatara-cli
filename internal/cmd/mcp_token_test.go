package cmd

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveMCPToken_NoCredentialsStartsUnauthenticated(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("OIDC_ISSUER", "")
	t.Setenv("CLI_OIDC_CLIENT_ID", "")
	t.Setenv("CLI_OIDC_CLIENT_SECRET", "")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tok, path := resolveMCPToken(context.Background(), logger)

	require.Nil(t, tok, "no credentials must yield a nil token, not an error")
	require.Empty(t, path, "no stored token means no token path to reload from")
}
