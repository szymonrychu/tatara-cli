package cmd_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/cmd"
)

func TestMCP_StartsWithoutToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("OIDC_ISSUER", "")
	t.Setenv("CLI_OIDC_CLIENT_ID", "")
	t.Setenv("CLI_OIDC_CLIENT_SECRET", "")

	root := cmd.NewRootCmd()
	root.SetArgs([]string{"mcp"})
	// tools/list needs no auth: the server must start unauthenticated. stdin is
	// at EOF in the test harness, so it serves over stdio and exits cleanly.
	err := root.Execute()
	require.NoError(t, err)
}

func TestMCP_RegisteredAsSubcommand(t *testing.T) {
	root := cmd.NewRootCmd()
	var found bool
	for _, c := range root.Commands() {
		if c.Name() == "mcp" {
			found = true
			require.NotNil(t, c.RunE, "mcp must have a runnable body")
		}
	}
	require.True(t, found)
}
