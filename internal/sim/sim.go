// Package sim models one job, two workers, an authoritative lease issuer, and
// a side-effect store. Virtual milliseconds describe event order, not latency.
package sim

import (
	"container/heap"
	"fmt"
	"math/rand"
)

const ModelVersion = "fencelab/v1"

type Policy string

const (
	LeaseOnly  Policy = "lease-only"
	Fenced     Policy = "fenced"
	Idempotent Policy = "fenced-idempotent"
)

type Scenario string

const (
	Paused  Scenario = "paused-worker"
	LostAck Scenario = "lost-ack"
	Healthy Scenario = "healthy"
)

type Config struct {
	Seed     int64    `json:"seed"`
	Scenario Scenario `json:"scenario"`
	Policy   Policy   `json:"policy"`
	LeaseMS  int      `json:"lease_ms"`
}

func DefaultConfig() Config {
	return Config{Seed: 7, Scenario: Paused, Policy: Idempotent, LeaseMS: 100}
}

func (c Config) Validate() error {
	if c.Policy != LeaseOnly && c.Policy != Fenced && c.Policy != Idempotent {
		return fmt.Errorf("policy must be lease-only, fenced, or fenced-idempotent")
	}
	if c.Scenario != Paused && c.Scenario != LostAck && c.Scenario != Healthy {
		return fmt.Errorf("scenario must be paused-worker, lost-ack, or healthy")
	}
	if c.LeaseMS < 20 || c.LeaseMS > 10000 {
		return fmt.Errorf("lease_ms must be between 20 and 10000")
	}
	return nil
}

type Plan struct {
	FirstWorkMS  int `json:"first_work_ms"`
	SecondWorkMS int `json:"second_work_ms"`
	ReturnAtMS   int `json:"return_at_ms"`
}

type State struct {
	Epoch        int    `json:"epoch"`
	Owner        string `json:"owner"`
	StorageFence int    `json:"storage_fence"`
	Writes       int    `json:"writes"`
	Completed    bool   `json:"completed"`
}

type Entry struct {
	Step    int    `json:"step"`
	AtMS    int    `json:"at_ms"`
	Actor   string `json:"actor"`
	Action  string `json:"action"`
	Token   int    `json:"token"`
	Outcome string `json:"outcome"`
	Detail  string `json:"detail"`
	State   State  `json:"state"`
}

type Effect struct {
	AtMS         int    `json:"at_ms"`
	Worker       string `json:"worker"`
	Token        int    `json:"token"`
	CurrentEpoch int    `json:"current_epoch"`
	Key          string `json:"key"`
}

type Violation struct {
	Step   int    `json:"step"`
	AtMS   int    `json:"at_ms"`
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

type Summary struct {
	AcceptedWrites  int  `json:"accepted_writes"`
	StaleWrites     int  `json:"stale_writes"`
	DuplicateWrites int  `json:"duplicate_writes"`
	RejectedWrites  int  `json:"rejected_writes"`
	Deduplicated    int  `json:"deduplicated"`
	Completed       bool `json:"completed"`
	Safe            bool `json:"safe"`
}

type Result struct {
	Model      string      `json:"model"`
	Config     Config      `json:"config"`
	Plan       Plan        `json:"plan"`
	Trace      []Entry     `json:"trace"`
	Effects    []Effect    `json:"effects"`
	Violations []Violation `json:"violations"`
	Summary    Summary     `json:"summary"`
}

type simulation struct {
	result   Result
	queue    eventQueue
	sequence int
	now      int
	state    State
}

// Run is isolated and deterministic. There are no sleeps, wall-clock reads,
// goroutines, network calls, or process-global random generators in the model.
func Run(c Config) (Result, error) {
	if err := c.Validate(); err != nil {
		return Result{}, err
	}
	random := rand.New(rand.NewSource(c.Seed))
	first := c.LeaseMS/4 + random.Intn(c.LeaseMS/4)
	second := c.LeaseMS/4 + random.Intn(c.LeaseMS/4)
	plan := Plan{
		FirstWorkMS: first, SecondWorkMS: second,
		ReturnAtMS: c.LeaseMS + 1 + random.Intn(second-1),
	}
	s := simulation{result: Result{
		Model: ModelVersion, Config: c, Plan: plan,
		Trace: []Entry{}, Effects: []Effect{}, Violations: []Violation{},
	}}
	s.schedule(0, "claim", 0, "worker-a")
	for s.queue.Len() > 0 {
		e := heap.Pop(&s.queue).(event)
		s.now = e.at
		switch e.kind {
		case "claim":
			s.claim(e.worker)
		case "pause":
			s.record(e.worker, "paused", e.token, "fault",
				"Worker A stops before writing. Its process still holds its original token.")
		case "expire":
			if !s.state.Completed && e.token == s.state.Epoch {
				s.record("authority", "lease_expired", e.token, "fault",
					"The lease expires; this does not kill the old worker or cancel its future write.")
				s.claim("worker-b")
			}
		case "write", "retry":
			s.write(e)
		default:
			return Result{}, fmt.Errorf("unknown internal event %q", e.kind)
		}
	}
	s.result.Summary.AcceptedWrites = len(s.result.Effects)
	s.result.Summary.Completed = s.state.Completed
	s.result.Summary.Safe = len(s.result.Violations) == 0
	return s.result, nil
}

func (s *simulation) record(actor, action string, token int, outcome, detail string) {
	s.result.Trace = append(s.result.Trace, Entry{
		Step: len(s.result.Trace), AtMS: s.now, Actor: actor, Action: action,
		Token: token, Outcome: outcome, Detail: detail, State: s.state,
	})
}

func (s *simulation) claim(worker string) {
	s.state.Epoch++
	s.state.Owner = worker
	token := s.state.Epoch
	s.record("authority", "lease_granted", token, "info",
		fmt.Sprintf("%s receives epoch %d; its lease expires at virtual t=%d.", worker, token, s.now+s.result.Config.LeaseMS))
	if s.result.Config.Policy != LeaseOnly {
		// The model requires a durable, acknowledged storage barrier before
		// dispatch. This is an explicit protocol assumption, not free atomicity
		// between a real distributed scheduler and an independent database.
		s.state.StorageFence = token
		s.record("store", "fence_raised", token, "info",
			"Storage acknowledges the new minimum token before the worker is dispatched.")
	}
	s.record(worker, "started", token, "info", "Execute logical job invoice-001; the effect key is stable across attempts.")
	s.schedule(s.now+s.result.Config.LeaseMS, "expire", token, worker)
	work := s.result.Plan.SecondWorkMS
	if token == 1 {
		work = s.result.Plan.FirstWorkMS
		if s.result.Config.Scenario == Paused {
			s.schedule(work/2, "pause", token, worker)
			s.schedule(s.result.Plan.ReturnAtMS, "write", token, worker)
			return
		}
	}
	s.schedule(s.now+work, "write", token, worker)
}

func (s *simulation) write(e event) {
	c := s.result.Config
	if c.Scenario == Paused && e.token == 1 {
		s.record(e.worker, "resumed", e.token, "fault", "The old process resumes before Worker B writes. It does not know its lease expired.")
	}
	action := "write_requested"
	if e.kind == "retry" {
		action = "retry_requested"
	}
	s.record(e.worker, action, e.token, "info", "Send the side effect to storage using the original attempt token.")
	if c.Policy != LeaseOnly && e.token < s.state.StorageFence {
		s.result.Summary.RejectedWrites++
		s.record("store", "stale_rejected", e.token, "rejected", "The storage barrier rejects this obsolete token before any side effect.")
		return
	}
	if c.Policy == Idempotent && len(s.result.Effects) > 0 {
		s.result.Summary.Deduplicated++
		s.record("store", "effect_deduplicated", e.token, "deduplicated", "The job's durable effect key already exists; return the original result without repeating the effect.")
		s.acknowledge(e)
		return
	}
	s.result.Effects = append(s.result.Effects, Effect{
		AtMS: s.now, Worker: e.worker, Token: e.token,
		CurrentEpoch: s.state.Epoch, Key: "invoice-001",
	})
	s.state.Writes++
	stale, duplicate := e.token < s.state.Epoch, s.state.Writes > 1
	outcome := "accepted"
	if stale || duplicate {
		outcome = "violation"
	}
	s.record("store", "effect_committed", e.token, outcome, "Storage commits the side effect. An acknowledgment is a separate event.")
	step := len(s.result.Trace) - 1
	if stale {
		s.result.Summary.StaleWrites++
		s.result.Violations = append(s.result.Violations, Violation{
			Step: step, AtMS: s.now, Kind: "stale-write",
			Detail: "A superseded attempt committed after a newer ownership epoch was issued.",
		})
	}
	if duplicate {
		s.result.Summary.DuplicateWrites++
		s.result.Violations = append(s.result.Violations, Violation{
			Step: step, AtMS: s.now, Kind: "duplicate-effect",
			Detail: "The same logical effect was committed more than once.",
		})
	}
	if c.Scenario == LostAck && e.token == 1 && e.kind != "retry" {
		s.record("network", "ack_lost", e.token, "fault", "The write committed, but its acknowledgment was lost. Neither worker nor scheduler can infer the outcome.")
		s.schedule(s.result.Plan.ReturnAtMS, "retry", e.token, e.worker)
		return
	}
	s.acknowledge(e)
}

func (s *simulation) acknowledge(e event) {
	if e.token != s.state.Epoch {
		s.record("authority", "stale_ack_ignored", e.token, "rejected",
			"The scheduler ignores the obsolete acknowledgment, but cannot undo a committed side effect.")
		return
	}
	s.state.Completed = true
	s.record("authority", "job_completed", e.token, "info", "The current owner reports success; no further attempt is needed.")
}

// Compare uses the same seeded plan for every policy, rather than changing
// the failure schedule to make the protected policy look better.
func Compare(c Config) ([]Result, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	results := make([]Result, 0, 3)
	for _, policy := range []Policy{LeaseOnly, Fenced, Idempotent} {
		c.Policy = policy
		result, err := Run(c)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}
