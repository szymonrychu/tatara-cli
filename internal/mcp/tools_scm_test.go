package mcp

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSCMTools_Count(t *testing.T) {
	require.Len(t, SCMTools(), 4) // was 3; mr_takeover_request added
}

func TestMRTakeoverRequest_BuildsOperatorPost(t *testing.T) {
	tl := toolByName(t, SCMTools(), "mr_takeover_request")
	require.Equal(t, TargetOperator, tl.Target)
	t.Setenv("TATARA_PROJECT", "proj-a")
	t.Setenv("TATARA_TASK", "review-task")
	method, path, body, err := tl.Build(map[string]any{
		"repo": "repo-a", "number": 9, "comment_external_id": "10",
	})
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, method)
	require.Equal(t, "/projects/proj-a/scm/mr-takeover", path)
	m := body.(map[string]any)
	require.Equal(t, "10", m["commentExternalId"])
	require.Equal(t, "review-task", m["task"])
	require.NotContains(t, m, "project") // project is in the path, stripped from body
}

func TestMRTakeoverRequest_RequiresCommentAndRepo(t *testing.T) {
	tl := toolByName(t, SCMTools(), "mr_takeover_request")
	_, _, _, err := tl.Build(map[string]any{"repo": "repo-a", "number": 9})
	require.Error(t, err) // comment_external_id required
	_, _, _, err = tl.Build(map[string]any{"comment_external_id": "10", "number": 9})
	require.Error(t, err) // repo required
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

// scmAdvertisedArgs pins ONE optional schema arg of scm_read to the query
// parameter Build must produce for it. It is the cli half of the operator's
// scm_read parameter table (tatara-operator#636). `since` was advertised for
// kind=issues|mr and forwarded for issues only, so an agent asking for recent
// merge requests got the whole list, oldest-first.
//
// Two args are spelled differently on the wire: since_days -> sinceDays and
// is_pr -> isPR. Integers are passed as Go ints here, which is the shape
// argString does NOT handle - a bespoke coercion path is how since_days came
// to be droppable.
//
// The table is hand-written, so its COMPLETENESS is pinned separately, against
// the schema literal itself: see TestSCMRead_ArgTableCoversEveryOptionalSchemaProperty.
func scmAdvertisedArgs() []struct {
	kind, arg string
	value     any
	want      string
} {
	return []struct {
		kind, arg string
		value     any
		want      string
	}{
		{"issues", "state", "closed", "state=closed"},
		{"issues", "since", "2026-08-20T00:00:00Z", "since=2026-08-20T00%3A00%3A00Z"},
		{"issues", "labels", "bug", "labels=bug"},
		{"issues", "limit", 5, "limit=5"},
		{"mr", "state", "merged", "state=merged"},
		{"mr", "since", "2026-08-20T00:00:00Z", "since=2026-08-20T00%3A00%3A00Z"},
		{"mr", "limit", 5, "limit=5"},
		{"comments", "is_pr", true, "isPR=true"},
		{"commits", "since_days", 7, "sinceDays=7"},
		{"commits", "limit", 5, "limit=5"},
	}
}

func TestSCMRead_EveryAdvertisedArgReachesTheQuery(t *testing.T) {
	t.Setenv("TATARA_PROJECT", "tatara")
	tl := toolByName(t, SCMTools(), "scm_read")
	for _, tc := range scmAdvertisedArgs() {
		t.Run(tc.kind+"/"+tc.arg, func(t *testing.T) {
			args := map[string]any{"kind": tc.kind, "repo": "tatara-operator", tc.arg: tc.value}
			if tc.kind == "ci" || tc.kind == "comments" {
				args["number"] = 291
			}
			_, path, _, err := tl.Build(args)
			require.NoError(t, err)
			require.Contains(t, path, tc.want,
				"scm_read advertises %q for kind=%s and Build drops it", tc.arg, tc.kind)
		})
	}
}

// scmReadStructuralArgs are the scm_read schema properties that are NOT per-kind
// optionals, so they are not table rows: kind and repo are required and pinned by
// TestSCMRead_KindPathMap / TestSCMRead_RepoIsRequiredOnEveryKind, project is
// resolved from TATARA_PROJECT, and number is pinned by
// TestSCMRead_NumberRequiredForCIAndComments.
var scmReadStructuralArgs = map[string]bool{"kind": true, "repo": true, "project": true, "number": true}

// A hand-written table only pins the args someone remembered to list: adding
// `author` to the schema and forgetting it in Build is `since`'s failure mode
// verbatim. The schema literal is in-package here, so the completeness half is
// reachable without a fetch or a cross-module import - unlike the operator's
// mirror of this table, which pins its count instead.
func TestSCMRead_ArgTableCoversEveryOptionalSchemaProperty(t *testing.T) {
	tl := toolByName(t, SCMTools(), "scm_read")
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(tl.Schema, &schema))

	covered := map[string]bool{}
	for _, tc := range scmAdvertisedArgs() {
		covered[tc.arg] = true
	}
	for name := range schema.Properties {
		if scmReadStructuralArgs[name] {
			continue
		}
		require.True(t, covered[name],
			"scm_read advertises %q and scmAdvertisedArgs does not pin it: add a row, "+
				"or add it to scmReadStructuralArgs if it is not a per-kind optional", name)
	}
	for arg := range covered {
		require.Contains(t, schema.Properties, arg,
			"scmAdvertisedArgs pins %q, which the scm_read schema no longer advertises", arg)
	}
}

// The schema description is the only place an agent learns that limit takes the
// NEWEST N and that since compares against the forge's updatedAt.
func TestSCMRead_SchemaDocumentsSinceAndLimitSemantics(t *testing.T) {
	tl := toolByName(t, SCMTools(), "scm_read")
	var schema struct {
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(tl.Schema, &schema))
	require.Contains(t, schema.Properties["since"].Description, "updatedAt")
	require.Contains(t, schema.Properties["limit"].Description, "NEWEST")
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
