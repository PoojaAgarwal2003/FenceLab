package cluster

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"testing"

	"github.com/hashicorp/raft"
)

func apply(t *testing.T, f *FSM, c Command) Reply {
	t.Helper()
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	return f.Apply(&raft.Log{Data: data}).(Reply)
}

func TestOwnershipRecoveryAndIdempotency(t *testing.T) {
	f := NewFSM()
	spec := Spec{Key: "invoice", Payload: "hello", MaxAttempts: 3}
	enqueue := Command{Operation: "enqueue", NowMS: 1000, Spec: spec}
	if r := apply(t, f, enqueue); r.Status != "enqueued" {
		t.Fatal(r)
	}
	if r := apply(t, f, enqueue); r.Status != "existing" {
		t.Fatal(r)
	}
	claim := Command{Operation: "claim", NowMS: 1000, Worker: "worker1", Request: "request1", LeaseMS: 1000}
	first := apply(t, f, claim)
	if first.Status != "claimed" || first.Job.Generation != 1 {
		t.Fatal(first)
	}
	if again := apply(t, f, claim); again.Job == nil || *again.Job != *first.Job {
		t.Fatal("lost claim reply created new ownership", again)
	}
	claim.Request = "request2"
	if r := apply(t, f, claim); r.Status != "busy" {
		t.Fatal(r)
	}
	claim.NowMS = 2000
	claim.Worker = "worker2"
	second := apply(t, f, claim)
	if second.Status != "claimed" || second.Job.Generation != 2 {
		t.Fatal(second)
	}
	old := Command{Operation: "complete", NowMS: 2001, Worker: "worker1", Key: "invoice", Generation: 1, Result: Digest("hello")}
	if r := apply(t, f, old); r.Status != "stale" {
		t.Fatal(r)
	}
	old.Worker = "worker2"
	old.Generation = 2
	if r := apply(t, f, old); r.Status != "completed" {
		t.Fatal(r)
	}
	if r := apply(t, f, old); r.Status != "deduplicated" {
		t.Fatal(r)
	}
	snap, err := f.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	restored := NewFSM()
	if err := restored.Restore(io.NopCloser(bytes.NewReader(snap.(*snapshot).data))); err != nil {
		t.Fatal(err)
	}
	if r := apply(t, restored, old); r.Status != "deduplicated" || r.Counts.Completed != 1 {
		t.Fatal(r)
	}
	enqueue.Spec.Payload = "different"
	if r := apply(t, restored, enqueue); r.Status != "conflict" {
		t.Fatal(r)
	}
}

func TestCapacityFairnessAndExhaustion(t *testing.T) {
	f := NewFSM()
	for i := 0; i < Capacity; i++ {
		class := 0
		if i%4 == 3 {
			class = 1
		}
		r := apply(t, f, Command{Operation: "enqueue", NowMS: 1000, Spec: Spec{Key: fmt.Sprintf("job-%03d", i), Class: class, MaxAttempts: 1}})
		if r.Status != "enqueued" {
			t.Fatal(r)
		}
	}
	if r := apply(t, f, Command{Operation: "enqueue", NowMS: 1000, Spec: Spec{Key: "overflow", MaxAttempts: 1}}); r.Status != "full" {
		t.Fatal(r)
	}
	for i := 0; i < 4; i++ {
		r := apply(t, f, Command{Operation: "claim", NowMS: 1000, Worker: fmt.Sprintf("worker%d", i), Request: "one", LeaseMS: 1000})
		if r.Job == nil || r.Job.Key != fmt.Sprintf("job-%03d", i) {
			t.Fatal(r)
		}
	}
	r := apply(t, f, Command{Operation: "claim", NowMS: 2000, Worker: "worker4", Request: "one", LeaseMS: 1000})
	if r.Counts.Exhausted != 4 || r.Counts.Leased != 1 {
		t.Fatal(r)
	}
}

func TestDeterministicExpiryAndSnapshotValidation(t *testing.T) {
	left, right := NewFSM(), NewFSM()
	for _, f := range []*FSM{left, right} {
		for i := 0; i < 8; i++ {
			apply(t, f, Command{Operation: "enqueue", NowMS: 1000, Spec: Spec{Key: fmt.Sprint(i), MaxAttempts: 2}})
			apply(t, f, Command{Operation: "claim", NowMS: 1000, Worker: "worker" + fmt.Sprint(i), Request: "one", LeaseMS: 1000})
		}
		apply(t, f, Command{Operation: "claim", NowMS: 2000, Worker: "new-worker", Request: "two", LeaseMS: 1000})
	}
	a, _ := left.Snapshot()
	b, _ := right.Snapshot()
	if !bytes.Equal(a.(*snapshot).data, b.(*snapshot).data) {
		t.Fatal("FSM depends on map iteration")
	}
	var state State
	if err := json.Unmarshal(a.(*snapshot).data, &state); err != nil {
		t.Fatal(err)
	}
	job := state.Jobs["0"]
	job.Status = "completed"
	job.Result = "forged"
	state.Jobs["0"] = job
	data, _ := json.Marshal(state)
	if err := right.Restore(io.NopCloser(bytes.NewReader(data))); err == nil {
		t.Fatal("invalid snapshot accepted")
	}
}
