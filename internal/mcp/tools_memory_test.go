package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMemoryTools_Count(t *testing.T) {
	require.Len(t, MemoryTools(), 5, "contract D.4")
}

func TestMemoryTools_Names(t *testing.T) {
	var got []string
	for _, tl := range MemoryTools() {
		got = append(got, tl.Name)
	}
	require.Equal(t, []string{"memory_query", "memory_describe", "memory_write", "memory_entity", "memory_edges"}, got)
}

func TestMemoryQuery_PostsToQueries(t *testing.T) {
	tl := toolByName(t, MemoryTools(), "memory_query")
	m, p, body, err := tl.Build(map[string]any{"query": "how does the reaper work"})
	require.NoError(t, err)
	require.Equal(t, "POST", m)
	require.Equal(t, "/queries", p)
	require.NotNil(t, body)
}

func TestMemoryDescribe_PostsToQueriesDescribe(t *testing.T) {
	tl := toolByName(t, MemoryTools(), "memory_describe")
	m, p, _, err := tl.Build(map[string]any{"query": "the operator"})
	require.NoError(t, err)
	require.Equal(t, "POST", m)
	require.Equal(t, "/queries:describe", p)
}

func TestMemoryEntity_OpMap(t *testing.T) {
	tl := toolByName(t, MemoryTools(), "memory_entity")
	for _, op := range []string{"get", "search", "patch"} {
		_, _, _, err := tl.Build(map[string]any{"op": op, "id": "e1", "q": "x", "patch": map[string]any{"a": 1}})
		require.NoError(t, err, "op=%s", op)
	}
	_, _, _, err := tl.Build(map[string]any{"op": "delete"})
	require.Error(t, err, "memory_entity has no delete op")
}

func TestMemoryEdges_OpMap(t *testing.T) {
	tl := toolByName(t, MemoryTools(), "memory_edges")
	for _, op := range []string{"list", "create", "delete"} {
		_, _, _, err := tl.Build(map[string]any{"op": op, "from": "a", "to": "b", "relation": "calls", "id": "e1"})
		require.NoError(t, err, "op=%s", op)
	}
}

func TestReapedMemoryToolsAreGone(t *testing.T) {
	live := map[string]bool{}
	for _, tl := range MemoryTools() {
		live[tl.Name] = true
	}
	for _, name := range []string{"get_memory", "delete_memory", "bulk_create_memories", "get_ingest_job", "create_memory", "query", "describe", "get_entity", "search_entities", "patch_entity", "list_edges", "create_edge", "delete_edge"} {
		require.False(t, live[name], "pre-contract memory tool %q must be gone", name)
	}
}

func TestMemoryTools_SchemasAreValidJSON(t *testing.T) {
	for _, tl := range MemoryTools() {
		var v any
		require.NoError(t, json.Unmarshal(tl.Schema, &v), "tool %s", tl.Name)
	}
}
