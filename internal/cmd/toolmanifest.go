package cmd

import (
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/szymonrychu/tatara-cli/internal/mcp"
)

// newToolManifestCmd emits the machine-readable tool surface (tool names and
// their top-level enum fields) as JSON on stdout. Published as a release
// artifact so downstream repos (agent-skills CI) can lint documented tool
// calls against the real schema instead of drifting silently.
func newToolManifestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tool-manifest",
		Short: "Print the MCP tool surface (names and enum fields) as JSON.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(mcp.GenerateToolManifest())
		},
	}
}
