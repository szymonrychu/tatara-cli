package mcp

import (
	"fmt"
	"log/slog"
	"sort"
)

// kindProfiles maps Task.status.agentKind to a TATARA_TOOL_PROFILE value. It is
// keyed on the AGENT kind (6 values), not the Task origin kind. tatara-operator
// carries the same map at internal/agent/pod.go - by contract, not by import
// (there is no shared module). testdata/agent-kinds.txt is the golden both
// repos check against; see TestAgentKinds_MatchTheOperatorsGolden.
//
// A key that is MISSING here while the operator still sets it is a fleet wedge:
// resolveProfile fails CLOSED, so the pod gets six tools, no submit_outcome,
// and its Task can never terminate. That was a live P0 (contract L.5) when
// "clarify" was absent here while pod.go set it. "clarify" is absent again as
// of contract 4, and this time deliberately: the kind is deleted platform-wide
// and the operator's migration rewrites every live clarify Task to implement.
var kindProfiles = map[string]string{
	"brainstorm":    "brainstorm",
	"documentation": "documentation",
	"implement":     "implement",
	"incident":      "incident",
	"refine":        "refine",
	"review":        "review",
}

// AgentKinds returns the six agent kinds, sorted.
func AgentKinds() []string {
	out := make([]string, 0, len(kindProfiles))
	for k := range kindProfiles {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// alwaysOn is served under EVERY profile, including the fail-closed empty one.
// Contract D.6. task_list is deliberately NOT here: an implement/review pod
// that can list every Task can wander into another Task's work.
var alwaysOn = []string{
	"task_get", "task_context", "task_note",
	"project_get", "repo_list", "report_internal_issue",
}

// profiles is contract D.6, one row per agent kind. Everything a profile is not
// granted is NOT REGISTERED for that pod: tools/list is per-profile. The code
// and memory groups are gated here too - they used to be unioned into every
// profile unconditionally, which is what the D.6 table exists to stop.
var profiles = map[string][]string{
	"brainstorm": {"task_list", "scm_read", "code_search", "code_context", "code_graph", "code_explain", "memory_query", "memory_describe", "memory_write", "memory_entity"},
	"incident":   {"task_list", "scm_read", "code_search", "code_context", "code_graph", "code_explain", "memory_query", "memory_describe", "memory_write", "memory_entity", "memory_edges"},
	// implement absorbed the deleted clarify profile's grants: the merged agent
	// conducts the conversation on the issue AND writes the code, so it needs
	// clarify's issue_write and its memory RECALL tools. It deliberately does
	// NOT gain task_list (contract D.6 denies it to implement) and it does NOT
	// gain memory_entity or memory_edges.
	"implement":     {"scm_read", "issue_write", "mr_write", "mr_takeover_request", "code_search", "code_context", "code_graph", "code_explain", "memory_query", "memory_describe", "memory_write"},
	"review":        {"scm_read", "mr_write", "mr_takeover_request", "code_search", "code_context", "code_graph", "code_explain", "memory_query", "memory_describe"},
	"refine":        {"task_list", "scm_read", "issue_write", "mr_write", "memory_query", "memory_describe"},
	"documentation": {"scm_read", "mr_write", "code_search", "code_context", "code_graph", "code_explain", "memory_query", "memory_describe", "memory_write", "memory_entity", "memory_edges"},
}

// resolveProfile FAILS CLOSED. Every agent pod on this platform shares ONE OIDC
// identity, so this allow-set is the ONLY thing separating a review pod from an
// implement pod: it IS the authz boundary. An empty or unknown profile therefore
// serves the always-on six and NOTHING else - in particular, no submit_outcome,
// so a pod with a profile we do not understand cannot terminate a Task.
// resolveProfile NEVER returns nil.
//
// If a pod is failing closed, the fix is to ADD its profile here. It is NEVER to
// loosen this branch. (The doc-comment that used to sit at the top of this file
// claimed "fail-open". It was wrong, and had been wrong for the life of the file.)
func resolveProfile(profile string, log *slog.Logger) map[string]bool {
	allow := make(map[string]bool, 20)
	for _, n := range alwaysOn {
		allow[n] = true
	}
	granted, ok := profiles[profile]
	if !ok {
		if profile == "" {
			log.Warn("TATARA_TOOL_PROFILE not set; failing closed to the always-on tools only",
				"action", "profile_unresolved", "tools", len(allow))
		} else {
			log.Warn("unknown TATARA_TOOL_PROFILE; failing closed to the always-on tools only",
				"action", "profile_unresolved", "profile", profile, "tools", len(allow))
		}
		return allow
	}
	for _, n := range granted {
		allow[n] = true
	}
	allow["submit_outcome"] = true
	return allow
}

// checkRefineMRWrite is contract J, RESIDUE 1: refine may hold mr_write, but only
// for action=comment (a backlog groomer may reply on an MR thread; it may not open
// one). The schema is shared across profiles, so this gate is code, not schema -
// on BOTH sides: the operator enforces the same rule at POST /scm/mr-write.
func checkRefineMRWrite(profile string, args map[string]any) error {
	if profile != "refine" {
		return nil
	}
	if argString(args, "action") != "comment" {
		return fmt.Errorf("mr_write: the refine profile may only use action=comment")
	}
	return nil
}
