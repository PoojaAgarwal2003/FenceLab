package sim

import "container/heap"

type event struct {
	at       int
	sequence int
	kind     string
	token    int
	worker   string
}

type eventQueue []event

func (q eventQueue) Len() int { return len(q) }
func (q eventQueue) Less(i, j int) bool {
	if q[i].at != q[j].at {
		return q[i].at < q[j].at
	}
	return q[i].sequence < q[j].sequence
}
func (q eventQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *eventQueue) Push(value any) {
	*q = append(*q, value.(event))
}
func (q *eventQueue) Pop() any {
	old := *q
	value := old[len(old)-1]
	*q = old[:len(old)-1]
	return value
}

func (s *simulation) schedule(at int, kind string, token int, worker string) {
	s.sequence++
	heap.Push(&s.queue, event{at: at, sequence: s.sequence, kind: kind, token: token, worker: worker})
}
