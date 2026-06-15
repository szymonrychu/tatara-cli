package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newMCPConfigCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "mcp-config DIR",
		Short: "Write or merge a .mcp.json entry registering tatara in DIR.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]
			bin, err := os.Executable()
			if err != nil {
				return err
			}
			abs, err := filepath.Abs(bin)
			if err != nil {
				return err
			}

			path := filepath.Join(dir, ".mcp.json")
			cfg := map[string]any{}
			if b, err := os.ReadFile(path); err == nil { //nolint:gosec // path is caller-supplied project dir
				if err := json.Unmarshal(b, &cfg); err != nil {
					return fmt.Errorf("mcp-config: parse existing %s: %w", path, err)
				}
			}
			servers, _ := cfg["mcpServers"].(map[string]any)
			if servers == nil {
				servers = map[string]any{}
			}
			if existing, ok := servers["tatara"].(map[string]any); ok {
				if !force && existing["command"] != abs {
					return fmt.Errorf("mcp-config: %s already has a tatara entry pointing at %v; pass --force to overwrite", path, existing["command"])
				}
				// Merge: preserve user-added keys (env, cwd, timeout, etc.);
				// only update command and args so we don't discard hand-edited fields.
				existing["command"] = abs
				existing["args"] = []string{"mcp"}
				servers["tatara"] = existing
			} else {
				servers["tatara"] = map[string]any{
					"command": abs,
					"args":    []string{"mcp"},
				}
			}
			cfg["mcpServers"] = servers

			out, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return err
			}
			if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil { //nolint:gosec // .mcp.json is committed in repos; 0o644 is correct
				return err
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Wrote tatara MCP entry to %s\n", path)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing tatara entry that points at a different command")
	return cmd
}
