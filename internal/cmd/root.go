package cmd

import (
	"github.com/spf13/cobra"

	"github.com/szymonrychu/tatara-cli/internal/version"
)

const (
	DefaultIssuer   = "https://auth.szymonrichert.pl/realms/master"
	DefaultClientID = "tatara-cli"
	DefaultScope    = "tatara"
)

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "tatara",
		Short:         "tatara platform CLI",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().String("base-url", "", "tatara-memory base URL (overrides TATARA_MEMORY_URL and config file)")
	root.PersistentFlags().CountP("verbose", "v", "increase log verbosity (-v info, -vv debug)")

	root.AddCommand(
		newLoginCmd(),
		newLogoutCmd(),
		newRawCmd(),
		newMCPCmd(),
		newMCPConfigCmd(),
	)
	return root
}
