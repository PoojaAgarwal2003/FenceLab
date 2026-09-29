package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/PoojaAgarwal2003/FenceLab/internal/sim"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, out, errOut io.Writer) int {
	if len(args) == 0 || args[0] == "-help" || args[0] == "help" {
		_, err := fmt.Fprintln(out, "FenceLab: lease, fencing, and idempotency laboratory\n\nCommands:\n  serve       Open the local interactive failure laboratory\n  run         Replay one v1 scenario and policy as JSON\n  compare     Replay the same v1 schedule under three policies\n  check       Explore bounded v1 seeded schedules and check expected outcomes\n  network     Execute a v2 message-fault scenario (-file)\n  search      Explore bounded v2 delivery orders (-file, -max-states, -max-depth)\n  replay      Replay a v2 event-decision witness (-file)\n  durability  Kill/recover 15 child processes at WAL boundaries (-dir)\n  recover     Recover and inspect an existing WAL (-wal)\n  bridge      Run four real actors with controlled delivery (-file or -replay, -dir)\n  workload    Measure bounded multi-job scheduling and durable effects (-file, -dir)\n\nUse COMMAND -help for flags. Model times are virtual milliseconds; workload metrics use measured elapsed time.")
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		return 0
	}
	command := args[0]
	if command == "workload" {
		return workloadCommand(ctx, args[1:], out, errOut)
	}
	if command == "bridge" {
		return bridgeCommand(ctx, args[1:], out, errOut)
	}
	if command == "actor" {
		return actorCommand(ctx, args[1:], out, errOut)
	}
	if command == "durability" || command == "recover" || command == "wal-probe" {
		return durableCommand(ctx, command, args[1:], out, errOut)
	}
	if command == "serve" {
		return serve(ctx, args[1:], errOut)
	}
	if command == "network" || command == "search" || command == "replay" {
		return modelCommand(ctx, command, args[1:], out, errOut)
	}
	if command != "run" && command != "compare" && command != "check" {
		fmt.Fprintf(errOut, "unknown command %q; use -help\n", command)
		return 2
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(errOut)
	config := sim.DefaultConfig()
	var scenario, policy string
	seeds := 100
	if command == "check" {
		flags.Int64Var(&config.Seed, "start-seed", 0, "first schedule seed")
		flags.IntVar(&seeds, "seeds", seeds, "number of seeds (1-10000); nine runs per seed")
	} else {
		flags.Int64Var(&config.Seed, "seed", config.Seed, "deterministic schedule seed")
		flags.StringVar(&scenario, "scenario", string(config.Scenario), "paused-worker, lost-ack, or healthy")
		if command == "run" {
			flags.StringVar(&policy, "policy", string(config.Policy), "lease-only, fenced, or fenced-idempotent")
		}
	}
	flags.IntVar(&config.LeaseMS, "lease-ms", config.LeaseMS, "virtual lease duration (20-10000)")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(errOut, "unexpected positional arguments")
		return 2
	}
	if scenario != "" {
		config.Scenario = sim.Scenario(scenario)
	}
	if policy != "" {
		config.Policy = sim.Policy(policy)
	}
	// Explicit empty values are invalid rather than silently selecting defaults.
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "scenario" {
			config.Scenario = sim.Scenario(scenario)
		}
		if f.Name == "policy" {
			config.Policy = sim.Policy(policy)
		}
	})
	var result any
	var err error
	exit := 0
	switch command {
	case "run":
		result, err = sim.Run(config)
	case "compare":
		result, err = sim.Compare(config)
	case "check":
		var report sim.Exploration
		report, err = sim.Explore(ctx, config.Seed, seeds, config.LeaseMS)
		result = report
		if report.UnexpectedOutcomes != 0 || report.ProtectedFailures != 0 {
			exit = 1
		}
	}
	if err != nil {
		fmt.Fprintln(errOut, err)
		if errors.Is(err, context.Canceled) {
			return 1
		}
		return 2
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	return exit
}
