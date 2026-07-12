package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/szymonrychu/tatara-cli/internal/client"
	"github.com/szymonrychu/tatara-cli/internal/obs"
	"github.com/szymonrychu/tatara-cli/internal/version"
)

// Server wraps an mcp-go MCPServer and dispatches tool calls via the HTTP clients.
type Server struct {
	srv       *server.MCPServer
	memory    *client.Client
	operator  *client.Client
	chat      *client.Client
	log       *slog.Logger
	toolCount int
	profile   string
	// allow is the resolved profile allow-set (never nil - resolveProfile
	// always fails closed to at least the alwaysOn set). Component 4a:
	// tools/list is now profile-invariant (every tool is always registered),
	// so allow is enforced at call time instead, in the register() dispatch
	// closure.
	allow map[string]bool
}

// NewServer registers every tool, always, regardless of profile: Component 4a
// requires tools/list to be byte-identical across every kind so all agent
// pods share one prompt-cache prefix (tools render first in Anthropic cache
// order; a per-kind filtered list fragments the cache). Per-profile
// restriction is enforced at call time instead (see register()), via the
// resolved allow-set. Both an unrecognized non-empty profile AND an empty
// profile fail closed to the alwaysOn set only at call time (the sole authz
// boundary, since all agents share one OIDC identity) - a missing profile is
// never a fail-open full tool grant. profile is read from TATARA_TOOL_PROFILE
// env (or --tool-profile flag) and passed in by the caller.
func NewServer(memory, operator, chat *client.Client, log *slog.Logger, profile string) *Server {
	allow := resolveProfile(profile, log)
	s := &Server{
		srv:      server.NewMCPServer("tatara", version.Version, server.WithToolCapabilities(true)),
		memory:   memory,
		operator: operator,
		chat:     chat,
		log:      log,
		profile:  profile,
		allow:    allow,
	}
	allTools := append(append(append(append(AllTools(), OperatorTools()...), ChatTools()...), PlatformTools()...), HandoffTools()...)
	for _, t := range allTools {
		s.register(t)
		s.toolCount++
	}
	profileLabel := profile
	if profileLabel == "" {
		profileLabel = "all"
	}
	allowedCount := len(allow)
	obs.RegisteredTools.WithLabelValues(profileLabel).Set(float64(allowedCount))
	log.Info("mcp server started", "profile", profile, "registered_tools", s.toolCount, "allowed_tools", allowedCount)
	return s
}

// ToolCount returns the number of registered (listed) tools. This is now
// profile-invariant: every tool is always registered so tools/list is
// byte-identical across profiles (Component 4a).
func (s *Server) ToolCount() int { return s.toolCount }

func (s *Server) clientFor(t Tool) *client.Client {
	switch t.Target {
	case TargetOperator:
		return s.operator
	case TargetChat:
		return s.chat
	default:
		return s.memory
	}
}

// buildTool constructs the mcp-go Tool for a tatara Tool. NewToolWithRawSchema
// sets only RawInputSchema: NewTool seeds a default object InputSchema, and a
// Tool with both InputSchema and RawInputSchema set fails tools/list
// marshalling, leaving the agent with zero tatara tools.
func buildTool(t Tool) mcplib.Tool {
	return mcplib.NewToolWithRawSchema(t.Name, t.Description, t.Schema)
}

// resourceID extracts the resource being acted on from args. It checks the
// common identifier keys in priority order, then falls back to TATARA_TASK env.
func resourceID(args map[string]any) string {
	for _, key := range []string{"id", "task", "repo", "room_id", "subtask"} {
		if v, ok := args[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return os.Getenv("TATARA_TASK")
}

func (s *Server) register(t Tool) {
	tool := buildTool(t)
	s.srv.AddTool(tool, func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		start := time.Now()
		args := req.GetArguments()
		rid := resourceID(args)

		// Call-time authz (Component 4a, G15): tools/list is now
		// profile-invariant, so the per-profile allow-set gates execution
		// here instead of registration. allow is never nil (resolveProfile
		// always fails closed); a tool not in the allow-set is denied
		// without ever reaching the handler/backend.
		if !s.allow[t.Name] {
			obs.ToolCallsTotal.WithLabelValues(t.Name, "denied").Inc()
			s.log.Warn("tool call denied: not in profile allow-set", "tool", t.Name, "profile", s.profile, "resource_id", rid)
			return mcplib.NewToolResultError(fmt.Sprintf("tool %q is not permitted for profile %q", t.Name, s.profile)), nil
		}

		// Local-handler path: Handler set => skip HTTP round-trip.
		if t.Handler != nil {
			result, err := t.Handler(args, s.log)
			elapsedMs := float64(time.Since(start).Milliseconds())
			obs.ToolCallDurationMs.WithLabelValues(t.Name).Observe(elapsedMs)
			if err != nil {
				obs.ToolCallsTotal.WithLabelValues(t.Name, "error").Inc()
				s.log.Error("tool error", "tool", t.Name, "duration_ms", elapsedMs, "resource_id", rid, "err", err)
				return mcplib.NewToolResultError(err.Error()), nil
			}
			obs.ToolCallsTotal.WithLabelValues(t.Name, "ok").Inc()
			s.log.Info("tool call", "tool", t.Name, "duration_ms", elapsedMs, "resource_id", rid, "status", "ok")
			return mcplib.NewToolResultText(result), nil
		}

		body, err := Invoke(ctx, s.clientFor(t), t, args)
		elapsedMs := float64(time.Since(start).Milliseconds())
		obs.ToolCallDurationMs.WithLabelValues(t.Name).Observe(elapsedMs)
		if err != nil {
			obs.ToolCallsTotal.WithLabelValues(t.Name, "error").Inc()
			s.log.Error("tool error", "tool", t.Name, "target", t.Target, "duration_ms", elapsedMs, "resource_id", rid, "err", err)
			return mcplib.NewToolResultError(err.Error()), nil
		}
		obs.ToolCallsTotal.WithLabelValues(t.Name, "ok").Inc()
		s.log.Info("tool call", "tool", t.Name, "target", t.Target, "duration_ms", elapsedMs, "resource_id", rid, "status", "ok")
		if len(body) == 0 {
			return mcplib.NewToolResultText(`{"ok":true}`), nil
		}
		var out any
		if json.Unmarshal(body, &out) == nil {
			pretty, _ := json.MarshalIndent(out, "", "  ")
			return mcplib.NewToolResultText(string(pretty)), nil
		}
		return mcplib.NewToolResultText(string(body)), nil
	})
}

// Run starts the stdio MCP server. It blocks until stdin closes or ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	stdio := server.NewStdioServer(s.srv)
	return stdio.Listen(ctx, os.Stdin, os.Stdout)
}
