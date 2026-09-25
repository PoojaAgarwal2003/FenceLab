package sim

import (
	"context"
	"errors"
	"math"
	"testing"
)

func TestExploreReportsIntentionalCounterexamples(t *testing.T) {
	report, err := Explore(context.Background(), -2, 10, 100)
	if err != nil {
		t.Fatal(err)
	}
	if report.Runs != 90 || report.ExpectedUnsafeRuns != 30 ||
		report.UnexpectedOutcomes != 0 || report.ProtectedFailures != 0 {
		t.Fatalf("unexpected exploration: %+v", report)
	}
	if report.FirstUnsafeWitness == nil || report.FirstUnsafeWitness.Config.Seed != -2 {
		t.Fatal("missing reproducible witness")
	}
	replay, err := Run(report.FirstUnsafeWitness.Config)
	if err != nil || replay.Summary.Safe {
		t.Fatal("witness does not reproduce")
	}
}

func TestExploreRejectsInvalidRangesAndHonorsCancellation(t *testing.T) {
	for _, tc := range []struct {
		start int64
		count int
		lease int
	}{{0, 0, 100}, {0, 10001, 100}, {math.MaxInt64, 2, 100}, {0, 1, 19}} {
		if _, err := Explore(context.Background(), tc.start, tc.count, tc.lease); err == nil {
			t.Fatalf("invalid exploration accepted: %+v", tc)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Explore(ctx, 0, 1, 100); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}
