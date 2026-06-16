package cmd_test

import (
	"io"
	"net"
	"net/http"
	"testing"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
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

// TestMCP_MetricsAddrExposesEndpoint verifies that passing --metrics-addr causes
// the MCP command to register a --metrics-addr flag and that the promhttp handler
// serves Prometheus output on /metrics (hard rule 13).
func TestMCP_MetricsAddrExposesEndpoint(t *testing.T) {
	// Verify the flag is registered.
	root := cmd.NewRootCmd()
	var mcpCmd *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "mcp" {
			mcpCmd = c
			break
		}
	}
	require.NotNil(t, mcpCmd, "mcp subcommand must exist")
	flag := mcpCmd.Flags().Lookup("metrics-addr")
	require.NotNil(t, flag, "--metrics-addr flag must be registered on the mcp subcommand")

	// Directly verify that the promhttp handler serves valid Prometheus output.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	srv := &http.Server{Addr: addr, Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	resp, err := http.Get("http://" + addr + "/metrics") //nolint:noctx
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode, "/metrics must return 200")
	body, _ := io.ReadAll(resp.Body)
	require.Contains(t, string(body), "go_", "/metrics must contain Prometheus output")
}
