package mcp

import (
	"os"
	"testing"
)

// injectedEnvNeutralised is the set of agent-runtime-injected variables the
// production code this suite reaches can read. TestMain clears every one of them
// so the suite is hermetic: `go test ./...` inside an agent pod must produce the
// same result as CI, which runs on a clean environment.
//
// The list is derived from what this package's import closure reads, not copied
// from internal/cmd/main_test.go - that one also clears XDG_CONFIG_HOME /
// XDG_STATE_HOME, which move auth.DefaultTokenPath() and which the agent runtime
// does not inject anyway.
//
// The last five are read in a package this one imports rather than here, so the
// guard in hermetic_test.go only sees them because it walks the whole
// module-local import closure:
//
//	RUN_ID, TATARA_TURN_ID  internal/client.correlationID(), on every
//	                        freshClient() this suite constructs. RUN_ID is the
//	                        entry doing the work in a pod: the wrapper builds the
//	                        agent's environment from a bare os.Environ() and
//	                        passes TATARA_TURN_ID only as extraEnv to the
//	                        lifecycle-hook subprocess, so correlationID() always
//	                        falls through to RUN_ID here. TATARA_TURN_ID is kept
//	                        defensively - it costs one unset and covers the
//	                        wrapper deciding to export it.
//	OIDC_ISSUER,            internal/auth.ClientCredsConfigured(), reached from
//	CLI_OIDC_CLIENT_ID,     internal/client. All three ARE set in an agent pod;
//	CLI_OIDC_CLIENT_SECRET  left alone, a test that grows a real token path would
//	                        mint client_credentials tokens against the live
//	                        issuer in a pod and not in CI.
var injectedEnvNeutralised = []string{
	"TATARA_MEMORY_DEGRADED",
	"TATARA_MEMORY_DISABLED",
	"TATARA_PROJECT",
	"TATARA_TASK",
	"RUN_ID",
	"TATARA_TURN_ID",
	"OIDC_ISSUER",
	"CLI_OIDC_CLIENT_ID",
	"CLI_OIDC_CLIENT_SECRET",
}

// TestMain neutralises the ambient agent-pod environment for the whole package.
//
// It does NOT make the per-test t.Setenv calls in degraded_test.go redundant:
// t.Setenv beats this unset (it sets the value for the test and restores the
// pre-test value on cleanup), which is exactly what those ten call sites rely
// on to drive the degraded gate ON and OFF deliberately. Deleting them because
// "TestMain already handles the environment" would delete the only coverage the
// gate has.
func TestMain(m *testing.M) {
	for _, k := range injectedEnvNeutralised {
		_ = os.Unsetenv(k)
	}
	os.Exit(m.Run())
}
