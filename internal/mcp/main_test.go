package mcp

import (
	"os"
	"testing"
)

// injectedEnvNeutralised is the set of agent-runtime-injected variables this
// package's production code reads. TestMain clears every one of them so the
// suite is hermetic: `go test ./...` inside an agent pod must produce the same
// result as CI, which runs on a clean environment.
//
// The list is derived from what THIS package reads, not copied from
// internal/cmd/main_test.go - that one also clears XDG_CONFIG_HOME /
// XDG_STATE_HOME, which move auth.DefaultTokenPath() and have no reader here.
//
// TATARA_TURN_ID and RUN_ID are read one package over, in
// internal/client.correlationID(), which every freshClient() in this suite
// constructs. They are covered by the guard only because scannedDirs in
// hermetic_test.go carries "../client" for them; drop that entry and these two
// stop being enforced.
var injectedEnvNeutralised = []string{
	"TATARA_MEMORY_DEGRADED",
	"TATARA_MEMORY_DISABLED",
	"TATARA_PROJECT",
	"TATARA_TASK",
	"TATARA_TURN_ID",
	"RUN_ID",
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
