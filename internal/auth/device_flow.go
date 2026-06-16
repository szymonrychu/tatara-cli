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

type DeviceCode struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type DeviceFlow struct {
	HTTP         *http.Client
	Issuer       string
	ClientID     string
	Scope        string
	TickOverride time.Duration // non-zero overrides poll interval (test use only)
}

var (
	ErrAuthPending  = errors.New("authorization_pending")
	ErrSlowDown     = errors.New("slow_down")
	ErrAccessDenied = errors.New("access_denied")
)

func (d *DeviceFlow) Start(ctx context.Context) (*DeviceCode, error) {
	body := url.Values{}
	body.Set("client_id", d.ClientID)
	body.Set("scope", d.Scope)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		d.Issuer+"/protocol/openid-connect/auth/device", strings.NewReader(body.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := d.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth: device start: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		var e struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		if jsonErr := json.Unmarshal(b, &e); jsonErr == nil && e.Error != "" {
			if e.ErrorDescription != "" {
				return nil, fmt.Errorf("auth: device start %d: %s: %s", resp.StatusCode, e.Error, e.ErrorDescription)
			}
			return nil, fmt.Errorf("auth: device start %d: %s", resp.StatusCode, e.Error)
		}
		return nil, fmt.Errorf("auth: device start %d", resp.StatusCode)
	}
	var dc DeviceCode
	if err := json.NewDecoder(resp.Body).Decode(&dc); err != nil {
		return nil, fmt.Errorf("auth: parse device code: %w", err)
	}
	return &dc, nil
}

func (d *DeviceFlow) Poll(ctx context.Context, code *DeviceCode) (*Token, error) {
	interval := d.TickOverride
	if interval == 0 {
		interval = time.Duration(code.Interval) * time.Second
		if interval == 0 {
			interval = 5 * time.Second
		}
	}
	deadline := time.Now().Add(time.Duration(code.ExpiresIn) * time.Second)
	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("auth: device code expired")
		}
		tok, err := d.exchange(ctx, code.DeviceCode)
		if err == nil {
			return tok, nil
		}
		switch {
		case errors.Is(err, ErrAuthPending):
			// wait and retry
		case errors.Is(err, ErrSlowDown):
			if d.TickOverride == 0 {
				interval += 5 * time.Second
				if interval > 30*time.Second {
					interval = 30 * time.Second
				}
			}
		default:
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

func (d *DeviceFlow) exchange(ctx context.Context, deviceCode string) (*Token, error) {
	body := url.Values{}
	body.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	body.Set("device_code", deviceCode)
	body.Set("client_id", d.ClientID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		d.Issuer+"/protocol/openid-connect/token", strings.NewReader(body.Encode()))
	if err != nil {
		return nil, fmt.Errorf("auth: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := d.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth: token exchange: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK {
		var tr struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			IDToken      string `json:"id_token"`
			ExpiresIn    int    `json:"expires_in"`
			TokenType    string `json:"token_type"`
		}
		if err := json.Unmarshal(raw, &tr); err != nil {
			return nil, fmt.Errorf("auth: parse token: %w", err)
		}
		return &Token{
			AccessToken:  tr.AccessToken,
			RefreshToken: tr.RefreshToken,
			IDToken:      tr.IDToken,
			ExpiresAt:    time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
			TokenType:    tr.TokenType,
		}, nil
	}
	var e struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &e)
	switch e.Error {
	case "authorization_pending":
		return nil, ErrAuthPending
	case "slow_down":
		return nil, ErrSlowDown
	case "access_denied":
		return nil, ErrAccessDenied
	default:
		return nil, fmt.Errorf("auth: %s: %s", e.Error, string(raw))
	}
}

func (d *DeviceFlow) httpClient() *http.Client {
	if d.HTTP != nil {
		return d.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}
