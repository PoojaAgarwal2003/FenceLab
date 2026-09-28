package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/PoojaAgarwal2003/FenceLab/internal/durable"
	"github.com/PoojaAgarwal2003/FenceLab/internal/sim"
)

var crashPoints = []string{"before-append", "after-header", "after-append", "after-sync", "after-apply"}

type crashCase = durable.CrashCase
type crashReport = durable.CrashReport

func durableCommand(ctx context.Context, command string, args []string, out, errOut io.Writer) int {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(errOut)
	var directory, path, operation, point string
	if command == "durability" {
		flags.StringVar(&directory, "dir", "", "new directory for 15 process-crash experiments; never overwrite")
	} else {
		flags.StringVar(&path, "wal", "", "local WAL file")
		if command == "wal-probe" {
			flags.StringVar(&operation, "operation", "", "internal crash harness operation")
			flags.StringVar(&point, "point", "", "internal crash harness checkpoint")
		}
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || (command == "durability" && directory == "") || (command != "durability" && path == "") {
		fmt.Fprintln(errOut, "provide the required -dir or -wal; no positional arguments")
		return 2
	}
	var value any
	var err error
	switch command {
	case "durability":
		var executable string
		executable, err = os.Executable()
		if err == nil {
			value, err = runCrashLab(ctx, executable, directory)
		}
	case "recover":
		// Inspection must not accidentally create a file for a mistyped path.
		if _, err = os.Stat(path); err == nil {
			var log *durable.Log
			var recovery durable.Recovery
			log, recovery, err = durable.Open(path, nil)
			if err == nil {
				value = struct {
					State    durable.State    `json:"state"`
					Recovery durable.Recovery `json:"recovery"`
				}{log.Snapshot(), recovery}
				err = log.Close()
			}
		}
	case "wal-probe":
		err = crashProbe(ctx, path, operation, point, out)
		if err == nil {
			err = fmt.Errorf("crash probe completed without reaching checkpoint")
		}
	}
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	return 0
}

func crashProbe(ctx context.Context, path, operation, point string, out io.Writer) error {
	valid := false
	for _, p := range crashPoints {
		valid = valid || point == p
	}
	if !valid || (operation != "reserve" && operation != "fence" && operation != "write") {
		return fmt.Errorf("invalid crash operation or checkpoint")
	}
	log, _, err := durable.Open(path, func(at string) error {
		if at != point {
			return nil
		}
		if err := json.NewEncoder(out).Encode(map[string]string{"point": at, "operation": operation}); err != nil {
			return err
		}
		<-ctx.Done() // Parent kills this process; no deferred close or flush executes.
		return ctx.Err()
	})
	if err != nil {
		return err
	}
	switch operation {
	case "reserve":
		_, err = log.Reserve()
	case "fence":
		err = log.Fence(2)
	case "write":
		_, err = log.Write(1, "invoice-001", sim.Idempotent)
	}
	return errors.Join(err, log.Close())
}

func killAtCheckpoint(ctx context.Context, executable, path, operation, point string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "wal-probe", "-wal", path, "-operation", operation, "-point", point)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	var checkpoint struct {
		Point     string `json:"point"`
		Operation string `json:"operation"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(pipe, 4096)).Decode(&checkpoint)
	killErr := cmd.Process.Kill()
	waitErr := cmd.Wait()
	if decodeErr != nil || checkpoint.Point != point || checkpoint.Operation != operation {
		return fmt.Errorf("checkpoint %s/%s: decode=%v wait=%v stderr=%s", operation, point, decodeErr, waitErr, stderr.String())
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var exit *exec.ExitError
	if killErr != nil || !errors.As(waitErr, &exit) {
		return fmt.Errorf("expected forced child termination: kill=%v wait=%v", killErr, waitErr)
	}
	return nil
}

func runCrashLab(ctx context.Context, executable, directory string) (crashReport, error) {
	report := crashReport{Version: "fencelab/durability-v1", Scope: "process-kill; local filesystem; ledger effects only; not power-loss proof", Cases: []crashCase{}, Safe: true}
	if err := ctx.Err(); err != nil {
		return report, err
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		return report, fmt.Errorf("create fresh experiment directory: %w", err)
	}
	for _, operation := range []string{"reserve", "fence", "write"} {
		for _, point := range crashPoints {
			if err := ctx.Err(); err != nil {
				return report, err
			}
			path := filepath.Join(directory, operation+"-"+point+".wal")
			log, _, err := durable.Open(path, nil)
			if err != nil {
				return report, err
			}
			_, reserveErr := log.Reserve()
			fenceErr := log.Fence(1)
			if err := errors.Join(reserveErr, fenceErr, log.Close()); err != nil {
				return report, err
			}
			if err := killAtCheckpoint(ctx, executable, path, operation, point); err != nil {
				return report, err
			}
			recovered, recovery, err := durable.Open(path, nil)
			if err != nil {
				return report, err
			}
			result := crashCase{Operation: operation, Point: point, Recovery: recovery, Recovered: recovered.Snapshot()}
			result.NextToken, err = recovered.Reserve()
			if err == nil {
				result.Retry, err = recovered.Write(result.NextToken, "invoice-001", sim.Idempotent)
			}
			final := recovered.Snapshot()
			err = errors.Join(err, recovered.Close())
			if err != nil {
				return report, err
			}
			s := result.Recovered
			result.Safe = s.Epoch >= 1 && s.Fence >= 1 && result.NextToken == s.Epoch+1 && len(final.Effects) == 1
			if point == "after-sync" || point == "after-apply" {
				switch operation {
				case "reserve":
					result.Safe = result.Safe && s.Epoch == 2
				case "fence":
					result.Safe = result.Safe && s.Fence == 2
				case "write":
					result.Safe = result.Safe && len(s.Effects) == 1 && result.Retry.Status == "deduplicated"
				}
			}
			report.Cases = append(report.Cases, result)
			report.Safe = report.Safe && result.Safe
			if !result.Safe {
				return report, fmt.Errorf("recovery invariant failed at %s/%s", operation, point)
			}
		}
	}
	return report, durable.CheckCrashReport(report)
}
