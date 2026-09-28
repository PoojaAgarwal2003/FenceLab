package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestPhysicalLabs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	executable := filepath.Join(t.TempDir(), "fencelab.exe")
	t.Cleanup(func() {
		// Windows can retain an image handle briefly after a killed child exits.
		deadline := time.Now().Add(5 * time.Second)
		for {
			err := os.Remove(executable)
			if err == nil || os.IsNotExist(err) {
				return
			}
			if time.Now().After(deadline) {
				t.Errorf("remove child executable: %v", err)
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
	build := exec.CommandContext(ctx, "go", "build", "-o", executable, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	directory := filepath.Join(t.TempDir(), "crash-lab")
	report, err := runCrashLab(ctx, executable, directory)
	if err != nil || !report.Safe || len(report.Cases) != 15 {
		t.Fatalf("crash laboratory: %+v %v", report, err)
	}
	if _, err := runCrashLab(ctx, executable, directory); err == nil {
		t.Fatal("overwrote existing experiment directory")
	}
	var out, stderr bytes.Buffer
	if code := run(ctx, []string{"recover", "-wal", filepath.Join(directory, "write-after-sync.wal")}, &out, &stderr); code != 0 {
		t.Fatal(code, stderr.String())
	}
	var decoded map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
}

func TestDurabilityCLIValidation(t *testing.T) {
	for _, args := range [][]string{{"durability"}, {"recover"}, {"durability", "-dir", "x", "extra"}, {"wal-probe", "-wal", "x"}} {
		var out, stderr bytes.Buffer
		if code := run(context.Background(), args, &out, &stderr); code == 0 || stderr.Len() == 0 {
			t.Fatal(args, code, stderr.String())
		}
	}
	path := filepath.Join(t.TempDir(), "absent.wal")
	var out, stderr bytes.Buffer
	if code := run(context.Background(), []string{"recover", "-wal", path}, &out, &stderr); code == 0 {
		t.Fatal("accepted missing WAL")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("inspection created a WAL")
	}
	for _, command := range []string{"recover", "durability", "wal-probe"} {
		if code := run(context.Background(), []string{command, "-help"}, &out, &stderr); code != 0 {
			t.Fatal(command, code)
		}
	}
}
