// Package obs holds Prometheus metric primitives for tatara-cli.
// The MCP server is a long-running process; counters and histograms here
// give operators visibility into tool-call rates, error rates, and latency
// even though there is no /metrics HTTP endpoint (the CLI is stdio-only, so
// metrics are populated in-process and available for push or scrape if wired
// up by the caller).
package obs

import "github.com/prometheus/client_golang/prometheus"

var (
	// ToolCallsTotal counts completed tool dispatches partitioned by tool name
	// and result ("ok" or "error").
	ToolCallsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "tatara_mcp_tool_calls_total",
		Help: "Total MCP tool calls dispatched, partitioned by tool and result.",
	}, []string{"tool", "result"})

	// ToolCallDurationMs observes the end-to-end latency of each tool call
	// (including the HTTP round-trip to the backend) in milliseconds.
	ToolCallDurationMs = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "tatara_mcp_tool_call_duration_ms",
		Help:    "Latency of MCP tool calls in milliseconds.",
		Buckets: []float64{5, 25, 100, 250, 500, 1000, 2500, 5000},
	}, []string{"tool"})

	// TokenRefreshTotal counts token refresh attempts partitioned by result
	// ("ok" or "error").
	TokenRefreshTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "tatara_token_refresh_total",
		Help: "Total token refresh attempts, partitioned by result.",
	}, []string{"result"})

	// ClientCredsMintTotal counts client_credentials token mint attempts
	// partitioned by result ("ok" or "error"). This is the primary auth path
	// for agent pods; operators need visibility into its failure rate.
	ClientCredsMintTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "tatara_client_creds_mint_total",
		Help: "Total client_credentials token mint attempts, partitioned by result.",
	}, []string{"result"})
)

func init() {
	prometheus.MustRegister(ToolCallsTotal, ToolCallDurationMs, TokenRefreshTotal, ClientCredsMintTotal)
}
