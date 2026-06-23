package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
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
//
// For the client-credentials case the token expiry is set so that
// ensureFreshLocked can compute meaningful freshness, and a cc-refresh func is
// returned via the second return value so callers can wire it into the client
// Config - without this the long-lived MCP server would get 401s after the
// (commonly ~5 min) cc token expires.
func resolveMCPToken(ctx context.Context, logger *slog.Logger) (*auth.Token, string, func(context.Context, *auth.Token) (*auth.Token, error)) {
	tokenPath, err := auth.DefaultTokenPath()
	if err != nil {
		logger.Warn("mcp: cannot resolve token path; starting unauthenticated", "reason", err.Error())
		return nil, "", nil
	}
	if token, lerr := auth.LoadToken(tokenPath); lerr == nil {
		return token, tokenPath, nil
	}
	tokStr, exp, ccErr := auth.AccessTokenWithExpiry(ctx)
	if ccErr != nil {
		logger.Warn("mcp: no credentials; starting unauthenticated, tool calls fail until `tatara login` or OIDC env is set", "reason", ccErr.Error())
		return nil, "", nil
	}
	ccRefresh := func(ctx context.Context, _ *auth.Token) (*auth.Token, error) {
		s, e, err := auth.AccessTokenWithExpiry(ctx)
		if err != nil {
			return nil, err
		}
		return &auth.Token{AccessToken: s, ExpiresAt: e, TokenType: "Bearer"}, nil
	}
	return &auth.Token{AccessToken: tokStr, ExpiresAt: exp, TokenType: "Bearer"}, "", ccRefresh
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

			token, tokenPath, ccRefresh := resolveMCPToken(ctx, logger)

			// copyToken returns an independent copy so each Client owns its own
			// token struct; concurrent refresh in one Client never races with another.
			copyToken := func(t *auth.Token) *auth.Token {
				if t == nil {
					return nil
				}
				cp := *t
				return &cp
			}

			wireCreds := func(cfg *client.Config) {
				if tokenPath != "" {
					cfg.Reload = func() (*auth.Token, error) { return auth.LoadToken(tokenPath) }
					cfg.Refresh = func(ctx context.Context, t *auth.Token) (*auth.Token, error) {
						return auth.RefreshToken(ctx, DefaultIssuer, DefaultClientID, t, nil)
					}
					cfg.Save = func(t *auth.Token) error { return auth.SaveToken(tokenPath, t) }
				} else if ccRefresh != nil {
					cfg.Refresh = ccRefresh
				}
			}

			cliCfg := client.Config{
				BaseURL:   base,
				Token:     copyToken(token),
				TokenPath: tokenPath,
			}
			wireCreds(&cliCfg)
			cli, err := client.New(cliCfg)
			if err != nil {
				return err
			}

			opBaseFlag, _ := cmd.Flags().GetString("operator-base-url")
			opBase := client.ResolveOperatorBaseURL(opBaseFlag, os.Getenv("TATARA_OPERATOR_URL"), fileCfg)
			opCfg := client.Config{
				BaseURL:   opBase,
				Token:     copyToken(token),
				TokenPath: tokenPath,
			}
			wireCreds(&opCfg)
			opCli, err := client.New(opCfg)
			if err != nil {
				return err
			}

			chatBaseFlag, _ := cmd.Flags().GetString("chat-base-url")
			chatBase := client.ResolveChatBaseURL(chatBaseFlag, os.Getenv("TATARA_CHAT_URL"), fileCfg)
			chatCfg := client.Config{
				BaseURL:   chatBase,
				Token:     copyToken(token),
				TokenPath: tokenPath,
			}
			wireCreds(&chatCfg)
			chatCli, err := client.New(chatCfg)
			if err != nil {
				return err
			}

			toolProfile, _ := cmd.Flags().GetString("tool-profile")
			if toolProfile == "" {
				toolProfile = os.Getenv("TATARA_TOOL_PROFILE")
			}
			srv := mcp.NewServer(cli, opCli, chatCli, logger, toolProfile)

			metricsAddr, _ := cmd.Flags().GetString("metrics-addr")
			if metricsAddr != "" {
				mux := http.NewServeMux()
				mux.Handle("/metrics", promhttp.Handler())
				metricsSrv := &http.Server{ //nolint:gosec // user-supplied addr
					Addr:              metricsAddr,
					Handler:           mux,
					ReadHeaderTimeout: 5 * time.Second,
					ReadTimeout:       10 * time.Second,
					WriteTimeout:      10 * time.Second,
					IdleTimeout:       60 * time.Second,
				}
				go func() {
					if serr := metricsSrv.ListenAndServe(); serr != nil && serr != http.ErrServerClosed {
						logger.Error("metrics server error", "err", serr)
					}
				}()
				defer func() { _ = metricsSrv.Shutdown(context.Background()) }()
				logger.Info("metrics server started", "addr", metricsAddr)
			}

			return srv.Run(ctx)
		},
	}
	c.Flags().String("operator-base-url", "", "tatara-operator REST base URL (overrides TATARA_OPERATOR_URL and config file)")
	c.Flags().String("chat-base-url", "", "tatara-chat REST base URL (overrides TATARA_CHAT_URL and config file)")
	c.Flags().String("metrics-addr", os.Getenv("TATARA_MCP_METRICS_ADDR"), "TCP address for the /metrics HTTP endpoint (e.g. 127.0.0.1:9090); empty disables it")
	c.Flags().String("tool-profile", "", "MCP tool profile to serve (overrides TATARA_TOOL_PROFILE env); empty serves full set (fail-open)")
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
