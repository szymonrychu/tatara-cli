package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/auth"
)

func newFakeIssuerForCmd(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"token_endpoint":"http://%s/token"}`, r.Host)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"cc-tok","expires_in":300,"token_type":"Bearer"}`)
	})
	return httptest.NewServer(mux)
}

func TestResolveMCPToken_NoCredentialsStartsUnauthenticated(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("OIDC_ISSUER", "")
	t.Setenv("CLI_OIDC_CLIENT_ID", "")
	t.Setenv("CLI_OIDC_CLIENT_SECRET", "")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	creds := resolveMCPToken(context.Background(), logger)

	require.Nil(t, creds.token, "no credentials must yield a nil token, not an error")
	require.Empty(t, creds.tokenPath, "no stored token means no token path to reload from")
	require.Nil(t, creds.ccRefresh, "no cc env means no cc refresh func")
	require.Contains(t, creds.authNote, "UNAUTHENTICATED",
		"an unauthenticated start must produce a note the agent can carry out of the pod")
	require.Contains(t, creds.authNote, "tatara login",
		"the note must name the remedy for a pod with no OIDC env at all")
}

// The mint stage is the whole point of the note: nine failure branches collapse
// into "every tool call 401s" without it, and no metric leaves this process.
func TestResolveMCPToken_AuthNoteCarriesTheMintStage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" {
			_, _ = fmt.Fprintf(w, `{"token_endpoint":"http://%s/token"}`, r.Host)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	auth.ResetTokenCache()
	defer auth.ResetTokenCache()

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OIDC_ISSUER", srv.URL)
	t.Setenv("CLI_OIDC_CLIENT_ID", "cid")
	t.Setenv("CLI_OIDC_CLIENT_SECRET", "secret")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	creds := resolveMCPToken(context.Background(), logger)

	require.Nil(t, creds.token, "a failed mint must still start the server, unauthenticated")
	require.Contains(t, creds.authNote, "stage=token_status",
		"the note must name WHICH of the nine mint branches failed")
}

// Finding 1: resolveMCPToken must wire a cc refresh func and set ExpiresAt when
// running under client-credentials auth (no stored token, OIDC env set). Without
// this the long-lived MCP server gets 401 after the cc token's ~5 min expiry.
func TestResolveMCPToken_CCCredsWireRefreshAndExpiry(t *testing.T) {
	srv := newFakeIssuerForCmd(t)
	defer srv.Close()
	auth.ResetTokenCache()
	defer auth.ResetTokenCache()

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OIDC_ISSUER", srv.URL)
	t.Setenv("CLI_OIDC_CLIENT_ID", "cid")
	t.Setenv("CLI_OIDC_CLIENT_SECRET", "secret")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	creds := resolveMCPToken(context.Background(), logger)

	require.NotNil(t, creds.token, "cc creds must yield a token")
	require.Equal(t, "cc-tok", creds.token.AccessToken)
	require.Empty(t, creds.tokenPath, "cc path has no stored token file")
	require.False(t, creds.token.ExpiresAt.IsZero(), "cc token must have a non-zero ExpiresAt so freshness math works")
	require.WithinDuration(t, time.Now().Add(300*time.Second), creds.token.ExpiresAt, 10*time.Second)
	require.NotNil(t, creds.ccRefresh, "cc creds must wire a refresh func so MCP server self-heals on expiry")
	require.Empty(t, creds.authNote, "an authenticated start must add nothing to any tool result")

	// Calling the refresh func must return a fresh token (verifies the func is wired correctly).
	refreshed, err := creds.ccRefresh(context.Background(), creds.token)
	require.NoError(t, err)
	require.NotNil(t, refreshed)
	require.False(t, refreshed.ExpiresAt.IsZero(), "refreshed token must have ExpiresAt set")
}
