package mcp

import "log/slog"

// toolProfileForKind maps a CRD task Kind to a TATARA_TOOL_PROFILE value.
// healthCheck uses Kind=brainstorm per the operator (same goal, same tools).
// Unknown/empty kinds return "" (resolveProfile treats empty specially: fail-open).
func toolProfileForKind(kind string) string {
	switch kind {
	case "implement":
		return "implement"
	case "review":
		return "review"
	case "triageIssue":
		return "triage"
	case "brainstorm", "healthCheck":
		return "brainstorm"
	case "issueLifecycle":
		return "lifecycle"
	case "incident":
		return "incident"
	case "refine":
		return "refine"
	case "documentation":
		return "documentation"
	default:
		return ""
	}
}

// groupMemory is the set of all 13 AllTools memory tools.
var groupMemory = []string{
	"create_memory",
	"get_memory",
	"delete_memory",
	"bulk_create_memories",
	"get_ingest_job",
	"query",
	"describe",
	"get_entity",
	"search_entities",
	"patch_entity",
	"list_edges",
	"create_edge",
	"delete_edge",
}

// groupCodeGraph is the set of all 19 post-Part-B code_* tools.
var groupCodeGraph = []string{
	"code_search",
	"code_entity",
	"code_neighbors",
	"code_callers",
	"code_callees",
	"code_dependents",
	"code_dependencies",
	"code_file_imports",
	"code_resource_graph",
	"code_cross_repo",
	"code_path",
	"code_important",
	"code_stats",
	"code_ambiguous_edges",
	"code_explain",
	"code_related",
	"code_hyperedges",
	"code_communities",
	"code_bridges",
}

// groupChat is the set of all 10 chat_* tools.
var groupChat = []string{
	"chat_create_room",
	"chat_list_rooms",
	"chat_get_room",
	"chat_close_room",
	"chat_add_participant",
	"chat_list_participants",
	"chat_remove_participant",
	"chat_send_message",
	"chat_poll_messages",
	"chat_get_log",
}

// groupHandoff is the read/write handoff continuation tools: profiles that
// carry work continuity get these (write/get/list), but NOT delete_handoff
// (see groupHandoffDelete - refine-only, the groomer).
var groupHandoff = []string{
	"write_handoff",
	"get_handoff",
	"list_handoffs",
}

// groupHandoffDelete is delete_handoff, gated to the refine profile alone
// (refine is the handoff groomer; every other continuity profile can write/
// read but must never delete another pod's handoff).
var groupHandoffDelete = []string{
	"delete_handoff",
}

// alwaysOn tools are unioned into every profile.
var alwaysOn = []string{
	"report_internal_issue",
	"project_get",
	"repo_list",
	"task_get",
}

// profileSpec defines the operator tool names (beyond alwaysOn) and whether chat is included.
type profileSpec struct {
	chat bool
	// handoff grants the read/write continuation tools (groupHandoff): the
	// work kinds that carry continuity across pods.
	handoff bool
	// handoffDelete grants delete_handoff (groupHandoffDelete). refine-only:
	// it is the handoff groomer, every other continuity profile is deny-by-default.
	handoffDelete bool
	operator      []string
}

var profiles = map[string]profileSpec{
	"refine": {
		// no chat: refine is a headless backlog groomer (close dups/done, edit-tighten survivors).
		// Deny-by-default: create_issue is omitted (issue creation = escalation + a labels[]
		// trigger-label vector). Every SCM-mutation / lifecycle-escalation tool is likewise absent.
		// refine DOES get handoff (list/get/write) + is the sole profile with handoffDelete: it
		// grooms stale/done handoffs.
		handoff:       true,
		handoffDelete: true,
		operator: []string{
			"task_list",
			"list_issues", "list_commits",
			"close_issue", "edit_issue",
			"comment_on_issue",
		},
	},
	"brainstorm": {
		chat:    true,
		handoff: true,
		operator: []string{
			"task_list", "subtask_list", "subtask_create", "subtask_update",
			"propose_issue", "comment_on_issue", "skip_research",
			"harness_state_get", "harness_state_cas",
		},
	},
	"implement": {
		chat:    false,
		handoff: true,
		operator: []string{
			"task_update", "subtask_list", "subtask_create", "subtask_update",
			"change_summary", "decline_implementation", "already_done", "submit_handover",
		},
	},
	"review": {
		chat: false,
		operator: []string{
			"task_update", "subtask_list",
			"review_verdict", "submit_handover",
		},
	},
	"triage": {
		chat: false,
		operator: []string{
			"task_list", "task_update", "subtask_list", "subtask_create", "subtask_update",
			"issue_outcome", "comment", "comment_on_issue",
		},
	},
	"lifecycle": {
		// UNION of all states: one long-lived pod spans Triage -> Implement -> MRCI -> Merge.
		// Per-lifecycle-state gating infeasible without restarting the MCP server.
		chat:    true,
		handoff: true,
		operator: []string{
			"task_list", "task_update", "subtask_list", "subtask_create", "subtask_update",
			"issue_outcome", "comment", "comment_on_issue", "change_summary",
			"decline_implementation", "already_done", "pr_outcome", "review_verdict", "submit_handover",
		},
	},
	"incident": {
		chat:    true,
		handoff: true,
		operator: []string{
			"task_list", "task_update", "subtask_list", "subtask_create", "subtask_update",
			"propose_issue", "comment_on_issue", "change_summary", "decline_implementation", "submit_handover",
			"harness_state_get", "harness_state_cas",
		},
	},
	"documentation": {
		// no chat: single-shot, no human/peer conversation.
		// handoff on: carries continuity same as implement/incident (reroll/blocked path).
		chat:    false,
		handoff: true,
		operator: []string{
			"task_update", "subtask_list", "subtask_create", "subtask_update",
			"change_summary", "decline_implementation", "already_done",
		},
	},
}

// resolveProfile returns the set of allowed tool names for the given profile.
// Empty profile returns nil (fail-open: all tools allowed) - this is a documented,
// separate case (e.g. local dev without TATARA_TOOL_PROFILE set) and is unchanged.
// An unrecognized NON-EMPTY profile string fails CLOSED to the alwaysOn set only -
// profile gating is the sole authz boundary (all agents share one OIDC identity),
// so a typo'd/unknown profile must never silently grant the full tool surface.
// A WARN is logged for both empty and unknown profiles.
func resolveProfile(profile string, log *slog.Logger) map[string]bool {
	if profile == "" {
		log.Warn("TATARA_TOOL_PROFILE not set; serving full tool set (fail-open)")
		return nil
	}
	spec, ok := profiles[profile]
	if !ok {
		log.Warn("unknown TATARA_TOOL_PROFILE; failing closed to alwaysOn tools only", "profile", profile)
		allow := make(map[string]bool)
		for _, n := range alwaysOn {
			allow[n] = true
		}
		return allow
	}

	allow := make(map[string]bool)
	// memory + code-graph always included
	for _, n := range groupMemory {
		allow[n] = true
	}
	for _, n := range groupCodeGraph {
		allow[n] = true
	}
	// chat conditional
	if spec.chat {
		for _, n := range groupChat {
			allow[n] = true
		}
	}
	// handoff conditional (write/get/list): continuity-carrying profiles only
	if spec.handoff {
		for _, n := range groupHandoff {
			allow[n] = true
		}
	}
	// handoffDelete conditional: refine (the groomer) only
	if spec.handoffDelete {
		for _, n := range groupHandoffDelete {
			allow[n] = true
		}
	}
	// profile-specific operator tools
	for _, n := range spec.operator {
		allow[n] = true
	}
	// alwaysOn unioned last (cannot be removed by a profile definition)
	for _, n := range alwaysOn {
		allow[n] = true
	}
	return allow
}
