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

	"github.com/szymonrychu/tatara-cli/internal/obs"
)

// ccHTTPClient is a shared http.Client with a per-request timeout, matching
// the timeout used by device_flow.go and refresh.go.
var ccHTTPClient = &http.Client{Timeout: 30 * time.Second}

// ClientCredentialsToken performs an OIDC client_credentials grant against the
// issuer's discovered token endpoint and returns the access token + expiry.
func ClientCredentialsToken(ctx context.Context, issuer, clientID, clientSecret string) (string, time.Time, error) {
	disco := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, disco, nil) //nolint:gosec // issuer URL is operator-injected env
	resp, err := ccHTTPClient.Do(req)                                     //nolint:gosec // taint flows from the nolinted line above
	if err != nil {
		obs.ClientCredsMintTotal.WithLabelValues("error").Inc()
		return "", time.Time{}, fmt.Errorf("oidc discovery: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		obs.ClientCredsMintTotal.WithLabelValues("error").Inc()
		return "", time.Time{}, fmt.Errorf("oidc discovery: status %d", resp.StatusCode)
	}
	var meta struct {
		TokenEndpoint string `json:"token_endpoint"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil || meta.TokenEndpoint == "" {
		obs.ClientCredsMintTotal.WithLabelValues("error").Inc()
		return "", time.Time{}, errors.New("oidc discovery: no token_endpoint")
	}
	form := url.Values{"grant_type": {"client_credentials"}}
	treq, _ := http.NewRequestWithContext(ctx, http.MethodPost, meta.TokenEndpoint, strings.NewReader(form.Encode()))
	treq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Keycloak confidential clients default to client_secret_basic: send the
	// client credentials via HTTP Basic auth (client_secret_post 401s).
	treq.SetBasicAuth(clientID, clientSecret)
	tresp, err := ccHTTPClient.Do(treq) //nolint:gosec // token endpoint URL is from issuer discovery, operator-injected
	if err != nil {
		obs.ClientCredsMintTotal.WithLabelValues("error").Inc()
		return "", time.Time{}, fmt.Errorf("token request: %w", err)
	}
	defer func() { _ = tresp.Body.Close() }()
	if tresp.StatusCode != http.StatusOK {
		obs.ClientCredsMintTotal.WithLabelValues("error").Inc()
		return "", time.Time{}, fmt.Errorf("token request: status %d", tresp.StatusCode)
	}
	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(tresp.Body).Decode(&tr); err != nil {
		obs.ClientCredsMintTotal.WithLabelValues("error").Inc()
		return "", time.Time{}, fmt.Errorf("token decode: %w", err)
	}
	if tr.AccessToken == "" {
		obs.ClientCredsMintTotal.WithLabelValues("error").Inc()
		return "", time.Time{}, errors.New("token response: empty access_token")
	}
	obs.ClientCredsMintTotal.WithLabelValues("ok").Inc()
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
	path, err := DefaultTokenPath()
	if err == nil {
		if t, lerr := LoadToken(path); lerr == nil && t.AccessToken != "" {
			return t.AccessToken, t.ExpiresAt, nil
		}
	}
	issuer := os.Getenv("OIDC_ISSUER")
	id := os.Getenv("CLI_OIDC_CLIENT_ID")
	secret := os.Getenv("CLI_OIDC_CLIENT_SECRET")
	if issuer == "" || id == "" || secret == "" {
		return "", time.Time{}, ErrNoToken
	}
	ccMu.Lock()
	defer ccMu.Unlock()
	if ccTok != "" && time.Now().Before(ccExp.Add(-30*time.Second)) {
		return ccTok, ccExp, nil
	}
	tok, exp, err := ClientCredentialsToken(ctx, issuer, id, secret)
	if err != nil {
		return "", time.Time{}, err
	}
	ccTok, ccExp = tok, exp
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
