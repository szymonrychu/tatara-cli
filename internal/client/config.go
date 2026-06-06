package client

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const DefaultBaseURL = "https://tatara.szymonrichert.pl/api/v1/memory"

const DefaultOperatorBaseURL = "https://tatara.szymonrichert.pl/api/v1/operator"

type FileConfig struct {
	BaseURL         string `yaml:"baseUrl"`
	OperatorBaseURL string `yaml:"operatorBaseUrl"`
	Issuer          string `yaml:"issuer"`
}

// ResolveBaseURL returns the first non-empty value of: flag, env, file, default.
func ResolveBaseURL(flag, env string, file *FileConfig) string {
	if flag != "" {
		return flag
	}
	if env != "" {
		return env
	}
	if file != nil && file.BaseURL != "" {
		return file.BaseURL
	}
	return DefaultBaseURL
}

// ResolveOperatorBaseURL returns the first non-empty of: flag, env, file, default.
func ResolveOperatorBaseURL(flag, env string, file *FileConfig) string {
	if flag != "" {
		return flag
	}
	if env != "" {
		return env
	}
	if file != nil && file.OperatorBaseURL != "" {
		return file.OperatorBaseURL
	}
	return DefaultOperatorBaseURL
}

func DefaultConfigPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("client: home: %w", err)
		}
		dir = filepath.Join(h, ".config")
	}
	return filepath.Join(dir, "tatara", "config.yaml"), nil
}

func LoadConfig(path string) (*FileConfig, error) {
	b, err := os.ReadFile(path) //nolint:gosec // path is caller-supplied config file
	if os.IsNotExist(err) {
		return &FileConfig{}, nil
	}
	if err != nil {
		return nil, err
	}
	var c FileConfig
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}
