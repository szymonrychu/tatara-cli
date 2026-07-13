package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlatformTools_Count(t *testing.T) {
	require.Len(t, PlatformTools(), 7, "contract D.5")
}

func TestPlatformTools_Names(t *testing.T) {
	var got []string
	for _, tl := range PlatformTools() {
		got = append(got, tl.Name)
	}
	require.ElementsMatch(t, []string{
		"task_get", "task_list", "task_context", "task_note",
		"project_get", "repo_list", "report_internal_issue",
	}, got)
}

func TestTaskNote_HasNoAgentArg(t *testing.T) {
	tl := toolByName(t, PlatformTools(), "task_note")
	var schema struct {
		Properties map[string]any `json:"properties"`
		Required   []string       `json:"required"`
	}
	require.NoError(t, json.Unmarshal(tl.Schema, &schema))
	require.NotContains(t, schema.Properties, "agent",
		"the operator stamps the writer from Task.status.agentKind; agent=operator must be unreachable from a pod (contract C.2.6, fix 19)")
	require.ElementsMatch(t, []string{"kind", "body"}, schema.Required)
}

func TestTaskNote_KindEnum(t *testing.T) {
	tl := toolByName(t, PlatformTools(), "task_note")
	var schema struct {
		Properties struct {
			Kind struct {
				Enum []string `json:"enum"`
			} `json:"kind"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(tl.Schema, &schema))
	require.Equal(t, []string{"note", "plan", "handoff"}, schema.Properties.Kind.Enum)
}

func TestTaskNote_PostsToNotes(t *testing.T) {
	t.Setenv("TATARA_TASK", "tatara-clarify-2026-07-12-m4z8q")
	tl := toolByName(t, PlatformTools(), "task_note")
	m, p, body, err := tl.Build(map[string]any{"kind": "handoff", "body": "Scope locked. 3 repos."})
	require.NoError(t, err)
	require.Equal(t, "POST", m)
	require.Equal(t, "/tasks/tatara-clarify-2026-07-12-m4z8q/notes", p)
	require.NotNil(t, body)
}

func TestTaskContext_NotesAndIndexArgs(t *testing.T) {
	t.Setenv("TATARA_TASK", "t1")
	tl := toolByName(t, PlatformTools(), "task_context")
	_, p, _, err := tl.Build(map[string]any{})
	require.NoError(t, err)
	require.Equal(t, "/tasks/t1/context", p, "task defaults to your own")

	_, p, _, err = tl.Build(map[string]any{"notes": "all"})
	require.NoError(t, err)
	require.Contains(t, p, "notes=all", "this is the read path for notes spilled out of the CR (contract C.2.5)")

	_, p, _, err = tl.Build(map[string]any{"index": true})
	require.NoError(t, err)
	require.Contains(t, p, "index=true")

	_, _, _, err = tl.Build(map[string]any{"notes": "some"})
	require.Error(t, err, "notes is recent|all")
}

func TestTaskUpdateIsGone(t *testing.T) {
	for _, tl := range allNewTools() {
		require.NotEqual(t, "task_update", tl.Name, "PATCH /tasks/{t} is deleted (contract C.1); a Task's status is operator-owned")
	}
}
