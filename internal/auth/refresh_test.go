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
