package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/PoojaAgarwal2003/FenceLab/internal/scheduler"
)

func TestWorkloadCommand(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.json")
	config := scheduler.Config{Version: scheduler.Version, Jobs: 4, Capacity: 2, Policy: scheduler.Fair, MaxAttempts: 2, Fault: "lost-ack-once", FaultEvery: 1, ReopenAfter: 1}
	data, _ := json.Marshal(config)
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(t.TempDir(), "run")
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"workload", "-file", file, "-dir", directory}, &out, &errOut)
	if code != 0 {
		t.Fatal(code, errOut.String())
	}
	var report scheduler.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Check(report); err != nil {
		t.Fatal(err)
	}
	if report.Counts.Rejected != 2 || report.Counts.Deduplicated != 2 {
		t.Fatal(report.Counts)
	}
	for _, args := range [][]string{{"workload"}, {"workload", "-unknown"}, {"workload", "-file", "missing", "-dir", directory}, {"workload", "-file", file, "-dir", directory, "extra"}} {
		out.Reset()
		errOut.Reset()
		if code := run(context.Background(), args, &out, &errOut); code != 2 {
			t.Fatal(args, code)
		}
	}
	if code := run(context.Background(), []string{"workload", "-file", file, "-dir", directory}, &out, &errOut); code != 1 {
		t.Fatal("overwrite", code)
	}
	if code := run(context.Background(), []string{"workload", "-help"}, &out, &errOut); code != 0 {
		t.Fatal("help", code)
	}
	if err := os.WriteFile(file, []byte(`{"version":"fencelab/workload-v1","jobs":8,"jobs":4}`), 0600); err != nil {
		t.Fatal(err)
	}
	if code := run(context.Background(), []string{"workload", "-file", file, "-dir", directory}, &out, &errOut); code != 2 {
		t.Fatal("duplicate config key", code)
	}
}
