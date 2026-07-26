package client

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// memoryURLForProject composes the per-project memory base URL for an
// ingress-shaped base (nginx rewrites /<project>/... and strips the prefix
// before it reaches the pod). It trims a trailing slash from base, then
// appends /<project> when project is non-empty. Unexported: only
// ResolveMemoryURL may call this, since a base sourced from
// TATARA_MEMORY_URL is already project-scoped and must never be passed
// through it.
func memoryURLForProject(base, project string) string {
	base = strings.TrimRight(base, "/")
	if project == "" {
		return base
	}
	return base + "/" + project
}

// ResolveMemoryURL resolves the final tatara-memory base URL callers should
// use, already project-scoped. Precedence is flag > env > file > default,
// same as ResolveBaseURL. TATARA_MEMORY_URL is injected per-project directly
// by the operator as a root-mounted service endpoint (mem-<project>.<ns>.svc),
// so a base sourced from env is already project-scoped and is returned as-is;
// a base sourced from flag, file, or the default is the shared ingress
// endpoint, which still needs /<project> appended for the ingress rewrite.
//
// envSet is os.LookupEnv's second return. An env var that is SET BUT EMPTY
// means "this pod has no memory backend" and resolves to "", which callers must
// treat as unconfigured; it must never fall through to the shared public
// default, or a project with no memory stack silently makes cross-cluster calls
// into someone else's memory. UNSET is a different thing entirely - a developer
// workstation with no tatara env at all - and keeps the file/default fallback.
func ResolveMemoryURL(flag, env string, envSet bool, file *FileConfig, project string) string {
	if flag != "" {
		return memoryURLForProject(flag, project)
	}
	if env != "" {
		return strings.TrimRight(env, "/")
	}
	if envSet {
		return ""
	}
	if file != nil && file.BaseURL != "" {
		return memoryURLForProject(file.BaseURL, project)
	}
	return memoryURLForProject(DefaultBaseURL, project)
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
