// Command eso-map maps External Secrets Operator configuration end to end.
package main

import (
	"fmt"
	"os"

	"github.com/vincent/eso-map/internal/cli"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
