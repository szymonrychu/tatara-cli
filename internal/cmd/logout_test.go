package cmd_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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

// TestLogout_EmitsStructuredInfoLog verifies hard rule 12: logout emits a
// slog JSON INFO record with action="logout" (not just fmt.Fprintln).
func TestLogout_EmitsStructuredInfoLog(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	tokenPath := filepath.Join(dir, "tatara", "token.json")
	require.NoError(t, auth.SaveToken(tokenPath, &auth.Token{
		AccessToken: "x",
		ExpiresAt:   time.Now().Add(time.Hour),
	}))

	var buf bytes.Buffer
	root := cmd.NewRootCmd()
	root.SetArgs([]string{"-v", "logout"})
	root.SetErr(&buf)
	require.NoError(t, root.Execute())

	// At least one JSON line must carry action=logout and level=INFO.
	found := false
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec["action"] == "logout" && rec["level"] == "INFO" {
			found = true
		}
	}
	assert.True(t, found, "logout must emit a JSON slog INFO record with action=logout; stderr: %s", buf.String())
}
