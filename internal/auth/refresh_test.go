package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/auth"
)

func TestRefreshToken(t *testing.T) {
	const clientID = "my-client"
	const oldRefresh = "old-rt"
	const newRefresh = "new-rt"
	const newAccess = "new-at"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/protocol/openid-connect/token", r.URL.Path)
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		form, err := url.ParseQuery(string(body))
		require.NoError(t, err)
		require.Equal(t, "refresh_token", form.Get("grant_type"))
		require.Equal(t, clientID, form.Get("client_id"))
		require.Equal(t, oldRefresh, form.Get("refresh_token"))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  newAccess,
			"refresh_token": newRefresh,
			"expires_in":    300,
			"token_type":    "Bearer",
		})
	}))
	defer srv.Close()

	old := &auth.Token{
		AccessToken:  "old-at",
		RefreshToken: oldRefresh,
		ExpiresAt:    time.Now().Add(-time.Minute),
		TokenType:    "Bearer",
	}

	tok, err := auth.RefreshToken(context.Background(), srv.URL, clientID, old, srv.Client())
	require.NoError(t, err)
	require.Equal(t, newAccess, tok.AccessToken)
	require.Equal(t, newRefresh, tok.RefreshToken)
	require.WithinDuration(t, time.Now().Add(300*time.Second), tok.ExpiresAt, 5*time.Second)
}

// Finding 2a: RefreshToken must reject a 200 response with empty access_token.
func TestRefreshTokenEmptyAccessToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Returns 200 but omits access_token.
		_, _ = w.Write([]byte(`{"refresh_token":"new-rt","expires_in":300,"token_type":"Bearer"}`))
	}))
	defer srv.Close()

	old := &auth.Token{AccessToken: "old-at", RefreshToken: "old-rt", ExpiresAt: time.Now().Add(-time.Minute)}
	_, err := auth.RefreshToken(context.Background(), srv.URL, "my-client", old, srv.Client())
	require.Error(t, err)
	require.Contains(t, err.Error(), "access_token")
}

// Finding 2b: RefreshToken must preserve old refresh token when response omits one.
func TestRefreshTokenPreservesOldRefreshTokenWhenOmitted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Returns new access_token but NO refresh_token (IdP chose not to rotate).
		_, _ = w.Write([]byte(`{"access_token":"new-at","expires_in":300,"token_type":"Bearer"}`))
	}))
	defer srv.Close()

	old := &auth.Token{AccessToken: "old-at", RefreshToken: "keep-rt", ExpiresAt: time.Now().Add(-time.Minute)}
	tok, err := auth.RefreshToken(context.Background(), srv.URL, "my-client", old, srv.Client())
	require.NoError(t, err)
	require.Equal(t, "new-at", tok.AccessToken)
	require.Equal(t, "keep-rt", tok.RefreshToken, "old refresh token must be preserved when response omits one")
}

func TestRefreshTokenInvalidGrantReturnsErrRefreshExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Token is not active"}`))
	}))
	defer srv.Close()

	old := &auth.Token{
		AccessToken:  "old-at",
		RefreshToken: "old-rt",
		ExpiresAt:    time.Now().Add(-time.Minute),
		TokenType:    "Bearer",
	}
	_, err := auth.RefreshToken(context.Background(), srv.URL, "my-client", old, srv.Client())
	require.Error(t, err)
	require.True(t, errors.Is(err, auth.ErrRefreshExpired), "expected ErrRefreshExpired, got: %v", err)
}
