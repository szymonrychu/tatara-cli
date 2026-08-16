package cmd_test

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/cmd"
)

// The MCP server is a stdio subprocess in a pod with no scrape target and no
// push path: a /metrics endpoint here was never collected by anything. It is
// gone, and it must not come back as "wired up" without an egress to match.
func TestMCP_HasNoMetricsEndpoint(t *testing.T) {
	root := cmd.NewRootCmd()
	var mcpCmd *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "mcp" {
			mcpCmd = c
		}
	}
	require.NotNil(t, mcpCmd, "mcp subcommand must exist")
	require.Nil(t, mcpCmd.Flags().Lookup("metrics-addr"),
		"--metrics-addr promised a scrape that never happened")
}

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

func TestMCP_ContractMismatchExitsNonZero(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("OIDC_ISSUER", "")
	t.Setenv("CLI_OIDC_CLIENT_ID", "")
	t.Setenv("CLI_OIDC_CLIENT_SECRET", "")
	t.Setenv("TATARA_CONTRACT_VERSION", "1")

	root := cmd.NewRootCmd()
	root.SetArgs([]string{"mcp"})
	err := root.Execute()
	require.Error(t, err, "an MCP server whose contract version does not match the operator must refuse to start")
	require.Contains(t, err.Error(), "agent contract mismatch")
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

// A pod with no memory stack gets TATARA_MEMORY_URL set but empty. The MCP
// server must still start (its memory tools stay listed and answer
// MEMORY_DEGRADED) rather than failing on a missing base URL - and it must not
// silently fall back to the shared public memory endpoint.
func TestMCP_StartsWithEmptyMemoryURL(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("OIDC_ISSUER", "")
	t.Setenv("CLI_OIDC_CLIENT_ID", "")
	t.Setenv("CLI_OIDC_CLIENT_SECRET", "")
	t.Setenv("TATARA_MEMORY_URL", "")

	root := cmd.NewRootCmd()
	root.SetArgs([]string{"mcp"})
	require.NoError(t, root.Execute())
}
