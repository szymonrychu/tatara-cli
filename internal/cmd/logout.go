package cmd

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/szymonrychu/tatara-cli/internal/auth"
)

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Forget the stored OIDC token.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			verbose, _ := cmd.Root().PersistentFlags().GetCount("verbose")
			log := slog.New(slog.NewJSONHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: verboseLevel(verbose)}))

			path, err := auth.DefaultTokenPath()
			if err != nil {
				return err
			}
			if err := auth.DeleteToken(path); err != nil {
				return err
			}
			log.Info("auth lifecycle", "action", "logout")
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Logged out.")
			return nil
		},
	}
}
