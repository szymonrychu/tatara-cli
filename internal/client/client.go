package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/szymonrychu/tatara-cli/internal/auth"
	"github.com/szymonrychu/tatara-cli/internal/obs"
)

// RefreshFunc is called when the current token is about to expire.
type RefreshFunc func(ctx context.Context, t *auth.Token) (*auth.Token, error)

// TransportError marks a failure that happened on the wire: the request left
// this process and no response came back (connection refused, DNS failure,
// timeout, reset). It exists so callers can tell "the backend is not there"
// from the other errors Do returns - a missing token, a failed refresh, an
// unencodable body - which say nothing about the backend's health. Error()
// delegates, so error strings are unchanged.
type TransportError struct{ Err error }

func (e *TransportError) Error() string { return e.Err.Error() }
func (e *TransportError) Unwrap() error { return e.Err }

// Client is an HTTP client that attaches bearer tokens and auto-refreshes on near-expiry.
type Client struct {
	mu        sync.Mutex
	base      string
	http      *http.Client
	token     *auth.Token
	tokenPath string
	refresh   RefreshFunc
	reload    func() (*auth.Token, error)
	save      func(*auth.Token) error
	log       *slog.Logger
	requestID string // correlation id stamped as X-Request-Id; fixed for the client's lifetime
}

// Config holds constructor parameters for Client.
type Config struct {
	BaseURL   string
	Token     *auth.Token
	TokenPath string // needed for the file lock during refresh; empty skips locking
	Refresh   RefreshFunc
	Reload    func() (*auth.Token, error) // re-read token from disk after acquiring lock
	Save      func(*auth.Token) error
	HTTP      *http.Client
	Log       *slog.Logger // optional; refresh outcomes logged at INFO/ERROR when set
}

// correlationID returns the per-turn id to stamp on outbound requests as
// X-Request-Id, so one agent turn is greppable across services. The wrapper
// relaunches the agent per turn and injects TATARA_TURN_ID, so reading it once
// at construction is correct. Falls back to RUN_ID (set for job-style runs),
// else empty. The value is already valid for the memory and operator
// validators, so no transform is applied.
func correlationID() string {
	if id := os.Getenv("TATARA_TURN_ID"); id != "" {
		return id
	}
	return os.Getenv("RUN_ID")
}

// New creates a Client from cfg. BaseURL is required.
func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New("client: BaseURL required")
	}
	h := cfg.HTTP
	if h == nil {
		h = &http.Client{Timeout: 60 * time.Second}
	}
	return &Client{
		base:      cfg.BaseURL,
		http:      h,
		token:     cfg.Token,
		tokenPath: cfg.TokenPath,
		refresh:   cfg.Refresh,
		reload:    cfg.Reload,
		save:      cfg.Save,
		log:       cfg.Log,
		requestID: correlationID(),
	}, nil
}

// Do sends an HTTP request to base+path. body may be nil, []byte, io.Reader, or any
// JSON-serializable value. Attaches Authorization header and refreshes token if needed.
//
// No-retry contract: Do does not retry on 401 or any other status. Token refresh
// (ensureFreshLocked) runs before the request is built, so the request is sent exactly
// once. Callers that pass an io.Reader body must not assume the body can be replayed; if
// a retry loop is ever added here, io.Reader bodies must be buffered into []byte first
// (or callers required to pass []byte) before that loop is introduced.
func (c *Client) Do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	start := time.Now()

	c.mu.Lock()
	if err := c.ensureFreshLocked(ctx); err != nil {
		c.mu.Unlock()
		return nil, err
	}
	var bearer string
	if c.token != nil {
		bearer = c.token.AccessToken
	}
	c.mu.Unlock()

	var bodyReader io.Reader
	if body != nil {
		switch b := body.(type) {
		case []byte:
			bodyReader = bytes.NewReader(b)
		case io.Reader:
			bodyReader = b
		default:
			buf := &bytes.Buffer{}
			if err := json.NewEncoder(buf).Encode(body); err != nil {
				return nil, fmt.Errorf("client: encode body: %w", err)
			}
			bodyReader = buf
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("client: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if c.requestID != "" {
		req.Header.Set("X-Request-Id", c.requestID)
	}
	resp, err := c.http.Do(req)
	durMs := float64(time.Since(start).Milliseconds())
	if c.log != nil {
		c.log.Info("client.Do",
			"method", method,
			"path", path,
			"status_code", statusCode(resp),
			"duration_ms", durMs,
			"request_id", c.requestID,
			"error", err,
		)
	}
	if err != nil {
		return nil, &TransportError{Err: err}
	}
	return resp, nil
}

// statusCode returns the HTTP status code from resp, or 0 when resp is nil.
func statusCode(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}

// ensureFreshLocked checks and refreshes the token when needed. Caller must hold c.mu.
func (c *Client) ensureFreshLocked(ctx context.Context) error {
	if c.token == nil {
		return auth.ErrNoToken
	}
	if time.Until(c.token.ExpiresAt) > 30*time.Second {
		return nil
	}
	if c.refresh == nil {
		return nil
	}
	// Acquire file lock to serialise concurrent refresh attempts across processes.
	if c.tokenPath != "" {
		lock, err := auth.AcquireLock(c.tokenPath)
		if err != nil {
			return fmt.Errorf("client: lock token: %w", err)
		}
		defer func() { _ = lock.Release() }()
		// Double-check: another process may have already refreshed while we waited.
		if c.reload != nil {
			fresh, err := c.reload()
			if err == nil && fresh != nil && time.Until(fresh.ExpiresAt) > 30*time.Second {
				c.token = fresh
				return nil
			}
		}
	}
	nt, err := c.refresh(ctx, c.token)
	if err != nil {
		obs.TokenRefreshTotal.WithLabelValues("error").Inc()
		if c.log != nil {
			c.log.Error("token refresh failed", "err", err)
		}
		return fmt.Errorf("client: refresh: %w", err)
	}
	obs.TokenRefreshTotal.WithLabelValues("ok").Inc()
	if c.log != nil {
		c.log.Info("token refreshed", "expires_at", nt.ExpiresAt)
	}
	c.token = nt
	if c.save != nil {
		return c.save(nt)
	}
	return nil
}
