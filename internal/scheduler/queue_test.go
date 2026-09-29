package scheduler

import (
	"errors"
	"fmt"
	"testing"
)

func TestCapacityDuplicateAndRetrySlots(t *testing.T) {
	q, err := New(1, 2, Fair)
	if err != nil {
		t.Fatal(err)
	}
	job := Job{Key: "invoice-1", Class: 1}
	if err := q.Admit(job); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(q.Admit(job), ErrDuplicate) {
		t.Fatal("duplicate admitted")
	}
	ticket, ok, err := q.Next()
	if !ok || err != nil || ticket.Attempt != 1 {
		t.Fatal(ticket, err)
	}
	if !errors.Is(q.Admit(Job{Key: "new"}), ErrFull) {
		t.Fatal("active job lost its slot")
	}
	if _, _, err := q.Next(); err == nil {
		t.Fatal("double dispatch")
	}
	if _, err := q.Finish("wrong", true); err == nil {
		t.Fatal("wrong completion")
	}
	if status, err := q.Finish(job.Key, false); err != nil || status != "retry" {
		t.Fatal(status, err)
	}
	ticket, _, _ = q.Next()
	if ticket.Attempt != 2 {
		t.Fatal(ticket)
	}
	if status, err := q.Finish(job.Key, false); err != nil || status != "exhausted" {
		t.Fatal(status, err)
	}
	if q.Outstanding() != 0 {
		t.Fatal("exhausted job retained a slot")
	}
	if !errors.Is(q.Admit(job), ErrDuplicate) {
		t.Fatal("terminal key reused")
	}
}

func TestWeightedOrderAndPriorityControl(t *testing.T) {
	for _, policy := range []Policy{Fair, Priority} {
		q, _ := New(10, 1, policy)
		for i := 0; i < 8; i++ {
			if err := q.Admit(Job{Key: fmt.Sprint(i), Class: i % 2}); err != nil {
				t.Fatal(err)
			}
		}
		var order []int
		for q.Outstanding() > 0 {
			ticket, _, err := q.Next()
			if err != nil {
				t.Fatal(err)
			}
			order = append(order, ticket.Job.Class)
			if _, err := q.Finish(ticket.Job.Key, true); err != nil {
				t.Fatal(err)
			}
		}
		if policy == Fair && fmt.Sprint(order) != "[0 0 0 1 0 1 1 1]" {
			t.Fatal(order)
		}
		if policy == Priority && fmt.Sprint(order) != "[0 0 0 0 1 1 1 1]" {
			t.Fatal(order)
		}
	}
}

func TestContinuousHighPriorityArrivals(t *testing.T) {
	for _, policy := range []Policy{Fair, Priority} {
		q, _ := New(4, 1, policy)
		if err := q.Admit(Job{Key: "ordinary", Class: 1}); err != nil {
			t.Fatal(err)
		}
		selected := false
		for i := 0; i < 50; i++ {
			if err := q.Admit(Job{Key: fmt.Sprintf("high-%d", i)}); err != nil {
				t.Fatal(err)
			}
			ticket, _, err := q.Next()
			if err != nil {
				t.Fatal(err)
			}
			if ticket.Job.Class == 1 {
				selected = true
				if ticket.Wait > 3 {
					t.Fatal("ordinary head waited beyond fair cycle", ticket)
				}
			}
			if _, err := q.Finish(ticket.Job.Key, true); err != nil {
				t.Fatal(err)
			}
			if selected {
				break
			}
		}
		if selected != (policy == Fair) {
			t.Fatal("starvation control failed", policy)
		}
	}
}

func TestFairBoundUnderRetrySaturation(t *testing.T) {
	for capacity := 1; capacity <= 32; capacity++ {
		q, _ := New(capacity, 5, Fair)
		for i := 0; i < capacity; i++ {
			if err := q.Admit(Job{Key: fmt.Sprint(i), Class: i % 2}); err != nil {
				t.Fatal(err)
			}
		}
		visits := map[string]int{}
		for q.Outstanding() > 0 {
			ticket, ok, err := q.Next()
			if !ok || err != nil || ticket.Wait > q.WaitBound() {
				t.Fatal(capacity, ticket, err)
			}
			visits[ticket.Job.Key]++
			if _, err := q.Finish(ticket.Job.Key, false); err != nil {
				t.Fatal(err)
			}
		}
		for _, n := range visits {
			if n != 5 {
				t.Fatal(n)
			}
		}
	}
}

func TestInvalidQueueInputs(t *testing.T) {
	for _, c := range []int{0, 129} {
		if _, err := New(c, 1, Fair); err == nil {
			t.Fatal(c)
		}
	}
	if _, err := New(1, 6, Fair); err == nil {
		t.Fatal("attempt bound")
	}
	if _, err := New(1, 1, "other"); err == nil {
		t.Fatal("policy")
	}
	q, _ := New(1, 1, Fair)
	for _, j := range []Job{{}, {Key: "x", Class: 2}, {Key: string([]byte{255})}} {
		if err := q.Admit(j); err == nil {
			t.Fatal("invalid job")
		}
	}
	if _, ok, err := q.Next(); ok || err != nil {
		t.Fatal(ok, err)
	}
}
