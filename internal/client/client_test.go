package client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
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

func TestClient_StampsRequestIDFromTurnID(t *testing.T) {
	t.Setenv("TATARA_TURN_ID", "turn-abc123")
	t.Setenv("RUN_ID", "job-should-be-ignored")

	var gotReqID string
	srv, cleanup := testServer(func(w http.ResponseWriter, r *http.Request) {
		gotReqID = r.Header.Get("X-Request-Id")
		w.WriteHeader(http.StatusOK)
	})
	defer cleanup()

	c, err := New(Config{BaseURL: srv.URL, Token: freshToken()})
	require.NoError(t, err)

	resp, err := c.Do(context.Background(), http.MethodGet, "/", nil)
	require.NoError(t, err)
	_ = resp.Body.Close()

	assert.Equal(t, "turn-abc123", gotReqID, "X-Request-Id must carry TATARA_TURN_ID verbatim")
}

func TestClient_StampsRequestIDFallsBackToRunID(t *testing.T) {
	t.Setenv("TATARA_TURN_ID", "")
	t.Setenv("RUN_ID", "ingest-job-42")

	var gotReqID string
	srv, cleanup := testServer(func(w http.ResponseWriter, r *http.Request) {
		gotReqID = r.Header.Get("X-Request-Id")
		w.WriteHeader(http.StatusOK)
	})
	defer cleanup()

	c, err := New(Config{BaseURL: srv.URL, Token: freshToken()})
	require.NoError(t, err)

	resp, err := c.Do(context.Background(), http.MethodGet, "/", nil)
	require.NoError(t, err)
	_ = resp.Body.Close()

	assert.Equal(t, "ingest-job-42", gotReqID, "X-Request-Id must fall back to RUN_ID when TATARA_TURN_ID is absent")
}

func TestClient_OmitsRequestIDWhenUnset(t *testing.T) {
	t.Setenv("TATARA_TURN_ID", "")
	t.Setenv("RUN_ID", "")

	var hadReqID bool
	srv, cleanup := testServer(func(w http.ResponseWriter, r *http.Request) {
		_, hadReqID = r.Header["X-Request-Id"]
		w.WriteHeader(http.StatusOK)
	})
	defer cleanup()

	c, err := New(Config{BaseURL: srv.URL, Token: freshToken()})
	require.NoError(t, err)

	resp, err := c.Do(context.Background(), http.MethodGet, "/", nil)
	require.NoError(t, err)
	_ = resp.Body.Close()

	assert.False(t, hadReqID, "X-Request-Id must be omitted when no correlation id is in the environment")
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

// TestClient_RefreshLogsSuccess verifies that a successful token refresh emits
// an INFO-level structured log when a logger is configured (hard rule 12).
func TestClient_RefreshLogsSuccess(t *testing.T) {
	newToken := &auth.Token{
		AccessToken: "refreshed",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		TokenType:   "Bearer",
	}
	srv, cleanup := testServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	defer cleanup()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	c, err := New(Config{
		BaseURL: srv.URL,
		Token:   nearExpiryToken(),
		Log:     logger,
		Refresh: func(_ context.Context, _ *auth.Token) (*auth.Token, error) {
			return newToken, nil
		},
	})
	require.NoError(t, err)

	resp, err := c.Do(context.Background(), http.MethodGet, "/", nil)
	require.NoError(t, err)
	_ = resp.Body.Close()

	logged := buf.String()
	assert.Contains(t, logged, "token refreshed", "INFO log must mention token refreshed")
	assert.Contains(t, logged, "expires_at", "INFO log must carry expires_at field")
}

// TestClient_RefreshLogsError verifies that a failed refresh emits an
// ERROR-level log when a logger is configured (hard rule 12).
func TestClient_RefreshLogsError(t *testing.T) {
	srv, cleanup := testServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	defer cleanup()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	c, err := New(Config{
		BaseURL: srv.URL,
		Token:   nearExpiryToken(),
		Log:     logger,
		Refresh: func(_ context.Context, _ *auth.Token) (*auth.Token, error) {
			return nil, errors.New("oauth server down")
		},
	})
	require.NoError(t, err)

	_, err = c.Do(context.Background(), http.MethodGet, "/", nil)
	require.Error(t, err)

	logged := buf.String()
	assert.Contains(t, logged, "token refresh failed", "ERROR log must mention refresh failure")
}

// TestClient_ConcurrentDo_NoDataRace verifies that concurrent Do calls from
// multiple goroutines on a near-expiry client do not produce a data race on
// c.token (finding 1: missing mutex). Run with -race to catch unsynchronized
// reads/writes; the test also asserts that every request succeeds.
func TestClient_ConcurrentDo_NoDataRace(t *testing.T) {
	newToken := &auth.Token{
		AccessToken: "concurrent-refreshed",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		TokenType:   "Bearer",
	}
	srv, cleanup := testServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	defer cleanup()

	c, err := New(Config{
		BaseURL: srv.URL,
		Token:   nearExpiryToken(),
		Refresh: func(_ context.Context, _ *auth.Token) (*auth.Token, error) {
			// Simulate slow refresh so goroutines overlap.
			time.Sleep(5 * time.Millisecond)
			return newToken, nil
		},
	})
	require.NoError(t, err)

	const n = 10
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			resp, doErr := c.Do(context.Background(), http.MethodGet, "/", nil)
			if doErr == nil {
				_ = resp.Body.Close()
			}
			errs <- doErr
		}()
	}
	for i := 0; i < n; i++ {
		require.NoError(t, <-errs)
	}
}

// A failed token refresh is one of two auth signals with no other producer
// anywhere, and no metric leaves this process - so it has to be logged, and the
// staged cause has to survive into the error the caller wraps into the tool
// result text.
func TestClient_RefreshFailureIsLoggedAndCarriesTheCause(t *testing.T) {
	srv, cleanup := testServer(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	defer cleanup()

	var buf bytes.Buffer
	c, err := New(Config{
		BaseURL: srv.URL,
		Token:   nearExpiryToken(),
		Log:     slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})),
		Refresh: func(_ context.Context, _ *auth.Token) (*auth.Token, error) {
			return nil, &auth.MintError{Stage: "token_status", Err: errors.New("status 401")}
		},
	})
	require.NoError(t, err)

	_, doErr := c.Do(context.Background(), http.MethodGet, "/", nil)
	require.Error(t, doErr)
	require.Contains(t, doErr.Error(), "stage=token_status",
		"the mint stage must survive into the error the tool result carries")
	require.Contains(t, buf.String(), "token refresh failed")
	require.Contains(t, buf.String(), "stage=token_status")
}

// A successful refresh logs at INFO so the transcript shows the self-heal.
func TestClient_RefreshSuccessIsLogged(t *testing.T) {
	srv, cleanup := testServer(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	defer cleanup()

	var buf bytes.Buffer
	c, err := New(Config{
		BaseURL: srv.URL,
		Token:   nearExpiryToken(),
		Log:     slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})),
		Refresh: func(_ context.Context, _ *auth.Token) (*auth.Token, error) {
			return &auth.Token{AccessToken: "fresh", ExpiresAt: time.Now().Add(time.Hour), TokenType: "Bearer"}, nil
		},
	})
	require.NoError(t, err)

	resp, err := c.Do(context.Background(), http.MethodGet, "/", nil)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Contains(t, buf.String(), "token refreshed")
}

// TestClient_NoLoggerSafe verifies that nil logger (default) does not panic
// during refresh success or failure.
func TestClient_NoLoggerSafe(t *testing.T) {
	srv, cleanup := testServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	defer cleanup()

	c, err := New(Config{
		BaseURL: srv.URL,
		Token:   nearExpiryToken(),
		// Log not set - must not panic
		Refresh: func(_ context.Context, _ *auth.Token) (*auth.Token, error) {
			return &auth.Token{AccessToken: "x", ExpiresAt: time.Now().Add(time.Hour)}, nil
		},
	})
	require.NoError(t, err)
	require.NotPanics(t, func() {
		resp, err := c.Do(context.Background(), http.MethodGet, "/", nil)
		require.NoError(t, err)
		_ = resp.Body.Close()
	})
}
