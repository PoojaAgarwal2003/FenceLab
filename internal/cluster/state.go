// Package cluster is the real replicated execution mode, independent of the labs.
package cluster

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sync"
	"unicode/utf8"

	"github.com/hashicorp/raft"
)

const Version = "fencelab/cluster-v1"
const MaxJobs = 4096
const Capacity = 256
const MaxWorkers = 64

var identifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

type Spec struct {
	Key         string `json:"key"`
	Payload     string `json:"payload"`
	Class       int    `json:"class"`
	WorkMS      int    `json:"work_ms"`
	MaxAttempts int    `json:"max_attempts"`
}

func (s Spec) validate() error {
	if !identifier.MatchString(s.Key) || len(s.Payload) > 512 || !utf8.ValidString(s.Payload) || s.Class < 0 || s.Class > 1 ||
		s.WorkMS < 0 || s.WorkMS > 10000 || s.MaxAttempts < 1 || s.MaxAttempts > 5 {
		return fmt.Errorf("key, payload, class, work duration or attempt budget outside bounds")
	}
	return nil
}

func Digest(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

type Job struct {
	Spec
	Status     string `json:"status"`
	Sequence   uint64 `json:"sequence"`
	Generation int    `json:"generation"`
	Worker     string `json:"worker"`
	DeadlineMS int64  `json:"deadline_ms"`
	Result     string `json:"result"`
}

type ClaimRecord struct {
	Request    string `json:"request"`
	Key        string `json:"key"`
	Generation int    `json:"generation"`
}

type State struct {
	Version  string                 `json:"version"`
	ClockMS  int64                  `json:"clock_ms"`
	Sequence uint64                 `json:"sequence"`
	Cursor   int                    `json:"cursor"`
	Jobs     map[string]Job         `json:"jobs"`
	Workers  map[string]ClaimRecord `json:"workers"`
	Receipts map[string]ClaimRecord `json:"receipts"`
}

type Command struct {
	Operation  string `json:"operation"`
	NowMS      int64  `json:"now_ms"`
	Spec       Spec   `json:"spec"`
	Worker     string `json:"worker"`
	Request    string `json:"request"`
	LeaseMS    int    `json:"lease_ms"`
	Key        string `json:"key"`
	Generation int    `json:"generation"`
	Result     string `json:"result"`
}

type Counts struct {
	Pending   int `json:"pending"`
	Leased    int `json:"leased"`
	Completed int `json:"completed"`
	Exhausted int `json:"exhausted"`
	Retained  int `json:"retained"`
}

type Reply struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	Job    *Job   `json:"job,omitempty"`
	Counts Counts `json:"counts"`
}

type FSM struct {
	mu    sync.RWMutex
	state State
}

func NewFSM() *FSM {
	return &FSM{state: State{Version: Version, Jobs: map[string]Job{}, Workers: map[string]ClaimRecord{}, Receipts: map[string]ClaimRecord{}}}
}

func bad(message string) Reply { return Reply{Status: "invalid", Error: message} }

func (f *FSM) Apply(entry *raft.Log) interface{} {
	var c Command
	if err := decode(bytes.NewReader(entry.Data), &c, 8192); err != nil {
		return bad(err.Error())
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.apply(c)
}

func (f *FSM) apply(c Command) Reply {
	if c.NowMS <= 0 || c.NowMS > 9007199254740991-60000 {
		return bad("invalid replicated clock")
	}
	switch c.Operation {
	case "enqueue":
		if err := c.Spec.validate(); err != nil {
			return bad(err.Error())
		}
	case "claim":
		if !identifier.MatchString(c.Worker) || !identifier.MatchString(c.Request) || c.LeaseMS < 1000 || c.LeaseMS > 60000 {
			return bad("invalid worker, request ID or lease duration")
		}
	case "complete":
		if !identifier.MatchString(c.Worker) || !identifier.MatchString(c.Key) || c.Generation < 1 || len(c.Result) != 64 {
			return bad("invalid completion")
		}
	default:
		return bad("unknown operation")
	}
	// Time is supplied once by the leader and replicated, never sampled by FSMs.
	// A clock jump can revoke a lease early but cannot authorize an old generation.
	f.state.ClockMS = max(f.state.ClockMS, c.NowMS)
	f.expire()
	switch c.Operation {
	case "enqueue":
		if job, found := f.state.Jobs[c.Spec.Key]; found {
			if job.Spec != c.Spec {
				return Reply{Status: "conflict", Error: "key already belongs to a different job"}
			}
			return Reply{Status: "existing", Job: &job, Counts: f.counts()}
		}
		counts := f.counts()
		if counts.Pending+counts.Leased >= Capacity || len(f.state.Jobs) >= MaxJobs {
			return Reply{Status: "full", Error: "outstanding capacity or retained-key limit reached", Counts: counts}
		}
		f.state.Sequence++
		job := Job{Spec: c.Spec, Status: "pending", Sequence: f.state.Sequence}
		f.state.Jobs[job.Key] = job
		return Reply{Status: "enqueued", Job: &job, Counts: f.counts()}
	case "claim":
		_, known := f.state.Workers[c.Worker]
		if !known && len(f.state.Workers) >= MaxWorkers {
			return Reply{Status: "full", Error: "worker identity limit reached"}
		}
		if previous, found := f.state.Receipts[c.Worker+":"+c.Request]; found {
			job := f.state.Jobs[previous.Key]
			if job.Status == "leased" && job.Worker == c.Worker && job.Generation == previous.Generation {
				return Reply{Status: "claimed", Job: &job, Counts: f.counts()}
			}
			return Reply{Status: "retired", Counts: f.counts()}
		}
		for _, job := range f.state.Jobs {
			if job.Status == "leased" && job.Worker == c.Worker {
				return Reply{Status: "busy", Error: "worker already has an active claim"}
			}
		}
		for range 4 {
			class := 0
			if f.state.Cursor == 3 {
				class = 1
			}
			f.state.Cursor = (f.state.Cursor + 1) % 4
			key := ""
			var oldest uint64
			for _, job := range f.state.Jobs {
				if job.Status == "pending" && job.Class == class && (key == "" || job.Sequence < oldest || (job.Sequence == oldest && job.Key < key)) {
					key, oldest = job.Key, job.Sequence
				}
			}
			if key == "" {
				continue
			}
			job := f.state.Jobs[key]
			job.Status = "leased"
			job.Generation++
			job.Worker = c.Worker
			job.DeadlineMS = f.state.ClockMS + int64(c.LeaseMS)
			f.state.Jobs[key] = job
			f.state.Workers[c.Worker] = ClaimRecord{Request: c.Request, Key: key, Generation: job.Generation}
			f.state.Receipts[c.Worker+":"+c.Request] = f.state.Workers[c.Worker]
			return Reply{Status: "claimed", Job: &job, Counts: f.counts()}
		}
		return Reply{Status: "empty", Counts: f.counts()}
	case "complete":
		job, found := f.state.Jobs[c.Key]
		if !found {
			return Reply{Status: "missing", Error: "job does not exist"}
		}
		if job.Worker != c.Worker || job.Generation != c.Generation {
			return Reply{Status: "stale", Error: "ownership generation changed"}
		}
		if c.Result != Digest(job.Payload) {
			return Reply{Status: "conflict", Error: "result does not match declared computation"}
		}
		if job.Status == "completed" {
			return Reply{Status: "deduplicated", Job: &job, Counts: f.counts()}
		}
		if job.Status != "leased" {
			return Reply{Status: "stale", Error: "lease is no longer active"}
		}
		job.Status = "completed"
		job.Result = c.Result
		f.state.Jobs[job.Key] = job
		return Reply{Status: "completed", Job: &job, Counts: f.counts()}
	}
	panic("validated operation not dispatched")
}

func (f *FSM) expire() {
	// Map iteration must not assign queue positions nondeterministically.
	for {
		key := ""
		var oldest uint64
		for _, job := range f.state.Jobs {
			if job.Status == "leased" && job.DeadlineMS <= f.state.ClockMS && (key == "" || job.Sequence < oldest || (job.Sequence == oldest && job.Key < key)) {
				key, oldest = job.Key, job.Sequence
			}
		}
		if key == "" {
			return
		}
		job := f.state.Jobs[key]
		job.Status = "exhausted"
		if job.Generation < job.MaxAttempts {
			job.Status = "pending"
			f.state.Sequence++
			job.Sequence = f.state.Sequence
		}
		f.state.Jobs[key] = job
	}
}

func (f *FSM) counts() Counts {
	c := Counts{Retained: len(f.state.Jobs)}
	for _, job := range f.state.Jobs {
		switch job.Status {
		case "pending":
			c.Pending++
		case "leased":
			c.Leased++
		case "completed":
			c.Completed++
		case "exhausted":
			c.Exhausted++
		}
	}
	return c
}

func (f *FSM) View(key string) Reply {
	f.mu.RLock()
	defer f.mu.RUnlock()
	r := Reply{Status: "ok", Counts: f.counts()}
	if key != "" {
		job, found := f.state.Jobs[key]
		if !found {
			return Reply{Status: "missing", Error: "job does not exist"}
		}
		r.Job = &job
	}
	return r
}

type snapshot struct{ data []byte }

func (f *FSM) Snapshot() (raft.FSMSnapshot, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	data, err := json.Marshal(f.state)
	return &snapshot{data: data}, err
}

func (s *snapshot) Persist(sink raft.SnapshotSink) error {
	if _, err := sink.Write(s.data); err != nil {
		return errors.Join(err, sink.Cancel())
	}
	if err := sink.Close(); err != nil {
		return errors.Join(err, sink.Cancel())
	}
	return nil
}
func (s *snapshot) Release() {}

func (f *FSM) Restore(reader io.ReadCloser) (err error) {
	defer func() { err = errors.Join(err, reader.Close()) }()
	var state State
	if err := decode(reader, &state, 32<<20); err != nil {
		return err
	}
	if state.Version != Version || state.Jobs == nil || state.Workers == nil || state.Receipts == nil ||
		len(state.Receipts) > MaxJobs*5 || len(state.Jobs) > MaxJobs ||
		len(state.Workers) > MaxWorkers || state.Cursor < 0 || state.Cursor > 3 || state.ClockMS < 0 ||
		state.ClockMS > 9007199254740991-60000 || state.Sequence > MaxJobs*5 {
		return fmt.Errorf("invalid queue snapshot header")
	}
	positions := map[uint64]bool{}
	totalClaims := 0
	for key, job := range state.Jobs {
		if key != job.Key || job.validate() != nil || job.Sequence == 0 || job.Sequence > state.Sequence ||
			positions[job.Sequence] || job.Generation < 0 || job.Generation > job.MaxAttempts {
			return fmt.Errorf("invalid snapshot job %q", key)
		}
		positions[job.Sequence] = true
		totalClaims += job.Generation
		if job.Generation > 0 && (!identifier.MatchString(job.Worker) || job.DeadlineMS <= 0) {
			return fmt.Errorf("missing ownership")
		}
		switch job.Status {
		case "pending":
			if job.Generation == job.MaxAttempts || job.Result != "" {
				return fmt.Errorf("invalid pending job")
			}
		case "leased":
			if job.Generation == 0 || job.Result != "" || job.DeadlineMS <= state.ClockMS {
				return fmt.Errorf("invalid active lease")
			}
			claim, found := state.Workers[job.Worker]
			if !found || claim.Key != key || claim.Generation != job.Generation {
				return fmt.Errorf("active lease missing its durable claim receipt")
			}
		case "completed":
			if job.Generation == 0 || job.Result != Digest(job.Payload) {
				return fmt.Errorf("invalid committed effect")
			}
		case "exhausted":
			if job.Generation != job.MaxAttempts || job.Result != "" {
				return fmt.Errorf("invalid exhausted job")
			}
		default:
			return fmt.Errorf("unknown job state")
		}
	}
	for worker, claim := range state.Workers {
		job, found := state.Jobs[claim.Key]
		if !identifier.MatchString(worker) || !identifier.MatchString(claim.Request) || !found ||
			claim.Generation < 1 || claim.Generation > job.Generation {
			return fmt.Errorf("invalid worker claim")
		}
		if receipt, found := state.Receipts[worker+":"+claim.Request]; !found || receipt != claim {
			return fmt.Errorf("missing durable claim receipt")
		}
	}
	if len(state.Receipts) != totalClaims {
		return fmt.Errorf("snapshot is missing historical claim receipts")
	}
	claims := map[string]bool{}
	for key, claim := range state.Receipts {
		job, found := state.Jobs[claim.Key]
		parts := bytes.SplitN([]byte(key), []byte(":"), 2)
		if len(parts) != 2 || !identifier.Match(parts[0]) || string(parts[1]) != claim.Request ||
			!identifier.MatchString(claim.Request) || !found || claim.Generation < 1 || claim.Generation > job.Generation {
			return fmt.Errorf("invalid historical claim receipt")
		}
		if _, found := state.Workers[string(parts[0])]; !found {
			return fmt.Errorf("receipt has no worker identity")
		}
		generationKey := fmt.Sprintf("%s:%d", claim.Key, claim.Generation)
		if claims[generationKey] || (claim.Generation == job.Generation && string(parts[0]) != job.Worker) {
			return fmt.Errorf("duplicate or mismatched claim generation")
		}
		claims[generationKey] = true
	}
	candidate := &FSM{state: state}
	counts := candidate.counts()
	if counts.Pending+counts.Leased > Capacity {
		return fmt.Errorf("snapshot exceeds queue capacity")
	}
	f.mu.Lock()
	f.state = state
	f.mu.Unlock()
	return nil
}

func decode(reader io.Reader, target any, limit int64) error {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > limit {
		return fmt.Errorf("input exceeds %d bytes", limit)
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("unexpected trailing JSON")
	}
	return nil
}
