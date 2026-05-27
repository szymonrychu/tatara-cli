package cmd

import "github.com/spf13/cobra"

func newMCPConfigCmd() *cobra.Command {
	return &cobra.Command{Use: "mcp-config", Short: "Write/merge .mcp.json."}
}
