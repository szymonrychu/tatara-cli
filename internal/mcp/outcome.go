package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// The submit_outcome schemas, contract D.1, copied verbatim. The
// description strings are the only documentation the model gets, and several of
// them (merge_order, reviewed_shas) are the sole warning about a failure mode
// with no other guard rail. Do not shorten them.
const implementOutcomeSchema = `{"type":"object","properties":{
  "task":{"type":"string"},
  "action":{"type":"string","enum":["submitted","declined","approved","discuss","rejected"]},
  "title":{"type":"string","description":"MR title. Required when action=submitted."},
  "body":{"type":"string","description":"MR body. Required when action=submitted."},
  "change_significance":{"type":"string","enum":["major","minor","patch"],
    "description":"Required when action=submitted. major=backward-incompatible; minor=backward-compatible feature; patch=fix. YOU own this level - a reviewer may raise it but can never lower it."},
  "merge_order":{"type":"array","items":{"type":"string"},
    "description":"REQUIRED when this task's MRs span more than one repo: the Repository CR names in dependency order, first-merged first. There is NO default. Get it wrong and a downstream repo ships against an API that has not merged yet."},
  "decline_reason":{"type":"string","description":"Required when action=declined."},
  "reason":{"type":"string","description":"ALWAYS required for action=approved, action=discuss and action=rejected. For approved, say in plain words WHAT you are treating as the go-ahead: name the maintainer and why you read their comment as approval, or - when tatara proposed this issue itself and no human has commented - say exactly that."},
  "approving_maintainer":{"type":"string","description":"Required for action=approved WHENEVER you are citing a human comment as the go-ahead: the login of the maintainer whose comment you are citing. Send it together with approval_citations - both, or neither, never one alone. OMIT BOTH only when tatara proposed this issue itself and no human has commented on it: the operator then grants on provenance alone and there is no comment author to name. It is a DECLARATION, not an authority - the operator refuses if it is not a verified maintainer, and refuses again if it does not match the author of the comment you cited. The citation stays the sole authority."},
  "plan_note_id":{"type":"string","description":"ALWAYS required for action=approved, including when no human commented: the id returned by the task_note(kind=\"plan\") call that wrote the plan being approved. The operator hashes that note's body at grant and re-checks the hash before you write code, so a plan swapped after approval is refused."},
  "approval_citations":{"type":"array","items":{"type":"object","properties":{
      "id":{"type":"string"},"quote":{"type":"string"}},
    "required":["id","quote"]},
    "description":"Required for action=approved whenever a maintainer has commented, and always sent together with approving_maintainer - both, or neither: ONE entry per issue this task owns. id is the external_id of the maintainer comment you are citing, copied verbatim from the <comment external_id=\"...\"> attribute already in your turn-0 bundle - do NOT re-crawl to find it; it does NOT have to be the newest comment on the thread. quote is a VERBATIM substring of that same comment's body. YOU judge whether the comment approves; the operator re-reads the comment itself and refuses if the id does not name a maintainer-authored non-bot comment on that issue, if your quote is not in it, or if it already approved once. If a LATER maintainer comment withdraws the approval you would otherwise cite, send action=discuss instead - do not cite a withdrawn approval. Omit this field, and approving_maintainer with it, ONLY when no human has commented at all."}},
 "required":["action"],"additionalProperties":false}`

// documentationOutcomeSchema was implementOutcomeSchema until the clarify
// fold. It is now its OWN const: implement's action enum grew the three
// approval-gate actions, and a documentation agent has no gate to drive - it
// writes docs and opens an MR, or holds the conversation open with discuss.
// Sharing the const would have handed it the two GATE actions (approved,
// rejected) its operator-side handler has no branch for. discuss is not one
// of those: it grants nothing and closes nothing, it is the same
// awaiting-human pause implement uses when the agent needs a human call
// before it can pick submitted or declined (G6/H1-B).
const documentationOutcomeSchema = `{"type":"object","properties":{
  "task":{"type":"string"},
  "action":{"type":"string","enum":["submitted","declined","discuss"]},
  "title":{"type":"string","description":"MR title. Required when action=submitted."},
  "body":{"type":"string","description":"MR body. Required when action=submitted."},
  "change_significance":{"type":"string","enum":["major","minor","patch"],
    "description":"Required when action=submitted. major=backward-incompatible; minor=backward-compatible feature; patch=fix. YOU own this level - a reviewer may raise it but can never lower it."},
  "merge_order":{"type":"array","items":{"type":"string"},
    "description":"REQUIRED when this task's MRs span more than one repo: the Repository CR names in dependency order, first-merged first. There is NO default. Get it wrong and a downstream repo ships against an API that has not merged yet."},
  "decline_reason":{"type":"string","description":"Required when action=declined."},
  "reason":{"type":"string","description":"Required when action=discuss: why you are pausing instead of finishing this turn with submitted or declined. The operator parks the task awaiting human input; the next human comment on an owned issue or MR resumes it."}},
 "required":["action"],"additionalProperties":false}`

const reviewOutcomeSchema = `{"type":"object","properties":{
  "task":{"type":"string"},
  "verdict":{"type":"string","enum":["approve","request_changes"]},
  "change_significance":{"type":"string","enum":["major","minor","patch"],
    "description":"Optional. It may only RAISE the level the implementer declared, never lower it."},
  "reviewed_shas":{"type":"array","minItems":1,"items":{"type":"object","properties":{
      "repo":{"type":"string"},"number":{"type":"integer"},"sha":{"type":"string"}},
    "required":["repo","number","sha"]},
    "description":"REQUIRED. The head SHA you ACTUALLY CHECKED OUT AND READ, per MR. The operator re-reads the live head and REFUSES your verdict if it moved while you were reviewing - anything pushed after your checkout would otherwise merge unreviewed under your approval."},
  "findings":{"type":"array","items":{"type":"object","properties":{
      "repo":{"type":"string"},"number":{"type":"integer"},
      "path":{"type":"string"},"line":{"type":"integer"},"body":{"type":"string"},
      "severity":{"type":"string","enum":["critical","high","medium","low"]}},
    "required":["repo","number","body","severity"]},
    "description":"Required (>=1) when verdict=request_changes. The OPERATOR posts these as the SCM review and its inline comments - you do not post them yourself."}},
 "required":["verdict","reviewed_shas"],"additionalProperties":false}`

const brainstormOutcomeSchema = `{"type":"object","properties":{
  "task":{"type":"string"},
  "action":{"type":"string","enum":["propose","skip","exhausted"]},
  "proposals":{"type":"array","minItems":1,"maxItems":5,"items":{"type":"object","properties":{
      "repo":{"type":"string"},"title":{"type":"string"},"body":{"type":"string"},
      "kind":{"type":"string","enum":["bug","improvement"]}},
    "required":["repo","title","body","kind"]}},
  "reason":{"type":"string","description":"Required when action=skip or action=exhausted. skip means nothing THIS cycle - the idea space is not dry, expect something next time. exhausted means nothing worth proposing until the project itself changes, and PAUSES brainstorming for this project until it does; one exhausted report is enough, no threshold."}},
 "required":["action"],"additionalProperties":false}`

const incidentOutcomeSchema = `{"type":"object","properties":{
  "task":{"type":"string"},
  "action":{"type":"string","enum":["file_issue","false_positive","comment_issue"]},
  "alert_rules":{"type":"array","minItems":1,"items":{"type":"string"}},
  "issue":{"type":"object","properties":{
      "repo":{"type":"string"},"title":{"type":"string"},"body":{"type":"string"},
      "parent":{"type":"object","properties":{
          "repo":{"type":"string"},"number":{"type":"integer"}},
        "required":["repo","number"],
        "description":"Optional. Set ONLY when this new issue is genuinely-new-but-related to an existing open tracker you found while surveying - never for a same-rule duplicate (file false_positive or let admission dedup handle that instead). The operator links it as a sub-issue; you never file the link yourself."}},
    "required":["repo","title","body"],
    "description":"Required when action=file_issue."},
  "comment":{"type":"object","properties":{
      "repo":{"type":"string"},"number":{"type":"integer"},"body":{"type":"string"}},
    "required":["repo","number","body"],
    "description":"Required when action=comment_issue. Appends fresh evidence as a comment on an existing OPEN incident tracker issue you found while surveying - use this when this alert is the SAME incident as that tracker, not a new problem."},
  "reason":{"type":"string"}},
 "required":["action","alert_rules","reason"],"additionalProperties":false}`

const refineOutcomeSchema = `{"type":"object","properties":{
  "task":{"type":"string"},
  "folds":{"type":"array","items":{"type":"object","properties":{"task":{"type":"string"}},
    "required":["task"]},
    "description":"Member Tasks to fold in: their Issues/MRs are adopted, then the member Task is deleted. A member with a running pod is REFUSED."},
  "closes":{"type":"array","items":{"type":"object","properties":{
      "repo":{"type":"string"},"number":{"type":"integer"},"reason":{"type":"string"}},
    "required":["repo","number","reason"]}},
  "links":{"type":"array","items":{"type":"object","properties":{
      "repo":{"type":"string"},"number":{"type":"integer"},"isPR":{"type":"boolean"}},
    "required":["repo","number"]}}},
 "required":[],"additionalProperties":false}`

// outcomeSchemas is contract D.1: one tool name, seven schemas, chosen from
// TATARA_TOOL_PROFILE at registration time. An agent cannot pick the wrong
// outcome tool because it only ever has one. This is what breaks the
// byte-identical-tools/list prompt-cache optimisation, and it is worth it.
//
// There is deliberately NO "clarify" key. The clarify kind is deleted, not
// aliased: resolveProfile fails closed, so a pod still claiming clarify gets no
// submit_outcome at all rather than a live path to approval that skips the
// gate - the exact hole #521 exists to close.
var outcomeSchemas = map[string]json.RawMessage{
	"implement":     json.RawMessage(implementOutcomeSchema),
	"documentation": json.RawMessage(documentationOutcomeSchema),
	"review":        json.RawMessage(reviewOutcomeSchema),
	"brainstorm":    json.RawMessage(brainstormOutcomeSchema),
	"incident":      json.RawMessage(incidentOutcomeSchema),
	"refine":        json.RawMessage(refineOutcomeSchema),
	// upgrade REUSES documentationOutcomeSchema deliberately: both are gate-less
	// code kinds whose action enum is submitted|declined. Duplicating the const
	// would let the two drift on a field nobody meant to change independently.
	"upgrade": json.RawMessage(documentationOutcomeSchema),
}

// outcomeDescriptions is the per-profile tool description. The review one is
// contract D.1's "Note for skill authors" verbatim: it is the ONLY place that
// corrects the obvious misreading of a verdict enum - that the agent is picking
// a forge review event. It is not.
var outcomeDescriptions = map[string]string{
	"implement":     "Finish this implement turn. Five actions. action=approved reports that you have the go-ahead on the plan you wrote: always set reason and plan_note_id, plus approving_maintainer AND approval_citations when a human comment is the go-ahead (omit both only when tatara proposed this issue itself and nobody has commented). The operator re-reads the cited comment and refuses if the citation does not hold up - a refusal is a normal result, not an error, and you keep talking. action=discuss holds the conversation open with a reason. action=rejected closes the issue with a reason. action=submitted opens the MR with the title, body and change_significance you own (plus merge_order when this task's MRs span more than one repo). action=declined declines the work with a decline_reason. This is the only way an implement task terminates.",
	"documentation": "Finish this documentation task. action=submitted with the MR title, the MR body and the change_significance you own (plus merge_order when this task's MRs span more than one repo), action=declined with a decline_reason, or action=discuss with a reason to pause and hold the conversation open instead of forcing submitted or declined this turn. This is the only way a documentation task terminates (discuss does not terminate it - it parks awaiting a human).",
	"review":        "Submit your review verdict. verdict=approve does NOT post an approving review and verdict=request_changes does NOT post a REQUEST_CHANGES review - GitHub 422s a self-authored PR for both events, and this platform has one bot identity. You do not choose a forge review event and you never post a review yourself: the operator posts a COMMENT review carrying your verdict and findings, under the bot identity, from this payload. On verdict=approve the operator then merges - the merge is the approval of record.",
	"brainstorm":    "Finish this brainstorm task. action=propose with 1 to 5 issue proposals, action=skip with a reason when nothing is worth proposing THIS cycle (transient - expect something the next session), or action=exhausted with a reason when nothing is worth proposing until the project itself changes (PAUSES brainstorming for this project until it does - use sparingly, only when you genuinely mean for scheduling to hold). A silent finish is not allowed.",
	"incident":      "Finish this incident task. action=file_issue with the issue to open, action=false_positive, or action=comment_issue with comment{repo,number,body} to append fresh evidence to an existing open tracker when this alert is the SAME incident as one you found while surveying. All three require the alert_rules that fired and a reason. On file_issue, set issue.parent only when the new issue is genuinely-new-but-related to an existing open tracker you found - never for a same-rule duplicate.",
	"refine":        "Finish this refine task: the member tasks to fold in, the issues to close, and the issues or MRs to link. At least one of the three lists must be non-empty.",
	"upgrade":       "Finish this upgrade turn. action=submitted with the MR title, the MR body and the change_significance you own, plus merge_order when this upgrade spans more than one repo - merge_order is the DEPENDENCY order the repos merge in and there is no default, so getting it backwards ships a chart against an image tag that never published. action=declined with a decline_reason when no upgrade unit is worth taking this cycle, or when the one you picked turns out to be unsafe - declined is a correct and common answer. action=discuss with a reason when you need a human call before you can pick either (a breaking-change changelog, an ambiguous version pin) - it pauses rather than terminates. submitted and declined are the only ways an upgrade task terminates.",
}

// outcomeArgMap renames the snake_case tool args to the camelCase wire fields
// the operator's DisallowUnknownFields decoder expects (contract C.2.7).
// Anything not listed here goes through unchanged.
var outcomeArgMap = map[string]string{
	"change_significance": "changeSignificance",
	"merge_order":         "mergeOrder",
	// decline_reason and the implement schema's top-level reason BOTH land on
	// the wire as "reason". validateImplementOutcome makes exactly one of them
	// legal per action, because buildOutcomePayload ranges over a map and the
	// winner of a collision would otherwise be Go's map iteration order.
	"decline_reason":       "reason",
	"reviewed_shas":        "reviewedSHAs",
	"alert_rules":          "alertRules",
	"approval_citations":   "approvalCitations",
	"approving_maintainer": "approvingMaintainer",
	"plan_note_id":         "planNoteId",
}

// OutcomeTool returns the submit_outcome tool for one profile. An empty or
// unknown profile gets NOTHING: resolveProfile fails closed, and a pod with no
// recognised profile must not be able to terminate a Task. Contract D.
func OutcomeTool(profile string) (Tool, bool) {
	schema, ok := outcomeSchemas[profile]
	if !ok {
		return Tool{}, false
	}
	return Tool{
		Name:        "submit_outcome",
		Description: outcomeDescriptions[profile],
		Target:      TargetOperator,
		Schema:      schema,
		Build: func(a map[string]any) (string, string, any, error) {
			task := argOrEnv(a, "task", "TATARA_TASK")
			if task == "" {
				return "", "", nil, fmt.Errorf("submit_outcome: no task argument and TATARA_TASK is unset")
			}
			payload, err := buildOutcomePayload(profile, a)
			if err != nil {
				return "", "", nil, err
			}
			return http.MethodPost, "/tasks/" + url.PathEscape(task) + "/outcome",
				map[string]any{"kind": profile, "payload": payload}, nil
		},
	}, true
}

// buildOutcomePayload validates client-side (a fast, legible error beats a 400
// the model has to interpret) and maps snake_case to camelCase.
func buildOutcomePayload(profile string, a map[string]any) (map[string]any, error) {
	if err := validateOutcome(profile, a); err != nil {
		return nil, err
	}
	payload := map[string]any{}
	for k, v := range a {
		if k == "task" {
			continue
		}
		if mapped, ok := outcomeArgMap[k]; ok {
			payload[mapped] = v
			continue
		}
		payload[k] = v
	}
	return payload, nil
}

// validateOutcome mirrors the operator's own /outcome gates (contract C.2.7)
// client-side. The one gate it CANNOT mirror is review coverage: the cli does
// not know the Task's owned-MR set at Build time, so "reviewed_shas covers
// every owned MR" stays operator-side.
func validateOutcome(profile string, a map[string]any) error {
	switch profile {
	case "implement":
		return validateImplementOutcome(a)
	case "documentation", "upgrade":
		return validateDocumentationOutcome(a)
	case "review":
		return validateReviewOutcome(a)
	case "brainstorm":
		return validateBrainstormOutcome(a)
	case "incident":
		return validateIncidentOutcome(a)
	case "refine":
		return validateRefineOutcome(a)
	default:
		return fmt.Errorf("submit_outcome: unknown profile %q", profile)
	}
}

// validateCodeOutcome is the submitted/declined/discuss half of the outcome
// contract: the whole of it for documentation and upgrade, and the two MR
// actions (not discuss - see validateImplementOutcome) for implement. It is
// shared rather than copied because the kinds must not drift on it - silent
// divergence between hand-maintained copies of one rule is the bug class this
// whole change exists to close.
//
// discuss here is NOT the approval-gate discuss implement's own switch
// handles (that one also refuses approving_maintainer/plan_note_id/
// approval_citations through refuseGateArgs, none of which exist on a code-kind
// schema in the first place). It is the same "pause and hold the conversation
// open" verdict, requiring only reason and refusing every MR-shaped field,
// submitted or declined would otherwise require.
func validateCodeOutcome(a map[string]any) error {
	switch argString(a, "action") {
	case "submitted":
		for _, k := range []string{"title", "body", "change_significance"} {
			if strings.TrimSpace(argString(a, k)) == "" {
				return fmt.Errorf("submit_outcome: %s required when action=submitted", k)
			}
		}
		if _, ok := a["decline_reason"]; ok {
			return fmt.Errorf("submit_outcome: decline_reason is only for action=declined")
		}
		return nil
	case "declined":
		if strings.TrimSpace(argString(a, "decline_reason")) == "" {
			return fmt.Errorf("submit_outcome: decline_reason required (non-empty) when action=declined")
		}
		for _, k := range []string{"title", "body", "change_significance", "merge_order"} {
			if _, ok := a[k]; ok {
				return fmt.Errorf("submit_outcome: %s is only for action=submitted", k)
			}
		}
		return nil
	case "discuss":
		if strings.TrimSpace(argString(a, "reason")) == "" {
			return fmt.Errorf("submit_outcome: reason required when action=discuss")
		}
		// refuseCodeArgs, not an inlined five-key list: the inline copy folded
		// decline_reason into the MR-shaped message and told the agent to move it
		// to action=submitted, which is the one other action that also refuses it.
		return refuseCodeArgs(a)
	case "":
		return fmt.Errorf("submit_outcome: action required: one of submitted|declined|discuss")
	default:
		return fmt.Errorf("submit_outcome: action must be one of submitted|declined|discuss")
	}
}

// validateDocumentationOutcome is deliberately nothing but validateCodeOutcome.
// A documentation agent writes docs and opens an MR, or pauses with discuss; it
// has no approval gate to drive, so approved/rejected are refused here as hard
// as they are absent from documentationOutcomeSchema.
func validateDocumentationOutcome(a map[string]any) error {
	return validateCodeOutcome(a)
}

// validateImplementOutcome adds the three approval-gate actions on top of the
// two code actions. The two sets are mutually exclusive in BOTH directions: a
// code outcome may not carry the gate fields (it would smuggle an approval past
// the gate) and a gate outcome may not carry the MR fields (decline_reason and
// reason both land on the wire as "reason", so accepting both would make the
// payload depend on map iteration order).
func validateImplementOutcome(a map[string]any) error {
	action := argString(a, "action")
	switch action {
	case "submitted", "declined":
		if err := validateCodeOutcome(a); err != nil {
			return err
		}
		if _, ok := a["reason"]; ok {
			return fmt.Errorf("submit_outcome: reason is only valid when action=approved, discuss or rejected; action=declined carries its reason in decline_reason")
		}
		return refuseGateArgs(a)
	case "approved":
		if err := refuseCodeArgs(a); err != nil {
			return err
		}
		// reason and plan_note_id are required on EVERY approval. The plan pin is
		// orthogonal to who approved: the agent writes a plan note on the
		// auto-approve path too, and pinning it is still the anti-scope-drift
		// control.
		for _, k := range []string{"reason", "plan_note_id"} {
			if strings.TrimSpace(argString(a, k)) == "" {
				return fmt.Errorf("submit_outcome: %s required when action=approved", k)
			}
		}
		if err := checkApprovalPairing(a); err != nil {
			return err
		}
		// Shape only. WHETHER a citation was needed, whether the cited id names a
		// maintainer's comment on that issue, whether the quote really occurs in
		// the body, and whether approving_maintainer agrees with the citation are
		// the OPERATOR's calls - it holds the mirror. A refusal there is a 200
		// with granted=false, not an error here.
		for i, raw := range outcomeList(a, "approval_citations") {
			c, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("submit_outcome: approval_citations[%d] must be an object with id and quote", i)
			}
			if strings.TrimSpace(argString(c, "id")) == "" {
				return fmt.Errorf("submit_outcome: approval_citations[%d].id required: the comment's external_id from your bundle", i)
			}
			if strings.TrimSpace(argString(c, "quote")) == "" {
				return fmt.Errorf("submit_outcome: approval_citations[%d].quote required: a VERBATIM substring of that comment's body", i)
			}
		}
		return nil
	case "discuss", "rejected":
		if err := refuseCodeArgs(a); err != nil {
			return err
		}
		if strings.TrimSpace(argString(a, "reason")) == "" {
			return fmt.Errorf("submit_outcome: reason required when action=%s", action)
		}
		// The gate fields belong to action=approved ALONE. The operator refuses
		// them here too (restapi/outcome.go gate(): 400 unexpected-field,
		// "approvingMaintainer, planNoteId and approvalCitations are only valid
		// when action=approved"), so mirroring it client-side costs the agent a
		// legible error instead of a round trip to be told the same thing - the
		// same reason submitted/declined refuse them above.
		return refuseGateArgs(a)
	case "":
		return fmt.Errorf("submit_outcome: action required: one of submitted|declined|approved|discuss|rejected")
	default:
		return fmt.Errorf("submit_outcome: action must be one of submitted|declined|approved|discuss|rejected")
	}
}

// checkApprovalPairing enforces the ONE thing the cli can know about who
// approved: approving_maintainer and approval_citations travel together.
//
// Both present is a human-cited approval. NEITHER present is the
// autoApproveTataraProposals carve-out, where the operator grants on provenance
// alone - a tatara-proposed issue that no human has commented on - and stamps
// the sentinel login "<tatara:auto>" with an empty CommentID. There is no
// comment author on that path, so there is no maintainer login to declare, and
// requiring one here would make the carve-out unreachable.
//
// Neither field is required, for the same reason approval_citations never was:
// WHETHER auto-approve applies depends on the project flag, the provenance
// marker and a mirror-vs-Spec hash comparison, none of which the cli can see.
// The cli enforces SHAPE; the operator is the trust boundary and refuses with a
// 200 + granted=false when the shape is fine but the authorisation is not.
//
// Half-populated is refused here because it is wrong on every path and would
// otherwise land as an operator-side approver-mismatch: a declared login with no
// citation has nothing to agree with.
func checkApprovalPairing(a map[string]any) error {
	maintainer := strings.TrimSpace(argString(a, "approving_maintainer"))
	if _, given := a["approving_maintainer"]; given && maintainer == "" {
		return fmt.Errorf("submit_outcome: approving_maintainer must not be blank when set: omit it entirely, with approval_citations, on the auto-approve path")
	}
	cited := outcomeListLen(a, "approval_citations") > 0
	switch {
	case maintainer != "" && !cited:
		return fmt.Errorf("submit_outcome: approving_maintainer requires approval_citations: name the comment you are reading as the go-ahead, or omit both when tatara proposed this issue and no human has commented")
	case maintainer == "" && cited:
		return fmt.Errorf("submit_outcome: approval_citations requires approving_maintainer: declare whose comment you are citing")
	}
	return nil
}

// refuseGateArgs rejects the approval-gate args on any action but approved. It
// is refuseCodeArgs' mirror image and exists for the same reason: the rule is
// one rule, refused identically on submitted, declined, discuss and rejected,
// and four hand-maintained copies of it is how one of them silently loses a
// field.
func refuseGateArgs(a map[string]any) error {
	for _, k := range []string{"approving_maintainer", "plan_note_id", "approval_citations"} {
		if _, ok := a[k]; ok {
			return fmt.Errorf("submit_outcome: %s is only valid when action=approved", k)
		}
	}
	return nil
}

// refuseCodeArgs rejects the MR-shaped args on a gate action.
func refuseCodeArgs(a map[string]any) error {
	if _, ok := a["decline_reason"]; ok {
		return fmt.Errorf("submit_outcome: decline_reason is only for action=declined")
	}
	for _, k := range []string{"title", "body", "change_significance", "merge_order"} {
		if _, ok := a[k]; ok {
			return fmt.Errorf("submit_outcome: %s is only for action=submitted", k)
		}
	}
	return nil
}

func validateReviewOutcome(a map[string]any) error {
	verdict := argString(a, "verdict")
	if verdict != "approve" && verdict != "request_changes" {
		return fmt.Errorf("submit_outcome: verdict required: one of approve|request_changes")
	}
	// REQUIRED, always. A missing entry is never a silent "unreviewed but fine":
	// the operator re-reads the live head of every owned MR and refuses a verdict
	// whose reported SHA moved, or that does not cover every MR this task owns.
	if outcomeListLen(a, "reviewed_shas") == 0 {
		return fmt.Errorf("submit_outcome: reviewed_shas required (>=1): report the head SHA you actually checked out and read, for EVERY MR this task owns")
	}
	if verdict == "request_changes" && outcomeListLen(a, "findings") == 0 {
		return fmt.Errorf("submit_outcome: findings required (>=1) when verdict=request_changes")
	}
	return nil
}

func validateBrainstormOutcome(a map[string]any) error {
	switch argString(a, "action") {
	case "propose":
		n := outcomeListLen(a, "proposals")
		if n < 1 || n > 5 {
			return fmt.Errorf("submit_outcome: proposals required (1..5) when action=propose")
		}
		return nil
	case "skip", "exhausted":
		if strings.TrimSpace(argString(a, "reason")) == "" {
			return fmt.Errorf("submit_outcome: reason required (non-empty) when action=%s", argString(a, "action"))
		}
		return nil
	case "":
		return fmt.Errorf("submit_outcome: action required: one of propose|skip|exhausted")
	default:
		return fmt.Errorf("submit_outcome: action must be one of propose|skip|exhausted")
	}
}

func validateIncidentOutcome(a map[string]any) error {
	action := argString(a, "action")
	if action != "file_issue" && action != "false_positive" && action != "comment_issue" {
		return fmt.Errorf("submit_outcome: action required: one of file_issue|false_positive|comment_issue")
	}
	if outcomeListLen(a, "alert_rules") == 0 {
		return fmt.Errorf("submit_outcome: alert_rules required (>=1) on all actions")
	}
	if strings.TrimSpace(argString(a, "reason")) == "" {
		return fmt.Errorf("submit_outcome: reason required (non-empty) on all actions")
	}
	if action == "comment_issue" {
		if _, ok := a["issue"]; ok {
			return fmt.Errorf("submit_outcome: issue is only for action=file_issue")
		}
		comment, _ := a["comment"].(map[string]any)
		if strings.TrimSpace(argString(comment, "repo")) == "" {
			return fmt.Errorf("submit_outcome: comment.repo required (non-empty) when action=comment_issue")
		}
		n, err := asInt(comment["number"])
		if err != nil || n <= 0 {
			return fmt.Errorf("submit_outcome: comment.number required (>0) when action=comment_issue")
		}
		if strings.TrimSpace(argString(comment, "body")) == "" {
			return fmt.Errorf("submit_outcome: comment.body required (non-empty) when action=comment_issue")
		}
		return nil
	}
	if action != "file_issue" {
		if _, ok := a["issue"]; ok {
			return fmt.Errorf("submit_outcome: issue is only for action=file_issue")
		}
		if _, ok := a["comment"]; ok {
			return fmt.Errorf("submit_outcome: comment is only for action=comment_issue")
		}
		return nil
	}
	if _, ok := a["comment"]; ok {
		return fmt.Errorf("submit_outcome: comment is only for action=comment_issue")
	}
	issue, _ := a["issue"].(map[string]any)
	for _, k := range []string{"repo", "title", "body"} {
		if strings.TrimSpace(argString(issue, k)) == "" {
			return fmt.Errorf("submit_outcome: issue.%s required when action=file_issue", k)
		}
	}
	if parentRaw, ok := issue["parent"]; ok {
		parent, _ := parentRaw.(map[string]any)
		if strings.TrimSpace(argString(parent, "repo")) == "" {
			return fmt.Errorf("submit_outcome: issue.parent.repo required (non-empty) when issue.parent is set")
		}
		n, err := asInt(parent["number"])
		if err != nil || n <= 0 {
			return fmt.Errorf("submit_outcome: issue.parent.number required (>0) when issue.parent is set")
		}
	}
	return nil
}

func validateRefineOutcome(a map[string]any) error {
	if outcomeListLen(a, "folds")+outcomeListLen(a, "closes")+outcomeListLen(a, "links") == 0 {
		return fmt.Errorf("submit_outcome: at least one of folds, closes, links must be non-empty")
	}
	return nil
}

// outcomeListLen is the length of a JSON array arg. A missing arg, a null and a
// non-array value all count as zero, which every caller treats as "not given".
func outcomeListLen(a map[string]any, key string) int {
	l, _ := a[key].([]any)
	return len(l)
}

// outcomeList is the JSON array arg itself. A missing arg, a null and a
// non-array value all yield an empty (nil) slice, so a range over the result
// is a no-op rather than a panic.
func outcomeList(a map[string]any, key string) []any {
	l, _ := a[key].([]any)
	return l
}
