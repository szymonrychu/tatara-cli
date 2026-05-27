package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/szymonrychu/tatara-cli/internal/auth"
)

func newLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Authenticate against Keycloak via OIDC device flow.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			path, err := auth.DefaultTokenPath()
			if err != nil {
				return err
			}
			flow := &auth.DeviceFlow{
				Issuer:   DefaultIssuer,
				ClientID: DefaultClientID,
				Scope:    DefaultScope,
			}
			code, err := flow.Start(ctx)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Open this URL in your browser to authorize:\n  %s\n\nUser code: %s\nWaiting for authorization...\n", code.VerificationURIComplete, code.UserCode)
			token, err := flow.Poll(ctx, code)
			if err != nil {
				return err
			}
			if err := auth.SaveToken(path, token); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Logged in. Token saved to %s\n", path)
			return nil
		},
	}
}
