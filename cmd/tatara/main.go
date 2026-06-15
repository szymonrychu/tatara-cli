package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/szymonrychu/tatara-cli/internal/cmd"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

// run wires a signal-cancelled context into the root command so long-running
// subcommands (the stdio MCP server) shut down gracefully on SIGINT/SIGTERM.
// The mcp-go ServeStdio helper used to install these handlers itself; since we
// now pass our own context to StdioServer.Listen, we must wire them here. Kept
// separate from main so the deferred stop() actually runs (main calls os.Exit).
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return cmd.NewRootCmd().ExecuteContext(ctx)
}
