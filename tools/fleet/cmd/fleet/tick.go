package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"os/signal"
	"syscall"
	"time"

	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/fleetd"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/herdr"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/resources"
	"github.com/LeanerCloud/dotclaude/tools/fleet/internal/state"
)

func runTick(e env, args []string) int {
	flags := flag.NewFlagSet("tick", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dryRun := flags.Bool("dry-run", false, "observe without dispatching")
	configPath := flags.String("config", "", "explicit configuration path")
	ledgerPath := flags.String("ledger", "", "explicit ledger path")
	outputPath := flags.String("output-dir", "", "private observer output directory")
	if flags.Parse(args) != nil || !*dryRun || *configPath == "" || *ledgerPath == "" || *outputPath == "" || flags.NArg() != 0 {
		msg(e.stderr, "usage: fleet tick --dry-run --config <path> --ledger <path> --output-dir <path>\n")
		return exitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	var result fleetd.Evaluation
	paths, err := fleetd.ReadObserverPaths(*configPath, *ledgerPath, *outputPath, e.home)
	if err == nil {
		o := fleetd.Observer{Run: herdr.ExecRunner, Resources: resources.NewSampler(), Home: e.home}
		result, err = o.Tick(ctx, *paths, time.Now())
	} else {
		result.Errors = []string{state.Scrub(err.Error())}
	}
	if err != nil {
		msg(e.stderr, "fleet tick: %s\n", state.Scrub(err.Error()))
	}
	if encodeErr := json.NewEncoder(e.stdout).Encode(result); encodeErr != nil {
		msg(e.stderr, "fleet tick: encode result: %s\n", state.Scrub(encodeErr.Error()))
		return exitFailure
	}
	if err != nil || !result.Complete {
		return exitFailure
	}
	return 0
}
