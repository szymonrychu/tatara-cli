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
	seen := map[string]bool{}
	for _, tl := range ChatTools() {
		var v any
		require.NoErrorf(t, json.Unmarshal(tl.Schema, &v), "chat tool %q has invalid JSON schema", tl.Name)
		require.Falsef(t, seen[tl.Name], "duplicate chat tool name %q", tl.Name)
		seen[tl.Name] = true
	}
}

func TestChatTools_BuildPaths(t *testing.T) {
	cases := []struct {
		tool   string
		args   map[string]any
		method string
		path   string
	}{
		{"chat_create_room", map[string]any{"name": "stream"}, http.MethodPost, "/rooms"},
		{"chat_list_rooms", map[string]any{}, http.MethodGet, "/rooms"},
		{"chat_list_rooms", map[string]any{"status": "active"}, http.MethodGet, "/rooms?status=active"},
		{"chat_get_room", map[string]any{"room_id": "r1"}, http.MethodGet, "/rooms/r1"},
		{"chat_close_room", map[string]any{"room_id": "r1"}, http.MethodDelete, "/rooms/r1"},
		{"chat_add_participant", map[string]any{"room_id": "r1", "name": "impl"}, http.MethodPost, "/rooms/r1/participants"},
		{"chat_list_participants", map[string]any{"room_id": "r1"}, http.MethodGet, "/rooms/r1/participants"},
		{"chat_remove_participant", map[string]any{"room_id": "r1", "participant_id": "p1"}, http.MethodDelete, "/rooms/r1/participants/p1"},
		{"chat_send_message", map[string]any{"room_id": "r1", "participant_id": "p1", "body": "hi"}, http.MethodPost, "/rooms/r1/messages"},
		{"chat_poll_messages", map[string]any{"room_id": "r1", "participant_id": "p1"}, http.MethodGet, "/rooms/r1/messages?participant=p1"},
		{"chat_get_log", map[string]any{"room_id": "r1"}, http.MethodGet, "/rooms/r1/log"},
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
	_, _, _, err := chatToolByName(t, "chat_create_room").Build(map[string]any{})
	require.Error(t, err) // name required
	_, _, _, err = chatToolByName(t, "chat_get_room").Build(map[string]any{})
	require.Error(t, err) // room_id required
	_, _, _, err = chatToolByName(t, "chat_add_participant").Build(map[string]any{"room_id": "r1"})
	require.Error(t, err) // name required
	_, _, _, err = chatToolByName(t, "chat_remove_participant").Build(map[string]any{"room_id": "r1"})
	require.Error(t, err) // participant_id required
	_, _, _, err = chatToolByName(t, "chat_send_message").Build(map[string]any{"room_id": "r1", "participant_id": "p1"})
	require.Error(t, err) // body required
	_, _, _, err = chatToolByName(t, "chat_poll_messages").Build(map[string]any{"room_id": "r1"})
	require.Error(t, err) // participant_id required
}

func TestChatTools_SendMessageBody(t *testing.T) {
	_, _, body, err := chatToolByName(t, "chat_send_message").Build(map[string]any{
		"room_id": "r1", "participant_id": "p1", "body": "hello", "kind": "system", "target": "p2",
	})
	require.NoError(t, err)
	m, ok := body.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "p1", m["participant_id"])
	require.Equal(t, "hello", m["body"])
	require.Equal(t, "system", m["kind"])
	require.Equal(t, "p2", m["target"])
}

func TestChatTools_AddParticipantDefaultsRoleOmitted(t *testing.T) {
	_, _, body, err := chatToolByName(t, "chat_add_participant").Build(map[string]any{"room_id": "r1", "name": "impl"})
	require.NoError(t, err)
	m, ok := body.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "impl", m["name"])
	_, hasRole := m["role"]
	require.False(t, hasRole) // role omitted so the server applies its default
}

func TestChatTools_Invoke(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"room-123","name":"stream"}`))
	}))
	defer srv.Close()
	c := freshClient(t, srv.URL)
	body, err := Invoke(context.Background(), c, chatToolByName(t, "chat_create_room"),
		map[string]any{"name": "stream", "created_by": "orchestrator"})
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "/rooms", gotPath)
	require.Equal(t, "stream", gotBody["name"])
	require.Equal(t, "orchestrator", gotBody["created_by"])
	require.Contains(t, string(body), "room-123")
}
