package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func chatToolByName(t *testing.T, name string) Tool {
	t.Helper()
	for _, tl := range ChatTools() {
		if tl.Name == name {
			return tl
		}
	}
	t.Fatalf("chat tool %q not found", name)
	return Tool{}
}

func TestChatTools_Count(t *testing.T) {
	require.Len(t, ChatTools(), 10)
}

func TestChatTools_TargetIsChat(t *testing.T) {
	for _, tl := range ChatTools() {
		require.Equal(t, TargetChat, tl.Target)
	}
}

func TestChatTools_SchemasAreValidJSON(t *testing.T) {
	for _, tl := range ChatTools() {
		var v any
		require.NoErrorf(t, json.Unmarshal(tl.Schema, &v), "chat tool %q has invalid JSON schema", tl.Name)
	}
}

func TestChatTools_BuildPaths(t *testing.T) {
	cases := []struct {
		tool   string
		args   map[string]any
		method string
		path   string
	}{
		{"chat_create_room", map[string]any{"name": "r", "created_by": "a"}, http.MethodPost, "/rooms"},
		{"chat_list_rooms", map[string]any{}, http.MethodGet, "/rooms"},
		{"chat_list_rooms", map[string]any{"status": "active"}, http.MethodGet, "/rooms?status=active"},
		{"chat_get_room", map[string]any{"room": "r1"}, http.MethodGet, "/rooms/r1"},
		{"chat_close_room", map[string]any{"room": "r1"}, http.MethodDelete, "/rooms/r1"},
		{"chat_add_participant", map[string]any{"room": "r1", "name": "p"}, http.MethodPost, "/rooms/r1/participants"},
		{"chat_list_participants", map[string]any{"room": "r1"}, http.MethodGet, "/rooms/r1/participants"},
		{"chat_remove_participant", map[string]any{"room": "r1", "participant": "p1"}, http.MethodDelete, "/rooms/r1/participants/p1"},
		{"chat_send_message", map[string]any{"room": "r1", "participant_id": "p1", "body": "hi"}, http.MethodPost, "/rooms/r1/messages"},
		{"chat_poll_messages", map[string]any{"room": "r1", "participant": "p1"}, http.MethodGet, "/rooms/r1/messages?participant=p1"},
		{"chat_get_log", map[string]any{"room": "r1"}, http.MethodGet, "/rooms/r1/log"},
		{"chat_get_log", map[string]any{"room": "r1", "after": float64(5), "limit": float64(10)}, http.MethodGet, "/rooms/r1/log?after=5&limit=10"},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			m, p, _, err := chatToolByName(t, c.tool).Build(c.args)
			require.NoError(t, err)
			require.Equal(t, c.method, m)
			require.Equal(t, c.path, p)
		})
	}
}

func TestChatTools_RequireArgs(t *testing.T) {
	_, _, _, err := chatToolByName(t, "chat_create_room").Build(map[string]any{"name": "r"})
	require.Error(t, err) // created_by required
	_, _, _, err = chatToolByName(t, "chat_get_room").Build(map[string]any{})
	require.Error(t, err) // room required
	_, _, _, err = chatToolByName(t, "chat_add_participant").Build(map[string]any{"room": "r1"})
	require.Error(t, err) // name required
	_, _, _, err = chatToolByName(t, "chat_remove_participant").Build(map[string]any{"room": "r1"})
	require.Error(t, err) // participant required
	_, _, _, err = chatToolByName(t, "chat_send_message").Build(map[string]any{"room": "r1", "participant_id": "p1"})
	require.Error(t, err) // body required
	_, _, _, err = chatToolByName(t, "chat_poll_messages").Build(map[string]any{"room": "r1"})
	require.Error(t, err) // participant required
}

func TestChatTools_SendMessageOptionalFields(t *testing.T) {
	_, _, body, err := chatToolByName(t, "chat_send_message").Build(map[string]any{
		"room": "r1", "participant_id": "p1", "body": "hi", "target": "p2", "kind": "system",
	})
	require.NoError(t, err)
	m, ok := body.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "p2", m["target"])
	require.Equal(t, "system", m["kind"])

	// Omitted optional fields are not sent.
	_, _, body2, err := chatToolByName(t, "chat_send_message").Build(map[string]any{
		"room": "r1", "participant_id": "p1", "body": "hi",
	})
	require.NoError(t, err)
	m2 := body2.(map[string]any)
	_, hasTarget := m2["target"]
	require.False(t, hasTarget)
	_, hasKind := m2["kind"]
	require.False(t, hasKind)
}

func TestChatTools_Invoke(t *testing.T) {
	var gotMethod, gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"messages":[],"count":0,"room_status":"active","has_more":false}`))
	}))
	defer srv.Close()
	c := freshClient(t, srv.URL)
	body, err := Invoke(context.Background(), c, chatToolByName(t, "chat_poll_messages"),
		map[string]any{"room": "r1", "participant": "p1"})
	require.NoError(t, err)
	require.Equal(t, http.MethodGet, gotMethod)
	require.Equal(t, "/rooms/r1/messages", gotPath)
	require.Equal(t, "participant=p1", gotQuery)
	require.Contains(t, string(body), "room_status")
}
