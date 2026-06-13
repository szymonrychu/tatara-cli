package cmd_test

import (
	"os"
	"testing"
)

// TestMain clears ambient configuration the runtime may have injected
// (project, service URLs, OIDC client credentials) so the command tests
// are hermetic regardless of the environment they run in. Tests that need
// a specific value set it explicitly with t.Setenv.
func TestMain(m *testing.M) {
	for _, k := range []string{
		"TATARA_TASK",
		"TATARA_PROJECT",
		"TATARA_MEMORY_URL",
		"TATARA_OPERATOR_URL",
		"TATARA_CHAT_URL",
		"OIDC_ISSUER",
		"CLI_OIDC_CLIENT_ID",
		"CLI_OIDC_CLIENT_SECRET",
		"XDG_CONFIG_HOME",
		"XDG_STATE_HOME",
	} {
		_ = os.Unsetenv(k)
	}
	os.Exit(m.Run())
}
