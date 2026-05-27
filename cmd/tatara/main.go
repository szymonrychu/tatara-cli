package main

import (
	"fmt"
	"os"

	"github.com/szymonrychu/tatara-cli/internal/cmd"
)

func main() {
	if err := cmd.NewRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}
