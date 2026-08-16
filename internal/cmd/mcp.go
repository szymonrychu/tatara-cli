package cmd

import (
	"context"
	"errors"
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

// mcpCreds is everything resolveMCPToken worked out about this pod's identity.
type mcpCreds struct {
	token     *auth.Token
	tokenPath string
	ccRefresh func(context.Context, *auth.Token) (*auth.Token, error)
	// authNote is empty when credentials resolved. When they did not, it says
	// WHY, and mcp.WithAuthNote carries it out on error tool results - the only
	// channel that leaves this process (see WithAuthNote).
	authNote string
}

// unauthenticatedNote renders the reason this pod has no credentials into one
// operator-readable line. The client-credentials mint is the primary auth path
// for every agent pod and has nine failure branches; MintError.Stage is what
// turns "every tool call 401s" into a cause.
func unauthenticatedNote(err error) string {
	reason := err.Error()
	if errors.Is(err, auth.ErrNoToken) {
		reason = "no stored token and no OIDC client-credentials env " +
			"(OIDC_ISSUER / CLI_OIDC_CLIENT_ID / CLI_OIDC_CLIENT_SECRET); run `tatara login` or inject them"
	}
	return "[tatara-mcp] auth: this MCP server started UNAUTHENTICATED - " + reason +
		". Backend tool calls will keep failing until credentials exist. This is a platform fault: " +
		"report it once with report_internal_issue(category=\"auth\"), which needs no auth."
}

// resolveMCPToken loads credentials for the MCP server without failing when
// none are present. Static MCP capabilities (tools/list) need no auth; only
// actual backend calls do. With no stored token and no client-credentials it
// returns a nil token so the server still starts and advertises its tools;
// individual tool invocations then fail until credentials exist, carrying
// mcpCreds.authNote so the cause travels with the symptom.
//
// For the client-credentials case the token expiry is set so that
// ensureFreshLocked can compute meaningful freshness, and a cc-refresh func is
// returned so callers can wire it into the client Config - without this the
// long-lived MCP server would get 401s after the (commonly ~5 min) cc token
// expires.
func resolveMCPToken(ctx context.Context, logger *slog.Logger) mcpCreds {
	tokenPath, err := auth.DefaultTokenPath()
	if err != nil {
		logger.Warn("mcp: cannot resolve token path; starting unauthenticated",
			"action", "auth_unresolved", "reason", err.Error())
		return mcpCreds{authNote: unauthenticatedNote(err)}
	}
	if token, lerr := auth.LoadToken(tokenPath); lerr == nil {
		return mcpCreds{token: token, tokenPath: tokenPath}
	}
	tokStr, exp, ccErr := auth.AccessTokenWithExpiry(ctx)
	if ccErr != nil {
		logger.Warn("mcp: no credentials; starting unauthenticated, tool calls fail until `tatara login` or OIDC env is set",
			"action", "auth_unresolved", "stage", auth.MintStage(ccErr), "reason", ccErr.Error())
		return mcpCreds{authNote: unauthenticatedNote(ccErr)}
	}
	ccRefresh := func(ctx context.Context, _ *auth.Token) (*auth.Token, error) {
		s, e, err := auth.AccessTokenWithExpiry(ctx)
		if err != nil {
			return nil, err
		}
		return &auth.Token{AccessToken: s, ExpiresAt: e, TokenType: "Bearer"}, nil
	}
	return mcpCreds{
		token:     &auth.Token{AccessToken: tokStr, ExpiresAt: exp, TokenType: "Bearer"},
		ccRefresh: ccRefresh,
	}
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
			project, _ := cmd.Flags().GetString("project")
			memEnv, memEnvSet := os.LookupEnv("TATARA_MEMORY_URL")
			base := client.ResolveMemoryURL(baseFlag, memEnv, memEnvSet, fileCfg, project)

			creds := resolveMCPToken(ctx, logger)

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
				// Without this the refresh success/failure logging in
				// internal/client is dead code - nothing else sets cfg.Log, and
				// a failed refresh is one of the two auth signals with no other
				// producer anywhere.
				cfg.Log = logger
				if creds.tokenPath != "" {
					cfg.Reload = func() (*auth.Token, error) { return auth.LoadToken(creds.tokenPath) }
					cfg.Refresh = func(ctx context.Context, t *auth.Token) (*auth.Token, error) {
						return auth.RefreshToken(ctx, DefaultIssuer, DefaultClientID, t, nil)
					}
					cfg.Save = func(t *auth.Token) error { return auth.SaveToken(creds.tokenPath, t) }
				} else if creds.ccRefresh != nil {
					cfg.Refresh = creds.ccRefresh
				}
			}

			// No memory base URL resolved: this pod has no memory backend
			// (TATARA_MEMORY_URL set but empty). Start anyway with a nil memory
			// client - the memory tools stay listed and answer MEMORY_DEGRADED.
			var cli *client.Client
			if base == "" {
				logger.Warn("no memory backend configured; memory tools will return MEMORY_DEGRADED",
					"action", "memory_unconfigured", "env", "TATARA_MEMORY_URL")
			} else {
				cliCfg := client.Config{
					BaseURL:   base,
					Token:     copyToken(creds.token),
					TokenPath: creds.tokenPath,
				}
				wireCreds(&cliCfg)
				cli, err = client.New(cliCfg)
				if err != nil {
					return err
				}
			}

			opBaseFlag, _ := cmd.Flags().GetString("operator-base-url")
			opBase := client.ResolveOperatorBaseURL(opBaseFlag, os.Getenv("TATARA_OPERATOR_URL"), fileCfg)
			opCfg := client.Config{
				BaseURL:   opBase,
				Token:     copyToken(creds.token),
				TokenPath: creds.tokenPath,
			}
			wireCreds(&opCfg)
			opCli, err := client.New(opCfg)
			if err != nil {
				return err
			}

			if err := mcp.CheckContractVersion(os.Getenv("TATARA_CONTRACT_VERSION")); err != nil {
				logger.Error("refusing to start the MCP server", "action", "contract_mismatch", "error", err.Error())
				return err
			}

			toolProfile, _ := cmd.Flags().GetString("tool-profile")
			if toolProfile == "" {
				toolProfile = os.Getenv("TATARA_TOOL_PROFILE")
			}
			srv := mcp.NewServer(cli, opCli, logger, toolProfile, mcp.WithAuthNote(creds.authNote))

			return srv.Run(ctx)
		},
	}
	c.Flags().String("operator-base-url", "", "tatara-operator REST base URL (overrides TATARA_OPERATOR_URL and config file)")
	c.Flags().String("tool-profile", "", "MCP tool profile to serve (overrides TATARA_TOOL_PROFILE env); empty fails closed to the alwaysOn tool set")
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
