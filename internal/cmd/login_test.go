package cmd_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/auth"
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

// TestLogin_AccessDeniedGivesFriendlyMessage verifies finding 3: when the device
// flow returns ErrAccessDenied the user sees a human-readable message, not the
// bare "access_denied" sentinel text.
func TestLogin_AccessDeniedGivesFriendlyMessage(t *testing.T) {
	// Stand up a fake OIDC server that immediately returns access_denied on the
	// token endpoint (simulates the user clicking "deny" in their browser).
	mux := http.NewServeMux()
	mux.HandleFunc("/protocol/openid-connect/auth/device", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return a minimal device code with a short expiry so Poll fires immediately.
		_, _ = w.Write([]byte(`{"device_code":"dc","user_code":"UC-123","verification_uri":"http://example.com/auth","verification_uri_complete":"http://example.com/auth?code=UC-123","expires_in":60,"interval":0}`))
	})
	mux.HandleFunc("/protocol/openid-connect/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"access_denied"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("OIDC_ISSUER", srv.URL)

	root := cmd.NewRootCmd()
	root.SetArgs([]string{"login"})
	err := root.Execute()

	require.Error(t, err)
	// Must NOT return the bare "access_denied" sentinel; must give guidance.
	require.NotEqual(t, auth.ErrAccessDenied.Error(), err.Error(),
		"ErrAccessDenied must not be returned verbatim; user needs a friendly message")
	require.Contains(t, err.Error(), "rerun",
		"error message must guide user to retry the login flow")
}
