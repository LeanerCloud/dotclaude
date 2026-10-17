// Command fleet is the deterministic supervisor for a herdr fleet of coding agents.
package main

import (
	"fmt"
	"io"
	"os"
)

var subcommands = []string{"tick", "cleanup", "heavy", "lock", "scratch", "scrub", "sandbox"}

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(stderr, "usage: fleet <%v>\n", subcommands)
		return 2
	}
	for _, s := range subcommands {
		if args[0] == s {
			fmt.Fprintf(stderr, "fleet %s: not implemented\n", s)
			return 2
		}
	}
	fmt.Fprintf(stderr, "fleet: unknown subcommand %q\n", args[0])
	return 2
}
