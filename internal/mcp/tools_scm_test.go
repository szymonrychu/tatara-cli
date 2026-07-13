package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSCMTools_Count(t *testing.T) {
	require.Len(t, SCMTools(), 3, "contract D.2: scm_read, issue_write, mr_write")
}

func TestSCMRead_KindPathMap(t *testing.T) {
	t.Setenv("TATARA_PROJECT", "tatara")
	tl := toolByName(t, SCMTools(), "scm_read")
	cases := map[string]string{
		"issues":   "/projects/tatara/scm/issues",
		"mr":       "/projects/tatara/scm/mrs",
		"comments": "/projects/tatara/scm/comments",
		"commits":  "/projects/tatara/scm/commits",
		"ci":       "/projects/tatara/scm/ci",
	}
	for kind, want := range cases {
		args := map[string]any{"kind": kind, "repo": "tatara-operator"}
		if kind == "ci" || kind == "comments" {
			args["number"] = 291
		}
		m, p, _, err := tl.Build(args)
		require.NoError(t, err, "kind=%s", kind)
		require.Equal(t, "GET", m)
		require.Contains(t, p, want, "kind=%s", kind)
	}
}

func TestSCMRead_RepoIsRequiredOnEveryKind(t *testing.T) {
	t.Setenv("TATARA_PROJECT", "tatara")
	tl := toolByName(t, SCMTools(), "scm_read")
	for _, kind := range []string{"issues", "mr", "comments", "commits", "ci"} {
		_, _, _, err := tl.Build(map[string]any{"kind": kind, "number": 1})
		require.Error(t, err, "kind=%s must require repo (contract C.1: an omitted repo fans out ~60 unpaced forge requests)", kind)
	}
}

func TestSCMRead_NumberRequiredForCIAndComments(t *testing.T) {
	t.Setenv("TATARA_PROJECT", "tatara")
	tl := toolByName(t, SCMTools(), "scm_read")
	for _, kind := range []string{"ci", "comments"} {
		_, _, _, err := tl.Build(map[string]any{"kind": kind, "repo": "tatara-operator"})
		require.Error(t, err, "kind=%s requires number", kind)
	}
}

func TestIssueWrite_HasNoStatusAndNoLabelsParam(t *testing.T) {
	tl := toolByName(t, SCMTools(), "issue_write")
	var schema struct {
		Properties map[string]any `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(tl.Schema, &schema))
	require.NotContains(t, schema.Properties, "status", "approval is operator-owned and agent-unwritable (contract C.2.12)")
	require.NotContains(t, schema.Properties, "labels", "a labels param would let an agent stamp the trigger label and self-escalate")
}

func TestIssueWrite_Actions(t *testing.T) {
	t.Setenv("TATARA_PROJECT", "tatara")
	t.Setenv("TATARA_TASK", "t1")
	tl := toolByName(t, SCMTools(), "issue_write")
	argsByAction := map[string]map[string]any{
		"create":  {"action": "create", "repo": "tatara-operator", "title": "t", "body": "b"},
		"edit":    {"action": "edit", "repo": "tatara-operator", "number": 291, "title": "t", "body": "b"},
		"close":   {"action": "close", "repo": "tatara-operator", "number": 291, "comment": "c"},
		"comment": {"action": "comment", "repo": "tatara-operator", "number": 291, "body": "b"},
	}
	for _, a := range []string{"create", "edit", "close", "comment"} {
		_, _, _, err := tl.Build(argsByAction[a])
		require.NoError(t, err, "action=%s", a)
	}
	_, _, _, err := tl.Build(map[string]any{"action": "reopen", "repo": "tatara-operator"})
	require.Error(t, err, "issue_write has exactly four actions")
}

func TestIssueWrite_CreateForbidsNumber(t *testing.T) {
	t.Setenv("TATARA_PROJECT", "tatara")
	tl := toolByName(t, SCMTools(), "issue_write")
	_, _, _, err := tl.Build(map[string]any{
		"action": "create", "repo": "r", "title": "t", "body": "b", "number": 291,
	})
	require.Error(t, err, "action=create forbids number: the operator assigns it")
}

func TestIssueWrite_CloseRequiresComment(t *testing.T) {
	t.Setenv("TATARA_PROJECT", "tatara")
	tl := toolByName(t, SCMTools(), "issue_write")
	_, _, _, err := tl.Build(map[string]any{"action": "close", "repo": "tatara-operator", "number": 291})
	require.Error(t, err, "every close cites its reason (contract C.2.12)")
}

func TestMRWrite_HasNoMergeAction(t *testing.T) {
	tl := toolByName(t, SCMTools(), "mr_write")
	var schema struct {
		Properties struct {
			Action struct {
				Enum []string `json:"enum"`
			} `json:"action"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(tl.Schema, &schema))
	require.Equal(t, []string{"open", "comment", "reply"}, schema.Properties.Action.Enum,
		"no merge, no approve, no request_changes: merge is operator-only and the operator posts every review (contract C.2.13, fix 14)")
}

func TestMRWrite_OpenForbidsNumber(t *testing.T) {
	t.Setenv("TATARA_PROJECT", "tatara")
	tl := toolByName(t, SCMTools(), "mr_write")
	_, _, _, err := tl.Build(map[string]any{
		"action": "open", "repo": "r", "title": "t", "body": "b", "number": 291,
	})
	require.Error(t, err, "action=open forbids number: the operator assigns it")
}

func TestMRWrite_ReplyRequiresInReplyTo(t *testing.T) {
	t.Setenv("TATARA_PROJECT", "tatara")
	tl := toolByName(t, SCMTools(), "mr_write")
	_, _, _, err := tl.Build(map[string]any{"action": "reply", "repo": "tatara-cli", "number": 80, "body": "ack"})
	require.Error(t, err, "action=reply needs the externalId it is replying to")
}

func TestIssueWrite_DescriptionDocumentsSyncVsDeferred(t *testing.T) {
	tl := toolByName(t, SCMTools(), "issue_write")
	require.Contains(t, tl.Description, "SYNCHRONOUS", "action=create returns the number inline; nothing else does (contract fix M7)")
	require.Contains(t, tl.Description, "DEFERRED", "edit/close/comment are posted by a reconciler, not in this call (contract fix M7)")
}

func TestMRWrite_DescriptionDocumentsSyncVsDeferred(t *testing.T) {
	tl := toolByName(t, SCMTools(), "mr_write")
	require.Contains(t, tl.Description, "SYNCHRONOUS", "action=open returns the number inline; nothing else does (contract fix M7)")
	require.Contains(t, tl.Description, "DEFERRED", "comment/reply are posted by a reconciler, not in this call (contract fix M7)")
}

func TestIssueWrite_SendsTaskFromEnv(t *testing.T) {
	t.Setenv("TATARA_PROJECT", "tatara")
	t.Setenv("TATARA_TASK", "tatara-implement-2026-07-12-abcd")
	tl := toolByName(t, SCMTools(), "issue_write")
	_, _, body, err := tl.Build(map[string]any{"action": "comment", "repo": "tatara-operator", "number": 291, "body": "b"})
	require.NoError(t, err)
	m, ok := body.(map[string]any)
	require.True(t, ok, "body must be a map")
	require.Equal(t, "tatara-implement-2026-07-12-abcd", m["task"], "issue_write must send task so the operator's taskParam/callerTask does not 400")
}

func TestMRWrite_SendsTaskFromEnv(t *testing.T) {
	t.Setenv("TATARA_PROJECT", "tatara")
	t.Setenv("TATARA_TASK", "tatara-implement-2026-07-12-abcd")
	tl := toolByName(t, SCMTools(), "mr_write")
	_, _, body, err := tl.Build(map[string]any{"action": "comment", "repo": "tatara-cli", "number": 80, "body": "b"})
	require.NoError(t, err)
	m, ok := body.(map[string]any)
	require.True(t, ok, "body must be a map")
	require.Equal(t, "tatara-implement-2026-07-12-abcd", m["task"], "mr_write must send task so the operator's taskParam/callerTask does not 400")
}
