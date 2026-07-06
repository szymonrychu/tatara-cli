package mcp

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// allRegisteredNames returns the union of all tool names across all groups.
func allRegisteredNames() map[string]bool {
	all := make(map[string]bool)
	for _, t := range AllTools() {
		all[t.Name] = true
	}
	for _, t := range OperatorTools() {
		all[t.Name] = true
	}
	for _, t := range ChatTools() {
		all[t.Name] = true
	}
	for _, t := range PlatformTools() {
		all[t.Name] = true
	}
	for _, t := range HandoffTools() {
		all[t.Name] = true
	}
	return all
}

func TestResolveProfile_EmptyReturnsNil(t *testing.T) {
	var buf strings.Builder
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	result := resolveProfile("", log)
	assert.Nil(t, result, "empty profile must return nil (fail-open)")
	// should log a WARN
	assert.Contains(t, buf.String(), "level=WARN", "empty profile must log WARN")
}

func TestResolveProfile_UnknownFailsClosed(t *testing.T) {
	var buf strings.Builder
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	result := resolveProfile("nonsense-kind", log)
	require.NotNil(t, result, "unknown non-empty profile must fail closed to the tightest safe set, not nil")
	assert.Contains(t, buf.String(), "level=WARN", "unknown profile must log WARN")
	// must not grant any write/mutation tools.
	for _, name := range []string{
		"change_summary", "decline_implementation", "already_done", "submit_handover",
		"review_verdict", "task_update", "subtask_create", "subtask_update",
		"comment_on_issue", "create_issue", "close_issue", "edit_issue",
	} {
		assert.False(t, result[name], "unknown profile must NOT grant %q", name)
	}
	// must only be the alwaysOn set.
	assert.Equal(t, len(alwaysOn), len(result), "unknown profile must be exactly the alwaysOn set")
	for _, name := range alwaysOn {
		assert.True(t, result[name], "unknown profile must include alwaysOn tool %q", name)
	}
}

func TestResolveProfile_KnownProfilesNonEmpty(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, profile := range []string{"brainstorm", "implement", "review", "triage", "lifecycle", "incident", "documentation"} {
		result := resolveProfile(profile, log)
		assert.NotNil(t, result, "profile %q must return non-nil set", profile)
		assert.Greater(t, len(result), 0, "profile %q must be non-empty", profile)
	}
}

func TestResolveProfile_AlwaysOnInAllProfiles(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	alwaysOn := []string{"report_internal_issue", "project_get", "repo_list", "task_get"}
	for _, profile := range []string{"brainstorm", "implement", "review", "triage", "lifecycle", "incident", "documentation"} {
		result := resolveProfile(profile, log)
		require.NotNil(t, result, "profile %q must resolve", profile)
		for _, name := range alwaysOn {
			assert.True(t, result[name], "profile %q must include alwaysOn tool %q", profile, name)
		}
	}
}

func TestResolveProfile_ReportInternalIssueInEveryProfile(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, profile := range []string{"brainstorm", "implement", "review", "triage", "lifecycle", "incident", "documentation"} {
		result := resolveProfile(profile, log)
		require.NotNil(t, result)
		assert.True(t, result["report_internal_issue"], "report_internal_issue must be in profile %q", profile)
	}
}

func TestResolveProfile_AllNamesExistInRegistries(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	registered := allRegisteredNames()
	for _, profile := range []string{"brainstorm", "implement", "review", "triage", "lifecycle", "incident", "documentation"} {
		result := resolveProfile(profile, log)
		require.NotNil(t, result, "profile %q must resolve", profile)
		for name := range result {
			assert.True(t, registered[name], "profile %q references unknown tool %q", profile, name)
		}
	}
}

func TestGroupCodeGraph_PostPartBNames(t *testing.T) {
	// After Part B, code_community and code_hyperedge must NOT exist.
	// code_communities and code_hyperedges take their roles (with discriminator).
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	result := resolveProfile("brainstorm", log) // brainstorm has full code-graph
	require.NotNil(t, result)

	assert.False(t, result["code_community"], "code_community must not exist after Part B merge")
	assert.False(t, result["code_hyperedge"], "code_hyperedge must not exist after Part B merge")
	assert.True(t, result["code_communities"], "code_communities must exist after Part B merge")
	assert.True(t, result["code_hyperedges"], "code_hyperedges must exist after Part B merge")
}

func TestGroupCodeGraph_Has19Tools(t *testing.T) {
	// After Part B: 21 - 2 = 19 code-graph tools.
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	all := allRegisteredNames()
	codeGraphCount := 0
	for name := range all {
		if strings.HasPrefix(name, "code_") {
			codeGraphCount++
		}
	}
	assert.Equal(t, 19, codeGraphCount, "AllTools must have exactly 19 code_* tools after Part B")

	// Also verify groupCodeGraph in a profile has exactly 19
	result := resolveProfile("brainstorm", log)
	require.NotNil(t, result)
	inProfile := 0
	for name := range result {
		if strings.HasPrefix(name, "code_") {
			inProfile++
		}
	}
	assert.Equal(t, 19, inProfile, "brainstorm profile must include exactly 19 code_* tools")
}

func TestBrainstormProfile_ChatIncluded(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	result := resolveProfile("brainstorm", log)
	require.NotNil(t, result)
	// brainstorm gets chat
	assert.True(t, result["chat_create_room"], "brainstorm must include chat_create_room")
}

func TestBrainstormProfile_CorrectOperatorTools(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	result := resolveProfile("brainstorm", log)
	require.NotNil(t, result)

	// must have
	for _, name := range []string{"task_list", "subtask_list", "subtask_create", "subtask_update",
		"propose_issue", "comment_on_issue", "skip_research"} {
		assert.True(t, result[name], "brainstorm must include %q", name)
	}
	// must NOT have
	for _, name := range []string{"change_summary", "review_verdict", "issue_outcome", "pr_outcome"} {
		assert.False(t, result[name], "brainstorm must NOT include %q", name)
	}
}

func TestBrainstormProfile_NoChatExclusions(t *testing.T) {
	// Brainstorm has chat; verify it also keeps comment_on_issue + propose_issue + skip_research
	// (the spec explicitly notes brainstorm must keep these)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	result := resolveProfile("brainstorm", log)
	require.NotNil(t, result)
	assert.True(t, result["comment_on_issue"])
	assert.True(t, result["propose_issue"])
	assert.True(t, result["skip_research"])
}

func TestTriageProfile_NoChangeSummaryPrOutcome(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	result := resolveProfile("triage", log)
	require.NotNil(t, result)
	assert.False(t, result["change_summary"], "triage must NOT include change_summary")
	assert.False(t, result["pr_outcome"], "triage must NOT include pr_outcome")
	// triage keeps issue_outcome and comment
	assert.True(t, result["issue_outcome"])
	assert.True(t, result["comment"])
	assert.True(t, result["comment_on_issue"])
}

func TestTriageProfile_NoChat(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	result := resolveProfile("triage", log)
	require.NotNil(t, result)
	assert.False(t, result["chat_create_room"], "triage must NOT include chat tools")
}

func TestLifecycleProfile_HasChatAndUnionTools(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	result := resolveProfile("lifecycle", log)
	require.NotNil(t, result)
	// lifecycle has chat
	assert.True(t, result["chat_create_room"])
	// lifecycle has union terminal set
	for _, name := range []string{
		"task_list", "task_update", "subtask_list", "subtask_create", "subtask_update",
		"issue_outcome", "comment", "comment_on_issue", "change_summary",
		"decline_implementation", "already_done", "pr_outcome", "review_verdict", "submit_handover",
	} {
		assert.True(t, result[name], "lifecycle must include %q", name)
	}
}

func TestIncidentProfile_HasChat(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	result := resolveProfile("incident", log)
	require.NotNil(t, result)
	assert.True(t, result["chat_create_room"], "incident must include chat")
}

func TestImplementProfile_NoChatNoTerminalDiscovery(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	result := resolveProfile("implement", log)
	require.NotNil(t, result)
	assert.False(t, result["chat_create_room"], "implement must NOT include chat tools")
	// must have code-mutation terminals
	assert.True(t, result["change_summary"])
	assert.True(t, result["decline_implementation"])
	assert.True(t, result["already_done"])
}

func TestReviewProfile_NoChatLimitedTools(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	result := resolveProfile("review", log)
	require.NotNil(t, result)
	assert.False(t, result["chat_create_room"], "review must NOT include chat tools")
	assert.True(t, result["review_verdict"])
	assert.True(t, result["submit_handover"])
}

func TestDocumentationProfile_CorrectOperatorTools(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	result := resolveProfile("documentation", log)
	require.NotNil(t, result, "documentation profile must resolve (not fail closed)")

	// must have: single-shot doc-editing terminals + subtask continuity.
	for _, name := range []string{
		"task_update", "subtask_list", "subtask_create", "subtask_update",
		"change_summary", "decline_implementation", "already_done",
	} {
		assert.True(t, result[name], "documentation must include %q", name)
	}
	// no chat; no issue/PR/review escalation surface (docs agent never touches
	// the triggering component repo's issue tracker, only opens/edits the docs MR).
	for _, name := range []string{
		"chat_create_room", "propose_issue", "comment_on_issue", "comment",
		"issue_outcome", "pr_outcome", "review_verdict", "create_issue", "close_issue", "edit_issue",
	} {
		assert.False(t, result[name], "documentation must NOT include %q", name)
	}
}

func TestDocumentationProfile_HasMemoryAndCodeGraph(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	result := resolveProfile("documentation", log)
	require.NotNil(t, result)
	assert.True(t, result["query"], "documentation profile must include memory tools")
	assert.True(t, result["code_search"], "documentation profile must include code-graph tools")
}

func TestDocumentationProfile_NoChat(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	result := resolveProfile("documentation", log)
	require.NotNil(t, result)
	assert.False(t, result["chat_create_room"], "documentation profile must NOT include chat tools")
}

func TestDocumentationProfile_HasHandoffNotDelete(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	result := resolveProfile("documentation", log)
	require.NotNil(t, result)
	for _, name := range []string{"write_handoff", "get_handoff", "list_handoffs"} {
		assert.True(t, result[name], "documentation must include %q", name)
	}
	assert.False(t, result["delete_handoff"], "documentation must NOT include delete_handoff (groomer-only)")
}

// TestKindToProfile: table-driven mapping from CRD kind -> profile name.
func TestKindToProfile(t *testing.T) {
	cases := []struct {
		kind    string
		profile string
	}{
		{"implement", "implement"},
		{"review", "review"},
		{"triageIssue", "triage"},
		{"brainstorm", "brainstorm"},
		{"issueLifecycle", "lifecycle"},
		{"incident", "incident"},
		{"healthCheck", "brainstorm"}, // healthCheck shares Kind=brainstorm
		{"refine", "refine"},
		{"documentation", "documentation"},
		{"unknown", ""},
		{"", ""},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			got := toolProfileForKind(c.kind)
			assert.Equal(t, c.profile, got, "kind=%q", c.kind)
		})
	}
}

func TestProfile_Refine(t *testing.T) {
	if toolProfileForKind("refine") != "refine" {
		t.Fatal("refine kind must map to refine profile")
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	result := resolveProfile("refine", log)
	require.NotNil(t, result, "refine profile must resolve")

	must := []string{
		"list_issues", "list_commits", "close_issue", "edit_issue",
		"comment_on_issue", "task_list", "task_get", "repo_list", "report_internal_issue",
	}
	for _, m := range must {
		assert.True(t, result[m], "refine profile missing %q", m)
	}
	// Deny-by-default: create_issue (escalation + trigger-label vector) and every
	// SCM-mutation / lifecycle-escalation tool must be absent from a refine pod.
	mustNot := []string{
		"create_issue",
		"propose_issue",
		"issue_outcome",
		"decline_implementation",
		"already_done",
		"review_verdict",
		"pr_outcome",
		"change_summary",
		"task_update",
		"subtask_create",
	}
	for _, no := range mustNot {
		assert.False(t, result[no], "refine profile must NOT grant %q", no)
	}
}

func TestRefineProfile_HasMemoryAndCodeGraph(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	result := resolveProfile("refine", log)
	require.NotNil(t, result)
	assert.True(t, result["query"], "refine profile must include memory tools")
	assert.True(t, result["code_search"], "refine profile must include code-graph tools")
}

func TestRefineProfile_NoChat(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	result := resolveProfile("refine", log)
	require.NotNil(t, result)
	assert.False(t, result["chat_create_room"], "refine profile must NOT include chat tools")
}

// TestHandoffGroup_WriteGetListInContinuityProfiles verifies the 5 continuity
// profiles (implement, lifecycle, incident, brainstorm, refine) get
// write_handoff/get_handoff/list_handoffs.
func TestHandoffGroup_WriteGetListInContinuityProfiles(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, profile := range []string{"implement", "lifecycle", "incident", "brainstorm", "refine", "documentation"} {
		result := resolveProfile(profile, log)
		require.NotNil(t, result, "profile %q must resolve", profile)
		for _, name := range []string{"write_handoff", "get_handoff", "list_handoffs"} {
			assert.True(t, result[name], "profile %q must include %q", profile, name)
		}
	}
}

// TestHandoffGroup_AbsentFromNonContinuityProfiles verifies review and triage
// get none of the 4 handoff tools.
func TestHandoffGroup_AbsentFromNonContinuityProfiles(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, profile := range []string{"review", "triage"} {
		result := resolveProfile(profile, log)
		require.NotNil(t, result, "profile %q must resolve", profile)
		for _, name := range []string{"write_handoff", "get_handoff", "list_handoffs", "delete_handoff"} {
			assert.False(t, result[name], "profile %q must NOT include %q", profile, name)
		}
	}
}

// TestHandoffDelete_RefineOnly verifies delete_handoff is granted only to
// refine (the groomer); every other continuity profile can write/get/list
// but must not delete.
func TestHandoffDelete_RefineOnly(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	refine := resolveProfile("refine", log)
	require.NotNil(t, refine)
	assert.True(t, refine["delete_handoff"], "refine must include delete_handoff")

	for _, profile := range []string{"implement", "lifecycle", "incident", "brainstorm"} {
		result := resolveProfile(profile, log)
		require.NotNil(t, result, "profile %q must resolve", profile)
		assert.False(t, result["delete_handoff"], "profile %q must NOT include delete_handoff (groomer-only)", profile)
	}
}

func TestSelfImproveProfileRemoved(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	// Kind no longer maps to a profile.
	assert.Equal(t, "", toolProfileForKind("selfImprove"), "selfImprove kind must not map to a profile")
	// The named profile no longer exists: resolveProfile fails closed to alwaysOn.
	result := resolveProfile("selfImprove", log)
	require.NotNil(t, result)
	assert.False(t, result["pr_outcome"], "removed selfImprove profile must not grant pr_outcome")
	assert.False(t, result["change_summary"], "removed selfImprove profile must not grant change_summary")
	assert.True(t, result["report_internal_issue"], "alwaysOn set must survive fail-closed")
}

func TestLifecycleProfileHasPrOutcomeAndCodeGraph(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	result := resolveProfile("lifecycle", log)
	require.NotNil(t, result)
	assert.True(t, result["pr_outcome"], "lifecycle must retain pr_outcome for close signalling")
	assert.True(t, result["code_search"], "lifecycle must include the code-graph group")
}

func TestBrainstormProfileHasCodeGraph(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	result := resolveProfile("brainstorm", log)
	require.NotNil(t, result)
	for _, tool := range []string{"code_search", "code_explain", "code_cross_repo", "code_important"} {
		assert.True(t, result[tool], "brainstorm must include code-graph tool %q", tool)
	}
	assert.True(t, result["propose_issue"], "brainstorm must retain propose_issue")
}
