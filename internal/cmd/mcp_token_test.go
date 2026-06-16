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
	tok, path, ccRefresh := resolveMCPToken(context.Background(), logger)

	require.Nil(t, tok, "no credentials must yield a nil token, not an error")
	require.Empty(t, path, "no stored token means no token path to reload from")
	require.Nil(t, ccRefresh, "no cc env means no cc refresh func")
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
	tok, path, ccRefresh := resolveMCPToken(context.Background(), logger)

	require.NotNil(t, tok, "cc creds must yield a token")
	require.Equal(t, "cc-tok", tok.AccessToken)
	require.Empty(t, path, "cc path has no stored token file")
	require.False(t, tok.ExpiresAt.IsZero(), "cc token must have a non-zero ExpiresAt so freshness math works")
	require.WithinDuration(t, time.Now().Add(300*time.Second), tok.ExpiresAt, 10*time.Second)
	require.NotNil(t, ccRefresh, "cc creds must wire a refresh func so MCP server self-heals on expiry")

	// Calling the refresh func must return a fresh token (verifies the func is wired correctly).
	refreshed, err := ccRefresh(context.Background(), tok)
	require.NoError(t, err)
	require.NotNil(t, refreshed)
	require.False(t, refreshed.ExpiresAt.IsZero(), "refreshed token must have ExpiresAt set")
}
