// Package scheduler implements a bounded, single-dispatcher workload scheduler.
// It has no clocks or storage dependencies; fairness is expressed in dispatches.
package scheduler

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

const MaxJobs = 128

var ErrFull = errors.New("outstanding-job capacity reached")
var ErrDuplicate = errors.New("job key was already admitted")

type Policy string

const Fair Policy = "fair"
const Priority Policy = "priority"

type Job struct {
	Key   string `json:"key"`
	Class int    `json:"class"` // 0: high priority, 1: ordinary
}

type Ticket struct {
	Job      Job
	Attempt  int
	Dispatch int
	Wait     int // Dispatches by other jobs since this attempt entered its FIFO.
}

type pending struct {
	job      Job
	attempts int
	enqueued int
}

// Queue is owned by one dispatcher. Capacity includes the active job so retries
// retain a slot and cannot be discarded by competing new arrivals.
type Queue struct {
	capacity    int
	maxAttempts int
	policy      Policy
	queues      [2][]*pending
	known       map[string]bool
	active      *pending
	outstanding int
	dispatches  int
	cursor      int
}

func New(capacity, maxAttempts int, policy Policy) (*Queue, error) {
	if capacity < 1 || capacity > MaxJobs || maxAttempts < 1 || maxAttempts > 5 ||
		(policy != Fair && policy != Priority) {
		return nil, fmt.Errorf("capacity must be 1-128, attempts 1-5, policy fair or priority")
	}
	return &Queue{capacity: capacity, maxAttempts: maxAttempts, policy: policy, known: map[string]bool{}}, nil
}

func (q *Queue) Admit(job Job) error {
	if len(job.Key) < 1 || len(job.Key) > 128 || !utf8.ValidString(job.Key) || job.Class < 0 || job.Class > 1 {
		return fmt.Errorf("invalid job key or class")
	}
	if q.known[job.Key] {
		return ErrDuplicate
	}
	if q.outstanding == q.capacity {
		return ErrFull
	}
	if len(q.known) == MaxJobs {
		return fmt.Errorf("experiment lifetime admission limit reached")
	}
	q.known[job.Key] = true
	q.outstanding++
	q.queues[job.Class] = append(q.queues[job.Class], &pending{job: job, enqueued: q.dispatches})
	return nil
}

func (q *Queue) Next() (Ticket, bool, error) {
	if q.active != nil {
		return Ticket{}, false, fmt.Errorf("finish active attempt before dispatching")
	}
	class := -1
	if q.policy == Priority {
		for i := range q.queues {
			if len(q.queues[i]) > 0 {
				class = i
				break
			}
		}
	} else {
		// Weighted round robin, 3 high-priority slots to 1 ordinary slot.
		// Empty classes are skipped without burning an actual dispatch.
		for range 4 {
			slot := q.cursor
			q.cursor = (q.cursor + 1) % 4
			candidate := 0
			if slot == 3 {
				candidate = 1
			}
			if len(q.queues[candidate]) > 0 {
				class = candidate
				break
			}
		}
	}
	if class == -1 {
		return Ticket{}, false, nil
	}
	p := q.queues[class][0]
	q.queues[class][0] = nil
	q.queues[class] = q.queues[class][1:]
	wait := q.dispatches - p.enqueued
	q.dispatches++
	p.attempts++
	q.active = p
	return Ticket{Job: p.job, Attempt: p.attempts, Dispatch: q.dispatches, Wait: wait}, true, nil
}

// Finish returns completed, retry, or exhausted. Failed attempts go to their
// class's FIFO tail, preventing a poison job from monopolizing dispatch.
func (q *Queue) Finish(key string, success bool) (string, error) {
	if q.active == nil || q.active.job.Key != key {
		return "", fmt.Errorf("completion does not match active job")
	}
	p := q.active
	q.active = nil
	if !success && p.attempts < q.maxAttempts {
		p.enqueued = q.dispatches
		q.queues[p.job.Class] = append(q.queues[p.job.Class], p)
		return "retry", nil
	}
	q.outstanding--
	if success {
		return "completed", nil
	}
	return "exhausted", nil
}

func (q *Queue) Outstanding() int { return q.outstanding }

// WaitBound is a conservative bound on other dispatches before one queued
// attempt runs. It is not a wall-clock latency bound or a completion promise.
func (q *Queue) WaitBound() int {
	if q.policy == Fair {
		return 4*q.capacity - 1
	}
	return -1 // Continuous higher-priority arrivals can starve ordinary jobs.
}
