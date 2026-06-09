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

func newMCPCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "mcp",
		Short: "Run the tatara MCP server over stdio.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			logger, closer, err := mcpLogger()
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

			tokenPath, err := auth.DefaultTokenPath()
			if err != nil {
				return err
			}
			token, err := auth.LoadToken(tokenPath)
			if err != nil {
				tokStr, ccErr := auth.AccessToken(ctx)
				if ccErr != nil {
					return ccErr
				}
				token = &auth.Token{AccessToken: tokStr, TokenType: "Bearer"}
				tokenPath = ""
			}

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

			srv := mcp.NewServer(cli, opCli, logger)
			return srv.Run(ctx)
		},
	}
	c.Flags().String("operator-base-url", "", "tatara-operator REST base URL (overrides TATARA_OPERATOR_URL and config file)")
	return c
}

func mcpLogger() (*slog.Logger, io.Closer, error) {
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
	return slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo})), f, nil
}
