package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

var outcomeProfiles = []string{"brainstorm", "incident", "clarify", "implement", "review", "refine", "documentation"}

func TestOutcomeTool_ExistsForAllSevenAgentKinds(t *testing.T) {
	for _, p := range outcomeProfiles {
		tl, ok := OutcomeTool(p)
		require.True(t, ok, "profile %q must have a submit_outcome; a pod with no terminal tool cannot finish its Task", p)
		require.Equal(t, "submit_outcome", tl.Name, "one name, seven schemas")
	}
}

func TestOutcomeTool_EmptyAndUnknownProfileHaveNone(t *testing.T) {
	for _, p := range []string{"", "triage", "lifecycle", "selfImprove", "nonsense"} {
		_, ok := OutcomeTool(p)
		require.False(t, ok, "profile %q must NOT get a submit_outcome (fail closed)", p)
	}
}

func TestOutcomeTool_SchemaGoldens(t *testing.T) {
	for _, p := range outcomeProfiles {
		tl, ok := OutcomeTool(p)
		require.True(t, ok)
		golden := filepath.Join("testdata", "outcome-"+p+".schema.json")
		want, err := os.ReadFile(golden)
		require.NoError(t, err, "golden %s", golden)
		var g, s any
		require.NoError(t, json.Unmarshal(want, &g))
		require.NoError(t, json.Unmarshal(tl.Schema, &s))
		require.Equal(t, g, s, "submit_outcome schema for %q drifted from contract D.1", p)
	}
}

func TestOutcomeTool_TargetIsOperator(t *testing.T) {
	for _, p := range outcomeProfiles {
		tl, _ := OutcomeTool(p)
		require.Equal(t, TargetOperator, tl.Target, "submit_outcome for %q must dispatch to the operator", p)
	}
}

func TestOutcome_PostsTheEnvelopeWithTheProfileAsKind(t *testing.T) {
	t.Setenv("TATARA_TASK", "t1")
	tl, _ := OutcomeTool("clarify")
	m, p, body, err := tl.Build(map[string]any{"decision": "implement", "reason": "szymonrychu said 'go ahead' on tatara-operator#291"})
	require.NoError(t, err)
	require.Equal(t, "POST", m)
	require.Equal(t, "/tasks/t1/outcome", p)

	raw, err := json.Marshal(body)
	require.NoError(t, err)
	var env struct {
		Kind    string         `json:"kind"`
		Payload map[string]any `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(raw, &env))
	require.Equal(t, "clarify", env.Kind, "kind MUST equal the pod's agent kind; the operator 409s a mismatch")
	require.Equal(t, "implement", env.Payload["decision"])
	require.Equal(t, "szymonrychu said 'go ahead' on tatara-operator#291", env.Payload["reason"])
}

func TestOutcome_RequiresATask(t *testing.T) {
	t.Setenv("TATARA_TASK", "")
	tl, _ := OutcomeTool("clarify")
	_, _, _, err := tl.Build(map[string]any{"decision": "close", "reason": "dup"})
	require.Error(t, err, "no task argument and no TATARA_TASK must fail fast")
}

func TestOutcome_TaskArgIsNotOnTheWire(t *testing.T) {
	t.Setenv("TATARA_TASK", "")
	tl, _ := OutcomeTool("clarify")
	_, p, body, err := tl.Build(map[string]any{"task": "t9", "decision": "close", "reason": "dup"})
	require.NoError(t, err)
	require.Equal(t, "/tasks/t9/outcome", p)
	raw, _ := json.Marshal(body)
	var env struct {
		Payload map[string]any `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(raw, &env))
	require.NotContains(t, env.Payload, "task", "task is the path segment; the operator's payload decoder uses DisallowUnknownFields")
}

func TestOutcome_ImplementMapsSnakeCaseArgsToCamelCasePayload(t *testing.T) {
	t.Setenv("TATARA_TASK", "t1")
	tl, _ := OutcomeTool("implement")
	_, _, body, err := tl.Build(map[string]any{
		"action":              "submitted",
		"title":               "fix: reaper",
		"body":                "Closes #291.",
		"change_significance": "minor",
		"merge_order":         []any{"tatara-operator", "tatara-cli"},
	})
	require.NoError(t, err)
	raw, _ := json.Marshal(body)
	var env struct {
		Payload map[string]any `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(raw, &env))
	require.Equal(t, "minor", env.Payload["changeSignificance"])
	require.Equal(t, []any{"tatara-operator", "tatara-cli"}, env.Payload["mergeOrder"])
	require.NotContains(t, env.Payload, "change_significance", "the wire is camelCase (contract C.2.7)")
	require.NotContains(t, env.Payload, "merge_order")
}

func TestOutcome_ImplementDeclineMapsReason(t *testing.T) {
	t.Setenv("TATARA_TASK", "t1")
	tl, _ := OutcomeTool("implement")
	_, _, body, err := tl.Build(map[string]any{"action": "declined", "decline_reason": "the issue is already fixed on main"})
	require.NoError(t, err)
	raw, _ := json.Marshal(body)
	var env struct {
		Payload map[string]any `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(raw, &env))
	require.Equal(t, "the issue is already fixed on main", env.Payload["reason"],
		"the tool arg is decline_reason; the wire field is reason (contract C.2.7)")
}

func TestOutcome_ImplementGates(t *testing.T) {
	t.Setenv("TATARA_TASK", "t1")
	for _, profile := range []string{"implement", "documentation"} {
		tl, _ := OutcomeTool(profile)
		t.Run(profile+"/no action", func(t *testing.T) {
			_, _, _, err := tl.Build(map[string]any{"title": "t", "body": "b", "change_significance": "patch"})
			require.Error(t, err)
		})
		t.Run(profile+"/submitted without title", func(t *testing.T) {
			_, _, _, err := tl.Build(map[string]any{"action": "submitted", "body": "b", "change_significance": "patch"})
			require.Error(t, err)
		})
		t.Run(profile+"/submitted without body", func(t *testing.T) {
			_, _, _, err := tl.Build(map[string]any{"action": "submitted", "title": "t", "change_significance": "patch"})
			require.Error(t, err)
		})
		t.Run(profile+"/submitted without change_significance", func(t *testing.T) {
			_, _, _, err := tl.Build(map[string]any{"action": "submitted", "title": "t", "body": "b"})
			require.Error(t, err)
		})
		t.Run(profile+"/submitted with decline_reason", func(t *testing.T) {
			_, _, _, err := tl.Build(map[string]any{
				"action": "submitted", "title": "t", "body": "b", "change_significance": "patch",
				"decline_reason": "no",
			})
			require.Error(t, err, "action=submitted forbids decline_reason (contract C.2.7)")
		})
		t.Run(profile+"/declined without reason", func(t *testing.T) {
			_, _, _, err := tl.Build(map[string]any{"action": "declined", "decline_reason": "   "})
			require.Error(t, err, "action=declined requires a NON-EMPTY decline_reason")
		})
		t.Run(profile+"/declined with title", func(t *testing.T) {
			_, _, _, err := tl.Build(map[string]any{"action": "declined", "decline_reason": "no", "title": "t"})
			require.Error(t, err, "action=declined forbids title/body/change_significance")
		})
	}
}

func TestOutcome_ReviewMapsReviewedSHAs(t *testing.T) {
	t.Setenv("TATARA_TASK", "t1")
	tl, _ := OutcomeTool("review")
	_, _, body, err := tl.Build(map[string]any{
		"verdict":       "approve",
		"reviewed_shas": []any{map[string]any{"repo": "tatara-cli", "number": float64(80), "sha": "abc123"}},
	})
	require.NoError(t, err)
	raw, _ := json.Marshal(body)
	var env struct {
		Payload map[string]any `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(raw, &env))
	require.Contains(t, env.Payload, "reviewedSHAs")
	require.NotContains(t, env.Payload, "reviewed_shas")
}

func TestOutcome_ReviewReviewedSHAsRequired(t *testing.T) {
	t.Setenv("TATARA_TASK", "t1")
	tl, _ := OutcomeTool("review")
	_, _, _, err := tl.Build(map[string]any{"verdict": "approve"})
	require.Error(t, err, "reviewed_shas is REQUIRED, not optional (contract D.1, addendum 5): a missing entry is a 400, never a silent 'unreviewed but fine'")
	_, _, _, err = tl.Build(map[string]any{"verdict": "approve", "reviewed_shas": []any{}})
	require.Error(t, err, "reviewed_shas must be non-empty client-side too, mirroring the schema's minItems:1")
}

func TestOutcome_ReviewRequestChangesNeedsFindings(t *testing.T) {
	t.Setenv("TATARA_TASK", "t1")
	tl, _ := OutcomeTool("review")
	_, _, _, err := tl.Build(map[string]any{
		"verdict":       "request_changes",
		"reviewed_shas": []any{map[string]any{"repo": "tatara-cli", "number": float64(80), "sha": "abc123"}},
	})
	require.Error(t, err, "verdict=request_changes with no findings tells the next implement pod nothing to fix")

	_, _, _, err = tl.Build(map[string]any{
		"verdict":       "request_changes",
		"reviewed_shas": []any{map[string]any{"repo": "tatara-cli", "number": float64(80), "sha": "abc123"}},
		"findings":      []any{},
	})
	require.Error(t, err, "an empty findings list is the same non-actionable request_changes")
}

func TestOutcome_ReviewRequiresVerdict(t *testing.T) {
	t.Setenv("TATARA_TASK", "t1")
	tl, _ := OutcomeTool("review")
	_, _, _, err := tl.Build(map[string]any{
		"reviewed_shas": []any{map[string]any{"repo": "tatara-cli", "number": float64(80), "sha": "abc123"}},
	})
	require.Error(t, err)
}

func TestOutcome_ReviewHasNoCommentVerdict(t *testing.T) {
	tl, _ := OutcomeTool("review")
	var schema struct {
		Properties struct {
			Verdict struct {
				Enum []string `json:"enum"`
			} `json:"verdict"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(tl.Schema, &schema))
	require.Equal(t, []string{"approve", "request_changes"}, schema.Properties.Verdict.Enum,
		"a non-decision has no stage to go to (contract D.1)")
}

func TestOutcome_ReviewDescriptionDoesNotImplyAgentPostsAReview(t *testing.T) {
	tl, _ := OutcomeTool("review")
	require.Contains(t, tl.Description, "does NOT post", "must explicitly deny that verdict maps 1:1 to a forge review event - both verdicts post as COMMENT under the bot identity (contract C.5.1b/D.1)")
	require.Contains(t, tl.Description, "do not choose", "the agent has no forge event to choose; mr_write has no approve/request_changes action either")
}

func TestOutcome_ClarifyRequiresDecisionAndReason(t *testing.T) {
	t.Setenv("TATARA_TASK", "t1")
	tl, _ := OutcomeTool("clarify")
	_, _, _, err := tl.Build(map[string]any{"decision": "close"})
	require.Error(t, err, "reason is always required (contract D.1)")
	_, _, _, err = tl.Build(map[string]any{"reason": "because"})
	require.Error(t, err, "decision is always required")
}

func TestOutcome_BrainstormGates(t *testing.T) {
	t.Setenv("TATARA_TASK", "t1")
	tl, _ := OutcomeTool("brainstorm")
	proposal := map[string]any{"repo": "tatara-cli", "title": "t", "body": "b", "kind": "bug"}

	_, _, _, err := tl.Build(map[string]any{"action": "propose"})
	require.Error(t, err, "action=propose requires 1..5 proposals")

	_, _, _, err = tl.Build(map[string]any{"action": "propose", "proposals": []any{
		proposal, proposal, proposal, proposal, proposal, proposal,
	}})
	require.Error(t, err, "more than 5 proposals is over the cap (contract D.1 maxItems:5)")

	_, _, _, err = tl.Build(map[string]any{"action": "skip"})
	require.Error(t, err, "action=skip requires a reason")

	_, _, body, err := tl.Build(map[string]any{"action": "propose", "proposals": []any{proposal}})
	require.NoError(t, err)
	raw, _ := json.Marshal(body)
	var env struct {
		Kind    string         `json:"kind"`
		Payload map[string]any `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(raw, &env))
	require.Equal(t, "brainstorm", env.Kind)
	require.Contains(t, env.Payload, "proposals")
}

func TestOutcome_BrainstormExhausted(t *testing.T) {
	t.Setenv("TATARA_TASK", "t1")
	tl, _ := OutcomeTool("brainstorm")

	_, _, _, err := tl.Build(map[string]any{"action": "exhausted"})
	require.Error(t, err, "action=exhausted requires a reason, same as skip")

	_, _, _, err = tl.Build(map[string]any{"action": "exhausted", "reason": "   "})
	require.Error(t, err, "a whitespace-only reason must not satisfy action=exhausted")

	_, _, body, err := tl.Build(map[string]any{"action": "exhausted", "reason": "idea space dry until the project moves"})
	require.NoError(t, err, "action=exhausted with a reason must be accepted, not rejected as an unknown action")
	raw, _ := json.Marshal(body)
	var env struct {
		Kind    string         `json:"kind"`
		Payload map[string]any `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(raw, &env))
	require.Equal(t, "brainstorm", env.Kind)
	require.Equal(t, "exhausted", env.Payload["action"])
	require.Equal(t, "idea space dry until the project moves", env.Payload["reason"])
}

func TestOutcomeTool_BrainstormSchemaAllowsExhausted(t *testing.T) {
	tl, _ := OutcomeTool("brainstorm")
	var schema struct {
		Properties struct {
			Action struct {
				Enum []string `json:"enum"`
			} `json:"action"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(tl.Schema, &schema))
	require.Equal(t, []string{"propose", "skip", "exhausted"}, schema.Properties.Action.Enum,
		"the schema enum is the ONLY documentation the model gets; a model cannot emit an action absent here")
}

func TestOutcome_IncidentGates(t *testing.T) {
	t.Setenv("TATARA_TASK", "t1")
	tl, _ := OutcomeTool("incident")
	rules := []any{"tatara-operator-reconcile-errors"}

	_, _, _, err := tl.Build(map[string]any{"action": "false_positive", "reason": "flapping threshold"})
	require.Error(t, err, "alert_rules >= 1 on BOTH actions (contract C.2.7)")

	_, _, _, err = tl.Build(map[string]any{"action": "false_positive", "alert_rules": rules})
	require.Error(t, err, "reason is required on BOTH actions")

	_, _, _, err = tl.Build(map[string]any{"action": "file_issue", "alert_rules": rules, "reason": "real"})
	require.Error(t, err, "action=file_issue requires issue{repo,title,body}")

	_, _, _, err = tl.Build(map[string]any{
		"action": "file_issue", "alert_rules": rules, "reason": "real",
		"issue": map[string]any{"repo": "tatara-operator", "title": "t"},
	})
	require.Error(t, err, "issue.body is required")

	_, _, body, err := tl.Build(map[string]any{
		"action": "file_issue", "alert_rules": rules, "reason": "real",
		"issue": map[string]any{"repo": "tatara-operator", "title": "t", "body": "b"},
	})
	require.NoError(t, err)
	raw, _ := json.Marshal(body)
	var env struct {
		Payload map[string]any `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(raw, &env))
	require.Equal(t, []any{"tatara-operator-reconcile-errors"}, env.Payload["alertRules"])
	require.NotContains(t, env.Payload, "alert_rules", "the wire is camelCase (contract C.2.7)")
}

func TestOutcome_IncidentFalsePositiveForbidsIssue(t *testing.T) {
	t.Setenv("TATARA_TASK", "t1")
	tl, _ := OutcomeTool("incident")
	rules := []any{"tatara-operator-reconcile-errors"}

	_, _, _, err := tl.Build(map[string]any{
		"action": "false_positive", "alert_rules": rules, "reason": "flapping threshold",
		"issue": map[string]any{"repo": "tatara-operator", "title": "t", "body": "b"},
	})
	require.Error(t, err, "action=false_positive forbids issue, mirroring the operator's gate (contract C.2.7)")

	_, _, _, err = tl.Build(map[string]any{
		"action": "false_positive", "alert_rules": rules, "reason": "flapping threshold",
	})
	require.NoError(t, err, "action=false_positive with no issue must be accepted")
}

func TestOutcome_IncidentIssueParent(t *testing.T) {
	t.Setenv("TATARA_TASK", "t1")
	tl, _ := OutcomeTool("incident")
	rules := []any{"tatara-operator-reconcile-errors"}
	baseIssue := map[string]any{"repo": "tatara-operator", "title": "t", "body": "b"}

	tests := []struct {
		name    string
		parent  any
		wantErr bool
	}{
		{"valid_parent_ok", map[string]any{"repo": "tatara-operator", "number": float64(320)}, false},
		{"missing_repo_errors", map[string]any{"number": float64(320)}, true},
		{"empty_repo_errors", map[string]any{"repo": "", "number": float64(320)}, true},
		{"missing_number_errors", map[string]any{"repo": "tatara-operator"}, true},
		{"zero_number_errors", map[string]any{"repo": "tatara-operator", "number": float64(0)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := map[string]any{}
			for k, v := range baseIssue {
				issue[k] = v
			}
			issue["parent"] = tt.parent
			_, _, body, err := tl.Build(map[string]any{
				"action": "file_issue", "alert_rules": rules, "reason": "real", "issue": issue,
			})
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			raw, _ := json.Marshal(body)
			var env struct {
				Payload map[string]any `json:"payload"`
			}
			require.NoError(t, json.Unmarshal(raw, &env))
			issuePayload, _ := env.Payload["issue"].(map[string]any)
			require.Contains(t, issuePayload, "parent")
		})
	}
}

func TestOutcome_IncidentNoParentStillOK(t *testing.T) {
	t.Setenv("TATARA_TASK", "t1")
	tl, _ := OutcomeTool("incident")
	rules := []any{"tatara-operator-reconcile-errors"}

	_, _, body, err := tl.Build(map[string]any{
		"action": "file_issue", "alert_rules": rules, "reason": "real",
		"issue": map[string]any{"repo": "tatara-operator", "title": "t", "body": "b"},
	})
	require.NoError(t, err, "action=file_issue with no issue.parent must still validate")
	raw, _ := json.Marshal(body)
	var env struct {
		Payload map[string]any `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(raw, &env))
	issuePayload, _ := env.Payload["issue"].(map[string]any)
	require.NotContains(t, issuePayload, "parent")
}

func TestOutcome_IncidentCommentIssue(t *testing.T) {
	t.Setenv("TATARA_TASK", "t1")
	tl, _ := OutcomeTool("incident")
	rules := []any{"tatara-operator-reconcile-errors"}

	_, _, body, err := tl.Build(map[string]any{
		"action": "comment_issue", "alert_rules": rules, "reason": "same incident, fresh evidence",
		"comment": map[string]any{"repo": "tatara-operator", "number": float64(291), "body": "still firing at 14:02 UTC"},
	})
	require.NoError(t, err, "valid comment_issue must be accepted")
	raw, _ := json.Marshal(body)
	var env struct {
		Payload map[string]any `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(raw, &env))
	commentPayload, _ := env.Payload["comment"].(map[string]any)
	require.Equal(t, "tatara-operator", commentPayload["repo"])
	require.Equal(t, float64(291), commentPayload["number"])
	require.Equal(t, "still firing at 14:02 UTC", commentPayload["body"])
}

func TestOutcome_IncidentCommentIssueGates(t *testing.T) {
	t.Setenv("TATARA_TASK", "t1")
	tl, _ := OutcomeTool("incident")
	rules := []any{"tatara-operator-reconcile-errors"}

	t.Run("missing comment", func(t *testing.T) {
		_, _, _, err := tl.Build(map[string]any{"action": "comment_issue", "alert_rules": rules, "reason": "same incident"})
		require.Error(t, err, "action=comment_issue requires comment{repo,number,body}")
	})

	t.Run("number<=0", func(t *testing.T) {
		_, _, _, err := tl.Build(map[string]any{
			"action": "comment_issue", "alert_rules": rules, "reason": "same incident",
			"comment": map[string]any{"repo": "tatara-operator", "number": float64(0), "body": "still firing"},
		})
		require.Error(t, err, "comment.number must be > 0")
	})

	t.Run("empty body", func(t *testing.T) {
		_, _, _, err := tl.Build(map[string]any{
			"action": "comment_issue", "alert_rules": rules, "reason": "same incident",
			"comment": map[string]any{"repo": "tatara-operator", "number": float64(291), "body": "   "},
		})
		require.Error(t, err, "comment.body required (non-empty)")
	})

	t.Run("also sets issue", func(t *testing.T) {
		_, _, _, err := tl.Build(map[string]any{
			"action": "comment_issue", "alert_rules": rules, "reason": "same incident",
			"comment": map[string]any{"repo": "tatara-operator", "number": float64(291), "body": "still firing"},
			"issue":   map[string]any{"repo": "tatara-operator", "title": "t", "body": "b"},
		})
		require.Error(t, err, "action=comment_issue forbids issue")
	})
}

func TestOutcome_IncidentFalsePositiveForbidsComment(t *testing.T) {
	t.Setenv("TATARA_TASK", "t1")
	tl, _ := OutcomeTool("incident")
	rules := []any{"tatara-operator-reconcile-errors"}

	_, _, _, err := tl.Build(map[string]any{
		"action": "false_positive", "alert_rules": rules, "reason": "flapping threshold",
		"comment": map[string]any{"repo": "tatara-operator", "number": float64(291), "body": "still firing"},
	})
	require.Error(t, err, "action=false_positive forbids comment")
}

func TestOutcome_RefineNeedsOneNonEmptyList(t *testing.T) {
	t.Setenv("TATARA_TASK", "t1")
	tl, _ := OutcomeTool("refine")

	_, _, _, err := tl.Build(map[string]any{})
	require.Error(t, err, "at least one of folds, closes, links must be non-empty (contract C.2.7)")

	_, _, _, err = tl.Build(map[string]any{"folds": []any{}, "closes": []any{}, "links": []any{}})
	require.Error(t, err, "three empty lists is the same no-op outcome")

	_, _, body, err := tl.Build(map[string]any{"links": []any{map[string]any{"repo": "tatara-cli", "number": float64(80)}}})
	require.NoError(t, err)
	raw, _ := json.Marshal(body)
	var env struct {
		Payload map[string]any `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(raw, &env))
	require.Contains(t, env.Payload, "links")
}

func TestOutcome_DocumentationSchemaEqualsImplement(t *testing.T) {
	impl, _ := OutcomeTool("implement")
	doc, _ := OutcomeTool("documentation")
	var a, b any
	require.NoError(t, json.Unmarshal(impl.Schema, &a))
	require.NoError(t, json.Unmarshal(doc.Schema, &b))
	require.Equal(t, a, b, "contract D.1: implement and documentation are identical")
}
