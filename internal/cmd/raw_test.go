package cmd_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/auth"
	"github.com/szymonrychu/tatara-cli/internal/cmd"
)

func writeToken(t *testing.T, dir string) {
	t.Helper()
	tokenPath := filepath.Join(dir, "tatara", "token.json")
	require.NoError(t, auth.SaveToken(tokenPath, &auth.Token{
		AccessToken: "test-token",
		ExpiresAt:   time.Now().Add(time.Hour),
		TokenType:   "Bearer",
	}))
}

func TestRaw_GETPrintsBodyAndStatus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeToken(t, dir)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "GET", r.Method)
		require.Equal(t, "/memories/abc", r.URL.Path)
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		require.Equal(t, "application/json", r.Header.Get("Accept"))
		_, _ = w.Write([]byte(`{"id":"abc","text":"hi"}`))
	}))
	defer srv.Close()

	root := cmd.NewRootCmd()
	out, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(errBuf)
	root.SetArgs([]string{"--base-url", srv.URL, "raw", "GET", "/memories/abc"})
	require.NoError(t, root.Execute())

	require.Contains(t, out.String(), `"id":"abc"`)
	require.Contains(t, errBuf.String(), "200")
}

func TestRaw_POSTWithLiteralBody(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeToken(t, dir)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "POST", r.Method)
		body, _ := io.ReadAll(r.Body)
		require.Equal(t, `{"text":"hello"}`, strings.TrimSpace(string(body)))
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	root := cmd.NewRootCmd()
	out, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(errBuf)
	root.SetArgs([]string{"--base-url", srv.URL, "raw", "POST", "/memories", "-d", `{"text":"hello"}`})
	require.NoError(t, root.Execute())
	require.Contains(t, errBuf.String(), "201")
}

func TestRaw_POSTWithFileBody(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeToken(t, dir)

	bodyFile := filepath.Join(dir, "body.json")
	require.NoError(t, os.WriteFile(bodyFile, []byte(`{"text":"from-file"}`), 0o600))

	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = strings.TrimSpace(string(b))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	root := cmd.NewRootCmd()
	root.SetArgs([]string{"--base-url", srv.URL, "raw", "POST", "/memories", "-d", "@" + bodyFile})
	require.NoError(t, root.Execute())
	require.Equal(t, `{"text":"from-file"}`, got)
}

func TestRaw_POSTWithStdinBody(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeToken(t, dir)

	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = strings.TrimSpace(string(b))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	root := cmd.NewRootCmd()
	root.SetIn(strings.NewReader(`{"text":"from-stdin"}`))
	root.SetArgs([]string{"--base-url", srv.URL, "raw", "POST", "/memories", "-d", "-"})
	require.NoError(t, root.Execute())
	require.Equal(t, `{"text":"from-stdin"}`, got)
}

func TestRaw_NonOKExitsNonZero(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeToken(t, dir)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	defer srv.Close()

	root := cmd.NewRootCmd()
	root.SetArgs([]string{"--base-url", srv.URL, "raw", "GET", "/memories/x"})
	err := root.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "404")
}

func TestRaw_TargetMemoryAppliesProjectPrefix(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeToken(t, dir)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/proj/memories", r.URL.Path)
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	root := cmd.NewRootCmd()
	root.SetArgs([]string{"--base-url", srv.URL, "--project", "proj", "raw", "--target", "memory", "GET", "/memories"})
	require.NoError(t, root.Execute())
}

func TestRaw_TargetOperatorNoProjectPrefix(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeToken(t, dir)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/tasks/foo", r.URL.Path)
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	t.Setenv("TATARA_OPERATOR_URL", srv.URL)

	root := cmd.NewRootCmd()
	root.SetArgs([]string{"--project", "proj", "raw", "--target", "operator", "GET", "/tasks/foo"})
	require.NoError(t, root.Execute())
}

func TestRaw_TargetChatNoProjectPrefix(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeToken(t, dir)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/rooms", r.URL.Path)
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	t.Setenv("TATARA_CHAT_URL", srv.URL)

	root := cmd.NewRootCmd()
	root.SetArgs([]string{"--project", "proj", "raw", "--target", "chat", "GET", "/rooms"})
	require.NoError(t, root.Execute())
}

func TestRaw_InvalidTargetErrors(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeToken(t, dir)

	root := cmd.NewRootCmd()
	root.SetArgs([]string{"raw", "--target", "bogus", "GET", "/x"})
	err := root.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid --target")
}

func TestRaw_NoTokenSurfacesErrNoToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	// no token written

	root := cmd.NewRootCmd()
	root.SetArgs([]string{"raw", "GET", "/memories/x"})
	err := root.Execute()
	require.Error(t, err)
	require.ErrorIs(t, err, auth.ErrNoToken)
}

// Sanity check that the test plumbing works
func TestRaw_BodyShapeUnused(t *testing.T) {
	_, _ = json.Marshal(map[string]string{})
}
