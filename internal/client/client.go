package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/szymonrychu/tatara-cli/internal/auth"
)

// RefreshFunc is called when the current token is about to expire.
type RefreshFunc func(ctx context.Context, t *auth.Token) (*auth.Token, error)

// Client is an HTTP client that attaches bearer tokens and auto-refreshes on near-expiry.
type Client struct {
	base      string
	http      *http.Client
	token     *auth.Token
	tokenPath string
	refresh   RefreshFunc
	reload    func() (*auth.Token, error)
	save      func(*auth.Token) error
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
	}, nil
}

// Do sends an HTTP request to base+path. body may be nil, []byte, io.Reader, or any
// JSON-serializable value. Attaches Authorization header and refreshes token if needed.
func (c *Client) Do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	if err := c.ensureFresh(ctx); err != nil {
		return nil, err
	}
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
	if c.token != nil {
		req.Header.Set("Authorization", "Bearer "+c.token.AccessToken)
	}
	return c.http.Do(req)
}

func (c *Client) ensureFresh(ctx context.Context) error {
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
		return fmt.Errorf("client: refresh: %w", err)
	}
	c.token = nt
	if c.save != nil {
		return c.save(nt)
	}
	return nil
}
