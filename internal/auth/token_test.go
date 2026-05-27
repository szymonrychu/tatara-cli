package auth_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/auth"
)

func TestTokenRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token.json")

	in := &auth.Token{
		AccessToken:  "at",
		RefreshToken: "rt",
		ExpiresAt:    time.Now().Add(time.Hour).UTC().Truncate(time.Second),
		TokenType:    "Bearer",
	}
	require.NoError(t, auth.SaveToken(path, in))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	got, err := auth.LoadToken(path)
	require.NoError(t, err)
	require.Equal(t, in.AccessToken, got.AccessToken)
	require.True(t, in.ExpiresAt.Equal(got.ExpiresAt))
}

func TestLoadTokenMissing(t *testing.T) {
	_, err := auth.LoadToken(filepath.Join(t.TempDir(), "absent.json"))
	require.ErrorIs(t, err, auth.ErrNoToken)
}

func TestDefaultTokenPathHonorsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-test")
	p, err := auth.DefaultTokenPath()
	require.NoError(t, err)
	require.Equal(t, "/tmp/xdg-test/tatara/token.json", p)
}
