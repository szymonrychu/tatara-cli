package auth_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/auth"
	"github.com/szymonrychu/tatara-cli/internal/obs"
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
		// Credentials must arrive via HTTP Basic auth (Keycloak client_secret_basic).
		id, secret, ok := r.BasicAuth()
		require.True(t, ok, "client credentials must be sent via Basic auth")
		require.Equal(t, "cid", id)
		require.Equal(t, "secret", secret)
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

// Finding 2: non-200 discovery response must surface status, not "no token_endpoint".
func TestClientCredentialsTokenDiscoveryNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("<html>Bad Gateway</html>"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	_, _, err := auth.ClientCredentialsToken(context.Background(), srv.URL, "cid", "secret")
	require.Error(t, err)
	require.Contains(t, err.Error(), "502", "error should mention the HTTP status")
}

// Finding: ClientCredsConfigured is the single source of truth for env-var presence.
func TestClientCredsConfigured(t *testing.T) {
	t.Run("all set", func(t *testing.T) {
		t.Setenv("OIDC_ISSUER", "https://issuer.example")
		t.Setenv("CLI_OIDC_CLIENT_ID", "id")
		t.Setenv("CLI_OIDC_CLIENT_SECRET", "secret")
		require.True(t, auth.ClientCredsConfigured())
	})
	t.Run("missing secret", func(t *testing.T) {
		t.Setenv("OIDC_ISSUER", "https://issuer.example")
		t.Setenv("CLI_OIDC_CLIENT_ID", "id")
		t.Setenv("CLI_OIDC_CLIENT_SECRET", "")
		require.False(t, auth.ClientCredsConfigured())
	})
	t.Run("all empty", func(t *testing.T) {
		t.Setenv("OIDC_ISSUER", "")
		t.Setenv("CLI_OIDC_CLIENT_ID", "")
		t.Setenv("CLI_OIDC_CLIENT_SECRET", "")
		require.False(t, auth.ClientCredsConfigured())
	})
}

// Finding 1: ClientCredentialsToken must reject a 200 response with empty access_token.
func TestClientCredentialsTokenEmptyAccessToken(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"token_endpoint":"http://%s/token"}`, r.Host)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Returns 200 but omits access_token (empty string in JSON).
		_, _ = fmt.Fprint(w, `{"expires_in":300,"token_type":"Bearer"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, _, err := auth.ClientCredentialsToken(context.Background(), srv.URL, "cid", "secret")
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty access_token")
}

// TestClientCredsMintTotal_IncrementedOnSuccess verifies that a successful cc mint
// increments obs.ClientCredsMintTotal{result="ok"} (hard rule 13).
func TestClientCredsMintTotal_IncrementedOnSuccess(t *testing.T) {
	srv := newFakeIssuer(t)
	defer srv.Close()

	before := testutil.ToFloat64(obs.ClientCredsMintTotal.WithLabelValues("ok"))
	_, _, err := auth.ClientCredentialsToken(context.Background(), srv.URL, "cid", "secret")
	require.NoError(t, err)
	after := testutil.ToFloat64(obs.ClientCredsMintTotal.WithLabelValues("ok"))
	assert.Equal(t, before+1, after, "ClientCredsMintTotal{ok} must increment on success")
}

// TestClientCredsMintTotal_IncrementedOnError verifies that a failed cc mint
// increments obs.ClientCredsMintTotal{result="error"} (hard rule 13).
func TestClientCredsMintTotal_IncrementedOnError(t *testing.T) {
	before := testutil.ToFloat64(obs.ClientCredsMintTotal.WithLabelValues("error"))
	_, _, err := auth.ClientCredentialsToken(context.Background(), "http://127.0.0.1:1", "cid", "secret")
	require.Error(t, err)
	after := testutil.ToFloat64(obs.ClientCredsMintTotal.WithLabelValues("error"))
	assert.Equal(t, before+1, after, "ClientCredsMintTotal{error} must increment on error")
}

// Finding: ClientCredentialsToken must respect context cancellation (proves it does not
// use http.DefaultClient with no timeout: a hung server + cancelled ctx must return promptly).
func TestClientCredentialsTokenRespectsContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Hang forever to simulate a blocked issuer.
		<-r.Context().Done()
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _, err := auth.ClientCredentialsToken(ctx, srv.URL, "cid", "secret")
	require.Error(t, err)
	require.Less(t, time.Since(start), 5*time.Second, "should cancel quickly, not hang")
}
