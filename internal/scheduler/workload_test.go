package scheduler

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/PoojaAgarwal2003/FenceLab/internal/durable"
)

func testConfig() Config {
	return Config{Version: Version, Jobs: 8, Capacity: 8, Policy: Fair, MaxAttempts: 3, Fault: "none", ReopenAfter: 4}
}

func TestRealWorkloads(t *testing.T) {
	for _, fault := range []string{"none", "fail-before-once", "lost-ack-once", "lost-ack-always"} {
		t.Run(fault, func(t *testing.T) {
			c := testConfig()
			c.Fault = fault
			if fault != "none" {
				c.FaultEvery = 4
			}
			dir := filepath.Join(t.TempDir(), "run")
			r, err := Run(context.Background(), dir, c)
			if err != nil {
				t.Fatal(err)
			}
			wantCompleted, wantEffects := 8, 8
			if fault == "lost-ack-always" {
				wantCompleted = 6
			}
			if r.Counts.Completed != wantCompleted || r.Counts.Effects != wantEffects || !r.Recovery.Performed {
				t.Fatal(r.Counts, r.Recovery)
			}
			if fault == "lost-ack-once" && r.Counts.Deduplicated != 2 {
				t.Fatal(r.Counts)
			}
			if fault == "lost-ack-always" && (r.Counts.Exhausted != 2 || r.Counts.Retries != 4) {
				t.Fatal(r.Counts)
			}
			if err := Check(r); err != nil {
				t.Fatal(err)
			}
			log, _, err := durable.Open(filepath.Join(dir, "effects.wal"), nil)
			if err != nil {
				t.Fatal(err)
			}
			state := log.Snapshot()
			if err := log.Close(); err != nil {
				t.Fatal(err)
			}
			if state.Epoch != 2 || state.Fence != 2 || len(state.Effects) != wantEffects {
				t.Fatal(state)
			}
			if _, err := Run(context.Background(), dir, c); err == nil {
				t.Fatal("existing data overwritten")
			}
		})
	}
}

func TestOverloadAndUnreachedCheckpoint(t *testing.T) {
	c := testConfig()
	c.Capacity = 2
	c.ReopenAfter = 8
	r, err := Run(context.Background(), filepath.Join(t.TempDir(), "run"), c)
	if err != nil {
		t.Fatal(err)
	}
	if r.Counts.Admitted != 2 || r.Counts.Rejected != 6 || r.Counts.HighWater != 2 || r.Recovery.Performed {
		t.Fatal(r)
	}
}

func TestPacedOffersAndCancellation(t *testing.T) {
	c := testConfig()
	c.OfferEveryMS = 1
	c.ServiceMS = 1
	r, err := Run(context.Background(), filepath.Join(t.TempDir(), "run"), c)
	if err != nil {
		t.Fatal(err)
	}
	if r.ElapsedNS < int64(7*time.Millisecond) {
		t.Fatal("virtual instead of elapsed time", r.ElapsedNS)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	path := filepath.Join(t.TempDir(), "not-created")
	if _, err := Run(ctx, path, c); err == nil {
		t.Fatal("cancellation ignored")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("cancelled run created directory", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	c.ServiceMS = 20
	if _, err := Run(ctx, filepath.Join(t.TempDir(), "cancelled"), c); err == nil {
		t.Fatal("mid-run cancellation ignored")
	}
}

func TestReportRejectsTampering(t *testing.T) {
	base, err := Run(context.Background(), filepath.Join(t.TempDir(), "run"), testConfig())
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Report){
		"throughput":     func(r *Report) { r.Throughput++ },
		"count":          func(r *Report) { r.Counts.Effects++ },
		"latency":        func(r *Report) { r.Completion.P99NS++ },
		"missing-order":  func(r *Report) { r.Order = r.Order[:len(r.Order)-1] },
		"bad-order":      func(r *Report) { r.Order[8] = 8 },
		"key":            func(r *Report) { r.Jobs[0].Key = "other" },
		"fault":          func(r *Report) { r.Jobs[0].Outcomes[0] = "lost-ack" },
		"wait":           func(r *Report) { r.Jobs[0].MaxWait++ },
		"negative":       func(r *Report) { r.Jobs[0].DoneNS = -1 },
		"recovery":       func(r *Report) { r.Recovery.Records++ },
		"recovery-time":  func(r *Report) { r.Recovery.OpenNS = r.ElapsedNS + 1 },
		"fair-bound":     func(r *Report) { r.FairBound++ },
		"oversize-order": func(r *Report) { r.Order = make([]int, 1000) },
		"serial-timing":  func(r *Report) { r.Jobs[7].DecidedNS = r.ElapsedNS },
		"service-delay":  func(r *Report) { r.Config.ServiceMS = 20 },
	} {
		t.Run(name, func(t *testing.T) {
			data, _ := json.Marshal(base)
			var r Report
			if err := json.Unmarshal(data, &r); err != nil {
				t.Fatal(err)
			}
			change(&r)
			if err := Check(r); err == nil {
				t.Fatal("tampered report accepted")
			}
		})
	}
}

func TestMaximumReportFitsImportLimit(t *testing.T) {
	c := testConfig()
	c.Jobs = 128
	c.Capacity = 128
	c.MaxAttempts = 5
	c.FaultEvery = 1
	c.Fault = "lost-ack-always"
	r, err := Run(context.Background(), filepath.Join(t.TempDir(), "run"), c)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if len(data)+1 > 65536 {
		t.Fatalf("report %d bytes exceeds inspector limit", len(data)+1)
	}
	t.Logf("maximum report: %d bytes", len(data)+1)
	if r.Counts.Exhausted != 128 || r.Counts.Attempts != 640 || r.Counts.Effects != 128 {
		t.Fatal(r.Counts)
	}
}

func TestNearestRankDistribution(t *testing.T) {
	d := distribution([]int64{4, 1, 2, 3})
	if d != (Distribution{P50NS: 2, P95NS: 4, P99NS: 4, MaxNS: 4}) {
		t.Fatal(d)
	}

	if distribution(nil) != (Distribution{}) {
		t.Fatal("empty samples")
	}
}

func TestWorkloadConfigBounds(t *testing.T) {
	for _, change := range []func(*Config){
		func(c *Config) { c.Jobs = 129 },
		func(c *Config) { c.Jobs = 0 },
		func(c *Config) { c.OfferEveryMS = -1 },
		func(c *Config) { c.ServiceMS = 21 },
		func(c *Config) { c.FaultEvery = 1 },
		func(c *Config) { c.Fault = "unknown" },
		func(c *Config) { c.Version = "other" },
		func(c *Config) { c.ReopenAfter = 1000 },
	} {
		c := testConfig()
		change(&c)
		if err := c.Validate(); err == nil {
			t.Fatal("invalid config accepted", c)
		}
	}
}
