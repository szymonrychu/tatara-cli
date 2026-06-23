package obs_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/szymonrychu/tatara-cli/internal/obs"
)

// isRegistered checks that a collector is already registered by attempting
// to re-register it and expecting AlreadyRegisteredError.
func isRegistered(c prometheus.Collector) bool {
	err := prometheus.DefaultRegisterer.Register(c)
	if err == nil {
		// Was not registered; clean up and return false.
		_ = prometheus.DefaultRegisterer.Unregister(c)
		return false
	}
	_, already := err.(prometheus.AlreadyRegisteredError)
	return already
}

func TestToolCallsTotal_Registered(t *testing.T) {
	// Verify the metric is in the default registry by checking Describe output.
	ch := make(chan *prometheus.Desc, 10)
	obs.ToolCallsTotal.Describe(ch)
	close(ch)
	count := 0
	for range ch {
		count++
	}
	require.Greater(t, count, 0, "ToolCallsTotal must describe at least one metric")

	// Verify it is registered (re-registration yields AlreadyRegisteredError).
	assert.True(t, isRegistered(obs.ToolCallsTotal), "tatara_mcp_tool_calls_total must be registered")
}

func TestToolCallDurationMs_Registered(t *testing.T) {
	ch := make(chan *prometheus.Desc, 10)
	obs.ToolCallDurationMs.Describe(ch)
	close(ch)
	count := 0
	for range ch {
		count++
	}
	require.Greater(t, count, 0, "ToolCallDurationMs must describe at least one metric")

	assert.True(t, isRegistered(obs.ToolCallDurationMs), "tatara_mcp_tool_call_duration_ms must be registered")
}

func TestTokenRefreshTotal_Registered(t *testing.T) {
	ch := make(chan *prometheus.Desc, 10)
	obs.TokenRefreshTotal.Describe(ch)
	close(ch)
	count := 0
	for range ch {
		count++
	}
	require.Greater(t, count, 0, "TokenRefreshTotal must describe at least one metric")

	assert.True(t, isRegistered(obs.TokenRefreshTotal), "tatara_token_refresh_total must be registered")
}

// TestToolCallsTotal_IncrementAndObserve verifies the label cardinality works
// (no panic on With/Inc/Observe).
func TestToolCallsTotal_IncrementAndObserve(t *testing.T) {
	require.NotPanics(t, func() {
		obs.ToolCallsTotal.WithLabelValues("test_tool", "ok").Inc()
		obs.ToolCallsTotal.WithLabelValues("test_tool", "error").Inc()
		obs.ToolCallDurationMs.WithLabelValues("test_tool").Observe(42)
		obs.TokenRefreshTotal.WithLabelValues("ok").Inc()
		obs.TokenRefreshTotal.WithLabelValues("error").Inc()
	})
}

func TestInternalIssueTotal_Registered(t *testing.T) {
	ch := make(chan *prometheus.Desc, 10)
	obs.InternalIssueTotal.Describe(ch)
	close(ch)
	count := 0
	for range ch {
		count++
	}
	require.Greater(t, count, 0, "InternalIssueTotal must describe at least one metric")
	assert.True(t, isRegistered(obs.InternalIssueTotal), "tatara_mcp_internal_issue_total must be registered")
}

func TestRegisteredTools_Registered(t *testing.T) {
	ch := make(chan *prometheus.Desc, 10)
	obs.RegisteredTools.Describe(ch)
	close(ch)
	count := 0
	for range ch {
		count++
	}
	require.Greater(t, count, 0, "RegisteredTools must describe at least one metric")
	assert.True(t, isRegistered(obs.RegisteredTools), "tatara_mcp_registered_tools must be registered")
}

func TestInternalIssueTotal_LabelCardinality(t *testing.T) {
	require.NotPanics(t, func() {
		obs.InternalIssueTotal.WithLabelValues("tool_error", "error").Inc()
		obs.InternalIssueTotal.WithLabelValues("auth", "warn").Inc()
		obs.RegisteredTools.WithLabelValues("brainstorm").Set(42)
		obs.RegisteredTools.WithLabelValues("all").Set(63)
	})
}
