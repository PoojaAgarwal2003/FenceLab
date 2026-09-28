package model

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/PoojaAgarwal2003/FenceLab/internal/sim"
)

func TestTransportExamplesAndReplay(t *testing.T) {
	for name, scenario := range Examples() {
		t.Run(name, func(t *testing.T) {
			r, err := Run(context.Background(), scenario)
			if err != nil {
				t.Fatal(err)
			}
			if r.Summary.Safe != (name != "eager") || !r.Summary.Completed || r.HaltReason != "quiescent" {
				t.Fatalf("unexpected outcome: %+v; halt=%s", r.Summary, r.HaltReason)
			}
			replayed, err := Replay(context.Background(), r.Replay)
			if err != nil || !reflect.DeepEqual(r, replayed) {
				t.Fatalf("exact replay differs: %v", err)
			}
			stale, duplicates := 0, 0
			keys := map[string]bool{}
			for _, e := range r.Effects {
				if e.Token < e.CurrentEpoch {
					stale++
				}
				if keys[e.Key] {
					duplicates++
				}
				keys[e.Key] = true
			}
			if stale != r.Summary.StaleWrites || duplicates != r.Summary.DuplicateWrites {
				t.Fatal("independent effect oracle differs")
			}
			for _, v := range r.Violations {
				if r.Trace[v.Step].Action != "effect_committed" || r.Trace[v.Step].AtMS != v.AtMS {
					t.Fatal("violation lacks an exact commit witness")
				}
			}
			for i, entry := range r.Trace {
				if i > 0 && entry.AtMS < r.Trace[i-1].AtMS {
					t.Fatal("virtual time went backwards")
				}
				if scenario.Protocol == "barrier" && entry.Action == "ownership_activated" && entry.State.StorageFence < entry.Token {
					t.Fatal("ownership activated before the fence")
				}
			}
		})
	}
}

func TestDelayPartitionDropAndDuplicate(t *testing.T) {
	s := Examples()["partition"]
	r, err := Run(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	held, healed := false, false
	for _, e := range r.Trace {
		held = held || e.Action == "partition_held"
		healed = healed || e.Action == "heal"
		if e.Action == "ownership_activated" && e.Token == 1 && e.AtMS < 40 {
			t.Fatal("partition allowed early acknowledgment")
		}
	}
	if !held || !healed {
		t.Fatal("partition/heal must be visible")
	}
	s = Examples()["lost-result"]
	r, _ = Run(context.Background(), s)
	if r.Summary.Deduplicated < 1 || r.Summary.AcceptedWrites != 1 {
		t.Fatal("duplicates were not idempotent")
	}
	s.Policy = sim.Fenced
	r, _ = Run(context.Background(), s)
	if r.Summary.DuplicateWrites == 0 {
		t.Fatal("fencing alone unexpectedly deduplicated writes")
	}
	s = Example("barrier")
	s.Faults = []Fault{{Kind: "fence-ack", Token: 1, Action: "drop"}}
	r, _ = Run(context.Background(), s)
	if r.Summary.Completed || !r.Summary.Safe || r.Summary.AcceptedWrites != 0 {
		t.Fatal("lost barrier acknowledgment must stall, not dispatch")
	}
}

func TestLateFencesNeverRegressAndClocksNeverExpireLeases(t *testing.T) {
	s := Example("eager")
	s.Faults = []Fault{{Kind: "fence", Token: 1, Action: "delay", DelayMS: 80}, {Kind: "result", Token: 1, Action: "drop"}}
	r, _ := Run(context.Background(), s)
	fence := 0
	for _, e := range r.Trace {
		if e.State.StorageFence < fence {
			t.Fatal("fence decreased")
		}
		fence = e.State.StorageFence
	}
	s = Example("barrier")
	baseline, _ := Run(context.Background(), s)
	s.ClockSkewMS = [2]int{-10000, 10000}
	skewed, _ := Run(context.Background(), s)
	if !reflect.DeepEqual(baseline.Effects, skewed.Effects) || baseline.Summary != skewed.Summary || !reflect.DeepEqual(baseline.Replay.Decisions, skewed.Replay.Decisions) {
		t.Fatal("worker clocks changed authoritative scheduling")
	}
}

func TestSearchShortestWitnessBoundsAndReplay(t *testing.T) {
	request := SearchRequest{Scenario: Example("eager"), Bounds: Bounds{MaxStates: 5000, MaxDepth: 80}}
	report, err := Search(context.Background(), request)
	if err != nil || report.Status != "counterexample" || report.Counterexample.Summary.Safe {
		t.Fatalf("missing counterexample: %+v, %v", report, err)
	}
	replayed, err := Replay(context.Background(), *report.Witness)
	if err != nil || !reflect.DeepEqual(replayed, *report.Counterexample) {
		t.Fatalf("counterexample replay differs: %v", err)
	}
	for i := 0; i < len(report.Witness.Decisions); i++ {
		short := *report.Witness
		short.Decisions = short.Decisions[:i]
		r, err := Replay(context.Background(), short)
		if err != nil || !r.Summary.Safe {
			t.Fatal("witness contains an unnecessary suffix")
		}
	}
	request.Bounds.MaxDepth = len(report.Witness.Decisions) - 1
	shorter, err := Search(context.Background(), request)
	if err != nil || shorter.Status != "depth-limit" || shorter.Witness != nil {
		t.Fatal("a shorter counterexample exists or depth cutoff was hidden")
	}
	request.Bounds = Bounds{MaxStates: 1, MaxDepth: 80}
	limited, _ := Search(context.Background(), request)
	if limited.Status != "state-limit" || limited.States != 1 {
		t.Fatal("state limit was hidden")
	}
	request.Scenario = Example("barrier")
	request.Bounds = Bounds{MaxStates: 5000, MaxDepth: 80}
	safe, err := Search(context.Background(), request)
	if err != nil || safe.Status != "exhausted" || safe.IncompleteJobs != 0 || safe.Witness != nil {
		t.Fatalf("barrier scenario not exhausted safely: %+v, %v", safe, err)
	}
	t.Logf("eager: %d states, depth %d; barrier: %d states, %d merges", report.States, len(report.Witness.Decisions), safe.States, safe.Merged)
}

func TestSearchMatchesIndependentUnmergedEnumeration(t *testing.T) {
	for _, protocol := range []string{"eager", "barrier"} {
		s := Example(protocol)
		frontier := []*machine{newMachine(s, false)}
		firstBadDepth := 0
		for depth := 1; depth <= 30 && len(frontier) > 0 && firstBadDepth == 0; depth++ {
			next := []*machine{}
			for _, m := range frontier {
				for _, id := range m.enabled() {
					n := m.clone()
					if err := n.advance(id); err != nil {
						t.Fatal(err)
					}
					if len(n.result.Violations) > 0 {
						firstBadDepth = depth
						break
					}
					next = append(next, n)
				}
			}
			frontier = next
			if len(frontier) > 100000 {
				t.Fatal("reference enumeration unexpectedly large")
			}
		}
		r, err := Search(context.Background(), SearchRequest{Scenario: s, Bounds: Bounds{MaxStates: 5000, MaxDepth: 30}})
		if err != nil {
			t.Fatal(err)
		}
		if firstBadDepth == 0 && r.Witness != nil || firstBadDepth != 0 && (r.Witness == nil || len(r.Witness.Decisions) != firstBadDepth) {
			t.Fatal("state merging changed counterexample existence or shortest depth")
		}
	}
}

func TestEnabledChoicesAndFingerprint(t *testing.T) {
	m := newMachine(Example("eager"), false)
	before, _ := m.fingerprint()
	n := m.clone()
	n.pending[0].AtMS++
	after, _ := n.fingerprint()
	if before == after {
		t.Fatal("delivery time omitted from hash")
	}
	for _, e := range m.pending {
		if e.AtMS > m.pending[0].AtMS {
			if err := m.advance(e.Message.ID); err == nil {
				t.Fatal("future event was enabled early")
			}
			break
		}
	}
	r, _ := Run(context.Background(), Example("eager"))
	r.Replay.Decisions[0] = 999
	if _, err := Replay(context.Background(), r.Replay); err == nil {
		t.Fatal("invalid replay decision accepted")
	}
}

func TestStrictJSONAndValidation(t *testing.T) {
	data, _ := json.Marshal(Example("barrier"))
	for _, invalid := range []string{
		string(data) + "{}",
		strings.Replace(string(data), `"name":`, `"unknown":`, 1),
		strings.Replace(string(data), `"name":`, `"version":"fencelab/v2","name":`, 1),
		strings.Replace(string(data), `"kind":"write"`, `"kind":"write","kind":"fence"`, 1),
		strings.Replace(string(data), `"clock_skew_ms":[0,0]`, `"clock_skew_ms":[0,0,1]`, 1),
		strings.Repeat(" ", MaxInput+1),
	} {
		var s Scenario
		if err := Decode(strings.NewReader(invalid), &s); err == nil {
			t.Fatal("invalid JSON accepted")
		}

	}
	var s Scenario
	if err := Decode(bytes.NewReader(data), &s); err != nil || s.Validate() != nil {
		t.Fatal("valid scenario rejected")
	}
	for _, modify := range []func(*Scenario){
		func(s *Scenario) { s.Version = "v1" },
		func(s *Scenario) { s.Protocol = "magic" },
		func(s *Scenario) { s.LeaseMS = 0 },
		func(s *Scenario) { s.WorkMS = s.LeaseMS },
		func(s *Scenario) { s.LatencyMS = -1 },
		func(s *Scenario) { s.Faults[0].DelayMS = -1 },
		func(s *Scenario) { s.Faults[0].Kind = "unknown" },
		func(s *Scenario) { s.Partitions = []Partition{{From: "store", To: "authority", StartMS: 10, EndMS: 1}} },
	} {
		s := Example("barrier")
		modify(&s)
		if err := s.Validate(); err == nil {
			t.Fatal("invalid scenario accepted")
		}
	}
}

func TestMessageOrderChangesOutcomeAndEquivalentStatesMerge(t *testing.T) {
	m := newMachine(Example("eager"), true)
	for len(m.pending) > 0 {
		choices := m.enabled()
		if err := m.advance(choices[len(choices)-1]); err != nil {
			t.Fatal(err)
		}
	}
	if !m.finish("quiescent").Summary.Safe {
		t.Fatal("new owner winning the same-time write race should deduplicate the old request")
	}
	s := Example("barrier")
	s.Faults = []Fault{{Kind: "dispatch", Token: 1, Action: "duplicate"}}
	report, err := Search(context.Background(), SearchRequest{Scenario: s, Bounds: Bounds{5000, 80}})
	if err != nil || report.Status != "exhausted" || report.Merged == 0 {
		t.Fatalf("equivalent states did not merge: %+v; %v", report, err)
	}
	s.Faults = []Fault{{Kind: "fence-ack", Token: 1, Action: "drop"}}
	report, err = Search(context.Background(), SearchRequest{Scenario: s, Bounds: Bounds{5000, 80}})
	if err != nil || report.Status != "exhausted" || report.IncompleteJobs == 0 {
		t.Fatal("quiescent stalled job must not be reported as completed")
	}
}

func TestCanceledOperations(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := Example("barrier")
	if _, err := Run(ctx, s); err != context.Canceled {
		t.Fatal("run ignored cancellation")
	}
	if _, err := Search(ctx, SearchRequest{Scenario: s, Bounds: Bounds{100, 20}}); err != context.Canceled {
		t.Fatal("search ignored cancellation")
	}
	r, _ := Run(context.Background(), s)
	if _, err := Replay(ctx, r.Replay); err != context.Canceled {
		t.Fatal("replay ignored cancellation")
	}
}
