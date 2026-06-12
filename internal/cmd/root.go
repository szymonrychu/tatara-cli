package cmd

import (
	"os"

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
	root.PersistentFlags().StringP("project", "p", os.Getenv("TATARA_PROJECT"), "tatara project (per-project memory path); env TATARA_PROJECT")
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
