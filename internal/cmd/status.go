package cmd

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/szymonrychu/tatara-cli/internal/auth"
	"github.com/szymonrychu/tatara-cli/internal/client"
)

func newStatusCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "status",
		Short: "Show local auth state and the resolved backend base URLs (no network calls).",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			tokenPath, err := auth.DefaultTokenPath()
			if err != nil {
				return err
			}

			token, err := auth.LoadToken(tokenPath)
			switch {
			case err == nil:
				_, _ = fmt.Fprintf(out, "Auth:     logged in (token %s)\n", expiryDesc(token.ExpiresAt))
			case errors.Is(err, auth.ErrNoToken):
				if clientCredsConfigured() {
					_, _ = fmt.Fprintln(out, "Auth:     client-credentials configured")
				} else {
					_, _ = fmt.Fprintf(out, "Auth:     not authenticated (%s)\n", auth.ErrNoToken)
				}
			default:
				return err
			}

			configPath, err := client.DefaultConfigPath()
			if err != nil {
				return err
			}
			fileCfg, err := client.LoadConfig(configPath)
			if err != nil {
				return err
			}

			baseFlag, _ := cmd.Flags().GetString("base-url")
			project, _ := cmd.Flags().GetString("project")
			memEnv, memEnvSet := os.LookupEnv("TATARA_MEMORY_URL")
			memBase := client.ResolveMemoryURL(baseFlag, memEnv, memEnvSet, fileCfg, project)
			if memBase == "" {
				memBase = "(not configured: TATARA_MEMORY_URL is set but empty)"
			}

			opBaseFlag, _ := cmd.Flags().GetString("operator-base-url")
			opBase := client.ResolveOperatorBaseURL(opBaseFlag, os.Getenv("TATARA_OPERATOR_URL"), fileCfg)

			if project == "" {
				project = "(none)"
			}
			_, _ = fmt.Fprintf(out, "Project:  %s\n", project)
			_, _ = fmt.Fprintf(out, "Token:    %s\n", tokenPath)
			_, _ = fmt.Fprintf(out, "Memory:   %s\n", memBase)
			_, _ = fmt.Fprintf(out, "Operator: %s\n", opBase)
			return nil
		},
	}
	c.Flags().String("operator-base-url", "", "tatara-operator REST base URL (overrides TATARA_OPERATOR_URL and config file)")
	return c
}

// expiryDesc describes how long until or since a token expires, without a
// network call. Mirrors the human phrasing in the task spec.
// Sign is checked before rounding so a token expired by <500ms is reported as
// "expired 0s ago" rather than "valid for 0s".
func expiryDesc(exp time.Time) string {
	raw := time.Until(exp)
	if raw >= 0 {
		return "valid for " + raw.Round(time.Second).String()
	}
	return "expired " + (-raw).Round(time.Second).String() + " ago"
}

// clientCredsConfigured delegates to auth so the env-var contract lives in one place.
func clientCredsConfigured() bool {
	return auth.ClientCredsConfigured()
}
