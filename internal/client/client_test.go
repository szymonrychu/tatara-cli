package client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/auth"
)

func freshToken() *auth.Token {
	return &auth.Token{
		AccessToken: "abc",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		TokenType:   "Bearer",
	}
}

func nearExpiryToken() *auth.Token {
	return &auth.Token{
		AccessToken: "old-token",
		ExpiresAt:   time.Now().Add(10 * time.Second),
		TokenType:   "Bearer",
	}
}

func testServer(handler http.HandlerFunc) (*httptest.Server, func()) {
	srv := httptest.NewServer(handler)
	return srv, srv.Close
}

func TestClient_AddsBearerHeader(t *testing.T) {
	var gotAuth string
	srv, cleanup := testServer(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	})
	defer cleanup()

	c, err := New(Config{BaseURL: srv.URL, Token: freshToken()})
	require.NoError(t, err)

	resp, err := c.Do(context.Background(), http.MethodGet, "/", nil)
	require.NoError(t, err)
	_ = resp.Body.Close()

	assert.Equal(t, "Bearer abc", gotAuth)
}

func TestClient_AddsAcceptHeader(t *testing.T) {
	var gotAccept string
	srv, cleanup := testServer(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		w.WriteHeader(http.StatusOK)
	})
	defer cleanup()

	c, err := New(Config{BaseURL: srv.URL, Token: freshToken()})
	require.NoError(t, err)

	resp, err := c.Do(context.Background(), http.MethodGet, "/path", nil)
	require.NoError(t, err)
	_ = resp.Body.Close()

	assert.Equal(t, "application/json", gotAccept)
}

func TestClient_SetsContentTypeWhenBody(t *testing.T) {
	var gotCT string
	srv, cleanup := testServer(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	})
	defer cleanup()

	c, err := New(Config{BaseURL: srv.URL, Token: freshToken()})
	require.NoError(t, err)

	resp, err := c.Do(context.Background(), http.MethodPost, "/", map[string]string{"key": "value"})
	require.NoError(t, err)
	_ = resp.Body.Close()

	assert.Equal(t, "application/json", gotCT)
}

func TestClient_NoContentTypeWhenNoBody(t *testing.T) {
	var gotCT string
	srv, cleanup := testServer(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	})
	defer cleanup()

	c, err := New(Config{BaseURL: srv.URL, Token: freshToken()})
	require.NoError(t, err)

	resp, err := c.Do(context.Background(), http.MethodGet, "/", nil)
	require.NoError(t, err)
	_ = resp.Body.Close()

	assert.Equal(t, "", gotCT)
}

func TestClient_EncodesStructBody(t *testing.T) {
	var gotBody []byte
	srv, cleanup := testServer(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})
	defer cleanup()

	c, err := New(Config{BaseURL: srv.URL, Token: freshToken()})
	require.NoError(t, err)

	resp, err := c.Do(context.Background(), http.MethodPost, "/", map[string]string{"hello": "world"})
	require.NoError(t, err)
	_ = resp.Body.Close()

	assert.JSONEq(t, `{"hello":"world"}`, string(gotBody))
}

func TestClient_PassesBytesBodyUnchanged(t *testing.T) {
	raw := []byte(`raw bytes payload`)
	var gotBody []byte
	srv, cleanup := testServer(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})
	defer cleanup()

	c, err := New(Config{BaseURL: srv.URL, Token: freshToken()})
	require.NoError(t, err)

	resp, err := c.Do(context.Background(), http.MethodPost, "/", raw)
	require.NoError(t, err)
	_ = resp.Body.Close()

	assert.Equal(t, raw, gotBody)
}

func TestClient_RefreshTriggeredOnNearExpiry(t *testing.T) {
	newToken := &auth.Token{
		AccessToken: "new-token",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		TokenType:   "Bearer",
	}

	var gotAuth string
	srv, cleanup := testServer(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	})
	defer cleanup()

	refreshCalled := false
	var savedToken *auth.Token

	c, err := New(Config{
		BaseURL: srv.URL,
		Token:   nearExpiryToken(),
		Refresh: func(ctx context.Context, t *auth.Token) (*auth.Token, error) {
			refreshCalled = true
			return newToken, nil
		},
		Save: func(t *auth.Token) error {
			savedToken = t
			return nil
		},
	})
	require.NoError(t, err)

	resp, err := c.Do(context.Background(), http.MethodGet, "/", nil)
	require.NoError(t, err)
	_ = resp.Body.Close()

	assert.True(t, refreshCalled, "RefreshFunc should have been called")
	assert.Equal(t, "Bearer new-token", gotAuth, "request should use refreshed token")
	assert.Equal(t, newToken, savedToken, "Save should have been called with new token")
}

func TestClient_NoRefreshWhenFresh(t *testing.T) {
	srv, cleanup := testServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	defer cleanup()

	refreshCalled := false

	c, err := New(Config{
		BaseURL: srv.URL,
		Token:   freshToken(),
		Refresh: func(ctx context.Context, t *auth.Token) (*auth.Token, error) {
			refreshCalled = true
			return t, nil
		},
	})
	require.NoError(t, err)

	resp, err := c.Do(context.Background(), http.MethodGet, "/", nil)
	require.NoError(t, err)
	_ = resp.Body.Close()

	assert.False(t, refreshCalled, "RefreshFunc should NOT have been called for a fresh token")
}

func TestClient_ErrNoTokenWhenAbsent(t *testing.T) {
	srv, cleanup := testServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	defer cleanup()

	c, err := New(Config{BaseURL: srv.URL, Token: nil})
	require.NoError(t, err)

	_, err = c.Do(context.Background(), http.MethodGet, "/", nil)
	assert.True(t, errors.Is(err, auth.ErrNoToken), "expected ErrNoToken, got: %v", err)
}

func TestClient_BaseURLRequired(t *testing.T) {
	_, err := New(Config{})
	assert.Error(t, err)
}

func TestClient_RefreshHonorsConcurrentReload(t *testing.T) {
	// Token is near-expiry on construction; Reload returns a freshly-refreshed
	// token simulating a parallel process that beat us to the lock.
	// Verify the Client uses the reloaded token and does NOT call Refresh.

	nearExpiry := &auth.Token{
		AccessToken: "stale",
		ExpiresAt:   time.Now().Add(10 * time.Second),
		TokenType:   "Bearer",
	}
	freshFromDisk := &auth.Token{
		AccessToken: "fresh-from-disk",
		ExpiresAt:   time.Now().Add(time.Hour),
		TokenType:   "Bearer",
	}
	refreshCalled := 0
	var seenAuth string
	srv, cleanup := testServer(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	})
	defer cleanup()

	cli, err := New(Config{
		BaseURL:   srv.URL,
		Token:     nearExpiry,
		TokenPath: filepath.Join(t.TempDir(), "token.json"),
		Reload:    func() (*auth.Token, error) { return freshFromDisk, nil },
		Refresh: func(ctx context.Context, t *auth.Token) (*auth.Token, error) {
			refreshCalled++
			return &auth.Token{AccessToken: "did-network-refresh", ExpiresAt: time.Now().Add(time.Hour)}, nil //nolint:gosec // test-only token literal
		},
	})
	require.NoError(t, err)

	resp, err := cli.Do(context.Background(), http.MethodGet, "/x", nil)
	require.NoError(t, err)
	_ = resp.Body.Close()

	require.Equal(t, 0, refreshCalled, "Reload returned a fresh token; Refresh must not be called")
	require.Equal(t, "Bearer fresh-from-disk", seenAuth)
}

func TestClient_PassesReaderBodyUnchanged(t *testing.T) {
	payload := "reader payload"
	var gotBody []byte
	srv, cleanup := testServer(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})
	defer cleanup()

	c, err := New(Config{BaseURL: srv.URL, Token: freshToken()})
	require.NoError(t, err)

	resp, err := c.Do(context.Background(), http.MethodPost, "/", bytes.NewBufferString(payload))
	require.NoError(t, err)
	_ = resp.Body.Close()

	assert.Equal(t, []byte(payload), gotBody)
}
