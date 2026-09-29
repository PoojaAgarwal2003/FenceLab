package scheduler

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"github.com/PoojaAgarwal2003/FenceLab/internal/durable"
	"github.com/PoojaAgarwal2003/FenceLab/internal/sim"
)

const Version = "fencelab/workload-v1"

type Config struct {
	Version      string `json:"version"`
	Jobs         int    `json:"jobs"`
	Capacity     int    `json:"capacity"`
	Policy       Policy `json:"policy"`
	MaxAttempts  int    `json:"max_attempts"`
	OfferEveryMS int    `json:"offer_every_ms"`
	ServiceMS    int    `json:"service_ms"`
	FaultEvery   int    `json:"fault_every"`
	Fault        string `json:"fault"`
	ReopenAfter  int    `json:"reopen_after"`
}

func (c Config) Validate() error {
	if _, err := New(c.Capacity, c.MaxAttempts, c.Policy); err != nil {
		return err
	}
	if c.Version != Version || c.Jobs < 1 || c.Jobs > MaxJobs ||
		c.OfferEveryMS < 0 || c.OfferEveryMS > 10 || c.ServiceMS < 0 || c.ServiceMS > 20 ||
		c.FaultEvery < 0 || c.FaultEvery > c.Jobs || c.ReopenAfter < 0 || c.ReopenAfter > c.Jobs*c.MaxAttempts {
		return fmt.Errorf("invalid workload bounds or version")
	}
	if c.Fault != "none" && c.Fault != "fail-before-once" && c.Fault != "lost-ack-once" && c.Fault != "lost-ack-always" {
		return fmt.Errorf("unknown workload fault")
	}
	if (c.Fault == "none") != (c.FaultEvery == 0) {
		return fmt.Errorf("fault and fault_every must both be enabled or disabled")
	}
	return nil
}

type JobResult struct {
	Key       string   `json:"key"`
	Class     int      `json:"class"`
	OfferedNS int64    `json:"offered_ns"`
	DecidedNS int64    `json:"decided_ns"`
	FirstNS   int64    `json:"first_ns"`
	DoneNS    int64    `json:"done_ns"`
	Status    string   `json:"status"`
	MaxWait   int      `json:"max_wait_dispatches"`
	Outcomes  []string `json:"outcomes"`
}

type Distribution struct {
	P50NS int64 `json:"p50_ns"`
	P95NS int64 `json:"p95_ns"`
	P99NS int64 `json:"p99_ns"`
	MaxNS int64 `json:"max_ns"`
}

type Counts struct {
	Admitted     int `json:"admitted"`
	Rejected     int `json:"rejected"`
	Completed    int `json:"completed"`
	Exhausted    int `json:"exhausted"`
	Attempts     int `json:"attempts"`
	Retries      int `json:"retries"`
	Effects      int `json:"effects"`
	Deduplicated int `json:"deduplicated"`
	FailedBefore int `json:"failed_before"`
	LostAcks     int `json:"lost_acks"`
	HighWater    int `json:"high_water"`
}

type RecoveryCost struct {
	Performed bool   `json:"performed"`
	After     int    `json:"after_attempt"`
	Records   uint64 `json:"records"`
	Effects   int    `json:"effects"`
	OpenNS    int64  `json:"open_ns"`
	BarrierNS int64  `json:"barrier_ns"`
	NextToken int    `json:"next_token"`
}

type Report struct {
	Version     string       `json:"version"`
	Config      Config       `json:"config"`
	Environment string       `json:"environment"`
	ElapsedNS   int64        `json:"elapsed_ns"`
	Throughput  float64      `json:"acknowledged_jobs_per_second"`
	Counts      Counts       `json:"counts"`
	Completion  Distribution `json:"completion_latency"`
	InitialWait Distribution `json:"initial_queue_wait"`
	ClassWait   [2]int       `json:"class_max_wait_dispatches"`
	FairBound   int          `json:"fair_wait_bound"`
	Recovery    RecoveryCost `json:"recovery"`
	Jobs        []JobResult  `json:"jobs"`
	// Order encodes admission as -(index+1), dispatch as index+1. Jobs carry
	// attempt outcomes. This compact journal allows exact selection validation.
	Order []int `json:"order"`
}

func jobAt(index int) Job {
	class := 0
	if index%4 == 3 {
		class = 1
	}
	return Job{Key: fmt.Sprintf("invoice-%03d", index+1), Class: class}
}

func waitUntil(ctx context.Context, at time.Time) error {
	duration := time.Until(at)
	if duration <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Run measures one real worker performing synchronous WAL effects. Arrival
// deadlines are open-loop, but admission is polled between nonpreemptive attempts.
// The declared offered timestamp includes that polling lag in completion latency.
func Run(ctx context.Context, directory string, c Config) (report Report, err error) {
	if err := c.Validate(); err != nil {
		return report, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return report, err
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		return report, fmt.Errorf("create new workload directory: %w", err)
	}
	path := filepath.Join(directory, "effects.wal")
	log, _, err := durable.Open(path, nil)
	if err != nil {
		return report, err
	}
	defer func() {
		if log != nil {
			err = errors.Join(err, log.Close())
		}
	}()
	token, err := log.Reserve()
	if err != nil {
		return report, err
	}
	if err := log.Fence(token); err != nil {
		return report, err
	}
	q, _ := New(c.Capacity, c.MaxAttempts, c.Policy)
	report = Report{Version: Version, Config: c, Environment: runtime.GOOS + "/" + runtime.GOARCH + " " + runtime.Version(), FairBound: q.WaitBound(), Jobs: make([]JobResult, c.Jobs), Order: []int{}}
	for i := range report.Jobs {
		job := jobAt(i)
		report.Jobs[i] = JobResult{Key: job.Key, Class: job.Class, OfferedNS: int64(i) * int64(c.OfferEveryMS) * int64(time.Millisecond), Outcomes: []string{}}
	}
	start := time.Now()
	next := 0
	actualEffects, actualDeduplicated := 0, 0
	for next < c.Jobs || q.Outstanding() > 0 {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		now := time.Since(start).Nanoseconds()
		for next < c.Jobs && report.Jobs[next].OfferedNS <= now {
			r := &report.Jobs[next]
			r.DecidedNS = time.Since(start).Nanoseconds()
			admitErr := q.Admit(jobAt(next))
			r.Status = "pending"
			if errors.Is(admitErr, ErrFull) {
				r.Status = "rejected"
			} else if admitErr != nil {
				return report, admitErr
			}
			report.Order = append(report.Order, -(next + 1))
			next++
		}
		if q.Outstanding() == 0 {
			if next < c.Jobs {
				if err := waitUntil(ctx, start.Add(time.Duration(report.Jobs[next].OfferedNS))); err != nil {
					return report, err
				}
			}
			continue
		}
		ticket, ok, err := q.Next()
		if err != nil || !ok {
			return report, fmt.Errorf("dispatch failed: %v", err)
		}
		index := 0
		for report.Jobs[index].Key != ticket.Job.Key {
			index++
		}
		r := &report.Jobs[index]
		report.Order = append(report.Order, index+1)
		if ticket.Attempt == 1 {
			r.FirstNS = time.Since(start).Nanoseconds()
		}
		r.MaxWait = max(r.MaxWait, ticket.Wait)
		if err := waitUntil(ctx, time.Now().Add(time.Duration(c.ServiceMS)*time.Millisecond)); err != nil {
			return report, err
		}
		inject := c.FaultEvery > 0 && (index+1)%c.FaultEvery == 0
		outcome := "ack"
		if inject && c.Fault == "fail-before-once" && ticket.Attempt == 1 {
			outcome = "failed-before"
		} else {
			write, err := log.Write(token, r.Key, sim.Idempotent)
			if err != nil {
				return report, err
			}
			if write.Status != "committed" && write.Status != "deduplicated" {
				return report, fmt.Errorf("unexpected store response %s", write.Status)
			}
			if write.Status == "committed" {
				actualEffects++
			} else {
				actualDeduplicated++
			}
			if inject && (c.Fault == "lost-ack-always" || (c.Fault == "lost-ack-once" && ticket.Attempt == 1)) {
				outcome = "lost-ack"
			}
		}
		r.Outcomes = append(r.Outcomes, outcome)
		status, err := q.Finish(r.Key, outcome == "ack")
		if err != nil {
			return report, err
		}
		r.Status = status
		if status != "retry" {
			r.DoneNS = time.Since(start).Nanoseconds()
		}
		if c.ReopenAfter == ticket.Dispatch {
			before := log.Snapshot()
			if err := log.Close(); err != nil {
				log = nil
				return report, err
			}
			log = nil
			openStart := time.Now()
			var recovery durable.Recovery
			log, recovery, err = durable.Open(path, nil)
			if err != nil {
				return report, err
			}
			openNS := time.Since(openStart).Nanoseconds()
			after := log.Snapshot()
			if recovery.TruncatedBytes != 0 || after.Epoch != before.Epoch || after.Fence != before.Fence || !slices.Equal(after.Effects, before.Effects) {
				return report, fmt.Errorf("reopened ledger differs from acknowledged state")
			}
			barrierStart := time.Now()
			token, err = log.Reserve()
			if err == nil {
				err = log.Fence(token)
			}
			if err != nil {
				return report, err
			}
			report.Recovery = RecoveryCost{Performed: true, After: ticket.Dispatch, Records: recovery.Records, Effects: len(after.Effects), OpenNS: openNS, BarrierNS: time.Since(barrierStart).Nanoseconds(), NextToken: token}
		}
	}
	report.ElapsedNS = time.Since(start).Nanoseconds()
	if err := populate(&report); err != nil {
		return report, err
	}
	if len(log.Snapshot().Effects) != report.Counts.Effects ||
		actualEffects != report.Counts.Effects || actualDeduplicated != report.Counts.Deduplicated {
		return report, fmt.Errorf("actual store responses differ from job outcomes")
	}
	if err := Check(report); err != nil {
		return report, err
	}
	return report, nil
}

func distribution(values []int64) Distribution {
	if len(values) == 0 {
		return Distribution{}
	}
	slices.Sort(values)
	at := func(percent int) int64 { return values[(len(values)*percent+99)/100-1] }
	return Distribution{P50NS: at(50), P95NS: at(95), P99NS: at(99), MaxNS: values[len(values)-1]}
}

// populate replays admission, selection, and retry accounting from the compact
// journal. It recomputes metrics instead of trusting imported summary fields.
func populate(r *Report) error {
	q, err := New(r.Config.Capacity, r.Config.MaxAttempts, r.Config.Policy)
	if err != nil {
		return err
	}
	counts := Counts{}
	visits := make([]int, len(r.Jobs))
	wait := make([]int, len(r.Jobs))
	statuses := make([]string, len(r.Jobs))
	effects := map[string]bool{}
	classWait := [2]int{}
	nextAdmission := 0
	var observedNS int64
	for _, entry := range r.Order {
		if entry == 0 || entry > len(r.Jobs) || entry < -len(r.Jobs) {
			return fmt.Errorf("invalid journal job index")
		}
		if entry < 0 {
			i := -entry - 1
			if i != nextAdmission {
				return fmt.Errorf("admission order changed")
			}
			nextAdmission++
			if r.Jobs[i].DecidedNS < observedNS {
				return fmt.Errorf("admission timestamps contradict serial journal")
			}
			observedNS = r.Jobs[i].DecidedNS
			err := q.Admit(jobAt(i))
			switch {
			case errors.Is(err, ErrFull):
				counts.Rejected++
				statuses[i] = "rejected"
			case err != nil:
				return err
			default:
				counts.Admitted++
				statuses[i] = "pending"
			}
			counts.HighWater = max(counts.HighWater, q.Outstanding())
			continue
		}
		i := entry - 1
		if visits[i] == 0 {
			if r.Jobs[i].FirstNS < observedNS {
				return fmt.Errorf("first dispatch predates journal position")
			}
			observedNS = r.Jobs[i].FirstNS
		}
		ticket, ok, err := q.Next()
		if err != nil || !ok || ticket.Job.Key != r.Jobs[i].Key || visits[i] >= len(r.Jobs[i].Outcomes) {
			return fmt.Errorf("journal violates selection or attempts")
		}
		wait[i] = max(wait[i], ticket.Wait)
		classWait[ticket.Job.Class] = max(classWait[ticket.Job.Class], ticket.Wait)
		outcome := r.Jobs[i].Outcomes[visits[i]]
		visits[i]++
		counts.Attempts++
		inject := r.Config.FaultEvery > 0 && (i+1)%r.Config.FaultEvery == 0
		expected := "ack"
		if inject {
			switch {
			case r.Config.Fault == "fail-before-once" && visits[i] == 1:
				expected = "failed-before"
			case r.Config.Fault == "lost-ack-always" || (r.Config.Fault == "lost-ack-once" && visits[i] == 1):
				expected = "lost-ack"
			}
		}
		if outcome != expected {
			return fmt.Errorf("outcome differs from declared fault")
		}
		if outcome == "failed-before" {
			counts.FailedBefore++
		} else {
			if effects[ticket.Job.Key] {
				counts.Deduplicated++
			} else {
				effects[ticket.Job.Key] = true
				counts.Effects++
			}
			if outcome == "lost-ack" {
				counts.LostAcks++
			}
		}
		status, err := q.Finish(ticket.Job.Key, outcome == "ack")
		if err != nil {
			return err
		}
		statuses[i] = status
		observedNS += int64(r.Config.ServiceMS) * int64(time.Millisecond)
		if status != "retry" {
			if r.Jobs[i].DoneNS < observedNS {
				return fmt.Errorf("completion timestamps contradict service delay or journal")
			}
			observedNS = r.Jobs[i].DoneNS
		}
		switch status {
		case "completed":
			counts.Completed++
		case "exhausted":
			counts.Exhausted++
		case "retry":
			counts.Retries++
		}
		if r.Recovery.Performed && counts.Attempts == r.Recovery.After {
			if r.Recovery.Effects != counts.Effects || r.Recovery.Records != uint64(2+counts.Effects) {
				return fmt.Errorf("recovery counts disagree with history")
			}
			observedNS += r.Recovery.OpenNS + r.Recovery.BarrierNS
		}
	}
	if observedNS > r.ElapsedNS {
		return fmt.Errorf("elapsed duration is shorter than the serial journal")
	}
	if nextAdmission != len(r.Jobs) || q.Outstanding() != 0 {
		return fmt.Errorf("incomplete workload journal")
	}
	var completion, initial []int64
	for i, j := range r.Jobs {
		if statuses[i] != j.Status || visits[i] != len(j.Outcomes) || wait[i] != j.MaxWait {
			return fmt.Errorf("job %d summary disagrees with journal", i)
		}
		if j.Status == "completed" {
			completion = append(completion, j.DoneNS-j.OfferedNS)
		}
		if visits[i] > 0 {
			initial = append(initial, j.FirstNS-j.DecidedNS)
		}
	}
	r.Counts = counts
	r.Completion = distribution(completion)
	r.InitialWait = distribution(initial)
	r.ClassWait = classWait
	r.Throughput = float64(counts.Completed) * 1e9 / float64(r.ElapsedNS)
	return nil
}

func Check(r Report) error {
	if err := r.Config.Validate(); err != nil {
		return err
	}
	if r.Version != Version || len(r.Environment) == 0 || len(r.Environment) > 100 || len(r.Jobs) != r.Config.Jobs ||
		len(r.Order) > r.Config.Jobs*(r.Config.MaxAttempts+1) || r.ElapsedNS <= 0 || r.ElapsedNS > int64(30*time.Second) ||
		math.IsNaN(r.Throughput) || math.IsInf(r.Throughput, 0) {
		return fmt.Errorf("invalid workload report bounds")
	}
	if r.Recovery.OpenNS < 0 || r.Recovery.OpenNS > r.ElapsedNS ||
		r.Recovery.BarrierNS < 0 || r.Recovery.BarrierNS > r.ElapsedNS-r.Recovery.OpenNS {
		return fmt.Errorf("invalid recovery durations")
	}
	for i, j := range r.Jobs {
		job := jobAt(i)
		if j.Key != job.Key || j.Class != job.Class || j.OfferedNS != int64(i*r.Config.OfferEveryMS)*int64(time.Millisecond) ||
			j.DecidedNS < j.OfferedNS || j.DecidedNS > r.ElapsedNS || len(j.Outcomes) > r.Config.MaxAttempts || j.MaxWait < 0 {
			return fmt.Errorf("invalid job %d", i)
		}
		if j.Status == "rejected" {
			if j.FirstNS != 0 || j.DoneNS != 0 || len(j.Outcomes) != 0 {
				return fmt.Errorf("rejected job executed")
			}
		} else if j.FirstNS < j.DecidedNS || j.DoneNS < j.FirstNS || j.DoneNS > r.ElapsedNS {
			return fmt.Errorf("invalid job timing")
		}
	}
	copy := r
	if err := populate(&copy); err != nil {
		return err
	}
	q, _ := New(r.Config.Capacity, r.Config.MaxAttempts, r.Config.Policy)
	if r.Counts != copy.Counts || r.Completion != copy.Completion || r.InitialWait != copy.InitialWait ||
		r.ClassWait != copy.ClassWait || r.FairBound != q.WaitBound() || math.Abs(r.Throughput-copy.Throughput) > 1e-9 {
		return fmt.Errorf("workload metrics disagree with journal")
	}
	if r.FairBound >= 0 && max(r.ClassWait[0], r.ClassWait[1]) > r.FairBound {
		return fmt.Errorf("fair dispatch bound exceeded")
	}
	performed := r.Config.ReopenAfter > 0 && r.Config.ReopenAfter <= r.Counts.Attempts
	if performed != r.Recovery.Performed {
		return fmt.Errorf("recovery probe status disagrees with requested checkpoint")
	}
	if performed {
		p := r.Recovery
		if p.After != r.Config.ReopenAfter || p.NextToken != 2 || p.OpenNS < 0 || p.BarrierNS < 0 ||
			p.OpenNS > r.ElapsedNS || p.BarrierNS > r.ElapsedNS-p.OpenNS {
			return fmt.Errorf("invalid recovery cost")
		}
	} else if r.Recovery != (RecoveryCost{}) {
		return fmt.Errorf("unexpected recovery fields")
	}
	return nil
}
