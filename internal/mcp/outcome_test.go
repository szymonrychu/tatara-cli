package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var outcomeProfiles = []string{"brainstorm", "incident", "implement", "review", "refine", "documentation", "upgrade"}

func TestOutcomeTool_ExistsForAllSevenAgentKinds(t *testing.T) {
	for _, p := range outcomeProfiles {
		tl, ok := OutcomeTool(p)
		require.True(t, ok, "profile %q must have a submit_outcome; a pod with no terminal tool cannot finish its Task", p)
		require.Equal(t, "submit_outcome", tl.Name, "one name, seven schemas")
	}
}

// TestOutcome_UpgradeRefusesTheGateActions: the upgrade agent has no approval
// gate. It is a scheduled kind, nobody filed an issue for it, and there is no
// maintainer comment to cite. Its outcome is the submitted/declined pair and
// nothing else - approved/discuss/rejected are refused exactly as hard as
// they are for documentation.
func TestOutcome_UpgradeRefusesTheGateActions(t *testing.T) {
	tool, ok := OutcomeTool("upgrade")
	require.True(t, ok, "upgrade profile must have submit_outcome")
	for _, action := range []string{"approved", "discuss", "rejected"} {
		_, _, _, err := tool.Build(map[string]any{"task": "t", "action": action, "reason": "r"})
		require.Error(t, err, "action=%s must be refused for the upgrade profile", action)
	}
}

func TestOutcome_UpgradeSubmittedCarriesMergeOrder(t *testing.T) {
	t.Setenv("TATARA_TASK", "t")
	tool, _ := OutcomeTool("upgrade")
	_, _, body, err := tool.Build(map[string]any{
		"task": "t", "action": "submitted",
		"title": "chore: cilium 1.16 -> 1.17", "body": "hop 1 of 4",
		"change_significance": "minor",
		"merge_order":         []any{"charts", "helmfile"},
	})
	require.NoError(t, err)
	raw, _ := json.Marshal(body)
	var env struct {
		Kind    string         `json:"kind"`
		Payload map[string]any `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(raw, &env))
	require.Equal(t, "upgrade", env.Kind)
	require.Contains(t, env.Payload, "mergeOrder", "merge_order must map to the camelCase mergeOrder wire field")
	require.Contains(t, env.Payload, "changeSignificance", "change_significance must map to changeSignificance")
}

func TestOutcomeTool_EmptyAndUnknownProfileHaveNone(t *testing.T) {
	for _, p := range []string{"", "clarify", "triage", "lifecycle", "selfImprove", "nonsense"} {
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
	tl, _ := OutcomeTool("implement")
	m, p, body, err := tl.Build(map[string]any{"action": "discuss", "reason": "szymonrychu asked for the scope to be narrowed first"})
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
	require.Equal(t, "implement", env.Kind, "kind MUST equal the pod's agent kind; the operator 409s a mismatch")
	require.Equal(t, "discuss", env.Payload["action"])
	require.Equal(t, "szymonrychu asked for the scope to be narrowed first", env.Payload["reason"])
}

func TestOutcome_RequiresATask(t *testing.T) {
	t.Setenv("TATARA_TASK", "")
	tl, _ := OutcomeTool("implement")
	_, _, _, err := tl.Build(map[string]any{"action": "rejected", "reason": "dup"})
	require.Error(t, err, "no task argument and no TATARA_TASK must fail fast")
}

func TestOutcome_TaskArgIsNotOnTheWire(t *testing.T) {
	t.Setenv("TATARA_TASK", "")
	tl, _ := OutcomeTool("implement")
	_, p, body, err := tl.Build(map[string]any{"task": "t9", "action": "rejected", "reason": "dup"})
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

func TestOutcome_ImplementActionEnumCarriesTheGateActions(t *testing.T) {
	tool, ok := OutcomeTool("implement")
	require.True(t, ok)

	var schema struct {
		Properties struct {
			Action struct {
				Enum []string `json:"enum"`
			} `json:"action"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(tool.Schema, &schema))
	require.ElementsMatch(t,
		[]string{"submitted", "declined", "approved", "discuss", "rejected"},
		schema.Properties.Action.Enum,
		"the schema enum is the ONLY documentation the model gets; a model cannot emit an action absent here")
}

// TestOutcome_ImplementApprovedRequiresTheGateFields covers the two fields that
// are required on EVERY approval. approving_maintainer is deliberately not one
// of them - see TestOutcome_ImplementApprovedPairsMaintainerWithCitations.
func TestOutcome_ImplementApprovedRequiresTheGateFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"no plan_note_id", map[string]any{
			"action": "approved", "reason": "r", "approving_maintainer": "szymonrychu",
			"approval_citations": []any{map[string]any{"id": "c1", "quote": "go ahead"}},
		}, "plan_note_id required"},
		{"no reason", map[string]any{
			"action": "approved", "approving_maintainer": "szymonrychu", "plan_note_id": "n-1",
			"approval_citations": []any{map[string]any{"id": "c1", "quote": "go ahead"}},
		}, "reason required"},
		{"no plan_note_id on the auto-approve path either", map[string]any{
			"action": "approved", "reason": "tatara proposed this issue; no human has commented",
		}, "plan_note_id required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateOutcome("implement", tc.args)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

// TestOutcome_ImplementApprovedPairsMaintainerWithCitations is the
// autoApproveTataraProposals carve-out, client-side.
//
// The operator grants approval with NO human comment when tatara itself
// proposed the issue (api/v1alpha1/proposal_marker.go: the evidence carries the
// sentinel Login "<tatara:auto>" and an EMPTY CommentID). On that path there is
// no comment author, so there is no maintainer login the agent could declare.
// Making approving_maintainer unconditionally required would make the carve-out
// physically unreachable and stop self-proposed work being implementable - it is
// live-enabled on two of the three Projects in the cluster.
//
// The cli enforces SHAPE only. WHETHER auto-approve applies is operator-side
// state (project flag + provenance marker + mirror-vs-Spec hash comparison) that
// the cli cannot see, so the cli must accept the omission and let the operator
// refuse it. Same reasoning that already makes approval_citations optional.
//
// What the cli CAN enforce is that the two travel together: both (a human
// comment is the go-ahead) or neither (auto-approve). Half-populated is always
// wrong and would otherwise surface as a confusing operator-side refusal -
// approver-mismatch, because there is no citation for the declared login to
// agree with.
func TestOutcome_ImplementApprovedPairsMaintainerWithCitations(t *testing.T) {
	citations := []any{map[string]any{"id": "c1", "quote": "go ahead"}}

	t.Run("neither is the auto-approve path and is accepted", func(t *testing.T) {
		require.NoError(t, validateOutcome("implement", map[string]any{
			"action": "approved", "plan_note_id": "n-1",
			"reason": "tatara proposed this issue itself and no human has commented",
		}))
	})

	t.Run("both is the human-cited path and is accepted", func(t *testing.T) {
		require.NoError(t, validateOutcome("implement", map[string]any{
			"action": "approved", "plan_note_id": "n-1", "reason": "szymonrychu said go ahead",
			"approving_maintainer": "szymonrychu", "approval_citations": citations,
		}))
	})

	t.Run("maintainer without citations is refused", func(t *testing.T) {
		require.ErrorContains(t, validateOutcome("implement", map[string]any{
			"action": "approved", "plan_note_id": "n-1", "reason": "r",
			"approving_maintainer": "szymonrychu",
		}), "approving_maintainer requires approval_citations")
	})

	t.Run("citations without a maintainer are refused", func(t *testing.T) {
		require.ErrorContains(t, validateOutcome("implement", map[string]any{
			"action": "approved", "plan_note_id": "n-1", "reason": "r",
			"approval_citations": citations,
		}), "approval_citations requires approving_maintainer")
	})

	t.Run("an empty citation list does not count as citing", func(t *testing.T) {
		require.ErrorContains(t, validateOutcome("implement", map[string]any{
			"action": "approved", "plan_note_id": "n-1", "reason": "r",
			"approving_maintainer": "szymonrychu", "approval_citations": []any{},
		}), "approving_maintainer requires approval_citations")
	})

	t.Run("a blank maintainer is refused rather than read as omitted", func(t *testing.T) {
		require.ErrorContains(t, validateOutcome("implement", map[string]any{
			"action": "approved", "plan_note_id": "n-1", "reason": "r",
			"approving_maintainer": "   ", "approval_citations": citations,
		}), "approving_maintainer must not be blank")
	})
}

func TestOutcome_ImplementApprovedCitationsAreShapeChecked(t *testing.T) {
	base := func(citations []any) map[string]any {
		return map[string]any{
			"action": "approved", "reason": "r", "approving_maintainer": "szymonrychu",
			"plan_note_id": "n-1", "approval_citations": citations,
		}
	}
	require.ErrorContains(t,
		validateOutcome("implement", base([]any{map[string]any{"quote": "go ahead"}})),
		"approval_citations[0].id required")
	require.ErrorContains(t,
		validateOutcome("implement", base([]any{map[string]any{"id": "c1", "quote": "  "}})),
		"approval_citations[0].quote required")
	require.ErrorContains(t,
		validateOutcome("implement", base([]any{"not-an-object"})),
		"approval_citations[0] must be an object")
	require.NoError(t, validateOutcome("implement", base(
		[]any{map[string]any{"id": "c1", "quote": "go ahead"}, map[string]any{"id": "c2", "quote": "yes please"}})),
		"one citation per owned issue: several well-formed entries must pass")
}

func TestOutcome_ImplementDiscussAndRejectedNeedOnlyAReason(t *testing.T) {
	for _, action := range []string{"discuss", "rejected"} {
		require.NoError(t, validateOutcome("implement",
			map[string]any{"action": action, "reason": "because"}))
		require.ErrorContains(t,
			validateOutcome("implement", map[string]any{"action": action}),
			"reason required")
	}
}

func TestOutcome_ImplementSubmittedStillRefusesTheGateFields(t *testing.T) {
	err := validateOutcome("implement", map[string]any{
		"action": "submitted", "title": "t", "body": "b", "change_significance": "patch",
		"approving_maintainer": "szymonrychu",
	})
	require.ErrorContains(t, err, "approving_maintainer is only valid when action=approved")
}

func TestOutcome_ImplementDeclinedStillRefusesTheGateFields(t *testing.T) {
	for _, k := range []string{"approving_maintainer", "plan_note_id", "approval_citations"} {
		err := validateOutcome("implement", map[string]any{
			"action": "declined", "decline_reason": "already fixed on main", k: "x",
		})
		require.ErrorContains(t, err, k+" is only valid when action=approved",
			"a code outcome must not be able to smuggle an approval past the gate")
	}
}

// TestOutcome_ImplementReasonAndDeclineReasonNeverCollide guards a collision
// this change created: outcomeArgMap sends decline_reason to the wire as
// `reason`, and the implement schema now ALSO carries a top-level `reason` for
// the gate actions. Both set payload["reason"], and buildOutcomePayload ranges
// over a map, so accepting both would make the wire value depend on Go's
// randomised map order. Exactly one of the two is legal per action.
func TestOutcome_ImplementReasonAndDeclineReasonNeverCollide(t *testing.T) {
	require.ErrorContains(t, validateOutcome("implement", map[string]any{
		"action": "declined", "decline_reason": "already fixed", "reason": "something else",
	}), "reason is only valid when action=approved, discuss or rejected")

	require.ErrorContains(t, validateOutcome("implement", map[string]any{
		"action": "submitted", "title": "t", "body": "b", "change_significance": "patch",
		"reason": "something else",
	}), "reason is only valid when action=approved, discuss or rejected")

	for _, action := range []string{"approved", "discuss", "rejected"} {
		require.ErrorContains(t, validateOutcome("implement", map[string]any{
			"action": action, "reason": "r", "approving_maintainer": "szymonrychu",
			"plan_note_id": "n-1", "decline_reason": "no",
		}), "decline_reason is only for action=declined")
	}
}

func TestOutcome_ApprovedMapsTheNewWireFields(t *testing.T) {
	got, err := buildOutcomePayload("implement", map[string]any{
		"action": "approved", "reason": "maintainer said go", "plan_note_id": "n-7",
		"approving_maintainer": "szymonrychu",
		"approval_citations":   []any{map[string]any{"id": "c1", "quote": "go ahead"}},
	})
	require.NoError(t, err)
	require.Equal(t, "szymonrychu", got["approvingMaintainer"])
	require.Equal(t, "n-7", got["planNoteId"])
	require.Contains(t, got, "approvalCitations")
	require.NotContains(t, got, "approving_maintainer")
	require.NotContains(t, got, "plan_note_id")
}

func TestOutcomeTool_HasNoClarifyProfile(t *testing.T) {
	_, ok := OutcomeTool("clarify")
	require.False(t, ok, "clarify is deleted; a pod claiming it must get NO submit_outcome (fail closed)")
}

// TestOutcome_DocumentationRefusesTheGateActions is the server-side half of
// TestOutcome_DocumentationActionEnumIsSubmittedOrDeclinedOnly: the schema is
// the only thing the model reads, but validateOutcome is what actually stops a
// documentation pod driving an approval gate it has no handler for.
func TestOutcome_DocumentationRefusesTheGateActions(t *testing.T) {
	for _, action := range []string{"approved", "discuss", "rejected"} {
		err := validateOutcome("documentation", map[string]any{
			"action": action, "reason": "r", "approving_maintainer": "szymonrychu", "plan_note_id": "n-1",
		})
		require.ErrorContains(t, err, "action must be one of submitted|declined")
	}
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

// TestOutcomeArgMapCoversEverySnakeCaseSchemaKey is the guard MEMORY.md:24 asks
// for. The outcome schemas and outcomeArgMap are two hand-maintained
// artefacts. A snake_case arg present in a schema but ABSENT from the map
// reaches the operator's DisallowUnknownFields decoder still snake_cased and
// 400s at runtime, with nothing in either repo catching it at build time.
func TestOutcomeArgMapCoversEverySnakeCaseSchemaKey(t *testing.T) {
	for profile, schema := range outcomeSchemas {
		var doc struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal([]byte(schema), &doc); err != nil {
			t.Fatalf("%s: schema is not valid JSON: %v", profile, err)
		}
		for key := range doc.Properties {
			if !strings.Contains(key, "_") {
				continue // already wire-shaped
			}
			if _, ok := outcomeArgMap[key]; !ok {
				t.Errorf("%s: schema property %q is snake_case but has no outcomeArgMap entry; "+
					"it would reach the operator snake_cased and 400", profile, key)
			}
		}
	}
}

// TestImplementSchemaCarriesApprovalCitations pins the field's exact shape on
// the schema that now owns it. Item keys are single words on purpose:
// outcomeArgMap renames TOP-LEVEL keys only, so a nested comment_id could
// never be converted.
func TestImplementSchemaCarriesApprovalCitations(t *testing.T) {
	var doc struct {
		Properties map[string]struct {
			Type  string `json:"type"`
			Items struct {
				Properties map[string]json.RawMessage `json:"properties"`
				Required   []string                   `json:"required"`
			} `json:"items"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(implementOutcomeSchema), &doc); err != nil {
		t.Fatalf("implementOutcomeSchema: %v", err)
	}
	p, ok := doc.Properties["approval_citations"]
	if !ok {
		t.Fatal("implementOutcomeSchema has no approval_citations property")
	}
	if p.Type != "array" {
		t.Fatalf("approval_citations type = %q, want array", p.Type)
	}
	for _, want := range []string{"id", "quote"} {
		if _, ok := p.Items.Properties[want]; !ok {
			t.Fatalf("approval_citations items missing %q", want)
		}
	}
	if got := outcomeArgMap["approval_citations"]; got != "approvalCitations" {
		t.Fatalf("outcomeArgMap[approval_citations] = %q, want approvalCitations", got)
	}
}

// TestDocumentationSchemaHasNoGateFields is the other half of the Task 4.1
// split: documentation must not merely refuse the gate ACTIONS, it must not
// advertise the gate FIELDS either. additionalProperties is false, so a field
// absent here is unreachable.
func TestDocumentationSchemaHasNoGateFields(t *testing.T) {
	tool, ok := OutcomeTool("documentation")
	require.True(t, ok)
	var doc struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(tool.Schema, &doc))
	for _, k := range []string{"reason", "approving_maintainer", "plan_note_id", "approval_citations"} {
		require.NotContains(t, doc.Properties, k,
			"documentation has no approval gate; %q must not be reachable from its schema", k)
	}
}

func TestOutcome_DocumentationActionEnumIsSubmittedOrDeclinedOnly(t *testing.T) {
	tool, ok := OutcomeTool("documentation")
	require.True(t, ok)

	var schema struct {
		Properties struct {
			Action struct {
				Enum []string `json:"enum"`
			} `json:"action"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(tool.Schema, &schema))
	require.ElementsMatch(t, []string{"submitted", "declined"}, schema.Properties.Action.Enum,
		"a documentation agent has no approval gate to drive; it must never be able to emit approved/discuss/rejected")
}
