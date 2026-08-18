package mcp

import "testing"

func TestGenerateToolManifest(t *testing.T) {
	m := GenerateToolManifest()

	byName := map[string]ToolManifestEntry{}
	for _, e := range m.Tools {
		byName[e.Name] = e
	}

	for _, want := range []string{"scm_read", "issue_write", "mr_write", "mr_takeover_request", "submit_outcome", "code_search", "memory_write"} {
		if _, ok := byName[want]; !ok {
			t.Errorf("manifest missing tool %q", want)
		}
	}

	so, ok := byName["submit_outcome"]
	if !ok {
		t.Fatal("manifest missing submit_outcome")
	}
	for field, want := range map[string][]string{
		// The three gate actions are load-bearing OUTSIDE this repo:
		// tatara-agent-skills' validate_tool_calls.py fetches the published
		// tool-manifest.json and hard-fails on an action literal absent here,
		// so the skills MR documenting submit_outcome(action="approved")
		// cannot go green until this ships in a release.
		"action":  {"submitted", "declined", "approved", "discuss", "rejected", "file_issue", "propose", "skip", "exhausted"},
		"verdict": {"approve", "request_changes"},
	} {
		got := map[string]bool{}
		for _, v := range so.Enums[field] {
			got[v] = true
		}
		for _, v := range want {
			if !got[v] {
				t.Errorf("submit_outcome.%s missing value %q, got %v", field, v, so.Enums[field])
			}
		}
	}

	// clarify's `decision` field is deleted with the profile. It must vanish
	// from the manifest, not linger with a stale value set: validate_tool_calls.py
	// skips fields the manifest does not track, so an absent `decision` makes
	// the skills repo's residual submit_outcome(decision=...) prose inert
	// rather than an error, which is what lets MR4 release before MR3 lands.
	if _, ok := so.Enums["decision"]; ok {
		t.Errorf("submit_outcome.decision must be gone with the clarify profile, got %v", so.Enums["decision"])
	}

	mw, ok := byName["mr_write"]
	if !ok {
		t.Fatal("manifest missing mr_write")
	}
	got := map[string]bool{}
	for _, v := range mw.Enums["action"] {
		got[v] = true
	}
	for _, v := range []string{"open", "comment", "reply"} {
		if !got[v] {
			t.Errorf("mr_write.action missing value %q, got %v", v, mw.Enums["action"])
		}
	}
}
