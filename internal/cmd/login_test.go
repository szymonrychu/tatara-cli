package cmd_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/cmd"
)

func TestLogin_RegisteredAsSubcommand(t *testing.T) {
	root := cmd.NewRootCmd()
	var login bool
	for _, c := range root.Commands() {
		if c.Name() == "login" {
			login = true
			require.NotNil(t, c.RunE, "login must have a runnable body")
		}
	}
	require.True(t, login)
}
