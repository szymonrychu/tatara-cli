package cmd_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/auth"
	"github.com/szymonrychu/tatara-cli/internal/cmd"
)

func TestMCP_ErrsWhenNoToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)

	root := cmd.NewRootCmd()
	root.SetArgs([]string{"mcp"})
	err := root.Execute()
	require.Error(t, err)
	require.ErrorIs(t, err, auth.ErrNoToken)
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
