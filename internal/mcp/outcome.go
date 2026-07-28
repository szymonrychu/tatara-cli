package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// The seven submit_outcome schemas, contract D.1, copied verbatim. The
// description strings are the only documentation the model gets, and several of
// them (merge_order, reviewed_shas) are the sole warning about a failure mode
// with no other guard rail. Do not shorten them.
const implementOutcomeSchema = `{"type":"object","properties":{
  "task":{"type":"string"},
  "action":{"type":"string","enum":["submitted","declined"]},
  "title":{"type":"string","description":"MR title. Required when action=submitted."},
  "body":{"type":"string","description":"MR body. Required when action=submitted."},
  "change_significance":{"type":"string","enum":["major","minor","patch"],
    "description":"Required when action=submitted. major=backward-incompatible; minor=backward-compatible feature; patch=fix. YOU own this level - a reviewer may raise it but can never lower it."},
  "merge_order":{"type":"array","items":{"type":"string"},
    "description":"REQUIRED when this task's MRs span more than one repo: the Repository CR names in dependency order, first-merged first. There is NO default. Get it wrong and a downstream repo ships against an API that has not merged yet."},
  "decline_reason":{"type":"string","description":"Required when action=declined."}},
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

const clarifyOutcomeSchema = `{"type":"object","properties":{
  "task":{"type":"string"},
  "decision":{"type":"string","enum":["implement","close","discuss"]},
  "reason":{"type":"string","description":"Required. For decision=implement, say in plain words WHO approved and WHY you read their comment as approval."},
  "approval_citations":{"type":"array","items":{"type":"object","properties":{
      "id":{"type":"string"},"quote":{"type":"string"}},
    "required":["id","quote"]},
    "description":"Required for decision=implement whenever a maintainer has commented: ONE entry per issue this task owns. id is that issue's most recent maintainer comment's external_id, copied verbatim from the <comment external_id=\"...\"> attribute already in your turn-0 bundle - do NOT re-crawl to find it. quote is a VERBATIM substring of that same comment's body. YOU judge whether the comment approves; the operator re-reads the comment itself and refuses if the author is not a maintainer, if it is not the most recent maintainer comment, if your quote is not in it, or if it already approved once. Omit only when no human has commented at all."}},
 "required":["decision","reason"],"additionalProperties":false}`

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
var outcomeSchemas = map[string]json.RawMessage{
	"implement":     json.RawMessage(implementOutcomeSchema),
	"documentation": json.RawMessage(implementOutcomeSchema), // identical, per contract D.1
	"review":        json.RawMessage(reviewOutcomeSchema),
	"clarify":       json.RawMessage(clarifyOutcomeSchema),
	"brainstorm":    json.RawMessage(brainstormOutcomeSchema),
	"incident":      json.RawMessage(incidentOutcomeSchema),
	"refine":        json.RawMessage(refineOutcomeSchema),
}

// outcomeDescriptions is the per-profile tool description. The review one is
// contract D.1's "Note for skill authors" verbatim: it is the ONLY place that
// corrects the obvious misreading of a verdict enum - that the agent is picking
// a forge review event. It is not.
var outcomeDescriptions = map[string]string{
	"implement":     "Finish this implement task. action=submitted with the MR title, the MR body and the change_significance you own (plus merge_order when this task's MRs span more than one repo), or action=declined with a decline_reason. This is the only way an implement task terminates.",
	"documentation": "Finish this documentation task. action=submitted with the MR title, the MR body and the change_significance you own (plus merge_order when this task's MRs span more than one repo), or action=declined with a decline_reason. This is the only way a documentation task terminates.",
	"review":        "Submit your review verdict. verdict=approve does NOT post an approving review and verdict=request_changes does NOT post a REQUEST_CHANGES review - GitHub 422s a self-authored PR for both events, and this platform has one bot identity. You do not choose a forge review event and you never post a review yourself: the operator posts a COMMENT review carrying your verdict and findings, under the bot identity, from this payload. On verdict=approve the operator then merges - the merge is the approval of record.",
	"clarify":       "Finish this clarify task with a decision: implement, close or discuss, plus the reason. For decision=implement, also set approval_citations (one entry per owned issue with a maintainer comment) with that comment's external_id and a verbatim quote; the operator independently re-reads each cited comment and refuses the decision if the citation does not hold up.",
	"brainstorm":    "Finish this brainstorm task. action=propose with 1 to 5 issue proposals, action=skip with a reason when nothing is worth proposing THIS cycle (transient - expect something the next session), or action=exhausted with a reason when nothing is worth proposing until the project itself changes (PAUSES brainstorming for this project until it does - use sparingly, only when you genuinely mean for scheduling to hold). A silent finish is not allowed.",
	"incident":      "Finish this incident task. action=file_issue with the issue to open, action=false_positive, or action=comment_issue with comment{repo,number,body} to append fresh evidence to an existing open tracker when this alert is the SAME incident as one you found while surveying. All three require the alert_rules that fired and a reason. On file_issue, set issue.parent only when the new issue is genuinely-new-but-related to an existing open tracker you found - never for a same-rule duplicate.",
	"refine":        "Finish this refine task: the member tasks to fold in, the issues to close, and the issues or MRs to link. At least one of the three lists must be non-empty.",
}

// outcomeArgMap renames the snake_case tool args to the camelCase wire fields
// the operator's DisallowUnknownFields decoder expects (contract C.2.7).
// Anything not listed here goes through unchanged.
var outcomeArgMap = map[string]string{
	"change_significance": "changeSignificance",
	"merge_order":         "mergeOrder",
	"decline_reason":      "reason",
	"reviewed_shas":       "reviewedSHAs",
	"alert_rules":         "alertRules",
	"approval_citations":  "approvalCitations",
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
	case "implement", "documentation":
		return validateImplementOutcome(a)
	case "review":
		return validateReviewOutcome(a)
	case "clarify":
		return validateClarifyOutcome(a)
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

func validateImplementOutcome(a map[string]any) error {
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
	case "":
		return fmt.Errorf("submit_outcome: action required: one of submitted|declined")
	default:
		return fmt.Errorf("submit_outcome: action must be one of submitted|declined")
	}
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

func validateClarifyOutcome(a map[string]any) error {
	switch argString(a, "decision") {
	case "implement", "close", "discuss":
	case "":
		return fmt.Errorf("submit_outcome: decision required: one of implement|close|discuss")
	default:
		return fmt.Errorf("submit_outcome: decision must be one of implement|close|discuss")
	}
	if strings.TrimSpace(argString(a, "reason")) == "" {
		return fmt.Errorf("submit_outcome: reason required (non-empty) on every clarify decision")
	}
	// Shape only. WHETHER a citation was needed, whether it is the most recent
	// maintainer comment, and whether the quote really occurs in the body are
	// the OPERATOR's calls - it holds the mirror. A refusal there is a 200 +
	// park, not an error here.
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
