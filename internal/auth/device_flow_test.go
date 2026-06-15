package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/auth"
)

func newDeviceFlow(t *testing.T, server *httptest.Server) *auth.DeviceFlow {
	t.Helper()
	return &auth.DeviceFlow{
		HTTP:         server.Client(),
		Issuer:       server.URL,
		ClientID:     "test-client",
		Scope:        "test",
		TickOverride: time.Millisecond,
	}
}

func respondJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func TestDeviceFlowHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/protocol/openid-connect/auth/device":
			respondJSON(w, http.StatusOK, map[string]any{
				"device_code":      "dc1",
				"user_code":        "ABCD-1234",
				"verification_uri": "https://example.com/activate",
				"expires_in":       60,
				"interval":         0,
			})
		case "/protocol/openid-connect/token":
			respondJSON(w, http.StatusOK, map[string]any{
				"access_token":  "at1",
				"refresh_token": "rt1",
				"id_token":      "idt1",
				"expires_in":    3600,
				"token_type":    "Bearer",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	df := newDeviceFlow(t, srv)
	ctx := context.Background()

	dc, err := df.Start(ctx)
	require.NoError(t, err)
	require.Equal(t, "dc1", dc.DeviceCode)
	require.Equal(t, "ABCD-1234", dc.UserCode)

	tok, err := df.Poll(ctx, dc)
	require.NoError(t, err)
	require.Equal(t, "at1", tok.AccessToken)
	require.Equal(t, "rt1", tok.RefreshToken)
}

func TestDeviceFlowPendingThenSuccess(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/protocol/openid-connect/auth/device":
			respondJSON(w, http.StatusOK, map[string]any{
				"device_code": "dc2",
				"user_code":   "XY-99",
				"expires_in":  60,
				"interval":    0,
			})
		case "/protocol/openid-connect/token":
			n := calls.Add(1)
			if n == 1 {
				respondJSON(w, http.StatusBadRequest, map[string]any{"error": "authorization_pending"})
				return
			}
			respondJSON(w, http.StatusOK, map[string]any{
				"access_token":  "at2",
				"refresh_token": "rt2",
				"expires_in":    3600,
				"token_type":    "Bearer",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	df := newDeviceFlow(t, srv)
	ctx := context.Background()

	dc, err := df.Start(ctx)
	require.NoError(t, err)

	tok, err := df.Poll(ctx, dc)
	require.NoError(t, err)
	require.Equal(t, "at2", tok.AccessToken)
	require.Equal(t, int32(2), calls.Load())
}

func TestDeviceFlowSlowDown(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/protocol/openid-connect/auth/device":
			respondJSON(w, http.StatusOK, map[string]any{
				"device_code": "dc3",
				"user_code":   "ZZ-00",
				"expires_in":  60,
				"interval":    0,
			})
		case "/protocol/openid-connect/token":
			n := calls.Add(1)
			if n == 1 {
				respondJSON(w, http.StatusBadRequest, map[string]any{"error": "slow_down"})
				return
			}
			respondJSON(w, http.StatusOK, map[string]any{
				"access_token":  "at3",
				"refresh_token": "rt3",
				"expires_in":    3600,
				"token_type":    "Bearer",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	df := newDeviceFlow(t, srv)
	ctx := context.Background()

	dc, err := df.Start(ctx)
	require.NoError(t, err)

	tok, err := df.Poll(ctx, dc)
	require.NoError(t, err)
	require.Equal(t, "at3", tok.AccessToken)
	require.Equal(t, int32(2), calls.Load())
}

func TestDeviceFlowAccessDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/protocol/openid-connect/auth/device":
			respondJSON(w, http.StatusOK, map[string]any{
				"device_code": "dc4",
				"user_code":   "AA-11",
				"expires_in":  60,
				"interval":    0,
			})
		case "/protocol/openid-connect/token":
			respondJSON(w, http.StatusBadRequest, map[string]any{"error": "access_denied"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	df := newDeviceFlow(t, srv)
	ctx := context.Background()

	dc, err := df.Start(ctx)
	require.NoError(t, err)

	_, err = df.Poll(ctx, dc)
	require.ErrorIs(t, err, auth.ErrAccessDenied)
}

// Finding 4: Poll must return the 'device code expired' sentinel (not a raw server error)
// when the deadline has passed. With the deadline check moved to the top of the loop, an
// already-expired code yields ErrDeviceExpired without making a network call.
func TestDeviceFlowExpiredCodeYieldsExpiredError(t *testing.T) {
	var exchanges atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/protocol/openid-connect/auth/device":
			respondJSON(w, http.StatusOK, map[string]any{
				"device_code": "dc-exp",
				"user_code":   "EX-PI",
				"expires_in":  1, // 1 second
				"interval":    0,
			})
		case "/protocol/openid-connect/token":
			exchanges.Add(1)
			// Server returns expired_token to simulate a real Keycloak response.
			respondJSON(w, http.StatusBadRequest, map[string]any{"error": "expired_token"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	df := &auth.DeviceFlow{
		HTTP:         srv.Client(),
		Issuer:       srv.URL,
		ClientID:     "test-client",
		Scope:        "test",
		TickOverride: time.Millisecond,
	}

	dc, err := df.Start(context.Background())
	require.NoError(t, err)

	// Manually set ExpiresIn to 0 so deadline is already in the past when Poll starts.
	dc.ExpiresIn = 0

	_, err = df.Poll(context.Background(), dc)
	require.Error(t, err)
	require.Contains(t, err.Error(), "expired", "should surface 'expired' sentinel, not raw server error")
	// With deadline check at top of loop, no exchange should occur.
	require.Equal(t, int32(0), exchanges.Load(), "no exchange should happen when deadline already passed")
}

func TestDeviceFlowContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/protocol/openid-connect/auth/device":
			respondJSON(w, http.StatusOK, map[string]any{
				"device_code": "dc5",
				"user_code":   "BB-22",
				"expires_in":  60,
				"interval":    0,
			})
		case "/protocol/openid-connect/token":
			respondJSON(w, http.StatusBadRequest, map[string]any{"error": "authorization_pending"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	df := newDeviceFlow(t, srv)
	ctx, cancel := context.WithCancel(context.Background())

	dc, err := df.Start(ctx)
	require.NoError(t, err)

	// Cancel after first pending response; Poll should return ctx.Err().
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	_, err = df.Poll(ctx, dc)
	require.ErrorIs(t, err, context.Canceled)
}
