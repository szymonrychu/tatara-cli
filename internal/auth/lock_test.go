package auth_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/auth"
)

func TestAcquireReleaseLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token.json")

	lock, err := auth.AcquireLock(path)
	require.NoError(t, err)
	require.NotNil(t, lock)

	require.NoError(t, lock.Release())

	// Re-acquire after release must succeed.
	lock2, err := auth.AcquireLock(path)
	require.NoError(t, err)
	require.NotNil(t, lock2)
	require.NoError(t, lock2.Release())
}
