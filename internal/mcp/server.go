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
	srv        *server.MCPServer
	memory     *client.Client
	operator   *client.Client
	log        *slog.Logger
	toolCount  int
	registered []string
	profile    string
	// allow is the resolved profile allow-set (never nil - resolveProfile
	// always fails closed to at least the alwaysOn set). It gates BOTH
	// registration (tools outside it are never listed) and, as belt and
	// braces, execution in the register() dispatch closure.
	allow map[string]bool
}

// NewServer builds the stdio MCP server for one agent pod.
//
// Tools OUTSIDE the profile's allow-set are NOT REGISTERED. tools/list is
// therefore per-profile, which deliberately gives up the byte-identical-list
// prompt-cache prefix every pod used to share (design accepted risk 3):
// submit_outcome's schema is shaped from the profile, so the lists could not be
// identical anyway, and a 17-tool list where all 17 work beats a 74-tool list
// where 68 return an authz error. Correct tool selection beats a cache hit. Do
// not "restore" the profile-invariant list: it comes back with the 68 tools the
// agent may not call.
//
// submit_outcome is ONE tool name whose schema IS the profile (contract D.1), so
// an agent cannot pick the wrong outcome tool - it only ever has one. An empty or
// unknown profile fails closed to the always-on six and gets no submit_outcome at
// all, so a pod whose profile we do not understand cannot terminate a Task. The
// call-time allow check in register() stays as belt and braces: every agent pod
// shares one OIDC identity, so this allow-set is the authz boundary.
//
// profile is read from TATARA_TOOL_PROFILE env (or --tool-profile flag) and
// passed in by the caller.
func NewServer(memory, operator *client.Client, log *slog.Logger, profile string) *Server {
	allow := resolveProfile(profile, log)
	s := &Server{
		srv:      server.NewMCPServer("tatara", version.Version, server.WithToolCapabilities(true)),
		memory:   memory,
		operator: operator,
		log:      log,
		profile:  profile,
		allow:    allow,
	}

	candidates := CodeTools()
	candidates = append(candidates, MemoryTools()...)
	candidates = append(candidates, SCMTools()...)
	candidates = append(candidates, PlatformTools()...)
	if outcome, ok := OutcomeTool(profile); ok {
		candidates = append(candidates, outcome)
	}

	for _, t := range candidates {
		if !allow[t.Name] {
			continue
		}
		s.register(t)
		s.toolCount++
		s.registered = append(s.registered, t.Name)
	}

	profileLabel := profile
	if profileLabel == "" {
		profileLabel = "none"
	}
	obs.RegisteredTools.WithLabelValues(profileLabel).Set(float64(s.toolCount))
	log.Info("mcp tool surface resolved",
		"action", "tools_registered", "profile", profile, "count", s.toolCount)
	return s
}

// ToolCount returns the number of registered (listed) tools: exactly the size of
// the profile's allow-set.
func (s *Server) ToolCount() int { return s.toolCount }

// RegisteredNames returns the names of the registered tools, in registration
// order. Callers that need a stable order must sort.
func (s *Server) RegisteredNames() []string {
	out := make([]string, len(s.registered))
	copy(out, s.registered)
	return out
}

func (s *Server) clientFor(t Tool) *client.Client {
	switch t.Target {
	case TargetOperator:
		return s.operator
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
	for _, key := range []string{"id", "task", "repo"} {
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

		// Call-time authz, belt and braces (G15). Tools outside the allow-set
		// are not registered at all, so this cannot normally fire; it stays
		// because all agent pods share ONE OIDC identity, which makes the
		// allow-set the authz boundary, and a boundary gets two checks.
		if !s.allow[t.Name] {
			obs.ToolCallsTotal.WithLabelValues(t.Name, "denied").Inc()
			s.log.Warn("tool call denied: not in profile allow-set", "tool", t.Name, "profile", s.profile, "resource_id", rid)
			return mcplib.NewToolResultError(fmt.Sprintf("tool %q is not permitted for profile %q", t.Name, s.profile)), nil
		}

		// Contract J, RESIDUE 1: refine holds mr_write for action=comment only.
		// The schema is shared across profiles, so the gate is code, and it runs
		// before Invoke - the operator enforces the same rule at POST
		// /scm/mr-write.
		if t.Name == "mr_write" {
			if err := checkRefineMRWrite(s.profile, args); err != nil {
				obs.ToolCallsTotal.WithLabelValues(t.Name, "denied").Inc()
				s.log.Warn("tool call denied: refine mr_write is comment-only", "tool", t.Name, "profile", s.profile, "resource_id", rid)
				return mcplib.NewToolResultError(err.Error()), nil
			}
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
