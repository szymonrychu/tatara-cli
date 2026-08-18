package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/szymonrychu/tatara-cli/internal/client"
)

// Target identifies which backend client a Tool is dispatched against.
type Target int

const (
	TargetMemory   Target = iota // default: existing tools hit tatara-memory
	TargetOperator               // operator tools hit tatara-operator
)

// Tool describes a tatara REST operation exposed as an MCP tool.
type Tool struct {
	Name        string
	Description string
	Schema      json.RawMessage
	Target      Target
	Build       func(args map[string]any) (method, path string, body any, err error)
	// Handler, when set, is called directly by register() instead of dispatching
	// an HTTP round-trip via clientFor/Invoke. Used for pure-local tools (e.g.
	// report_internal_issue) that execute in the cli process and need no backend.
	// log is the server's injected structured logger (same one used by register).
	Handler func(args map[string]any, log *slog.Logger) (string, error)
}

// argString coerces string or JSON number args to a string.
// JSON-decoded numbers always arrive as float64 (never int); the float64 branch
// handles both integer-valued and fractional numbers.
func argString(a map[string]any, k string) string {
	switch v := a[k].(type) {
	case string:
		return v
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return ""
	}
}

// requiredString reads a required string arg, erroring rather than defaulting.
func requiredString(a map[string]any, key string) (string, error) {
	s := argString(a, key)
	if s == "" {
		return "", fmt.Errorf("missing required argument %q", key)
	}
	return s, nil
}

// addOptional copies present args into a query string. Absent args are omitted
// entirely - never sent as an empty value, which a server would read as a
// filter for the empty string.
func addOptional(v url.Values, a map[string]any, keys ...string) {
	for _, k := range keys {
		if raw, ok := a[k]; ok {
			v.Set(k, fmt.Sprintf("%v", raw))
		}
	}
}

// asInt coerces a JSON number (which arrives as float64) to an int.
func asInt(v any) (int, error) {
	switch n := v.(type) {
	case float64:
		return int(n), nil
	case int:
		return n, nil
	}
	return 0, fmt.Errorf("not a number: %v", v)
}

// argOrEnv returns the arg value if non-empty, otherwise falls back to the
// named environment variable. This lets operator tool calls omit task= and
// project= when the wrapper Pod has injected TATARA_TASK / TATARA_PROJECT.
func argOrEnv(a map[string]any, key, envKey string) string {
	if v := argString(a, key); v != "" {
		return v
	}
	return os.Getenv(envKey)
}

// validCategories is the set of allowed category enum values for report_internal_issue.
var validCategories = map[string]bool{
	"tool_error":              true,
	"directive_contradiction": true,
	"workspace_broken":        true,
	"memory_inconsistent":     true,
	"graph_inconsistent":      true,
	"auth":                    true,
	"other":                   true,
}

// validSeverities is the set of allowed severity enum values for report_internal_issue.
var validSeverities = map[string]bool{
	"warn":  true,
	"error": true,
}

// PlatformTools returns the 7 always-reachable platform tools (contract D.5):
// task_get, task_list, task_context, task_note, project_get, repo_list and
// report_internal_issue. Six of the seven are alwaysOn (task_list is not -
// contract D.6 grants it only to brainstorm, incident and refine).
func PlatformTools() []Tool {
	return []Tool{
		{
			Name:        "task_get",
			Description: "Get a Task by name. Defaults to TATARA_TASK env when task is omitted.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"task":{"type":"string"}}}`),
			Target:      TargetOperator,
			Build: func(a map[string]any) (string, string, any, error) {
				tk := argOrEnv(a, "task", "TATARA_TASK")
				if tk == "" {
					return "", "", nil, fmt.Errorf("task required")
				}
				return http.MethodGet, "/tasks/" + url.PathEscape(tk), nil, nil
			},
		},
		{
			Name:        "task_list",
			Description: "List Tasks in a Project. Defaults to TATARA_PROJECT env when project is omitted.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"project":{"type":"string"}}}`),
			Target:      TargetOperator,
			Build: func(a map[string]any) (string, string, any, error) {
				p := argOrEnv(a, "project", "TATARA_PROJECT")
				if p == "" {
					return "", "", nil, fmt.Errorf("project required")
				}
				return http.MethodGet, "/projects/" + url.PathEscape(p) + "/tasks", nil, nil
			},
		},
		{
			Name:        "task_context",
			Description: "Re-read your Task's context bundle: its issues, merge requests, comment threads and notes. Defaults to your own Task.",
			Target:      TargetOperator,
			Schema: json.RawMessage(`{"type":"object","properties":{
  "task":{"type":"string","description":"Any Task in this project. Defaults to your own."},
  "index":{"type":"boolean","description":"Return the compact project-wide Task index instead of one Task's full bundle."},
  "notes":{"type":"string","enum":["recent","all"],
    "description":"recent (default) renders the notes in the bundle; all rehydrates the full note history, including notes spilled to memory. Use it when the <notes> marker says notes were elided."}},
 "additionalProperties":false}`),
			Build: func(a map[string]any) (string, string, any, error) {
				task := argOrEnv(a, "task", "TATARA_TASK")
				if task == "" {
					return "", "", nil, fmt.Errorf("task_context: no task argument and TATARA_TASK is unset")
				}
				v := url.Values{}
				if n := argString(a, "notes"); n != "" {
					if n != "recent" && n != "all" {
						return "", "", nil, fmt.Errorf("task_context: notes must be recent or all, got %q", n)
					}
					v.Set("notes", n)
				}
				if idx, ok := a["index"].(bool); ok && idx {
					v.Set("index", "true")
				}
				p := "/tasks/" + url.PathEscape(task) + "/context"
				if len(v) > 0 {
					p += "?" + v.Encode()
				}
				return "GET", p, nil, nil
			},
		},
		{
			Name:        "task_note",
			Description: "Append a note to your Task. This is the platform's ONLY agent-to-agent channel: the next pod on this Task reads your notes in its context bundle. Write a kind=handoff note before you stop.",
			Target:      TargetOperator,
			Schema: json.RawMessage(`{"type":"object","properties":{
  "task":{"type":"string","description":"Defaults to your own."},
  "kind":{"type":"string","enum":["note","plan","handoff"],"description":"REQUIRED. handoff is what the next pod reads first."},
  "body":{"type":"string","description":"REQUIRED. Truncated to 4096 bytes."}},
 "required":["kind","body"],"additionalProperties":false}`),
			Build: func(a map[string]any) (string, string, any, error) {
				task := argOrEnv(a, "task", "TATARA_TASK")
				if task == "" {
					return "", "", nil, fmt.Errorf("task_note: no task argument and TATARA_TASK is unset")
				}
				kind, err := requiredString(a, "kind")
				if err != nil {
					return "", "", nil, err
				}
				switch kind {
				case "note", "plan", "handoff":
				default:
					return "", "", nil, fmt.Errorf("task_note: kind must be note, plan or handoff, got %q", kind)
				}
				body, err := requiredString(a, "body")
				if err != nil {
					return "", "", nil, err
				}
				return "POST", "/tasks/" + url.PathEscape(task) + "/notes",
					map[string]any{"kind": kind, "body": body}, nil
			},
		},
		{
			Name:        "project_get",
			Description: "Get a Project by name. Defaults to TATARA_PROJECT env when project is omitted.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"project":{"type":"string"}}}`),
			Target:      TargetOperator,
			Build: func(a map[string]any) (string, string, any, error) {
				p := argOrEnv(a, "project", "TATARA_PROJECT")
				if p == "" {
					return "", "", nil, fmt.Errorf("project required")
				}
				return http.MethodGet, "/projects/" + url.PathEscape(p), nil, nil
			},
		},
		{
			Name:        "repo_list",
			Description: "List Repositories in a Project. Defaults to TATARA_PROJECT env when project is omitted.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"project":{"type":"string"}}}`),
			Target:      TargetOperator,
			Build: func(a map[string]any) (string, string, any, error) {
				p := argOrEnv(a, "project", "TATARA_PROJECT")
				if p == "" {
					return "", "", nil, fmt.Errorf("project required")
				}
				return http.MethodGet, "/projects/" + url.PathEscape(p) + "/repositories", nil, nil
			},
		},
		{
			Name:        "report_internal_issue",
			Description: "Report a platform-internal issue (tool error, directive contradiction, broken workspace, memory/graph inconsistency, auth problem, or other). Emits a structured ERROR log that the wrapper ships to the platform. Does NOT create a durable GitHub issue. Use this when you detect a systematic problem the platform team should know about.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"category":{"type":"string","enum":["tool_error","directive_contradiction","workspace_broken","memory_inconsistent","graph_inconsistent","auth","other"],"description":"Issue category."},"severity":{"type":"string","enum":["warn","error"],"description":"Severity level; defaults to error."},"description":{"type":"string","description":"Human-readable description of the issue. Required and must be non-empty."},"offending_tool":{"type":"string","description":"MCP tool name that triggered the issue, if applicable."},"resource_id":{"type":"string","description":"Resource identifier (task, repo, entity ID) related to the issue, if applicable."}},"required":["category","description"],"additionalProperties":false}`),
			// No Target or Build; Handler is set below.
			Handler: func(args map[string]any, log *slog.Logger) (string, error) {
				category, _ := args["category"].(string)
				if !validCategories[category] {
					return "", fmt.Errorf("category required: must be one of tool_error, directive_contradiction, workspace_broken, memory_inconsistent, graph_inconsistent, auth, other")
				}
				severity, _ := args["severity"].(string)
				if severity == "" {
					severity = "error"
				}
				if !validSeverities[severity] {
					return "", fmt.Errorf("severity must be one of warn, error")
				}
				description, _ := args["description"].(string)
				if strings.TrimSpace(description) == "" {
					return "", fmt.Errorf("description required (non-empty)")
				}

				offendingTool, _ := args["offending_tool"].(string)
				resourceID, _ := args["resource_id"].(string)
				project := os.Getenv("TATARA_PROJECT")
				task := os.Getenv("TATARA_TASK")

				logAttrs := []any{
					"category", category,
					"severity", severity,
					"description", description,
				}
				if offendingTool != "" {
					logAttrs = append(logAttrs, "offending_tool", offendingTool)
				}
				if resourceID != "" {
					logAttrs = append(logAttrs, "resource_id", resourceID)
				}
				if project != "" {
					logAttrs = append(logAttrs, "project", project)
				}
				if task != "" {
					logAttrs = append(logAttrs, "task", task)
				}

				// Log at the appropriate level via the server's injected logger.
				if severity == "warn" {
					log.Warn("internal issue reported", logAttrs...)
				} else {
					log.Error("internal issue reported", logAttrs...)
				}

				return fmt.Sprintf("internal issue reported: category=%s severity=%s", category, severity), nil
			},
		},
	}
}

// Invoke executes a tool against the given client and returns the raw response body.
func Invoke(ctx context.Context, c *client.Client, t Tool, args map[string]any) ([]byte, error) {
	method, path, body, err := t.Build(args)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		// For auth failures, return a generic message to avoid leaking token
		// details or internal proxy headers the backend may echo. Drain (capped)
		// so the connection can be reused.
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, errBodyCap))
			return nil, &statusError{resp.StatusCode,
				fmt.Sprintf("tatara: %s %s -> %d: authentication/authorization failed", method, path, resp.StatusCode)}
		}
		// Cap the error body to keep error strings (and memory) bounded.
		ebuf, err := io.ReadAll(io.LimitReader(resp.Body, errBodyCap))
		if err != nil {
			return nil, &statusError{resp.StatusCode,
				fmt.Sprintf("tatara: %s %s -> %d: read body: %v", method, path, resp.StatusCode, err)}
		}
		// A 409 whose reason the operator has taught this cli to act on
		// (head-moved, pr-not-ready) is not a failure: it names exactly what the
		// agent needs to fix and wants a resubmit, not a dead end. Render it as a
		// normal tool result, not a tool error.
		if resp.StatusCode == http.StatusConflict {
			if msg, ok := refusalGuidance(ebuf); ok {
				return []byte(msg), nil
			}
		}
		return nil, &statusError{resp.StatusCode,
			fmt.Sprintf("tatara: %s %s -> %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(ebuf)))}
	}
	// Success: read the full body. Tool results (graph queries, memory lists) are
	// routinely larger than the error cap and must not be truncated.
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("tatara: %s %s: read body: %w", method, path, err)
	}
	return buf, nil
}

// statusError is an Invoke failure that carries the backend's HTTP status code,
// so a caller can tell a 5xx (the backend is broken) from a 4xx (the request
// was). Error() renders exactly what the plain fmt.Errorf used to.
type statusError struct {
	code int
	msg  string
}

func (e *statusError) Error() string { return e.msg }

// errBodyCap bounds how many bytes of an error response body we read into an
// error message, preventing a hostile or broken backend from forcing unbounded
// memory use or multi-megabyte error strings.
const errBodyCap = 4096

// refusalGuidance renders the operator's STRUCTURED 409 as a normal tool
// result. The operator's readiness.go:87-92 has claimed this repo does it
// since pr-not-ready existed; it did not - every refusal but head-moved
// reached the agent as an unparsed JSON blob inside a tool ERROR. This is the
// reason-keyed renderer that fixes that.
//
// It decodes {reason, message, liveSHA, blocked[]} and returns rendered
// guidance for:
//   - "head-moved" (see tatara-operator /tasks/{t}/outcome): the message plus
//     the live SHA the agent should re-review against.
//   - "pr-not-ready" (see internal/restapi/readiness.go prNotReadyResponse):
//     the message plus one line per blocked MR naming repo, number and
//     blockers.
//
// Any other reason - including no reason at all - returns ok=false, so it
// stays a hard tool error. This carve-out is a CLOSED list, not a catch-all:
// an unrecognised reason is not something this cli knows how to turn into
// actionable guidance yet.
func refusalGuidance(body []byte) (string, bool) {
	var r struct {
		Reason  string `json:"reason"`
		Message string `json:"message"`
		LiveSHA string `json:"liveSHA"`
		Blocked []struct {
			Repo     string   `json:"repo"`
			Number   int      `json:"number"`
			HeadSHA  string   `json:"headSHA"`
			Blockers []string `json:"blockers"`
			Detail   string   `json:"detail"`
		} `json:"blocked"`
	}
	if json.Unmarshal(body, &r) != nil {
		return "", false
	}
	switch r.Reason {
	case "head-moved":
		msg := r.Message
		if r.LiveSHA != "" {
			msg += "\n\nliveSHA: " + r.LiveSHA
		}
		return msg, true
	case "pr-not-ready":
		msg := r.Message
		for _, m := range r.Blocked {
			// The head is part of the identity of what was judged, not decoration:
			// the operator judges each MR at its LIVE head, and an agent with more
			// than one push in flight cannot otherwise tell which one was read.
			// Only the ci-red detail ever embedded a SHA; conflict and
			// unresolved-review name none.
			ref := fmt.Sprintf("%s!%d", m.Repo, m.Number)
			if m.HeadSHA != "" {
				ref += "@" + m.HeadSHA
			}
			msg += fmt.Sprintf("\n\n%s [%s]: %s", ref, strings.Join(m.Blockers, ","), m.Detail)
		}
		return msg, true
	default:
		return "", false
	}
}
