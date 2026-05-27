package client

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveBaseURL_FlagWins(t *testing.T) {
	got := ResolveBaseURL("https://flag.example.com", "https://env.example.com", &FileConfig{BaseURL: "https://file.example.com"})
	assert.Equal(t, "https://flag.example.com", got)
}

func TestResolveBaseURL_EnvWinsOverFileAndDefault(t *testing.T) {
	got := ResolveBaseURL("", "https://env.example.com", &FileConfig{BaseURL: "https://file.example.com"})
	assert.Equal(t, "https://env.example.com", got)
}

func TestResolveBaseURL_FileWinsOverDefault(t *testing.T) {
	got := ResolveBaseURL("", "", &FileConfig{BaseURL: "https://file.example.com"})
	assert.Equal(t, "https://file.example.com", got)
}

func TestResolveBaseURL_DefaultWhenAllEmpty(t *testing.T) {
	got := ResolveBaseURL("", "", &FileConfig{})
	assert.Equal(t, DefaultBaseURL, got)
}

func TestResolveBaseURL_NilFileConfig(t *testing.T) {
	got := ResolveBaseURL("", "", nil)
	assert.Equal(t, DefaultBaseURL, got)
}

func TestLoadConfig_MissingFileReturnsEmptyNotError(t *testing.T) {
	cfg, err := LoadConfig("/tmp/tatara-nonexistent-config-12345.yaml")
	require.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, "", cfg.BaseURL)
}

func TestLoadConfig_ParsesYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(path, []byte("baseUrl: https://parsed.example.com\nissuer: https://issuer.example.com\n"), 0o600)
	require.NoError(t, err)

	cfg, err := LoadConfig(path)
	require.NoError(t, err)
	assert.Equal(t, "https://parsed.example.com", cfg.BaseURL)
	assert.Equal(t, "https://issuer.example.com", cfg.Issuer)
}

func TestDefaultConfigPath_HonorsXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/my-xdg-config")
	got, err := DefaultConfigPath()
	require.NoError(t, err)
	assert.Equal(t, "/tmp/my-xdg-config/tatara/config.yaml", got)
}

func TestDefaultConfigPath_FallsBackToHomeDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	got, err := DefaultConfigPath()
	require.NoError(t, err)
	home, _ := os.UserHomeDir()
	assert.Equal(t, filepath.Join(home, ".config", "tatara", "config.yaml"), got)
}
