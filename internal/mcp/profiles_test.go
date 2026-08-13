package mcp

import (
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

// TestKindProfiles_HasAllSevenAgentKindsAndNoClarify is the regression test
// for the live P0 (contract L.5) in its new form. The P0 was a MISSING key:
// the operator set TATARA_TOOL_PROFILE=clarify while this map had no clarify
// key, so every clarify pod failed closed to 6 tools with no submit_outcome.
// As of contract 4 the key is absent on purpose - the kind is deleted
// platform-wide - and this test pins BOTH halves so neither an accidental
// omission nor an accidental resurrection passes. "upgrade" is the 7th kind
// (2026-08-13): a scheduled agent kind that opens MRs the same way implement
// does, with no approval gate.
func TestKindProfiles_HasAllSevenAgentKindsAndNoClarify(t *testing.T) {
	want := []string{"brainstorm", "documentation", "implement", "incident", "refine", "review", "upgrade"}
	var got []string
	for k := range kindProfiles {
		got = append(got, k)
	}
	sort.Strings(got)
	require.Equal(t, want, got, "contract G.9: the map is keyed on the 7 AGENT kinds")
	require.NotContains(t, kindProfiles, "clarify",
		"clarify is deleted, not aliased: a surviving key would preserve a path to approval that skips the gate")
}

// TestUpgradeProfile_HasMRWriteButNoIssueWriteOrTaskList: the upgrade agent
// opens MRs and reads code, but it drives NO approval gate (issue_write) -
// nobody filed an issue for a scheduled upgrade - and must not be able to
// enumerate other Tasks' work (task_list). Unit-level dedup goes through
// task_context(index=true), which is always-on.
func TestUpgradeProfile_HasMRWriteButNoIssueWriteOrTaskList(t *testing.T) {
	allow := resolveProfile("upgrade", discard())
	for _, want := range []string{"mr_write", "scm_read", "code_search", "code_graph", "memory_write", "submit_outcome"} {
		require.True(t, allow[want], "upgrade profile must grant %s", want)
	}
	for _, deny := range []string{"issue_write", "task_list", "mr_takeover_request", "memory_entity", "memory_edges"} {
		require.False(t, allow[deny], "upgrade profile must NOT grant %s", deny)
	}
}

// TestAgentKinds_MatchTheOperatorsGolden is the ANTI-DRIFT test. tatara-cli and
// tatara-operator each carry a copy of the agent-kind set - by contract, not by
// import (there is no shared module: contract G). The golden file is the
// contract's copy, and BOTH repos check against a byte-identical copy of it.
// Drift between the two is exactly the P0 above; this test is what catches it
// next time.
func TestAgentKinds_MatchTheOperatorsGolden(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "agent-kinds.txt"))
	require.NoError(t, err)
	var want []string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			want = append(want, line)
		}
	}
	sort.Strings(want)
	require.Equal(t, want, AgentKinds(),
		"tatara-cli's profile set and tatara-operator's kindProfiles set must be IDENTICAL (contract G.9)")
	for _, k := range want {
		_, ok := OutcomeTool(k)
		require.True(t, ok, "agent kind %q has no submit_outcome schema", k)
	}
}

// TestKindProfiles_IsIdentity: the env value the operator sets IS the agent
// kind (contract G.9), so every key maps to itself. A non-identity entry is a
// translation layer, and a translation layer is what hid the clarify P0.
func TestKindProfiles_IsIdentity(t *testing.T) {
	for kind, profile := range kindProfiles {
		require.Equal(t, kind, profile, "TATARA_TOOL_PROFILE is the agent kind verbatim")
		_, ok := profiles[profile]
		require.True(t, ok, "kind %q maps to profile %q, which has no allow-set", kind, profile)
	}
}

// TestRetiredKindsAreGone: triage, lifecycle, selfImprove, healthCheck, and -
// as of contract 4 - clarify.
func TestRetiredKindsAreGone(t *testing.T) {
	for _, k := range []string{"clarify", "triage", "lifecycle", "triageIssue", "issueLifecycle", "selfImprove", "healthCheck"} {
		_, ok := profiles[k]
		require.False(t, ok, "retired kind %q must not be a profile", k)
	}
}

// TestAlwaysOnIsExactlyTheContractSix (contract D.6). task_list is deliberately
// NOT always-on: an implement/review pod that can list every Task can wander
// into another Task's work.
func TestAlwaysOnIsExactlyTheContractSix(t *testing.T) {
	require.ElementsMatch(t,
		[]string{"task_get", "task_context", "task_note", "project_get", "repo_list", "report_internal_issue"},
		alwaysOn)
	require.NotContains(t, alwaysOn, "task_list")
}

// TestProfileGatingTable_IsContractD6Verbatim is the D.6 table, transcribed.
//
// CONTRACT ERRATUM: contract D.6's summary line ("Counts (incl. always-on):
// brainstorm 17, incident 20, clarify 14, implement 17, review 16, refine 13,
// documentation 19") is arithmetically inconsistent with the gating table
// directly above it, which is the normative artifact. The surface is exactly 20
// tools, so a profile's count is 20 minus the cells the table marks "-":
// incident is denied issue_write and mr_write, so it is 18 and cannot be 20.
// review (15) and documentation (18) are likewise one below the summary. The
// rows win; the counts below are derived from them.
//
// The clarify row is DELETED at contract 4 and its issue_write / memory-recall
// grants are folded into implement, which is why implement is 18 and no longer
// the summary's 17. The upgrade row is new at 2026-08-13 (not part of contract
// D.6's original table): it is implement's code/memory grants plus mr_write,
// without issue_write or task_list.
func TestProfileGatingTable_IsContractD6Verbatim(t *testing.T) {
	alwaysOnSix := []string{"task_get", "task_context", "task_note", "project_get", "repo_list", "report_internal_issue"}
	table := map[string][]string{
		"brainstorm":    {"submit_outcome", "task_list", "scm_read", "code_search", "code_context", "code_graph", "code_explain", "memory_query", "memory_describe", "memory_write", "memory_entity"},
		"incident":      {"submit_outcome", "task_list", "scm_read", "code_search", "code_context", "code_graph", "code_explain", "memory_query", "memory_describe", "memory_write", "memory_entity", "memory_edges"},
		"implement":     {"submit_outcome", "scm_read", "issue_write", "mr_write", "mr_takeover_request", "code_search", "code_context", "code_graph", "code_explain", "memory_query", "memory_describe", "memory_write"},
		"review":        {"submit_outcome", "scm_read", "mr_write", "mr_takeover_request", "code_search", "code_context", "code_graph", "code_explain", "memory_query", "memory_describe"},
		"refine":        {"submit_outcome", "task_list", "scm_read", "issue_write", "mr_write", "memory_query", "memory_describe"},
		"documentation": {"submit_outcome", "scm_read", "mr_write", "code_search", "code_context", "code_graph", "code_explain", "memory_query", "memory_describe", "memory_write", "memory_entity", "memory_edges"},
		"upgrade":       {"submit_outcome", "scm_read", "mr_write", "code_search", "code_context", "code_graph", "code_explain", "memory_query", "memory_describe", "memory_write"},
	}
	counts := map[string]int{
		"brainstorm": 17, "incident": 18,
		"implement": 18, "review": 16, "refine": 13, "documentation": 18, "upgrade": 16,
	}
	require.Len(t, table, len(profiles), "every agent kind has a row and every row is an agent kind")
	for kind, extra := range table {
		allow := resolveProfile(kind, discard())
		want := append(append([]string{}, alwaysOnSix...), extra...)
		var got []string
		for n := range allow {
			got = append(got, n)
		}
		require.ElementsMatch(t, want, got, "profile %q does not match contract D.6", kind)
		require.Equal(t, counts[kind], len(allow), "profile %q tool count (contract D.6)", kind)
	}
}

// TestProfileGating_CodeAndMemoryAreNotUnconditional: the pre-P0 code unioned
// every memory tool and every code tool into every profile. Contract D.6
// profile-gates both groups.
func TestProfileGating_CodeAndMemoryAreNotUnconditional(t *testing.T) {
	refine := resolveProfile("refine", discard())
	for _, n := range []string{"code_search", "code_context", "code_graph", "code_explain"} {
		require.False(t, refine[n], "refine is a backlog groomer: it reads issues, not code (contract D.6)")
	}
	review := resolveProfile("review", discard())
	require.False(t, review["memory_write"], "graph-mutating memory tools are denied to reviewing pods")
}

// TestImplementAbsorbedClarifysGrantsButNotItsScopeCreep pins the clarify fold.
// The merged agent conducts the conversation on the issue AND writes the code,
// so it takes clarify's issue_write and memory RECALL tools. Everything clarify
// never had stays denied - the fold is a union of two allow-sets, not a licence
// to widen implement.
func TestImplementAbsorbedClarifysGrantsButNotItsScopeCreep(t *testing.T) {
	implement := resolveProfile("implement", discard())
	for _, n := range []string{"issue_write", "memory_query", "memory_describe"} {
		require.True(t, implement[n], "implement must absorb clarify's %q: the merged agent talks on the issue as well as writing code", n)
	}
	for _, n := range []string{"task_list", "memory_entity", "memory_edges"} {
		require.False(t, implement[n], "the clarify fold must not hand implement %q (contract D.6)", n)
	}
}

func TestResolveProfile_EmptyFailsClosedToExactlySixTools(t *testing.T) {
	for _, p := range []string{"", "nonsense", "triage"} {
		allow := resolveProfile(p, discard())
		var got []string
		for n := range allow {
			got = append(got, n)
		}
		require.ElementsMatch(t,
			[]string{"task_get", "task_context", "task_note", "project_get", "repo_list", "report_internal_issue"},
			got, "profile %q must fail CLOSED to the always-on six: all agents share one OIDC identity, so this is the authz boundary", p)
		require.NotContains(t, got, "submit_outcome", "a pod with no recognised profile must not be able to terminate a Task")
	}
}

func TestResolveProfile_NeverReturnsNil(t *testing.T) {
	require.NotNil(t, resolveProfile("", discard()))
	require.NotNil(t, resolveProfile("implement", discard()))
}

func TestRefine_MRWriteIsCommentOnly(t *testing.T) {
	// Contract J, RESIDUE 1: refine holds mr_write, but only action=comment.
	// The schema is shared, so the gate is cli-side (here) AND operator-side.
	require.True(t, resolveProfile("refine", discard())["mr_write"])
	require.Error(t, checkRefineMRWrite("refine", map[string]any{"action": "open", "repo": "r", "title": "t", "body": "b"}))
	require.Error(t, checkRefineMRWrite("refine", map[string]any{"action": "reply", "repo": "r", "number": 1, "in_reply_to": "1", "body": "b"}))
	require.NoError(t, checkRefineMRWrite("refine", map[string]any{"action": "comment", "repo": "r", "number": 1, "body": "b"}))
	require.NoError(t, checkRefineMRWrite("implement", map[string]any{"action": "open", "repo": "r", "title": "t", "body": "b"}))
}

// TestFourRegistriesAgreeOnTheAgentKindSet is the guard finding 3 of the
// upgrade-kind review asked for. resolveProfile sets allow["submit_outcome"]
// = true for ANY key present in profiles, unconditionally - but server.go:83
// only actually registers submit_outcome when outcomeSchemas[profile] exists.
// A kind added to kindProfiles/profiles but forgotten in outcomeSchemas would
// therefore produce an allow-set that CLAIMS a terminal tool the pod does not
// have: the Task could never terminate. Pin all four sources of the agent-kind
// set - kindProfiles, profiles, outcomeSchemas, and AgentKinds() itself - to
// be exactly identical, so any future kind addition that misses one of the
// four hand-edited maps fails here instead of shipping a live wedge.
func TestFourRegistriesAgreeOnTheAgentKindSet(t *testing.T) {
	keysOf := func(m map[string]bool) []string {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}

	kindProfilesKeys := map[string]bool{}
	for k := range kindProfiles {
		kindProfilesKeys[k] = true
	}
	profilesKeys := map[string]bool{}
	for k := range profiles {
		profilesKeys[k] = true
	}
	outcomeSchemasKeys := map[string]bool{}
	for k := range outcomeSchemas {
		outcomeSchemasKeys[k] = true
	}

	agentKinds := AgentKinds()

	require.Equal(t, agentKinds, keysOf(kindProfilesKeys), "kindProfiles must carry exactly the agent kinds")
	require.Equal(t, agentKinds, keysOf(profilesKeys), "profiles must carry exactly the agent kinds")
	require.Equal(t, agentKinds, keysOf(outcomeSchemasKeys), "outcomeSchemas must carry exactly the agent kinds - a kind missing here has an allow-set claiming submit_outcome that server.go never registers")
}

func TestAllProfileNamesExistInTheRegistries(t *testing.T) {
	live := map[string]bool{}
	for _, tl := range append(append(append(CodeTools(), MemoryTools()...), SCMTools()...), PlatformTools()...) {
		live[tl.Name] = true
	}
	live["submit_outcome"] = true
	for kind := range profiles {
		for name := range resolveProfile(kind, discard()) {
			require.True(t, live[name], "profile %q grants %q, which no constructor produces", kind, name)
		}
	}
}
