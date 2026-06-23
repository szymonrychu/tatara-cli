package mcp

import (
	"context"
	"encoding/json"
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
}

// NewServer registers tools filtered by profile. Empty/unknown profile serves the
// full set (fail-open). profile is read from TATARA_TOOL_PROFILE env (or --tool-profile
// flag) and passed in by the caller.
func NewServer(memory, operator, chat *client.Client, log *slog.Logger, profile string) *Server {
	allow := resolveProfile(profile, log)
	s := &Server{
		srv:      server.NewMCPServer("tatara", version.Version, server.WithToolCapabilities(true)),
		memory:   memory,
		operator: operator,
		chat:     chat,
		log:      log,
		profile:  profile,
	}
	allTools := append(append(append(AllTools(), OperatorTools()...), ChatTools()...), PlatformTools()...)
	for _, t := range allTools {
		if allow == nil || allow[t.Name] {
			s.register(t)
			s.toolCount++
		}
	}
	profileLabel := profile
	if profileLabel == "" {
		profileLabel = "all"
	}
	obs.RegisteredTools.WithLabelValues(profileLabel).Set(float64(s.toolCount))
	log.Info("mcp server started", "profile", profile, "registered_tools", s.toolCount)
	return s
}

// ToolCount returns the number of registered tools (profile-aware).
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
