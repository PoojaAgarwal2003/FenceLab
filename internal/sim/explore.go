package sim

import (
	"context"
	"fmt"
	"math"
)

type Witness struct {
	Config     Config      `json:"config"`
	Violations []Violation `json:"violations"`
}

type Exploration struct {
	Model              string   `json:"model"`
	StartSeed          int64    `json:"start_seed"`
	Seeds              int      `json:"seeds"`
	Runs               int      `json:"runs"`
	ProtectedFailures  int      `json:"protected_failures"`
	ExpectedUnsafeRuns int      `json:"expected_unsafe_runs"`
	UnexpectedOutcomes int      `json:"unexpected_outcomes"`
	FirstUnsafeWitness *Witness `json:"first_unsafe_witness"`
	FirstUnexpected    *Witness `json:"first_unexpected"`
	Scope              string   `json:"scope"`
}

// Explore samples seeded timings within three fixed scenario templates.
// It is regression exploration, not exhaustive model checking or a proof.
func Explore(ctx context.Context, start int64, seeds, leaseMS int) (Exploration, error) {
	if seeds < 1 || seeds > 10000 {
		return Exploration{}, fmt.Errorf("seeds must be between 1 and 10000")
	}
	if start > math.MaxInt64-int64(seeds-1) {
		return Exploration{}, fmt.Errorf("seed range overflows int64")
	}
	c := DefaultConfig()
	c.LeaseMS = leaseMS
	if err := c.Validate(); err != nil {
		return Exploration{}, err
	}
	report := Exploration{
		Model: ModelVersion, StartSeed: start, Seeds: seeds,
		Scope: "Seeded timings in three fixed templates; not exhaustive interleaving exploration or a distributed-systems proof.",
	}
	for offset := 0; offset < seeds; offset++ {
		c.Seed = start + int64(offset)
		for _, scenario := range []Scenario{Paused, LostAck, Healthy} {
			if err := ctx.Err(); err != nil {
				return Exploration{}, err
			}
			c.Scenario = scenario
			results, err := Compare(c)
			if err != nil {
				return Exploration{}, err
			}
			for _, r := range results {
				report.Runs++
				wantSafe := scenario == Healthy || r.Config.Policy == Idempotent ||
					(scenario == Paused && r.Config.Policy == Fenced)
				if !r.Summary.Safe && r.Config.Policy == Idempotent {
					report.ProtectedFailures++
				}
				if !wantSafe && !r.Summary.Safe {
					report.ExpectedUnsafeRuns++
					if report.FirstUnsafeWitness == nil {
						report.FirstUnsafeWitness = &Witness{Config: r.Config, Violations: r.Violations}
					}
				}
				if r.Summary.Safe != wantSafe || !r.Summary.Completed {
					report.UnexpectedOutcomes++
					if report.FirstUnexpected == nil {
						report.FirstUnexpected = &Witness{Config: r.Config, Violations: r.Violations}
					}
				}
			}
		}
	}
	return report, nil
}
