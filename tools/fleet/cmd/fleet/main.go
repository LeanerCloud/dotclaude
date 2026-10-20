// Command fleet is the deterministic supervisor for a herdr fleet of coding agents.
package main

import (
	"fmt"
	"io"
	"os"
)

var subcommands = []string{"tick", "cleanup", "heavy", "lock", "scratch", "scrub", "sandbox"}

func main() {
	e, err := processEnv()
	if err != nil {
		msg(os.Stderr, "fleet: %v\n", err)
		os.Exit(exitFailure)
	}
	os.Exit(dispatch(e, os.Args[1:]))
}

// dispatch runs the implemented subcommands and falls back to run for the rest.
func dispatch(e env, args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "tick":
			return runTick(e, args[1:])
		case "heavy":
			return runHeavy(e, args[1:])
		case "lock":
			return runLock(e, args[1:])
		}
	}
	return run(args, e.stderr)
}

func run(args []string, stderr io.Writer) int {
	if len(args) == 0 {
		msg(stderr, "usage: fleet <%v>\n", subcommands)
		return 2
	}
	for _, s := range subcommands {
		if args[0] == s {
			msg(stderr, "fleet %s: not implemented\n", s)
			return 2
		}
	}
	msg(stderr, "fleet: unknown subcommand %q\n", args[0])
	return 2
}

// msg writes a diagnostic; a failed write to stderr has nowhere left to be reported.
func msg(w io.Writer, format string, args ...any) { _, _ = fmt.Fprintf(w, format, args...) }
