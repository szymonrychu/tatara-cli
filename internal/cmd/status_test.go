package cmd_test

import (
	"bytes"
	"path/filepath"
	"strings"
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

			// Assert only against the "Auth:" line: t.TempDir() bakes the
			// subtest name into the token path, so checking the whole output
			// for substrings like "client-credentials" would match the path.
			authLine := strings.SplitN(runStatus(t), "\n", 2)[0]
			for _, want := range tc.wantContain {
				require.Contains(t, authLine, want)
			}
			for _, absent := range tc.wantAbsent {
				require.NotContains(t, authLine, absent)
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
		"--project", "proj1",
	)
	require.Contains(t, out, "Memory:   https://mem.example/proj1")
	require.Contains(t, out, "Operator: https://op.example")
	require.Contains(t, out, "Project:  proj1")
}

func TestStatus_EnvOverridesURLs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	saveToken(t, dir, time.Now().Add(time.Hour))
	t.Setenv("TATARA_MEMORY_URL", "https://mem.env")
	t.Setenv("TATARA_OPERATOR_URL", "https://op.env")

	out := runStatus(t)
	require.Contains(t, out, "Memory:   https://mem.env")
	require.Contains(t, out, "Operator: https://op.env")
}

// tatara-cli#88/#91: TATARA_MEMORY_URL is already project-scoped (one
// root-mounted tatara-memory deployment per project), unlike --base-url or
// the config file, so a --project flag must NOT get appended to it. Mirrors
// TestRaw_TargetMemoryEnvSourcedBaseNotProjectPrefixed in raw_test.go so all
// three call sites (mcp/raw/status) carry symmetric call-site coverage, not
// just the shared resolver's unit tests.
func TestStatus_EnvMemoryURLWithProjectNotPrefixed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	saveToken(t, dir, time.Now().Add(time.Hour))
	t.Setenv("TATARA_MEMORY_URL", "https://mem.env")

	out := runStatus(t, "--project", "proj")
	require.Contains(t, out, "Memory:   https://mem.env")
	require.NotContains(t, out, "https://mem.env/proj")
	require.Contains(t, out, "Project:  proj")
}

// A token that expires exactly now is already slightly in the past by the time
// expiryDesc runs. After the sign-before-rounding fix it must report "expired",
// not "valid for 0s". A token expiring 1s in the future must still report "valid for".
func TestStatus_ExpiryAtExactlyNow(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	// ExpiresAt = time.Now() is already expired by the time the status command
	// reads it. After the fix (sign checked before rounding) it shows "expired".
	saveToken(t, dir, time.Now())
	authLine := strings.SplitN(runStatus(t), "\n", 2)[0]
	require.Contains(t, authLine, "expired", "token expiring exactly now is already in the past; must show expired")
	require.NotContains(t, authLine, "valid for")
}

// Finding 3: expiryDesc must check sign before rounding so a sub-second-expired
// token reports "expired" not "valid for 0s".
func TestStatus_SubSecondExpiredShowsExpired(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	// 1ms in the past - old code rounds to 0s -> "valid for 0s"; new code -> "expired 0s ago"
	saveToken(t, dir, time.Now().Add(-time.Millisecond))
	authLine := strings.SplitN(runStatus(t), "\n", 2)[0]
	require.Contains(t, authLine, "expired", "sub-second-expired token must report expired, not valid for 0s")
	require.NotContains(t, authLine, "valid for")
}

// A token expiring 2s from now must still report "valid for".
func TestStatus_FutureTokenShowsValidFor(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	saveToken(t, dir, time.Now().Add(2*time.Second))
	authLine := strings.SplitN(runStatus(t), "\n", 2)[0]
	require.Contains(t, authLine, "valid for")
	require.NotContains(t, authLine, "expired")
}

// Finding 3: status clientCredsConfigured delegates to auth so env-var contract lives once.
// We exercise this indirectly: set the three env vars, confirm status reports client-credentials.
func TestStatus_ClientCredsConfiguredDelegates(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("OIDC_ISSUER", "https://issuer.example")
	t.Setenv("CLI_OIDC_CLIENT_ID", "id")
	t.Setenv("CLI_OIDC_CLIENT_SECRET", "secret")
	authLine := strings.SplitN(runStatus(t), "\n", 2)[0]
	require.Contains(t, authLine, "client-credentials configured")
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

// Symmetric with TestRaw_EmptyMemoryURLIsNotThePublicDefault and
// TestMCP_StartsWithEmptyMemoryURL: status must report the pod as having no
// memory backend, never the shared public default.
func TestStatus_EmptyMemoryURLReportsNotConfigured(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	saveToken(t, dir, time.Now().Add(time.Hour))
	t.Setenv("TATARA_MEMORY_URL", "")

	out := runStatus(t)
	require.Contains(t, out, "not configured")
	require.NotContains(t, out, client.DefaultBaseURL)
}
