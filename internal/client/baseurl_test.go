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
