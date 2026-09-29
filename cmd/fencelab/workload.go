package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/PoojaAgarwal2003/FenceLab/internal/scheduler"
)

func workloadCommand(ctx context.Context, args []string, out, errOut io.Writer) int {
	flags := flag.NewFlagSet("workload", flag.ContinueOnError)
	flags.SetOutput(errOut)
	file := flags.String("file", "", "bounded fencelab/workload-v1 config JSON")
	directory := flags.String("dir", "", "new directory for the real effect ledger; never overwrite")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || *file == "" || *directory == "" {
		fmt.Fprintln(errOut, "-file and -dir are required; no positional arguments")
		return 2
	}
	var config scheduler.Config
	err := readModelFile(*file, &config)
	if err == nil {
		err = config.Validate()
	}
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 2
	}
	report, err := scheduler.Run(ctx, *directory, config)
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
