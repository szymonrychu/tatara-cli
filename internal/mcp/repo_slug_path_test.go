package mcp

import "testing"

func TestRepoSlugPath_TwoSegments(t *testing.T) {
	// "owner/repo" must become two path segments (slash preserved), matching the
	// operator's {owner}/{repo} routes - NOT a single %2F-escaped segment.
	if got := repoSlugPath("szymonrychu/tatara-cli"); got != "szymonrychu/tatara-cli" {
		t.Fatalf("want two segments owner/repo, got %q", got)
	}
	// a bare repo (no slash) stays a single escaped segment
	if got := repoSlugPath("tatara-cli"); got != "tatara-cli" {
		t.Fatalf("bare repo: got %q", got)
	}
}
