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
	var target string
	cmd := &cobra.Command{
		Use:   "raw VERB PATH",
		Short: "Authenticated REST passthrough to a tatara backend.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			verb := strings.ToUpper(args[0])
			path := args[1]
			ctx := cmd.Context()

			configPath, err := client.DefaultConfigPath()
			if err != nil {
				return err
			}
			fileCfg, err := client.LoadConfig(configPath)
			if err != nil {
				return err
			}

			base, err := rawBaseURL(cmd, target, fileCfg)
			if err != nil {
				return err
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
	cmd.Flags().StringVar(&target, "target", "memory", "backend to target: memory, operator, or chat")
	cmd.Flags().String("operator-base-url", "", "tatara-operator REST base URL (overrides TATARA_OPERATOR_URL and config file)")
	cmd.Flags().String("chat-base-url", "", "tatara-chat REST base URL (overrides TATARA_CHAT_URL and config file)")
	return cmd
}

// rawBaseURL resolves the base URL for the selected target. memory honours the
// persistent --base-url/--project flags; operator and chat use their own
// --*-base-url flag, env var and config file.
func rawBaseURL(cmd *cobra.Command, target string, fileCfg *client.FileConfig) (string, error) {
	switch target {
	case "memory", "":
		baseFlag, _ := cmd.Flags().GetString("base-url")
		base := client.ResolveBaseURL(baseFlag, os.Getenv("TATARA_MEMORY_URL"), fileCfg)
		project, _ := cmd.Flags().GetString("project")
		return client.MemoryURLForProject(base, project), nil
	case "operator":
		opBaseFlag, _ := cmd.Flags().GetString("operator-base-url")
		return client.ResolveOperatorBaseURL(opBaseFlag, os.Getenv("TATARA_OPERATOR_URL"), fileCfg), nil
	case "chat":
		chatBaseFlag, _ := cmd.Flags().GetString("chat-base-url")
		return client.ResolveChatBaseURL(chatBaseFlag, os.Getenv("TATARA_CHAT_URL"), fileCfg), nil
	default:
		return "", fmt.Errorf("raw: unknown target %q (want memory, operator, or chat)", target)
	}
}
