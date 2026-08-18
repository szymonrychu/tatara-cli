package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// MintError wraps a client_credentials mint failure with the stage at which it
// failed. The mint is the primary auth path for every agent pod and it has NINE
// distinct failure branches; a stdio subprocess has no metrics egress, so the
// stage has to travel on the error itself or the cause is unrecoverable. Stage
// is one of: discovery_request, discovery_call, discovery_status,
// discovery_decode, token_request, token_call, token_status, token_decode,
// token_empty.
type MintError struct {
	Stage string
	Err   error
}

func (e *MintError) Error() string {
	return fmt.Sprintf("client_credentials mint: stage=%s: %v", e.Stage, e.Err)
}

func (e *MintError) Unwrap() error { return e.Err }

// MintStage returns the stage of the first *MintError in err's chain, or "" when
// err is nil or carries no mint failure.
func MintStage(err error) string {
	var me *MintError
	if errors.As(err, &me) {
		return me.Stage
	}
	return ""
}

// mintErr builds the staged error returned by every ClientCredentialsToken
// failure branch.
func mintErr(stage string, err error) (string, time.Time, error) {
	return "", time.Time{}, &MintError{Stage: stage, Err: err}
}

// ccHTTPClient is a shared http.Client with a per-request timeout, matching
// the timeout used by device_flow.go and refresh.go.
var ccHTTPClient = &http.Client{Timeout: 30 * time.Second}

// ClientCredentialsToken performs an OIDC client_credentials grant against the
// issuer's discovered token endpoint and returns the access token + expiry.
func ClientCredentialsToken(ctx context.Context, issuer, clientID, clientSecret string) (string, time.Time, error) {
	disco := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, disco, nil) //nolint:gosec // issuer URL is operator-injected env
	if err != nil {
		return mintErr("discovery_request", fmt.Errorf("oidc discovery request: %w", err))
	}
	resp, err := ccHTTPClient.Do(req) //nolint:gosec // taint flows from the nolinted line above
	if err != nil {
		return mintErr("discovery_call", fmt.Errorf("oidc discovery: %w", err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return mintErr("discovery_status", fmt.Errorf("oidc discovery: status %d", resp.StatusCode))
	}
	var meta struct {
		TokenEndpoint string `json:"token_endpoint"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil || meta.TokenEndpoint == "" {
		return mintErr("discovery_decode", errors.New("oidc discovery: no token_endpoint"))
	}
	form := url.Values{"grant_type": {"client_credentials"}}
	treq, err := http.NewRequestWithContext(ctx, http.MethodPost, meta.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return mintErr("token_request", fmt.Errorf("token request build: %w", err))
	}
	treq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Keycloak confidential clients default to client_secret_basic: send the
	// client credentials via HTTP Basic auth (client_secret_post 401s).
	treq.SetBasicAuth(clientID, clientSecret)
	tresp, err := ccHTTPClient.Do(treq) //nolint:gosec // token endpoint URL is from issuer discovery, operator-injected
	if err != nil {
		return mintErr("token_call", fmt.Errorf("token request: %w", err))
	}
	defer func() { _ = tresp.Body.Close() }()
	if tresp.StatusCode != http.StatusOK {
		return mintErr("token_status", fmt.Errorf("token request: status %d", tresp.StatusCode))
	}
	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(tresp.Body).Decode(&tr); err != nil {
		return mintErr("token_decode", fmt.Errorf("token decode: %w", err))
	}
	if tr.AccessToken == "" {
		return mintErr("token_empty", errors.New("token response: empty access_token"))
	}
	return tr.AccessToken, time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second), nil
}

var (
	ccMu  sync.Mutex
	ccTok string
	ccExp time.Time
)

// AccessToken returns the bearer token to use for API calls. It prefers a stored
// token (from tatara login). If none exists and OIDC_ISSUER / CLI_OIDC_CLIENT_ID /
// CLI_OIDC_CLIENT_SECRET are set, it mints a client_credentials token and caches
// it in memory, refreshing when within 30s of expiry. Returns ErrNoToken when
// neither path is available.
func AccessToken(ctx context.Context) (string, error) {
	tok, _, err := AccessTokenWithExpiry(ctx)
	return tok, err
}

// AccessTokenWithExpiry is like AccessToken but also returns the token expiry.
// For client_credentials tokens the expiry is sourced from the OIDC expires_in
// response field. For stored (device-flow) tokens the expiry is whatever was
// saved in the token file.
func AccessTokenWithExpiry(ctx context.Context) (string, time.Time, error) {
	// Only use the stored token if it is still fresh (>30s remaining). An expired
	// stored token must fall through to the client_credentials grant so that an
	// agent pod with a stale token.json on its config volume does not return a 401.
	path, err := DefaultTokenPath()
	if err == nil {
		if t, lerr := LoadToken(path); lerr == nil && t.AccessToken != "" && time.Until(t.ExpiresAt) > 30*time.Second {
			return t.AccessToken, t.ExpiresAt, nil
		}
	}
	issuer := os.Getenv("OIDC_ISSUER")
	id := os.Getenv("CLI_OIDC_CLIENT_ID")
	secret := os.Getenv("CLI_OIDC_CLIENT_SECRET")
	if issuer == "" || id == "" || secret == "" {
		return "", time.Time{}, ErrNoToken
	}
	// Check the in-memory cache under the lock. If the cached token is still fresh
	// return it immediately without making a network call.
	ccMu.Lock()
	if ccTok != "" && time.Now().Before(ccExp.Add(-30*time.Second)) {
		tok, exp := ccTok, ccExp
		ccMu.Unlock()
		return tok, exp, nil
	}
	ccMu.Unlock()
	// Mint a new token WITHOUT holding ccMu. ClientCredentialsToken makes two HTTP
	// round-trips (discovery + token) each up to 30s. Holding the lock across those
	// calls would serialize every goroutine needing a token behind one slow OIDC call.
	tok, exp, err := ClientCredentialsToken(ctx, issuer, id, secret)
	if err != nil {
		return "", time.Time{}, err
	}
	// Store the fresh token under the lock, but only if it is still newer than what
	// another concurrent goroutine may have already written (last-write-wins is fine
	// since tokens from the same issuer are interchangeable; we just avoid clobbering
	// a newer entry with an older one in a race).
	ccMu.Lock()
	if exp.After(ccExp) {
		ccTok, ccExp = tok, exp
	}
	ccMu.Unlock()
	return tok, exp, nil
}

// ResetTokenCache clears the in-memory client-credentials token cache. Used in tests.
func ResetTokenCache() {
	ccMu.Lock()
	ccTok, ccExp = "", time.Time{}
	ccMu.Unlock()
}

// ClientCredsConfigured reports whether the three env vars required for the
// client_credentials grant are all non-empty. This is the single authoritative
// definition; status.go calls this instead of re-encoding the var names.
func ClientCredsConfigured() bool {
	return os.Getenv("OIDC_ISSUER") != "" &&
		os.Getenv("CLI_OIDC_CLIENT_ID") != "" &&
		os.Getenv("CLI_OIDC_CLIENT_SECRET") != ""
}
