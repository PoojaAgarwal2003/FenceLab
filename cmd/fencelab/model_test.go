package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/PoojaAgarwal2003/FenceLab/internal/model"
)

func scenarioFile(t *testing.T) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "scenario.json")
	data, err := json.Marshal(model.Example("eager"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0600); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestModelCLIEndToEnd(t *testing.T) {
	file := scenarioFile(t)
	witness := filepath.Join(t.TempDir(), "witness.json")
	var stdout, stderr bytes.Buffer
	args := []string{"search", "-file", file, "-witness", witness}
	if code := run(context.Background(), args, &stdout, &stderr); code != 0 {
		t.Fatalf("search: %d %s", code, &stderr)
	}
	var report model.SearchReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil || report.Status != "counterexample" {
		t.Fatalf("invalid search result: %s", &stdout)
	}
	stdout.Reset()
	if code := run(context.Background(), []string{"replay", "-file", witness}, &stdout, &stderr); code != 0 {
		t.Fatalf("replay: %d %s", code, &stderr)
	}
	var result model.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result.Summary.Safe || result.HaltReason != "prefix" {
		t.Fatal("CLI witness did not reproduce its violation")
	}
	original, _ := os.ReadFile(witness)
	stdout.Reset()
	if code := run(context.Background(), args, &stdout, &stderr); code != 1 {
		t.Fatal("existing witness silently overwritten")
	}
	after, _ := os.ReadFile(witness)
	if !bytes.Equal(original, after) {
		t.Fatal("witness changed")
	}
}

func TestModelCLIContracts(t *testing.T) {
	file := scenarioFile(t)
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"network", "-file", file}, 0},
		{[]string{"network", "-help"}, 0},
		{[]string{"network"}, 2},
		{[]string{"network", "-file", file, "extra"}, 2},
		{[]string{"search", "-file", file, "-max-depth", "1"}, 3},
		{[]string{"search", "-file", file, "-max-states", "1"}, 3},
		{[]string{"search", "-file", file, "-max-states", "10001"}, 2},
		{[]string{"replay", "-file", file}, 2},
	} {
		var out, stderr bytes.Buffer
		if code := run(context.Background(), tc.args, &out, &stderr); code != tc.code {
			t.Fatalf("%v: code %d, want %d: %s", tc.args, code, tc.code, &stderr)
		}
		if tc.code == 2 && (stderr.Len() == 0 || out.Len() != 0) {
			t.Fatal("invalid command must produce an explicit error, not result JSON")
		}
	}
	var stderr bytes.Buffer
	if code := run(context.Background(), []string{"network", "-file", file}, failedWriter{}, &stderr); code != 1 {
		t.Fatal("output failure not surfaced")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := run(ctx, []string{"search", "-file", file}, &bytes.Buffer{}, &stderr); code != 1 {
		t.Fatal("search cancellation not surfaced")
	}
}
