package cmd_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/cmd"
)

func TestRootCmd_RegistersAllSubcommands(t *testing.T) {
	root := cmd.NewRootCmd()
	names := []string{}
	for _, c := range root.Commands() {
		names = append(names, c.Name())
	}
	require.ElementsMatch(t, []string{"login", "logout", "raw", "mcp", "mcp-config"}, names)
}

func TestRootCmd_HasPersistentFlags(t *testing.T) {
	root := cmd.NewRootCmd()
	require.NotNil(t, root.PersistentFlags().Lookup("base-url"))
	require.NotNil(t, root.PersistentFlags().Lookup("verbose"))
	require.NotNil(t, root.PersistentFlags().Lookup("project"))
}

func TestRootCmd_ProjectFlagShorthand(t *testing.T) {
	root := cmd.NewRootCmd()
	f := root.PersistentFlags().ShorthandLookup("p")
	require.NotNil(t, f)
	require.Equal(t, "project", f.Name)
}

func TestRootCmd_HelpWorks(t *testing.T) {
	root := cmd.NewRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{"--help"})
	require.NoError(t, root.Execute())
	require.Contains(t, buf.String(), "login")
	require.Contains(t, buf.String(), "mcp-config")
}
