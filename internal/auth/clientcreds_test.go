package auth_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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

// stagedIssuer stands up a fake issuer whose discovery and token responses are
// both caller-controlled, so every failure branch of the mint is reachable.
func stagedIssuer(t *testing.T, disco, token http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", disco)
	mux.HandleFunc("/token", token)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// A mint failure is the ONE auth signal an agent pod has no other producer for,
// and there is no metrics egress from a stdio subprocess - so the stage has to
// ride the error itself. Nine branches collapsed onto one "error" cannot say
// which call failed; MintError.Stage can.
func TestClientCredentialsToken_StageOnEveryFailureBranch(t *testing.T) {
	okDisco := func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"token_endpoint":"http://%s/token"}`, r.Host)
	}
	notCalled := func(http.ResponseWriter, *http.Request) {
		t.Error("token endpoint must not be reached for this case")
	}

	cases := []struct {
		name  string
		stage string
		// issuer overrides the fake issuer URL entirely when non-empty.
		issuer string
		disco  http.HandlerFunc
		token  http.HandlerFunc
	}{
		{
			name: "discovery request build", stage: "discovery_request",
			// DEL is an invalid control character in a URL, so
			// http.NewRequestWithContext fails before any I/O.
			issuer: "http://\x7fbad.invalid",
		},
		{
			name: "discovery round-trip", stage: "discovery_call",
			issuer: "http://127.0.0.1:1",
		},
		{
			name: "discovery status", stage: "discovery_status",
			disco: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) },
			token: notCalled,
		},
		{
			name: "discovery decode", stage: "discovery_decode",
			disco: func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, `{}`) },
			token: notCalled,
		},
		{
			name: "token request build", stage: "token_request",
			disco: func(w http.ResponseWriter, _ *http.Request) {
				// The JSON \u007f escape decodes to DEL, an invalid URL control
				// character: the token request fails to build, before any I/O.
				_, _ = fmt.Fprint(w, "{\"token_endpoint\":\"http://\\u007fbad.invalid/token\"}")
			},
			token: notCalled,
		},
		{
			name: "token round-trip", stage: "token_call",
			disco: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprint(w, `{"token_endpoint":"http://127.0.0.1:1/token"}`)
			},
			token: notCalled,
		},
		{
			name: "token status", stage: "token_status",
			disco: okDisco,
			token: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) },
		},
		{
			name: "token decode", stage: "token_decode",
			disco: okDisco,
			token: func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, `not json`) },
		},
		{
			name: "token empty", stage: "token_empty",
			disco: okDisco,
			token: func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, `{"expires_in":300}`) },
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			issuer := c.issuer
			if issuer == "" {
				issuer = stagedIssuer(t, c.disco, c.token).URL
			}
			_, _, err := auth.ClientCredentialsToken(context.Background(), issuer, "cid", "secret")
			require.Error(t, err)
			assert.Equal(t, c.stage, auth.MintStage(err), "MintStage must name the failing branch")
			assert.Contains(t, err.Error(), "stage="+c.stage,
				"the stage must survive into the error text, which is what reaches the transcript")
		})
	}
}

// A successful mint carries no stage: MintStage on a non-mint error is empty.
func TestMintStage_EmptyForSuccessAndForeignErrors(t *testing.T) {
	srv := newFakeIssuer(t)
	defer srv.Close()

	_, _, err := auth.ClientCredentialsToken(context.Background(), srv.URL, "cid", "secret")
	require.NoError(t, err)
	assert.Empty(t, auth.MintStage(nil))
	assert.Empty(t, auth.MintStage(auth.ErrNoToken))
}

// The mint error must stay unwrappable so callers can still match on the
// underlying transport/decoding error.
func TestMintError_Unwraps(t *testing.T) {
	inner := errors.New("boom")
	err := error(&auth.MintError{Stage: "token_call", Err: inner})
	assert.ErrorIs(t, err, inner)
	assert.Equal(t, "token_call", auth.MintStage(err))
}

// Finding 2 (audit-r3): An expired stored token must fall through to client_credentials,
// not be returned as-is. We write a token file whose ExpiresAt is 2 minutes in the past,
// configure cc env vars pointing at a real fake issuer, and assert we get a fresh cc token.
func TestAccessTokenWithExpiry_ExpiredStoredTokenFallsBackToCC(t *testing.T) {
	srv := newFakeIssuer(t)
	defer srv.Close()

	// Write an expired token.json to a temp XDG_CONFIG_HOME.
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("OIDC_ISSUER", srv.URL)
	t.Setenv("CLI_OIDC_CLIENT_ID", "cid")
	t.Setenv("CLI_OIDC_CLIENT_SECRET", "secret")
	auth.ResetTokenCache()

	// Persist an expired token so LoadToken succeeds but the token is stale.
	expiredTok := auth.Token{
		AccessToken: "expired-tok",
		ExpiresAt:   time.Now().Add(-2 * time.Minute),
	}
	path, err := auth.DefaultTokenPath()
	require.NoError(t, err)
	require.NoError(t, auth.SaveToken(path, &expiredTok))

	tok, _, err := auth.AccessTokenWithExpiry(context.Background())
	require.NoError(t, err)
	require.Equal(t, "tok-123", tok, "expired stored token must NOT be returned; cc mint must be used instead")
}

// Finding 1 (audit-r3): AccessTokenWithExpiry must not hold ccMu across the HTTP round-trip.
// We verify this by counting concurrent in-flight discovery calls: if the lock is held
// during the network call the second goroutine can't enter at all, so maxConcurrent stays 1.
// With the lock released during the network call both goroutines overlap and maxConcurrent==2.
func TestAccessTokenWithExpiry_ConcurrentCallsDoNotSerialize(t *testing.T) {
	const callDelay = 60 * time.Millisecond
	var (
		inFlight    atomic.Int32
		maxInFlight atomic.Int32
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		cur := inFlight.Add(1)
		defer inFlight.Add(-1)
		// Update maxInFlight if this is a new peak.
		for {
			old := maxInFlight.Load()
			if cur <= old || maxInFlight.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(callDelay)
		_, _ = fmt.Fprintf(w, `{"token_endpoint":"http://%s/token"}`, r.Host)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"cc-tok","expires_in":300,"token_type":"Bearer"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("OIDC_ISSUER", srv.URL)
	t.Setenv("CLI_OIDC_CLIENT_ID", "cid")
	t.Setenv("CLI_OIDC_CLIENT_SECRET", "secret")
	auth.ResetTokenCache()

	errCh := make(chan error, 2)
	for range 2 {
		go func() {
			_, _, err := auth.AccessTokenWithExpiry(context.Background())
			errCh <- err
		}()
	}
	require.NoError(t, <-errCh)
	require.NoError(t, <-errCh)
	require.Equal(t, int32(2), maxInFlight.Load(),
		"both goroutines must overlap in the HTTP call (ccMu must not be held during network call)")
}

// Finding 3 (audit-r3): ClientCredentialsToken must return a clean error (not panic)
// when the issuer URL is malformed (contains a control character, making
// http.NewRequestWithContext return a non-nil error).
func TestClientCredentialsToken_MalformedIssuerReturnsError(t *testing.T) {
	// A URL with a control char (\x01) is rejected by http.NewRequestWithContext.
	malformed := "http://127.0.0.1/\x01bad"
	_, _, err := auth.ClientCredentialsToken(context.Background(), malformed, "cid", "secret")
	require.Error(t, err, "malformed issuer URL must return error, not panic")
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
