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
// TATARA_TURN_ID from tatara-claude-code-wrapper's cmd/wrapper/app.go. The
// wrapper hands os.Environ() to the agent process wholesale, so `go test ./...`
// inside a pod runs with every one of these set while CI runs clean.
//
// It is data, not a contract: nothing imports it and nothing in the operator
// has to know it exists. Adding a name that the operator dropped costs a stale
// line; missing one the operator added costs a package that goes non-hermetic
// again, which is why the list is deliberately the whole surface rather than
// the subset this package happens to read today.
var injectedByTheAgentRuntime = []string{
	"CHECKOUT_BRANCH",
	"DEFAULT_CALLBACK_URL",
	"EFFORT",
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
	"TATARA_WORKSPACE_FULL_CLONE",
}

// TestPackageEnvReadsAreNeutralised is the guard the previous two rounds of
// this bug did not have. Making the suite hermetic once fixes today; it does
// nothing about the variable someone wires up in six months, which is precisely
// what happened between internal/cmd/main_test.go landing (2026-06-13) and
// TATARA_MEMORY_DEGRADED arriving in this package (2026-07-26).
//
// So it asserts the invariant rather than the symptom: every agent-runtime
// variable named by this package's PRODUCTION code must be in TestMain's unset
// list. It scans the AST for string literals equal to an injected name, which
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

	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	fset := token.NewFileSet()
	var scanned int
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		require.NoError(t, err, "parsing %s", name)
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
	require.NotZero(t, scanned, "the scan found no production files; it would pass vacuously")
}
