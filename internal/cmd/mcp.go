package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/szymonrychu/tatara-cli/internal/auth"
	"github.com/szymonrychu/tatara-cli/internal/client"
	"github.com/szymonrychu/tatara-cli/internal/mcp"
)

// resolveMCPToken loads credentials for the MCP server without failing when
// none are present. Static MCP capabilities (tools/list) need no auth; only
// actual backend calls do. With no stored token and no client-credentials it
// returns a nil token so the server still starts and advertises its tools;
// individual tool invocations then fail until credentials exist.
func resolveMCPToken(ctx context.Context, logger *slog.Logger) (*auth.Token, string) {
	tokenPath, err := auth.DefaultTokenPath()
	if err != nil {
		logger.Warn("mcp: cannot resolve token path; starting unauthenticated", "reason", err.Error())
		return nil, ""
	}
	if token, lerr := auth.LoadToken(tokenPath); lerr == nil {
		return token, tokenPath
	}
	tokStr, ccErr := auth.AccessToken(ctx)
	if ccErr != nil {
		logger.Warn("mcp: no credentials; starting unauthenticated, tool calls fail until `tatara login` or OIDC env is set", "reason", ccErr.Error())
		return nil, ""
	}
	return &auth.Token{AccessToken: tokStr, TokenType: "Bearer"}, ""
}

func newMCPCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "mcp",
		Short: "Run the tatara MCP server over stdio.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			verbose, _ := cmd.Flags().GetCount("verbose")
			logger, closer, err := mcpLogger(verboseLevel(verbose))
			if err != nil {
				return err
			}
			defer func() { _ = closer.Close() }()

			baseFlag, _ := cmd.Flags().GetString("base-url")
			configPath, err := client.DefaultConfigPath()
			if err != nil {
				return err
			}
			fileCfg, err := client.LoadConfig(configPath)
			if err != nil {
				return err
			}
			base := client.ResolveBaseURL(baseFlag, os.Getenv("TATARA_MEMORY_URL"), fileCfg)
			project, _ := cmd.Flags().GetString("project")
			base = client.MemoryURLForProject(base, project)

			token, tokenPath := resolveMCPToken(ctx, logger)

			cliCfg := client.Config{
				BaseURL:   base,
				Token:     token,
				TokenPath: tokenPath,
			}
			if tokenPath != "" {
				cliCfg.Reload = func() (*auth.Token, error) { return auth.LoadToken(tokenPath) }
				cliCfg.Refresh = func(ctx context.Context, t *auth.Token) (*auth.Token, error) {
					return auth.RefreshToken(ctx, DefaultIssuer, DefaultClientID, t, nil)
				}
				cliCfg.Save = func(t *auth.Token) error { return auth.SaveToken(tokenPath, t) }
			}
			cli, err := client.New(cliCfg)
			if err != nil {
				return err
			}

			opBaseFlag, _ := cmd.Flags().GetString("operator-base-url")
			opBase := client.ResolveOperatorBaseURL(opBaseFlag, os.Getenv("TATARA_OPERATOR_URL"), fileCfg)
			opCfg := client.Config{
				BaseURL:   opBase,
				Token:     token,
				TokenPath: tokenPath,
			}
			if tokenPath != "" {
				opCfg.Reload = func() (*auth.Token, error) { return auth.LoadToken(tokenPath) }
				opCfg.Refresh = func(ctx context.Context, t *auth.Token) (*auth.Token, error) {
					return auth.RefreshToken(ctx, DefaultIssuer, DefaultClientID, t, nil)
				}
				opCfg.Save = func(t *auth.Token) error { return auth.SaveToken(tokenPath, t) }
			}
			opCli, err := client.New(opCfg)
			if err != nil {
				return err
			}

			chatBaseFlag, _ := cmd.Flags().GetString("chat-base-url")
			chatBase := client.ResolveChatBaseURL(chatBaseFlag, os.Getenv("TATARA_CHAT_URL"), fileCfg)
			chatCfg := client.Config{
				BaseURL:   chatBase,
				Token:     token,
				TokenPath: tokenPath,
			}
			if tokenPath != "" {
				chatCfg.Reload = func() (*auth.Token, error) { return auth.LoadToken(tokenPath) }
				chatCfg.Refresh = func(ctx context.Context, t *auth.Token) (*auth.Token, error) {
					return auth.RefreshToken(ctx, DefaultIssuer, DefaultClientID, t, nil)
				}
				chatCfg.Save = func(t *auth.Token) error { return auth.SaveToken(tokenPath, t) }
			}
			chatCli, err := client.New(chatCfg)
			if err != nil {
				return err
			}

			srv := mcp.NewServer(cli, opCli, chatCli, logger)
			return srv.Run(ctx)
		},
	}
	c.Flags().String("operator-base-url", "", "tatara-operator REST base URL (overrides TATARA_OPERATOR_URL and config file)")
	c.Flags().String("chat-base-url", "", "tatara-chat REST base URL (overrides TATARA_CHAT_URL and config file)")
	return c
}

func mcpLogger(level slog.Level) (*slog.Logger, io.Closer, error) {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return nil, nil, err
		}
		dir = filepath.Join(h, ".local", "state")
	}
	if err := os.MkdirAll(filepath.Join(dir, "tatara"), 0o700); err != nil { //nolint:gosec // dir is XDG_STATE_HOME or ~/.local/state
		return nil, nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "tatara", "mcp.log"), //nolint:gosec // path derived from XDG_STATE_HOME
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("mcp: open log: %w", err)
	}
	return slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{Level: level})), f, nil
}
