package cmd_test

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/auth"
	"github.com/szymonrychu/tatara-cli/internal/client"
	"github.com/szymonrychu/tatara-cli/internal/cmd"
)

func TestStatus_AuthStates(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(t *testing.T, dir string)
		wantContain []string
		wantAbsent  []string
	}{
		{
			name: "valid stored token",
			setup: func(t *testing.T, dir string) {
				saveToken(t, dir, time.Now().Add(time.Hour))
			},
			wantContain: []string{"logged in", "valid for"},
			wantAbsent:  []string{"expired", "not authenticated"},
		},
		{
			name: "expired stored token",
			setup: func(t *testing.T, dir string) {
				saveToken(t, dir, time.Now().Add(-3*time.Minute))
			},
			wantContain: []string{"logged in", "expired", "ago"},
			wantAbsent:  []string{"valid for"},
		},
		{
			name: "no token but client-credentials configured",
			setup: func(t *testing.T, _ string) {
				t.Setenv("OIDC_ISSUER", "https://issuer.example")
				t.Setenv("CLI_OIDC_CLIENT_ID", "id")
				t.Setenv("CLI_OIDC_CLIENT_SECRET", "secret")
			},
			wantContain: []string{"client-credentials configured"},
			wantAbsent:  []string{"logged in", "not authenticated"},
		},
		{
			name:        "no token and no client-credentials",
			setup:       func(_ *testing.T, _ string) {},
			wantContain: []string{"not authenticated", auth.ErrNoToken.Error()},
			wantAbsent:  []string{"logged in", "client-credentials"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", dir)
			tc.setup(t, dir)

			out := runStatus(t)
			for _, want := range tc.wantContain {
				require.Contains(t, out, want)
			}
			for _, absent := range tc.wantAbsent {
				require.NotContains(t, out, absent)
			}
		})
	}
}

func TestStatus_DefaultURLsAndProject(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	saveToken(t, dir, time.Now().Add(time.Hour))

	out := runStatus(t)
	require.Contains(t, out, client.DefaultBaseURL)
	require.Contains(t, out, client.DefaultOperatorBaseURL)
	require.Contains(t, out, client.DefaultChatBaseURL)
	require.Contains(t, out, "Project:  (none)")
	require.Contains(t, out, filepath.Join(dir, "tatara", "token.json"))
}

func TestStatus_FlagsAndProjectOverrideURLs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	saveToken(t, dir, time.Now().Add(time.Hour))

	out := runStatus(t,
		"--base-url", "https://mem.example",
		"--operator-base-url", "https://op.example",
		"--chat-base-url", "https://chat.example",
		"--project", "proj1",
	)
	require.Contains(t, out, "Memory:   https://mem.example/proj1")
	require.Contains(t, out, "Operator: https://op.example")
	require.Contains(t, out, "Chat:     https://chat.example")
	require.Contains(t, out, "Project:  proj1")
}

func TestStatus_EnvOverridesURLs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	saveToken(t, dir, time.Now().Add(time.Hour))
	t.Setenv("TATARA_MEMORY_URL", "https://mem.env")
	t.Setenv("TATARA_OPERATOR_URL", "https://op.env")
	t.Setenv("TATARA_CHAT_URL", "https://chat.env")

	out := runStatus(t)
	require.Contains(t, out, "Memory:   https://mem.env")
	require.Contains(t, out, "Operator: https://op.env")
	require.Contains(t, out, "Chat:     https://chat.env")
}

func saveToken(t *testing.T, dir string, exp time.Time) {
	t.Helper()
	tokenPath := filepath.Join(dir, "tatara", "token.json")
	require.NoError(t, auth.SaveToken(tokenPath, &auth.Token{
		AccessToken: "test-token",
		ExpiresAt:   exp,
		TokenType:   "Bearer",
	}))
}

func runStatus(t *testing.T, args ...string) string {
	t.Helper()
	root := cmd.NewRootCmd()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"status"}, args...))
	require.NoError(t, root.Execute())
	return out.String()
}
