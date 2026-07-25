package client

import (
	"testing"
)

func TestMemoryURLForProject(t *testing.T) {
	if got := MemoryURLForProject("https://h/api/v1/memory", "alpha"); got != "https://h/api/v1/memory/alpha" {
		t.Fatalf("got %s", got)
	}
	if got := MemoryURLForProject("https://h/api/v1/memory/", "alpha"); got != "https://h/api/v1/memory/alpha" {
		t.Fatalf("trailing slash: %s", got)
	}
	if got := MemoryURLForProject("https://h/api/v1/memory", ""); got != "https://h/api/v1/memory" {
		t.Fatalf("no project: %s", got)
	}
}

func TestResolveMemoryURL_EnvSourcedBaseIsAlreadyProjectScoped(t *testing.T) {
	got := ResolveMemoryURL("", "http://mem-alpha.tatara.svc:8080", &FileConfig{BaseURL: "https://file.example.com"}, "alpha")
	if got != "http://mem-alpha.tatara.svc:8080" {
		t.Fatalf("env-sourced base must be returned unchanged, got %s", got)
	}
}

func TestResolveMemoryURL_EnvSourcedBaseUnchangedWithEmptyProject(t *testing.T) {
	got := ResolveMemoryURL("", "http://mem-alpha.tatara.svc:8080", nil, "")
	if got != "http://mem-alpha.tatara.svc:8080" {
		t.Fatalf("env-sourced base must be returned unchanged even with no project, got %s", got)
	}
}

func TestResolveMemoryURL_DefaultBaseGetsProjectAppended(t *testing.T) {
	got := ResolveMemoryURL("", "", nil, "alpha")
	if got != DefaultBaseURL+"/alpha" {
		t.Fatalf("got %s", got)
	}
}

func TestResolveMemoryURL_FileBaseGetsProjectAppended(t *testing.T) {
	got := ResolveMemoryURL("", "", &FileConfig{BaseURL: "https://file.example.com"}, "alpha")
	if got != "https://file.example.com/alpha" {
		t.Fatalf("got %s", got)
	}
}

func TestResolveMemoryURL_FlagBaseGetsProjectAppended(t *testing.T) {
	got := ResolveMemoryURL("https://flag.example.com", "http://mem-alpha.tatara.svc:8080", nil, "alpha")
	if got != "https://flag.example.com/alpha" {
		t.Fatalf("flag base must be treated as ingress-shaped like file/default, got %s", got)
	}
}

func TestResolveMemoryURL_EmptyProjectDefaultBase(t *testing.T) {
	got := ResolveMemoryURL("", "", nil, "")
	if got != DefaultBaseURL {
		t.Fatalf("got %s", got)
	}
}
