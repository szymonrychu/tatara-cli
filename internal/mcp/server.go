package mcp

import (
	"context"
	"encoding/json"
	"log/slog"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/szymonrychu/tatara-cli/internal/client"
	"github.com/szymonrychu/tatara-cli/internal/version"
)

// Server wraps an mcp-go MCPServer and dispatches tool calls via the HTTP clients.
type Server struct {
	srv      *server.MCPServer
	memory   *client.Client
	operator *client.Client
	log      *slog.Logger
}

// NewServer registers the tatara-memory tools (against memory) and the
// tatara-operator tools (against operator).
func NewServer(memory, operator *client.Client, log *slog.Logger) *Server {
	s := &Server{
		srv:      server.NewMCPServer("tatara", version.Version, server.WithToolCapabilities(true)),
		memory:   memory,
		operator: operator,
		log:      log,
	}
	for _, t := range AllTools() {
		s.register(t)
	}
	for _, t := range OperatorTools() {
		s.register(t)
	}
	return s
}

// ToolCount returns the number of registered tools (test/observability helper).
func (s *Server) ToolCount() int { return len(AllTools()) + len(OperatorTools()) }

func (s *Server) clientFor(t Tool) *client.Client {
	if t.Target == TargetOperator {
		return s.operator
	}
	return s.memory
}

func (s *Server) register(t Tool) {
	tool := mcplib.NewTool(
		t.Name,
		mcplib.WithDescription(t.Description),
		mcplib.WithRawInputSchema(t.Schema),
	)
	s.srv.AddTool(tool, func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		args := req.GetArguments()
		body, err := Invoke(ctx, s.clientFor(t), t, args)
		if err != nil {
			s.log.Error("tool error", "tool", t.Name, "err", err)
			return mcplib.NewToolResultError(err.Error()), nil
		}
		var out any
		if json.Unmarshal(body, &out) == nil {
			pretty, _ := json.MarshalIndent(out, "", "  ")
			return mcplib.NewToolResultText(string(pretty)), nil
		}
		return mcplib.NewToolResultText(string(body)), nil
	})
}

// Run starts the stdio MCP server. It blocks until stdin closes.
func (s *Server) Run(_ context.Context) error {
	return server.ServeStdio(s.srv)
}
