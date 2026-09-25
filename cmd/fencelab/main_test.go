package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/PoojaAgarwal2003/FenceLab/internal/sim"
)

func TestCLICompareAndReplay(t *testing.T) {
	var out, stderr bytes.Buffer
	if code := run(context.Background(), []string{"compare", "-seed", "42", "-scenario", "lost-ack"}, &out, &stderr); code != 0 {
		t.Fatalf("code %d: %s", code, &stderr)
	}
	var results []sim.Result
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 || results[0].Summary.Safe || results[1].Summary.Safe || !results[2].Summary.Safe {
		t.Fatal("comparison does not expose fencing's duplicate-effect limit")
	}
	out.Reset()
	if code := run(context.Background(), []string{"run", "-policy", "lease-only"}, &out, &stderr); code != 0 {
		t.Fatal("an intentional unsafe demonstration is not a CLI failure")
	}
	var replay sim.Result
	if err := json.Unmarshal(out.Bytes(), &replay); err != nil || replay.Summary.Safe {
		t.Fatal("run output did not preserve counterexample")
	}
}

func TestCLIHelpAndErrors(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
	}{
		{nil, 0}, {[]string{"-help"}, 0}, {[]string{"run", "-help"}, 0},
		{[]string{"unknown"}, 2}, {[]string{"run", "-policy", ""}, 2},
		{[]string{"compare", "-scenario", ""}, 2}, {[]string{"run", "-lease-ms", "0"}, 2},
		{[]string{"run", "extra"}, 2}, {[]string{"check", "-seeds", "0"}, 2},
		{[]string{"check", "-seeds", "5"}, 0}, {[]string{"run", "-bad"}, 2},
	} {
		var out, stderr bytes.Buffer
		if code := run(context.Background(), tc.args, &out, &stderr); code != tc.code {
			t.Fatalf("%v: exit %d want %d; %s", tc.args, code, tc.code, &stderr)
		}
		if tc.code != 0 && stderr.Len() == 0 {
			t.Fatal("error was silently discarded")
		}
		if tc.code != 0 && out.Len() > 0 {
			t.Fatal("failure produced success-shaped JSON")
		}
	}
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestCLIWriteFailure(t *testing.T) {
	var stderr bytes.Buffer
	if code := run(context.Background(), []string{"run"}, failedWriter{}, &stderr); code != 1 {
		t.Fatalf("write failure exit %d", code)
	}
}
