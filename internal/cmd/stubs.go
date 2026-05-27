package cmd

import "github.com/spf13/cobra"

func newRawCmd() *cobra.Command {
	return &cobra.Command{Use: "raw", Short: "Authenticated REST passthrough."}
}
func newMCPCmd() *cobra.Command {
	return &cobra.Command{Use: "mcp", Short: "Run the tatara MCP server."}
}
func newMCPConfigCmd() *cobra.Command {
	return &cobra.Command{Use: "mcp-config", Short: "Write/merge .mcp.json."}
}
