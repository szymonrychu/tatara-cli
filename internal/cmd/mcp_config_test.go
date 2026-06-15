package cmd_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/cmd"
)

func TestMCPConfig_WritesFreshFile(t *testing.T) {
	dir := t.TempDir()
	root := cmd.NewRootCmd()
	root.SetArgs([]string{"mcp-config", dir})
	require.NoError(t, root.Execute())

	b, err := os.ReadFile(filepath.Join(dir, ".mcp.json")) //nolint:gosec // test temp dir
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	servers := got["mcpServers"].(map[string]any)
	tatara := servers["tatara"].(map[string]any)
	require.NotEmpty(t, tatara["command"])
	args, ok := tatara["args"].([]any)
	require.True(t, ok)
	require.Equal(t, "mcp", args[0])
}

func TestMCPConfig_MergesIntoExistingFile(t *testing.T) {
	dir := t.TempDir()
	existing := map[string]any{
		"mcpServers": map[string]any{
			"other": map[string]any{"command": "/some/other/bin"},
		},
	}
	b, _ := json.Marshal(existing)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".mcp.json"), b, 0o644)) //nolint:gosec // test temp dir

	root := cmd.NewRootCmd()
	root.SetArgs([]string{"mcp-config", dir})
	require.NoError(t, root.Execute())

	raw, err := os.ReadFile(filepath.Join(dir, ".mcp.json")) //nolint:gosec // test temp dir
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))
	servers := got["mcpServers"].(map[string]any)
	require.NotNil(t, servers["other"], "existing entry preserved")
	require.NotNil(t, servers["tatara"], "tatara entry added")
}

func TestMCPConfig_RefusesExistingDifferentCommand(t *testing.T) {
	dir := t.TempDir()
	existing := map[string]any{
		"mcpServers": map[string]any{
			"tatara": map[string]any{"command": "/somewhere/else"},
		},
	}
	b, _ := json.Marshal(existing)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".mcp.json"), b, 0o644)) //nolint:gosec // test temp dir

	root := cmd.NewRootCmd()
	root.SetArgs([]string{"mcp-config", dir})
	require.Error(t, root.Execute())
}

func TestMCPConfig_ForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	existing := map[string]any{
		"mcpServers": map[string]any{
			"tatara": map[string]any{"command": "/somewhere/else"},
		},
	}
	b, _ := json.Marshal(existing)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".mcp.json"), b, 0o644)) //nolint:gosec // test temp dir

	root := cmd.NewRootCmd()
	root.SetArgs([]string{"mcp-config", dir, "--force"})
	require.NoError(t, root.Execute())

	raw, err := os.ReadFile(filepath.Join(dir, ".mcp.json")) //nolint:gosec // test temp dir
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))
	servers := got["mcpServers"].(map[string]any)
	tatara := servers["tatara"].(map[string]any)
	require.NotEqual(t, "/somewhere/else", tatara["command"])
}

func TestMCPConfig_SameCommandIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	root := cmd.NewRootCmd()
	root.SetArgs([]string{"mcp-config", dir})
	require.NoError(t, root.Execute())
	// Second invocation with the same binary path - should succeed without --force.
	root2 := cmd.NewRootCmd()
	root2.SetArgs([]string{"mcp-config", dir})
	require.NoError(t, root2.Execute())
}

// Finding 4: mcp-config must preserve user-added keys (env, timeout, cwd) when the
// command matches and no --force is given.
func TestMCPConfig_PreservesExtraKeysOnSameCommand(t *testing.T) {
	dir := t.TempDir()

	// First write so we know the real binary path.
	root := cmd.NewRootCmd()
	root.SetArgs([]string{"mcp-config", dir})
	require.NoError(t, root.Execute())

	// Now hand-add extra keys to the tatara entry.
	raw, err := os.ReadFile(filepath.Join(dir, ".mcp.json")) //nolint:gosec // test temp dir
	require.NoError(t, err)
	var cfg map[string]any
	require.NoError(t, json.Unmarshal(raw, &cfg))
	servers := cfg["mcpServers"].(map[string]any)
	tatara := servers["tatara"].(map[string]any)
	tatara["env"] = map[string]any{"TATARA_PROJECT": "myproject"}
	tatara["timeout"] = 30
	servers["tatara"] = tatara
	cfg["mcpServers"] = servers
	out, _ := json.MarshalIndent(cfg, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".mcp.json"), append(out, '\n'), 0o644)) //nolint:gosec // test temp dir

	// Run mcp-config again - same binary, no --force.
	root2 := cmd.NewRootCmd()
	root2.SetArgs([]string{"mcp-config", dir})
	require.NoError(t, root2.Execute())

	// Verify extra keys survived.
	raw2, err := os.ReadFile(filepath.Join(dir, ".mcp.json")) //nolint:gosec // test temp dir
	require.NoError(t, err)
	var cfg2 map[string]any
	require.NoError(t, json.Unmarshal(raw2, &cfg2))
	servers2 := cfg2["mcpServers"].(map[string]any)
	tatara2 := servers2["tatara"].(map[string]any)
	require.NotNil(t, tatara2["env"], "user-added env key must be preserved")
	require.NotNil(t, tatara2["timeout"], "user-added timeout key must be preserved")
}
