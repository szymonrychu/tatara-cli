package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrRefreshExpired is returned when the server rejects the refresh token with
// invalid_grant (expired or revoked). Callers should stop retrying and prompt
// the user to run `tatara login` again.
var ErrRefreshExpired = errors.New("auth: refresh token expired (run `tatara login`)")

func RefreshToken(ctx context.Context, issuer, clientID string, t *Token, client *http.Client) (*Token, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	body := url.Values{}
	body.Set("grant_type", "refresh_token")
	body.Set("client_id", clientID)
	body.Set("refresh_token", t.RefreshToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		issuer+"/protocol/openid-connect/token", strings.NewReader(body.Encode()))
	if err != nil {
		return nil, fmt.Errorf("auth: refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth: refresh: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		var oauthErr struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &oauthErr)
		if oauthErr.Error == "invalid_grant" {
			return nil, ErrRefreshExpired
		}
		return nil, fmt.Errorf("auth: refresh %d: %s", resp.StatusCode, string(raw))
	}
	var tr struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if err := json.Unmarshal(raw, &tr); err != nil {
		return nil, fmt.Errorf("auth: parse refresh response: %w", err)
	}
	return &Token{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		IDToken:      tr.IDToken,
		ExpiresAt:    time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
		TokenType:    tr.TokenType,
	}, nil
}
