package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	IDToken      string    `json:"id_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
	TokenType    string    `json:"token_type"`
}

var ErrNoToken = errors.New("auth: no token (run `tatara login`)")

func DefaultTokenPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("auth: resolve home: %w", err)
		}
		dir = filepath.Join(h, ".config")
	}
	return filepath.Join(dir, "tatara", "token.json"), nil
}

func LoadToken(path string) (*Token, error) {
	b, err := os.ReadFile(path) //nolint:gosec // path is caller-controlled
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoToken
	}
	if err != nil {
		return nil, fmt.Errorf("auth: read token: %w", err)
	}
	var t Token
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, fmt.Errorf("auth: parse token: %w", err)
	}
	return &t, nil
}

func SaveToken(path string, t *Token) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("auth: mkdir: %w", err)
	}
	b, err := json.MarshalIndent(t, "", "  ") //nolint:gosec // intentional: token file stores credentials
	if err != nil {
		return fmt.Errorf("auth: marshal token: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // path is caller-controlled
	if err != nil {
		return fmt.Errorf("auth: open token file: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(b); err != nil {
		return fmt.Errorf("auth: write token: %w", err)
	}
	return nil
}

func DeleteToken(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
