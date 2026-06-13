package cmd

import (
	"context"
	"fmt"
	"io"
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
		Short: "Authenticated REST passthrough to a tatara backend (memory, operator, or chat).",
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

			var base string
			switch targetFlag {
			case "memory":
				base = client.ResolveBaseURL(baseFlag, os.Getenv("TATARA_MEMORY_URL"), fileCfg)
				project, _ := cmd.Flags().GetString("project")
				base = client.MemoryURLForProject(base, project)
			case "operator":
				base = client.ResolveOperatorBaseURL("", os.Getenv("TATARA_OPERATOR_URL"), fileCfg)
			case "chat":
				base = client.ResolveChatBaseURL("", os.Getenv("TATARA_CHAT_URL"), fileCfg)
			default:
				return fmt.Errorf("invalid --target %q: must be one of memory, operator, chat", targetFlag)
			}

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

			var body io.Reader
			switch {
			case dataFlag == "":
				body = nil
			case dataFlag == "-":
				body = cmd.InOrStdin()
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

			cfg := client.Config{
				BaseURL:   base,
				Token:     token,
				TokenPath: tokenPath,
			}
			if tokenPath != "" {
				cfg.Reload = func() (*auth.Token, error) { return auth.LoadToken(tokenPath) }
				cfg.Refresh = func(ctx context.Context, t *auth.Token) (*auth.Token, error) {
					return auth.RefreshToken(ctx, DefaultIssuer, DefaultClientID, t, nil)
				}
				cfg.Save = func(t *auth.Token) error { return auth.SaveToken(tokenPath, t) }
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
	cmd.Flags().StringVar(&targetFlag, "target", "memory", "Backend to call: memory, operator, or chat")
	return cmd
}
