package client

import (
	"testing"
)

func TestMemoryURLForProject(t *testing.T) {
	if got := memoryURLForProject("https://h/api/v1/memory", "alpha"); got != "https://h/api/v1/memory/alpha" {
		t.Fatalf("got %s", got)
	}
	if got := memoryURLForProject("https://h/api/v1/memory/", "alpha"); got != "https://h/api/v1/memory/alpha" {
		t.Fatalf("trailing slash: %s", got)
	}
	if got := memoryURLForProject("https://h/api/v1/memory", ""); got != "https://h/api/v1/memory" {
		t.Fatalf("no project: %s", got)
	}
}

func TestResolveMemoryURL_EnvSourcedBaseIsAlreadyProjectScoped(t *testing.T) {
	got := ResolveMemoryURL("", "http://mem-alpha.tatara.svc:8080", true, &FileConfig{BaseURL: "https://file.example.com"}, "alpha")
	if got != "http://mem-alpha.tatara.svc:8080" {
		t.Fatalf("env-sourced base must be returned unchanged, got %s", got)
	}
}

func TestResolveMemoryURL_EnvSourcedBaseUnchangedWithEmptyProject(t *testing.T) {
	got := ResolveMemoryURL("", "http://mem-alpha.tatara.svc:8080", true, nil, "")
	if got != "http://mem-alpha.tatara.svc:8080" {
		t.Fatalf("env-sourced base must be returned unchanged even with no project, got %s", got)
	}
}

func TestResolveMemoryURL_DefaultBaseGetsProjectAppended(t *testing.T) {
	got := ResolveMemoryURL("", "", false, nil, "alpha")
	if got != DefaultBaseURL+"/alpha" {
		t.Fatalf("got %s", got)
	}
}

func TestResolveMemoryURL_FileBaseGetsProjectAppended(t *testing.T) {
	got := ResolveMemoryURL("", "", false, &FileConfig{BaseURL: "https://file.example.com"}, "alpha")
	if got != "https://file.example.com/alpha" {
		t.Fatalf("got %s", got)
	}
}

func TestResolveMemoryURL_FlagBaseGetsProjectAppended(t *testing.T) {
	got := ResolveMemoryURL("https://flag.example.com", "http://mem-alpha.tatara.svc:8080", true, nil, "alpha")
	if got != "https://flag.example.com/alpha" {
		t.Fatalf("flag base must be treated as ingress-shaped like file/default, got %s", got)
	}
}

func TestResolveMemoryURL_EmptyProjectDefaultBase(t *testing.T) {
	got := ResolveMemoryURL("", "", false, nil, "")
	if got != DefaultBaseURL {
		t.Fatalf("got %s", got)
	}
}

// An agent pod with no memory stack gets TATARA_MEMORY_URL set to the empty
// string. That must resolve to "no memory backend configured", NOT to the
// shared public ingress: a pod silently making cross-cluster calls to another
// project's memory is worse than no memory at all.
func TestResolveMemoryURL_ExplicitlyEmptyEnvIsUnconfigured(t *testing.T) {
	got := ResolveMemoryURL("", "", true, &FileConfig{BaseURL: "https://file.example.com"}, "alpha")
	if got != "" {
		t.Fatalf("set-but-empty env must resolve to no backend, got %s", got)
	}
}

// Unset is NOT the same as set-but-empty: a developer running tatara on a
// workstation with no env at all keeps the file/default fallback.
func TestResolveMemoryURL_UnsetEnvStillFallsBackToFile(t *testing.T) {
	got := ResolveMemoryURL("", "", false, &FileConfig{BaseURL: "https://file.example.com"}, "alpha")
	if got != "https://file.example.com/alpha" {
		t.Fatalf("unset env must keep the file fallback, got %s", got)
	}
}

// An explicit flag still wins: a human debugging a degraded pod can point the
// CLI at a live backend.
func TestResolveMemoryURL_FlagWinsOverExplicitlyEmptyEnv(t *testing.T) {
	got := ResolveMemoryURL("https://flag.example.com", "", true, nil, "alpha")
	if got != "https://flag.example.com/alpha" {
		t.Fatalf("got %s", got)
	}
}
