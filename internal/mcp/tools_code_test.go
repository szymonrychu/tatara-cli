package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodeTools_Count(t *testing.T) {
	require.Len(t, CodeTools(), 4, "contract D.3: code_search, code_context, code_graph, code_explain")
}

func TestCodeTools_Names(t *testing.T) {
	var got []string
	for _, tl := range CodeTools() {
		got = append(got, tl.Name)
	}
	require.Equal(t, []string{"code_search", "code_context", "code_graph", "code_explain"}, got)
}

func TestCodeContext_RelPathMap(t *testing.T) {
	tl := toolByName(t, CodeTools(), "code_context")
	cases := map[string]string{
		"entity":       "/code/entity",
		"neighbors":    "/code/neighbors",
		"callers":      "/code/callers",
		"callees":      "/code/callees",
		"dependents":   "/code/dependents",
		"dependencies": "/code/dependencies",
		"file_imports": "/code/file-imports",
		"related":      "/code-graph/related",
		"cross_repo":   "/code/cross-repo",
	}
	for rel, wantPath := range cases {
		_, path, _, err := tl.Build(map[string]any{"repo": "tatara-cli", "rel": rel, "id": "x"})
		require.NoError(t, err, "rel=%s", rel)
		require.Contains(t, path, wantPath, "rel=%s", rel)
	}
	_, _, _, err := tl.Build(map[string]any{"repo": "tatara-cli", "rel": "nope"})
	require.Error(t, err, "an unknown rel must be refused, never silently defaulted")
}

func TestCodeGraph_OpPathMap(t *testing.T) {
	tl := toolByName(t, CodeTools(), "code_graph")
	cases := map[string]string{
		"path":           "/code-graph/path",
		"important":      "/code-graph/important",
		"stats":          "/code-graph/stats",
		"ambiguous":      "/code-graph/ambiguous",
		"communities":    "/code-graph/communities",
		"hyperedges":     "/code-graph/hyperedges",
		"bridges":        "/code-graph/bridges",
		"resource_graph": "/code/resource-graph",
	}
	for op, wantPath := range cases {
		_, path, _, err := tl.Build(map[string]any{"repo": "tatara-cli", "op": op})
		require.NoError(t, err, "op=%s", op)
		require.Contains(t, path, wantPath, "op=%s", op)
	}
}

func TestCodeGraph_CommunityAndHyperedgeSingulars(t *testing.T) {
	tl := toolByName(t, CodeTools(), "code_graph")
	_, path, _, err := tl.Build(map[string]any{"repo": "tatara-cli", "op": "communities", "community": "7"})
	require.NoError(t, err)
	require.Contains(t, path, "/code-graph/community", "a named community reads the singular endpoint")

	_, path, _, err = tl.Build(map[string]any{"repo": "tatara-cli", "op": "hyperedges", "id": "h1"})
	require.NoError(t, err)
	require.Contains(t, path, "/code-graph/hyperedge", "a named hyperedge reads the singular endpoint")
}

func TestCodeContext_DepthCappedAtFour(t *testing.T) {
	tl := toolByName(t, CodeTools(), "code_context")
	_, _, _, err := tl.Build(map[string]any{"repo": "tatara-cli", "rel": "callers", "id": "x", "depth": 5})
	require.Error(t, err, "depth max is 4 (contract D.3)")
}

func TestCodeTools_SchemasAreValidJSON(t *testing.T) {
	for _, tl := range CodeTools() {
		var v any
		require.NoError(t, json.Unmarshal(tl.Schema, &v), "tool %s", tl.Name)
	}
}
