package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// scmNumber renders the "number" arg as a decimal string for query/body use.
// argString only handles string/float64 (real JSON-RPC args always decode as
// float64); asInt additionally accepts a bare Go int, so both representations
// resolve to the same wire value.
func scmNumber(a map[string]any) string {
	if s := argString(a, "number"); s != "" {
		return s
	}
	if n, err := asInt(a["number"]); err == nil {
		return strconv.Itoa(n)
	}
	return ""
}

// SCMTools returns the 3 forge tools (contract D.2). The agent pod has NO forge
// token and gh/glab are banned; these are its only view of, and only voice on,
// the forge. scm_read(issues|mr|comments) is served from tatara's own CR mirror
// of the forge - free, fast, at most one sweep stale. Only kind=ci leaves the
// cluster, and the operator paces it to one fetch per 20s per PR.
func SCMTools() []Tool {
	return []Tool{
		{
			Name:        "scm_read",
			Description: "Read issues, merge requests, comment threads, commits, or CI status from the forge. issues, mr, and comments are served from tatara's own mirror of the forge (fast, free, at most one hour stale) - only kind=ci leaves the cluster, and the operator paces it to one fetch per 20s per PR.",
			Target:      TargetOperator,
			Schema: json.RawMessage(`{"type":"object","properties":{
  "kind":{"type":"string","enum":["issues","mr","comments","commits","ci"],
    "description":"issues|mr|comments are served from tatara's own mirror of the forge (fast, free, at most one hour stale). ci is a live read of the forge and is paced server-side to one fetch per 20s per PR."},
  "project":{"type":"string"},
  "repo":{"type":"string","description":"Repository CR name, e.g. tatara-operator. REQUIRED."},
  "number":{"type":"integer","description":"Required for kind=ci and kind=comments."},
  "is_pr":{"type":"boolean","description":"kind=comments only: read the MR thread instead of the issue thread."},
  "state":{"type":"string","enum":["open","closed","merged","all"],
    "description":"kind=issues (open|closed|all) and kind=mr (open|merged|closed|all)."},
  "since":{"type":"string","description":"kind=issues|mr only. RFC3339."},
  "labels":{"type":"string","description":"kind=issues only. Comma-separated."},
  "since_days":{"type":"integer","description":"kind=commits only. Default 30."},
  "limit":{"type":"integer"}},
 "required":["kind","repo"],"additionalProperties":false}`),
			Build: func(a map[string]any) (string, string, any, error) {
				kind := argString(a, "kind")
				switch kind {
				case "issues", "mr", "comments", "commits", "ci":
				default:
					return "", "", nil, fmt.Errorf("kind must be one of issues, mr, comments, commits, ci")
				}
				p := argOrEnv(a, "project", "TATARA_PROJECT")
				if p == "" {
					return "", "", nil, fmt.Errorf("project required")
				}
				repo := argString(a, "repo")
				if repo == "" {
					return "", "", nil, fmt.Errorf("repo required: an omitted repo would fan out ~60 unpaced forge requests")
				}
				_, hasNumber := a["number"]
				if (kind == "ci" || kind == "comments") && !hasNumber {
					return "", "", nil, fmt.Errorf("number required for kind=%s", kind)
				}

				q := url.Values{}
				q.Set("repo", repo)
				var subpath string
				switch kind {
				case "issues":
					subpath = "issues"
					addOptional(q, a, "state", "since", "labels", "limit")
				case "mr":
					subpath = "mrs"
					addOptional(q, a, "state", "limit")
				case "comments":
					subpath = "comments"
					q.Set("number", scmNumber(a))
					if v, ok := a["is_pr"].(bool); ok && v {
						q.Set("isPR", "true")
					}
				case "commits":
					subpath = "commits"
					if v := argString(a, "since_days"); v != "" {
						q.Set("sinceDays", v)
					}
					addOptional(q, a, "limit")
				case "ci":
					subpath = "ci"
					q.Set("number", scmNumber(a))
				}
				return http.MethodGet, "/projects/" + url.PathEscape(p) + "/scm/" + subpath + "?" + q.Encode(), nil, nil
			},
		},
		{
			Name:        "issue_write",
			Description: "Create, edit, close, or comment on a forge issue. action=create is SYNCHRONOUS: the forge post happens inline and the response returns the issue number and url. action=edit, action=close, and action=comment are DEFERRED: this call returns a 200 ack with no comment id - a reconciler posts to the forge, and the resulting comment appears in the next task_context read.",
			Target:      TargetOperator,
			Schema: json.RawMessage(`{"type":"object","properties":{
  "action":{"type":"string","enum":["create","edit","close","comment"]},
  "project":{"type":"string"},"repo":{"type":"string"},
  "number":{"type":"integer"},"title":{"type":"string"},"body":{"type":"string"},
  "comment":{"type":"string","description":"Required for action=close: every close cites its reason."}},
 "required":["action","repo"],"additionalProperties":false}`),
			Build: func(a map[string]any) (string, string, any, error) {
				action := argString(a, "action")
				repo := argString(a, "repo")
				if repo == "" {
					return "", "", nil, fmt.Errorf("repo required")
				}
				p := argOrEnv(a, "project", "TATARA_PROJECT")
				if p == "" {
					return "", "", nil, fmt.Errorf("project required")
				}
				_, hasNumber := a["number"]
				switch action {
				case "create":
					// The operator creates the Issue CR and assigns the number; a
					// caller-supplied number is rejected client-side to mirror that gate.
					if hasNumber {
						return "", "", nil, fmt.Errorf("action=create forbids number: the operator assigns it")
					}
					if argString(a, "title") == "" || argString(a, "body") == "" {
						return "", "", nil, fmt.Errorf("action=create requires title and body")
					}
				case "edit":
					if !hasNumber {
						return "", "", nil, fmt.Errorf("action=edit requires number")
					}
					if argString(a, "title") == "" && argString(a, "body") == "" {
						return "", "", nil, fmt.Errorf("action=edit requires at least one of title, body")
					}
				case "close":
					if !hasNumber {
						return "", "", nil, fmt.Errorf("action=close requires number")
					}
					if strings.TrimSpace(argString(a, "comment")) == "" {
						return "", "", nil, fmt.Errorf("action=close requires comment: every close cites its reason")
					}
				case "comment":
					if !hasNumber {
						return "", "", nil, fmt.Errorf("action=comment requires number")
					}
					if argString(a, "body") == "" {
						return "", "", nil, fmt.Errorf("action=comment requires body")
					}
				default:
					return "", "", nil, fmt.Errorf("action must be one of create, edit, close, comment")
				}
				body := map[string]any{}
				for k, v := range a {
					if k == "project" {
						continue
					}
					body[k] = v
				}
				body["task"] = argOrEnv(a, "task", "TATARA_TASK")
				return http.MethodPost, "/projects/" + url.PathEscape(p) + "/scm/issue-write", body, nil
			},
		},
		{
			Name:        "mr_write",
			Description: "Open a merge request, or comment/reply on one. action=open is SYNCHRONOUS: the response returns the MR number, url, and existing (true if you already opened one for this task). action=comment and action=reply are DEFERRED: this call returns a 200 ack with no comment id - a reconciler posts to the forge, and the resulting comment appears in the next task_context read.",
			Target:      TargetOperator,
			Schema: json.RawMessage(`{"type":"object","properties":{
  "action":{"type":"string","enum":["open","comment","reply"]},
  "project":{"type":"string"},"repo":{"type":"string"},
  "number":{"type":"integer"},"title":{"type":"string"},"body":{"type":"string"},
  "in_reply_to":{"type":"string","description":"Required for action=reply: the externalId from scm_read(kind=comments)."}},
 "required":["action","repo"],"additionalProperties":false}`),
			Build: func(a map[string]any) (string, string, any, error) {
				action := argString(a, "action")
				repo := argString(a, "repo")
				if repo == "" {
					return "", "", nil, fmt.Errorf("repo required")
				}
				p := argOrEnv(a, "project", "TATARA_PROJECT")
				if p == "" {
					return "", "", nil, fmt.Errorf("project required")
				}
				_, hasNumber := a["number"]
				switch action {
				case "open":
					// The operator creates the MergeRequest CR and assigns the number;
					// a caller-supplied number is rejected client-side to mirror that gate.
					if hasNumber {
						return "", "", nil, fmt.Errorf("action=open forbids number: the operator assigns it")
					}
					if argString(a, "title") == "" || argString(a, "body") == "" {
						return "", "", nil, fmt.Errorf("action=open requires title and body")
					}
				case "comment":
					if !hasNumber {
						return "", "", nil, fmt.Errorf("action=comment requires number")
					}
					if argString(a, "body") == "" {
						return "", "", nil, fmt.Errorf("action=comment requires body")
					}
				case "reply":
					if !hasNumber {
						return "", "", nil, fmt.Errorf("action=reply requires number")
					}
					if argString(a, "in_reply_to") == "" {
						return "", "", nil, fmt.Errorf("action=reply requires in_reply_to: the externalId being replied to")
					}
					if argString(a, "body") == "" {
						return "", "", nil, fmt.Errorf("action=reply requires body")
					}
				default:
					return "", "", nil, fmt.Errorf("action must be one of open, comment, reply")
				}
				body := map[string]any{}
				for k, v := range a {
					if k == "project" {
						continue
					}
					body[k] = v
				}
				if v, ok := body["in_reply_to"]; ok {
					delete(body, "in_reply_to")
					body["inReplyTo"] = v
				}
				body["task"] = argOrEnv(a, "task", "TATARA_TASK")
				return http.MethodPost, "/projects/" + url.PathEscape(p) + "/scm/mr-write", body, nil
			},
		},
	}
}
