// Command kemory is the Kemory CLI.
package main

import (
	"os"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/cli"
)

func main() {
	os.Exit(cli.Execute(os.Args[1:]))
}
