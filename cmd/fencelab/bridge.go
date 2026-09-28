package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/PoojaAgarwal2003/FenceLab/internal/bridge"
	"github.com/PoojaAgarwal2003/FenceLab/internal/model"
	"github.com/PoojaAgarwal2003/FenceLab/internal/sim"
)

func bridgeCommand(ctx context.Context, args []string, out, errOut io.Writer) int {
	flags := flag.NewFlagSet("bridge", flag.ContinueOnError)
	flags.SetOutput(errOut)
	file := flags.String("file", "", "v2 scenario JSON")
	replayFile := flags.String("replay", "", "exact v2 witness JSON (instead of -file)")
	directory := flags.String("dir", "", "new directory for authority/store WALs; never overwrite")
	seed := flags.Int64("seed", 7, "equal-time delivery-order seed; ignored for exact replay")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || *directory == "" || (*file == "") == (*replayFile == "") {
		fmt.Fprintln(errOut, "-dir and exactly one of -file or -replay are required")
		return 2
	}
	var scenario model.Scenario
	var replay *model.ReplayFile
	var err error
	if *replayFile != "" {
		replay = &model.ReplayFile{}
		err = readModelFile(*replayFile, replay)
		scenario = replay.Scenario
	} else {
		err = readModelFile(*file, &scenario)
	}
	if err == nil {
		err = scenario.Validate()
	}
	if err == nil && replay != nil {
		_, err = model.Replay(ctx, *replay)
	}
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 2
	}
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	report, err := bridge.Run(ctx, executable, *directory, scenario, *seed, replay)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	return 0
}

func actorCommand(ctx context.Context, args []string, out, errOut io.Writer) int {
	flags := flag.NewFlagSet("actor", flag.ContinueOnError)
	flags.SetOutput(errOut)
	role := flags.String("role", "", "authority, worker-a, worker-b, or store")
	wal := flags.String("wal", "", "WAL for authority/store")
	policy := flags.String("policy", string(sim.Idempotent), "storage policy")
	mode := flags.String("protocol", "barrier", "barrier or eager")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(errOut, "unexpected actor arguments")
		return 2
	}
	if err := bridge.ServeActor(ctx, *role, *wal, sim.Policy(*policy), *mode, os.Stdin, out); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	return 0
}
