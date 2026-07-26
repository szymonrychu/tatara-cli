package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/szymonrychu/tatara-cli/internal/auth"
	"github.com/szymonrychu/tatara-cli/internal/client"
)

func newRawCmd() *cobra.Command {
	var dataFlag string
	var targetFlag string
	cmd := &cobra.Command{
		Use:   "raw VERB PATH",
		Short: "Authenticated REST passthrough to a tatara backend (memory or operator).",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			verb := strings.ToUpper(args[0])
			path := args[1]
			ctx := cmd.Context()

			baseFlag, _ := cmd.Flags().GetString("base-url")
			configPath, err := client.DefaultConfigPath()
			if err != nil {
				return err
			}
			fileCfg, err := client.LoadConfig(configPath)
			if err != nil {
				return err
			}

			opBaseFlag, _ := cmd.Flags().GetString("operator-base-url")

			var base string
			switch targetFlag {
			case "memory":
				project, _ := cmd.Flags().GetString("project")
				memEnv, memEnvSet := os.LookupEnv("TATARA_MEMORY_URL")
				base = client.ResolveMemoryURL(baseFlag, memEnv, memEnvSet, fileCfg, project)
				if base == "" {
					return fmt.Errorf("raw: no memory backend configured (TATARA_MEMORY_URL is set but empty); pass --base-url to target one explicitly")
				}
			case "operator":
				base = client.ResolveOperatorBaseURL(opBaseFlag, os.Getenv("TATARA_OPERATOR_URL"), fileCfg)
			default:
				return fmt.Errorf("invalid --target %q: must be one of memory, operator", targetFlag)
			}

			tokenPath, err := auth.DefaultTokenPath()
			if err != nil {
				return err
			}
			token, err := auth.LoadToken(tokenPath)
			if err != nil {
				tokStr, exp, ccErr := auth.AccessTokenWithExpiry(ctx)
				if ccErr != nil {
					return ccErr
				}
				token = &auth.Token{AccessToken: tokStr, ExpiresAt: exp, TokenType: "Bearer"}
				tokenPath = ""
			}

			var body io.Reader
			switch {
			case dataFlag == "":
				body = nil
			case dataFlag == "-":
				in := cmd.InOrStdin()
				if f, ok := in.(*os.File); ok {
					if fi, err := f.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
						return fmt.Errorf("raw: stdin is a terminal; pipe data or use -d @file or -d '<json>'")
					}
				}
				body = in
			case strings.HasPrefix(dataFlag, "@"):
				f, err := os.Open(dataFlag[1:])
				if err != nil {
					return err
				}
				defer func() { _ = f.Close() }()
				body = f
			default:
				body = strings.NewReader(dataFlag)
			}

			verbose, _ := cmd.Flags().GetCount("verbose")
			rawLogger := slog.New(slog.NewJSONHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: verboseLevel(verbose)}))

			cfg := client.Config{
				BaseURL:   base,
				Token:     token,
				TokenPath: tokenPath,
				Log:       rawLogger,
			}
			if tokenPath != "" {
				cfg.Reload = func() (*auth.Token, error) { return auth.LoadToken(tokenPath) }
				cfg.Refresh = func(ctx context.Context, t *auth.Token) (*auth.Token, error) {
					return auth.RefreshToken(ctx, DefaultIssuer, DefaultClientID, t, nil)
				}
				cfg.Save = func(t *auth.Token) error { return auth.SaveToken(tokenPath, t) }
			} else {
				// Client-credentials fallback: wire a refresh func so that an expiring
				// cc token is re-minted instead of being sent stale and returning 401.
				// Mirrors the same fix in resolveMCPToken (mcp.go).
				cfg.Refresh = func(ctx context.Context, _ *auth.Token) (*auth.Token, error) {
					s, e, err := auth.AccessTokenWithExpiry(ctx)
					if err != nil {
						return nil, err
					}
					return &auth.Token{AccessToken: s, ExpiresAt: e, TokenType: "Bearer"}, nil
				}
			}
			cli, err := client.New(cfg)
			if err != nil {
				return err
			}

			resp, err := cli.Do(ctx, verb, path, body)
			if err != nil {
				return err
			}
			defer func() { _ = resp.Body.Close() }()
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), resp.Status)
			if _, err := io.Copy(cmd.OutOrStdout(), resp.Body); err != nil {
				return err
			}
			if resp.StatusCode >= http.StatusBadRequest {
				return fmt.Errorf("HTTP %d", resp.StatusCode)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&dataFlag, "data", "d", "", "Request body (literal JSON, @file, or - for stdin)")
	cmd.Flags().StringVar(&targetFlag, "target", "memory", "Backend to call: memory or operator")
	cmd.Flags().String("operator-base-url", "", "tatara-operator REST base URL (overrides TATARA_OPERATOR_URL and config file)")
	return cmd
}
