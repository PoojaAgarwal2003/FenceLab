package model

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/PoojaAgarwal2003/FenceLab/internal/sim"
)

type Bounds struct {
	MaxStates int `json:"max_states"`
	MaxDepth  int `json:"max_depth"`
}

func (b Bounds) Validate() error {
	if b.MaxStates < 1 || b.MaxStates > 10000 || b.MaxDepth < 1 || b.MaxDepth > 128 {
		return fmt.Errorf("max_states must be 1-10000 and max_depth 1-128")
	}
	return nil
}

type SearchRequest struct {
	Scenario Scenario `json:"scenario"`
	Bounds   Bounds   `json:"bounds"`
}

type SearchReport struct {
	Model          string      `json:"model"`
	Status         string      `json:"status"`
	Bounds         Bounds      `json:"bounds"`
	States         int         `json:"states"`
	Transitions    int         `json:"transitions"`
	Merged         int         `json:"merged"`
	TerminalStates int         `json:"terminal_states"`
	IncompleteJobs int         `json:"incomplete_jobs"`
	DepthReached   int         `json:"depth_reached"`
	DepthCutoffs   int         `json:"depth_cutoffs"`
	Witness        *ReplayFile `json:"witness"`
	Counterexample *Result     `json:"counterexample"`
	Minimality     string      `json:"minimality"`
	Scope          string      `json:"scope"`
}

// The hash excludes display traces, but includes all state that can affect
// enabled transitions, effects, and safety. IDs remain part of replay semantics.
func (m *machine) fingerprint() ([32]byte, error) {
	pending := slices.Clone(m.pending)
	slices.SortFunc(pending, func(a, b Event) int {
		if a.AtMS != b.AtMS {
			return a.AtMS - b.AtMS
		}
		return a.Message.ID - b.Message.ID
	})
	data, err := json.Marshal(struct {
		Now        int
		NextID     int
		Issued     int
		State      sim.State
		Started    [3]bool
		Dispatched [3]bool
		Acked      [3]bool
		Pending    []Event
		Effects    []sim.Effect
	}{m.now, m.nextID, m.issued, m.state, m.started, m.dispatched, m.acked, pending, m.result.Effects})
	return sha256.Sum256(data), err
}

type searchNode struct {
	machine  *machine
	parent   int
	decision int
	depth    int
}

// Search is BFS over equal-time deliveries and timers for ONE fixed scenario.
// A found prefix is shortest in event decisions; it does not minimize the fault
// file or prove safety outside this finite two-attempt model.
func Search(ctx context.Context, request SearchRequest) (SearchReport, error) {
	if err := request.Scenario.Validate(); err != nil {
		return SearchReport{}, err
	}
	if err := request.Bounds.Validate(); err != nil {
		return SearchReport{}, err
	}
	report := SearchReport{
		Model: Version, Bounds: request.Bounds, States: 1,
		Scope:      "Equal-time message/timer interleavings in one fixed, finite two-attempt scenario. Not arbitrary delays, crashes, consensus, or a distributed-systems proof.",
		Minimality: "No counterexample found.",
	}
	root := newMachine(request.Scenario, false)
	hash, err := root.fingerprint()
	if err != nil {
		return SearchReport{}, err
	}
	seen := map[[32]byte]bool{hash: true}
	nodes := []searchNode{{machine: root, parent: -1}}
	for head := 0; head < len(nodes); head++ {
		if err := ctx.Err(); err != nil {
			return SearchReport{}, err
		}
		node := nodes[head]
		if len(node.machine.pending) == 0 {
			report.TerminalStates++
			if !node.machine.state.Completed {
				report.IncompleteJobs++
			}
			continue
		}
		if node.depth == request.Bounds.MaxDepth {
			report.DepthCutoffs++
			continue
		}
		for _, decision := range node.machine.enabled() {
			if err := ctx.Err(); err != nil {
				return SearchReport{}, err
			}
			child := node.machine.clone()
			if err := child.advance(decision); err != nil {
				return SearchReport{}, err
			}
			report.Transitions++
			report.DepthReached = max(report.DepthReached, node.depth+1)
			if len(child.result.Violations) > 0 {
				decisions := []int{decision}
				for i := head; nodes[i].parent != -1; i = nodes[i].parent {
					decisions = append(decisions, nodes[i].decision)
				}
				slices.Reverse(decisions)
				replay := ReplayFile{Version: Version, Scenario: request.Scenario, Decisions: decisions}
				result, err := Replay(ctx, replay)
				if err != nil {
					return SearchReport{}, err
				}
				report.Status = "counterexample"
				report.Witness, report.Counterexample = &replay, &result
				report.Minimality = "Shortest violating event-decision prefix for this scenario (breadth-first search); no irrelevant suffix. Fault rules are not minimized."
				return report, nil
			}
			hash, err := child.fingerprint()
			if err != nil {
				return SearchReport{}, err
			}
			if seen[hash] {
				report.Merged++
				continue
			}
			if report.States == request.Bounds.MaxStates {
				report.Status = "state-limit"
				return report, nil
			}
			seen[hash] = true
			report.States++
			nodes = append(nodes, searchNode{machine: child, parent: head, decision: decision, depth: node.depth + 1})
		}
		// Parent links retain decisions, not full expanded machine snapshots.
		nodes[head].machine = nil
	}
	report.Status = "exhausted"
	if report.DepthCutoffs > 0 {
		report.Status = "depth-limit"
	}
	return report, nil
}
