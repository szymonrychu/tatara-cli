package cmd

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/szymonrychu/tatara-cli/internal/client"
)

func newRawCmd() *cobra.Command {
	var dataFlag string
	cmd := &cobra.Command{
		Use:   "raw VERB PATH",
		Short: "Authenticated REST passthrough to tatara-memory.",
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
			base := client.ResolveBaseURL(baseFlag, os.Getenv("TATARA_MEMORY_URL"), fileCfg)
			project, _ := cmd.Flags().GetString("project")
			base = client.MemoryURLForProject(base, project)

			token, tokenPath, err := loadToken(ctx)
			if err != nil {
				return err
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

			cli, err := buildClient(base, token, tokenPath)
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
	return cmd
}
