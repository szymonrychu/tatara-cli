package cmd

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVerboseLevel(t *testing.T) {
	cases := []struct {
		count int
		want  slog.Level
	}{
		{count: 0, want: slog.LevelWarn},
		{count: 1, want: slog.LevelInfo},
		{count: 2, want: slog.LevelDebug},
		{count: 3, want: slog.LevelDebug},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, verboseLevel(tc.count))
	}
}
