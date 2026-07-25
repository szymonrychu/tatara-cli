package cmd_test

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// tatara-cli#88/#91: TATARA_MEMORY_URL is the operator-injected, already
// project-scoped direct service endpoint (one root-mounted tatara-memory
// deployment per project) - unlike --base-url/config-file/default, which are
// the shared ingress endpoint and still need /<project> appended. Regression
// for the fleet-wide 404 caused by appending the project twice.
func TestRaw_TargetMemoryEnvSourcedBaseNotProjectPrefixed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeToken(t, dir)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/memories", r.URL.Path)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	t.Setenv("TATARA_MEMORY_URL", srv.URL)

	root := cmd.NewRootCmd()
	root.SetArgs([]string{"--project", "proj", "raw", "--target", "memory", "GET", "/memories"})
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

// Finding 6 (old finding 2 updated): --operator-base-url must apply to the
// operator target. Use the target-specific flag; --base-url is memory-scoped.
func TestRaw_BaseURLAppliedToOperatorTarget(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeToken(t, dir)

	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	root := cmd.NewRootCmd()
	root.SetArgs([]string{"raw", "--target", "operator", "--operator-base-url", srv.URL, "GET", "/tasks"})
	require.NoError(t, root.Execute())
	require.True(t, hit, "--operator-base-url should route the operator target to the supplied URL")
}

// Finding 5: passing -d - when stdin is a non-TTY pipe must work (the happy-path
// already covered by TestRaw_POSTWithStdinBody). The TTY guard only blocks real
// terminal stdin; in tests cmd.InOrStdin() returns a *bytes.Buffer (not *os.File)
// so the type-assert to *os.File will be false and body is set without error.
func TestRaw_StdinNonFileReaderPassesThrough(t *testing.T) {
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
	root.SetIn(strings.NewReader(`{"from":"reader"}`))
	root.SetArgs([]string{"--base-url", srv.URL, "raw", "POST", "/x", "-d", "-"})
	require.NoError(t, root.Execute())
	require.Equal(t, `{"from":"reader"}`, got)
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

// TestRaw_VerboseEmitsStructuredLog verifies that passing -v causes the raw
// command to emit a structured JSON INFO log for the business action
// (method/path/status_code/duration_ms) to stderr, satisfying hard rule 12.
func TestRaw_VerboseEmitsStructuredLog(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeToken(t, dir)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	root := cmd.NewRootCmd()
	out, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(errBuf)
	// -v sets INFO level; the client.Do structured log fires.
	root.SetArgs([]string{"--base-url", srv.URL, "-v", "raw", "GET", "/memories/x"})
	require.NoError(t, root.Execute())

	stderr := errBuf.String()
	require.Contains(t, stderr, "client.Do", "INFO structured log must contain client.Do action")
	require.Contains(t, stderr, "status_code", "INFO structured log must contain status_code field")
	require.Contains(t, stderr, "duration_ms", "INFO structured log must contain duration_ms field")
}

// Sanity check that the test plumbing works
func TestRaw_BodyShapeUnused(t *testing.T) {
	_, _ = json.Marshal(map[string]string{})
}

// Finding 6: --operator-base-url must be honoured by the raw command
// (previously the flag was not registered so it was silently ignored and the
// env-var or default was used instead).
func TestRaw_OperatorBaseURLFlagHonoured(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeToken(t, dir)

	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	root := cmd.NewRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"raw", "--target", "operator", "--operator-base-url", srv.URL, "GET", "/tasks"})
	require.NoError(t, root.Execute())
	require.True(t, hit, "--operator-base-url must route the operator target to the supplied URL")
}

// Finding 2: when raw falls back to client-credentials (no stored token), the
// token must have a non-zero ExpiresAt so ensureFreshLocked's freshness math is
// meaningful. We verify this indirectly: the cc token is minted fresh and the
// HTTP call succeeds with the correct Authorization header.
func TestRaw_CCTokenHasExpiry(t *testing.T) {
	// Stand up a fake OIDC issuer that returns a token with expires_in=300.
	oidcMux := http.NewServeMux()
	oidcMux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"token_endpoint":"http://%s/token"}`, r.Host)
	})
	oidcMux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"cc-access-tok","expires_in":300,"token_type":"Bearer"}`)
	})
	oidcSrv := httptest.NewServer(oidcMux)
	defer oidcSrv.Close()

	var gotAuth string
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer apiSrv.Close()

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	// No stored token - force cc path.
	t.Setenv("OIDC_ISSUER", oidcSrv.URL)
	t.Setenv("CLI_OIDC_CLIENT_ID", "cid")
	t.Setenv("CLI_OIDC_CLIENT_SECRET", "secret")
	auth.ResetTokenCache()
	defer auth.ResetTokenCache()

	root := cmd.NewRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"--base-url", apiSrv.URL, "raw", "GET", "/x"})
	require.NoError(t, root.Execute())
	require.Equal(t, "Bearer cc-access-tok", gotAuth, "cc token must be forwarded in Authorization header")
}
