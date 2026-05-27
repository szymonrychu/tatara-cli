package main

import (
	"fmt"
	"os"

	"github.com/szymonrychu/tatara-cli/internal/version"
)

func main() {
	fmt.Printf("tatara %s (%s) built %s\n", version.Version, version.Commit, version.Date)
	os.Exit(0)
}
