package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/PoojaAgarwal2003/FenceLab/internal/model"
)

func readModelFile(name string, target any) error {
	file, err := os.Open(name)
	if err != nil {
		return err
	}
	decodeErr := model.Decode(file, target)
	return errors.Join(decodeErr, file.Close())
}

func modelCommand(ctx context.Context, command string, args []string, out, errOut io.Writer) int {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(errOut)
	file := flags.String("file", "", "versioned scenario JSON (replay: witness JSON)")
	bounds := model.Bounds{MaxStates: 2000, MaxDepth: 80}
	witness := ""
	if command == "search" {
		flags.IntVar(&bounds.MaxStates, "max-states", bounds.MaxStates, "stored state limit (1-10000)")
		flags.IntVar(&bounds.MaxDepth, "max-depth", bounds.MaxDepth, "event-decision depth (1-128)")
		flags.StringVar(&witness, "witness", "", "create a replay JSON file if a counterexample is found; never overwrite")
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *file == "" || flags.NArg() != 0 {
		fmt.Fprintln(errOut, "-file is required; no positional arguments accepted")
		return 2
	}
	var result any
	var err error
	exit := 0
	if command == "replay" {
		var replay model.ReplayFile
		err = readModelFile(*file, &replay)
		if err == nil {
			result, err = model.Replay(ctx, replay)
		}
	} else {
		var scenario model.Scenario
		err = readModelFile(*file, &scenario)
		if err == nil {
			if command == "network" {
				result, err = model.Run(ctx, scenario)
			} else {
				var report model.SearchReport
				report, err = model.Search(ctx, model.SearchRequest{Scenario: scenario, Bounds: bounds})
				result = report
				if report.Status == "state-limit" || report.Status == "depth-limit" {
					exit = 3
				}
				if err == nil && witness != "" && report.Witness != nil {
					var file *os.File
					file, err = os.OpenFile(witness, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
					if err == nil {
						encoder := json.NewEncoder(file)
						encoder.SetIndent("", "  ")
						err = errors.Join(encoder.Encode(report.Witness), file.Close())
					}
					if err != nil {
						fmt.Fprintln(errOut, "writing witness:", err)
						return 1
					}
				}
			}
		}
	}
	if err != nil {
		fmt.Fprintln(errOut, err)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
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
