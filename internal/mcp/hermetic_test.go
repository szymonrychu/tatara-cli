package mcp

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// injectedByTheAgentRuntime is every environment variable the tatara agent
// runtime puts into an agent pod: tatara-operator's internal/agent/pod.go, plus
// TATARA_TURN_ID, which tatara-claude-code-wrapper names but hands only to its
// lifecycle-hook subprocess. The wrapper hands os.Environ() to the agent process
// wholesale, so `go test ./...` inside a pod runs with these set while CI runs
// clean.
//
// It is data, not a contract: nothing imports it and nothing in the operator
// has to know it exists. Adding a name that the operator dropped costs a stale
// line; missing one the operator added costs a package that goes non-hermetic
// again, which is why the list is deliberately the whole surface rather than
// the subset this package happens to read today.
//
// Names reached through a Go constant in pod.go (EnvAgentPodTTLSeconds,
// EnvTurnTimeoutSeconds) and the secretEnv/SecretKeyRef block are included by
// hand: grepping pod.go for `Name: "..."` misses both, which is how the first
// draft of this list lost six of them.
var injectedByTheAgentRuntime = []string{
	"AGENT_POD_TTL_SECONDS",
	"CALLBACK_HMAC_SECRET",
	"CHECKOUT_BRANCH",
	"CLAUDE_CODE_OAUTH_TOKEN",
	"CLI_OIDC_CLIENT_ID",
	"CLI_OIDC_CLIENT_SECRET",
	"DEFAULT_CALLBACK_URL",
	"EFFORT",
	"GIT_TOKEN",
	"GIT_USER_EMAIL",
	"GIT_USER_NAME",
	"MISE_ALWAYS_KEEP_DOWNLOAD",
	"MODEL",
	"OIDC_AUDIENCE",
	"OIDC_ISSUER",
	"OPERATOR_PUSH_URL",
	"PERMISSION_MODE",
	"POD_NAME",
	"REPO_BRANCH",
	"REPO_URL",
	"RUN_ID",
	"TASK_BRANCH",
	"TATARA_CONTRACT_VERSION",
	"TATARA_EXTRA_MCP_SERVERS",
	"TATARA_EXTRA_SKILL_SOURCES",
	"TATARA_GRAFANA_MCP_URL",
	"TATARA_KIND",
	"TATARA_MEMORY_DEGRADED",
	"TATARA_MEMORY_DISABLED",
	"TATARA_MEMORY_URL",
	"TATARA_OPERATOR_URL",
	"TATARA_PROJECT",
	"TATARA_REPO",
	"TATARA_REPOS",
	"TATARA_SERENA_URL",
	"TATARA_SKILLS_REF",
	"TATARA_SKILLS_REPO",
	"TATARA_SKILL_PROFILE",
	"TATARA_SOURCE_BASE_SHA",
	"TATARA_SOURCE_HEAD_SHA",
	"TATARA_SOURCE_REPO",
	"TATARA_TASK",
	"TATARA_TOOL_PROFILE",
	"TATARA_TURN_ID",
	"TURN_TIMEOUT_SECONDS",
	"TATARA_WORKSPACE_FULL_CLONE",
}

// productionFiles lists the non-test .go files in dir. Test files are excluded
// on purpose: a _test.go naming an injected variable is a test driving the gate
// deliberately (degraded_test.go does exactly that, ten times), not a read that
// makes the suite non-hermetic.
func productionFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err, "reading %s", dir)

	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		out = append(out, filepath.Join(dir, name))
	}
	return out
}

// modulePath is the import prefix that marks a dependency as module-local, and
// moduleRoot is where this package's directory sits relative to the module root,
// so an import path maps back to a directory on disk.
const (
	modulePath = "github.com/szymonrychu/tatara-cli/"
	moduleRoot = "../.."
)

// scannedDirs walks the module-local import closure of this package and returns
// every directory in it, starting at ".".
//
// A hand-written list was the first shape of this: {".", "../client"}, because
// internal/client.correlationID() reads RUN_ID on every client construction and
// that read is invisible from internal/mcp alone. But internal/client itself
// imports internal/auth, which reads OIDC_ISSUER / CLI_OIDC_CLIENT_ID /
// CLI_OIDC_CLIENT_SECRET - so a list that stopped at hop 2 of a 3-package
// closure left the guard's own invariant false on the commit that added it.
// Stopping at any fixed hop is arbitrary; the closure is not.
func scannedDirs(t *testing.T) []string {
	t.Helper()
	seen := map[string]bool{".": true}
	var out []string
	queue := []string{"."}
	for len(queue) > 0 {
		dir := queue[0]
		queue = queue[1:]
		out = append(out, dir)

		for _, path := range productionFiles(t, dir) {
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			require.NoError(t, err, "parsing imports of %s", path)

			for _, imp := range f.Imports {
				imported, err := strconv.Unquote(imp.Path.Value)
				if err != nil || !strings.HasPrefix(imported, modulePath) {
					continue
				}
				next := filepath.Join(moduleRoot, strings.TrimPrefix(imported, modulePath))
				if !seen[next] {
					seen[next] = true
					queue = append(queue, next)
				}
			}
		}
	}
	return out
}

// TestPackageEnvReadsAreNeutralised is the guard the previous two rounds of
// this bug did not have. Making the suite hermetic once fixes today; it does
// nothing about the variable someone wires up in six months, which is precisely
// what happened between internal/cmd/main_test.go landing (2026-06-13) and
// TATARA_MEMORY_DEGRADED arriving in this package (2026-07-26).
//
// So it asserts the invariant rather than the symptom: every agent-runtime
// variable named by the PRODUCTION code this package's tests can reach - its own
// and every module-local package it imports, transitively - must be in TestMain's
// unset list. It scans the AST for string literals equal to an injected name, which
// catches argOrEnv(a, "task", "TATARA_TASK") and any other indirection, not
// just a literal os.Getenv call. Equality is exact, so prose that merely
// mentions a variable - an error message, a doc comment's identifier, a log
// line - does not trip it.
func TestPackageEnvReadsAreNeutralised(t *testing.T) {
	injected := make(map[string]bool, len(injectedByTheAgentRuntime))
	for _, k := range injectedByTheAgentRuntime {
		injected[k] = true
	}
	neutralised := make(map[string]bool, len(injectedEnvNeutralised))
	for _, k := range injectedEnvNeutralised {
		neutralised[k] = true
	}

	dirs := scannedDirs(t)
	require.Subset(t, dirs, []string{".", moduleRoot + "/internal/client", moduleRoot + "/internal/auth"},
		"the closure walk must resolve module-local imports; these three are its floor, not its ceiling")

	fset := token.NewFileSet()
	var scanned int
	for _, dir := range dirs {
		for _, path := range productionFiles(t, dir) {
			f, err := parser.ParseFile(fset, path, nil, 0)
			require.NoError(t, err, "parsing %s", path)
			scanned++

			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				v, err := strconv.Unquote(lit.Value)
				if err != nil || !injected[v] {
					return true
				}
				require.True(t, neutralised[v],
					"%s names the agent-runtime-injected %s, but injectedEnvNeutralised in main_test.go "+
						"does not clear it: this package's tests are not hermetic in an agent pod. "+
						"Add it there, or confirm the literal is not an env read.",
					fset.Position(lit.Pos()), v)
				return true
			})
		}
	}
	require.NotZero(t, scanned, "the scan found no production files; it would pass vacuously")
}
