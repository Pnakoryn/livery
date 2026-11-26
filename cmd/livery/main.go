// Command livery installs and updates a team's shared coding-agent setup on a machine.
package main

import (
	"os"

	"github.com/Pnakoryn/livery/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
