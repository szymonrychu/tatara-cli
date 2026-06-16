package cmd

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/szymonrychu/tatara-cli/internal/auth"
)

func newLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Authenticate against Keycloak via OIDC device flow.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			verbose, _ := cmd.Root().PersistentFlags().GetCount("verbose")
			log := slog.New(slog.NewJSONHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: verboseLevel(verbose)}))

			path, err := auth.DefaultTokenPath()
			if err != nil {
				return err
			}
			issuer := os.Getenv("OIDC_ISSUER")
			if issuer == "" {
				issuer = DefaultIssuer
			}
			flow := &auth.DeviceFlow{
				Issuer:   issuer,
				ClientID: DefaultClientID,
				Scope:    DefaultScope,
			}
			start := time.Now()
			code, err := flow.Start(ctx)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Open this URL in your browser to authorize:\n  %s\n\nUser code: %s\nWaiting for authorization...\n", code.VerificationURIComplete, code.UserCode)
			token, err := flow.Poll(ctx, code)
			if err != nil {
				if errors.Is(err, auth.ErrAccessDenied) {
					return fmt.Errorf("authorization denied; rerun `tatara login` and approve the request")
				}
				return err
			}
			if err := auth.SaveToken(path, token); err != nil {
				return err
			}
			log.Info("auth lifecycle", "action", "login", "result", "ok", "duration_ms", time.Since(start).Milliseconds())
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Logged in. Token saved to %s\n", path)
			return nil
		},
	}
}
