package cmd

import (
	"context"

	"github.com/szymonrychu/tatara-cli/internal/auth"
	"github.com/szymonrychu/tatara-cli/internal/client"
)

// loadToken returns the stored login token and its on-disk path. When no stored
// token exists it falls back to the client-credentials grant for headless use;
// the returned path is then empty, signalling buildClient to skip reload/refresh/save
// (client-creds tokens are re-minted per call by auth.AccessToken).
func loadToken(ctx context.Context) (*auth.Token, string, error) {
	tokenPath, err := auth.DefaultTokenPath()
	if err != nil {
		return nil, "", err
	}
	token, err := auth.LoadToken(tokenPath)
	if err != nil {
		tokStr, ccErr := auth.AccessToken(ctx)
		if ccErr != nil {
			return nil, "", ccErr
		}
		return &auth.Token{AccessToken: tokStr, TokenType: "Bearer"}, "", nil
	}
	return token, tokenPath, nil
}

// buildClient constructs a client for baseURL with token attached. When tokenPath
// is non-empty (a stored login token) it wires reload/refresh/save so the client
// transparently refreshes the token under a file lock.
func buildClient(baseURL string, token *auth.Token, tokenPath string) (*client.Client, error) {
	cfg := client.Config{
		BaseURL:   baseURL,
		Token:     token,
		TokenPath: tokenPath,
	}
	if tokenPath != "" {
		cfg.Reload = func() (*auth.Token, error) { return auth.LoadToken(tokenPath) }
		cfg.Refresh = func(ctx context.Context, t *auth.Token) (*auth.Token, error) {
			return auth.RefreshToken(ctx, DefaultIssuer, DefaultClientID, t, nil)
		}
		cfg.Save = func(t *auth.Token) error { return auth.SaveToken(tokenPath, t) }
	}
	return client.New(cfg)
}
