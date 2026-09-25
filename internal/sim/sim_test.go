package sim

import (
	"container/heap"
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

func TestQueueStableAtEqualTime(t *testing.T) {
	q := eventQueue{}
	for _, e := range []event{{at: 4, sequence: 3}, {at: 2, sequence: 2}, {at: 2, sequence: 1}} {
		heap.Push(&q, e)
	}
	for _, want := range []int{1, 2, 3} {
		if got := heap.Pop(&q).(event).sequence; got != want {
			t.Fatalf("sequence = %d, want %d", got, want)
		}
	}
}

func TestKnownFailureMatrix(t *testing.T) {
	cases := []struct {
		scenario                                          Scenario
		policy                                            Policy
		writes, stale, duplicates, rejected, deduplicated int
	}{
		{Paused, LeaseOnly, 2, 1, 1, 0, 0},
		{Paused, Fenced, 1, 0, 0, 1, 0},
		{Paused, Idempotent, 1, 0, 0, 1, 0},
		{LostAck, LeaseOnly, 3, 1, 2, 0, 0},
		{LostAck, Fenced, 2, 0, 1, 1, 0},
		{LostAck, Idempotent, 1, 0, 0, 1, 1},
		{Healthy, LeaseOnly, 1, 0, 0, 0, 0},
		{Healthy, Fenced, 1, 0, 0, 0, 0},
		{Healthy, Idempotent, 1, 0, 0, 0, 0},
	}
	for _, tc := range cases {
		t.Run(string(tc.scenario)+"/"+string(tc.policy), func(t *testing.T) {
			c := DefaultConfig()
			c.Scenario, c.Policy = tc.scenario, tc.policy
			r, err := Run(c)
			if err != nil {
				t.Fatal(err)
			}
			want := Summary{
				AcceptedWrites: tc.writes, StaleWrites: tc.stale, DuplicateWrites: tc.duplicates,
				RejectedWrites: tc.rejected, Deduplicated: tc.deduplicated,
				Completed: true, Safe: tc.stale == 0 && tc.duplicates == 0,
			}
			if r.Summary != want {
				t.Fatalf("summary = %+v, want %+v", r.Summary, want)
			}
		})
	}
}

func TestReplayIsByteIdentical(t *testing.T) {
	for _, seed := range []int64{math.MinInt64, -1, 0, 7, math.MaxInt64} {
		c := DefaultConfig()
		c.Seed = seed
		first, err := Run(c)
		if err != nil {
			t.Fatal(err)
		}
		second, err := Run(c)
		if err != nil {
			t.Fatal(err)
		}
		a, _ := json.Marshal(first)
		b, _ := json.Marshal(second)
		if string(a) != string(b) {
			t.Fatalf("replay differs for seed %d", seed)
		}
	}
}

func TestSeededSchedulesAndIndependentEffectOracle(t *testing.T) {
	for seed := int64(0); seed < 1000; seed++ {
		for _, scenario := range []Scenario{Paused, LostAck, Healthy} {
			c := DefaultConfig()
			c.Seed, c.Scenario = seed, scenario
			c.LeaseMS = 20 + int(seed)%9981
			results, err := Compare(c)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range results {
				if r.Plan != results[0].Plan {
					t.Fatal("comparison changed failure schedule")
				}
				if r.Plan.ReturnAtMS <= c.LeaseMS || r.Plan.ReturnAtMS >= c.LeaseMS+r.Plan.SecondWorkMS {
					t.Fatal("old request must return after handoff but before replacement write")
				}
				stale := 0
				for _, effect := range r.Effects {
					if effect.Token < effect.CurrentEpoch {
						stale++
					}
				}
				duplicates := max(0, len(r.Effects)-1)
				if stale != r.Summary.StaleWrites || duplicates != r.Summary.DuplicateWrites {
					t.Fatal("effect oracle disagrees with reported safety")
				}
				if !r.Summary.Completed || (r.Config.Policy == Idempotent && (stale != 0 || duplicates != 0)) {
					t.Fatalf("seed %d/%s did not complete safely: %+v", seed, scenario, r.Summary)
				}
				last := -1
				for i, entry := range r.Trace {
					if entry.AtMS < last || entry.Step != i {
						t.Fatal("trace not ordered")
					}
					last = entry.AtMS
				}
				for _, v := range r.Violations {
					if r.Trace[v.Step].Action != "effect_committed" || r.Trace[v.Step].AtMS != v.AtMS {
						t.Fatal("violation lacks exact trace witness")
					}
				}
			}
		}
	}
}

func TestStorageBarrierPrecedesReplacementDispatch(t *testing.T) {
	r, err := Run(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	fences := map[int]int{}
	for _, entry := range r.Trace {
		switch entry.Action {
		case "fence_raised":
			fences[entry.Token] = entry.Step
		case "started":
			if fence, ok := fences[entry.Token]; !ok || fence >= entry.Step {
				t.Fatal("worker dispatched before storage acknowledged fence")
			}
		}
	}
}

func TestValidationAndNoSharedState(t *testing.T) {
	for _, edit := range []func(*Config){
		func(c *Config) { c.LeaseMS = 19 },
		func(c *Config) { c.LeaseMS = 10001 },
		func(c *Config) { c.Policy = "unknown" },
		func(c *Config) { c.Scenario = "unknown" },
	} {
		c := DefaultConfig()
		edit(&c)
		if _, err := Run(c); err == nil {
			t.Fatalf("invalid config accepted: %+v", c)
		}
		if _, err := Compare(c); err == nil {
			t.Fatal("comparison ignored invalid config")
		}
	}
	a, _ := Run(DefaultConfig())
	b, _ := Run(DefaultConfig())
	if !reflect.DeepEqual(a, b) {
		t.Fatal("independent runs differ")
	}
	a.Trace[0].Detail = "changed"
	if a.Trace[0].Detail == b.Trace[0].Detail {
		t.Fatal("runs share mutable storage")
	}
}

func BenchmarkCompare(b *testing.B) {
	for b.Loop() {
		if _, err := Compare(DefaultConfig()); err != nil {
			b.Fatal(err)
		}
	}
}
