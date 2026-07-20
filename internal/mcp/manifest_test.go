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
		"action":   {"submitted", "declined", "file_issue", "propose"},
		"decision": {"implement", "close", "discuss"},
		"verdict":  {"approve", "request_changes"},
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
