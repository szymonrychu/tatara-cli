package cmd_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/auth"
	"github.com/szymonrychu/tatara-cli/internal/cmd"
)

func TestLogout_DeletesToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	tokenPath := filepath.Join(dir, "tatara", "token.json")
	require.NoError(t, auth.SaveToken(tokenPath, &auth.Token{
		AccessToken: "x",
		ExpiresAt:   time.Now().Add(time.Hour),
	}))

	root := cmd.NewRootCmd()
	root.SetArgs([]string{"logout"})
	require.NoError(t, root.Execute())

	_, err := os.Stat(tokenPath)
	require.True(t, os.IsNotExist(err), "token file should be gone")
}

func TestLogout_IdempotentWhenNoToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	root := cmd.NewRootCmd()
	root.SetArgs([]string{"logout"})
	require.NoError(t, root.Execute())
}
