package auth_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/auth"
)

func newFakeIssuer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"token_endpoint":"http://%s/token"}`, r.Host)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		require.Equal(t, "client_credentials", r.Form.Get("grant_type"))
		require.Equal(t, "cid", r.Form.Get("client_id"))
		require.Equal(t, "secret", r.Form.Get("client_secret"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"tok-123","expires_in":300,"token_type":"Bearer"}`)
	})
	return httptest.NewServer(mux)
}

func TestClientCredentialsToken(t *testing.T) {
	srv := newFakeIssuer(t)
	defer srv.Close()

	tok, exp, err := auth.ClientCredentialsToken(context.Background(), srv.URL, "cid", "secret")
	require.NoError(t, err)
	require.Equal(t, "tok-123", tok)
	require.WithinDuration(t, time.Now().Add(300*time.Second), exp, 5*time.Second)
}

func TestTokenFallsBackToClientCreds(t *testing.T) {
	srv := newFakeIssuer(t)
	defer srv.Close()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no stored token
	t.Setenv("OIDC_ISSUER", srv.URL)
	t.Setenv("CLI_OIDC_CLIENT_ID", "cid")
	t.Setenv("CLI_OIDC_CLIENT_SECRET", "secret")
	auth.ResetTokenCache()
	tok, err := auth.AccessToken(context.Background())
	require.NoError(t, err)
	require.Equal(t, "tok-123", tok)
}

func TestTokenNoEnvStillErrNoToken(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OIDC_ISSUER", "")
	t.Setenv("CLI_OIDC_CLIENT_ID", "")
	t.Setenv("CLI_OIDC_CLIENT_SECRET", "")
	auth.ResetTokenCache()
	_, err := auth.AccessToken(context.Background())
	require.ErrorIs(t, err, auth.ErrNoToken)
}
