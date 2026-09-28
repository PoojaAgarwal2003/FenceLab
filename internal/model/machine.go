package model

import (
	"context"
	"fmt"
	"slices"

	"github.com/PoojaAgarwal2003/FenceLab/internal/sim"
)

type Envelope struct {
	ID    int    `json:"id"`
	Kind  string `json:"kind"`
	From  string `json:"from"`
	To    string `json:"to"`
	Token int    `json:"token"`
	Key   string `json:"key"`
}

type Event struct {
	AtMS    int      `json:"at_ms"`
	Message Envelope `json:"message"`
}

type ReplayFile struct {
	Version   string   `json:"version"`
	Scenario  Scenario `json:"scenario"`
	Decisions []int    `json:"decisions"`
}

type Result struct {
	Model      string          `json:"model"`
	Scenario   Scenario        `json:"scenario"`
	Trace      []sim.Entry     `json:"trace"`
	Effects    []sim.Effect    `json:"effects"`
	Violations []sim.Violation `json:"violations"`
	Summary    sim.Summary     `json:"summary"`
	Pending    []Event         `json:"pending"`
	HaltReason string          `json:"halt_reason"`
	Replay     ReplayFile      `json:"replay"`
}

type machine struct {
	scenario   Scenario
	now        int
	nextID     int
	issued     int
	state      sim.State
	started    [3]bool
	dispatched [3]bool
	acked      [3]bool
	pending    []Event
	result     Result
	capture    bool
}

func worker(token int) string {
	if token == 1 {
		return "worker-a"
	}
	return "worker-b"
}

func newMachine(s Scenario, capture bool) *machine {
	m := &machine{scenario: s, capture: capture, result: Result{
		Model: Version, Scenario: s, Trace: []sim.Entry{}, Effects: []sim.Effect{},
		Violations: []sim.Violation{}, Replay: ReplayFile{Version: Version, Scenario: s, Decisions: []int{}},
	}}
	for _, p := range s.Partitions {
		m.enqueue(p.StartMS, Envelope{Kind: "partition", From: p.From, To: p.To})
		m.enqueue(p.EndMS, Envelope{Kind: "heal", From: p.From, To: p.To})
	}
	m.claim()
	return m
}

func (m *machine) clone() *machine {
	n := *m
	n.pending = slices.Clone(m.pending)
	n.result.Effects = slices.Clone(m.result.Effects)
	n.result.Violations = slices.Clone(m.result.Violations)
	return &n
}

func (m *machine) record(actor, action string, token int, outcome, detail string) {
	if m.capture {
		m.result.Trace = append(m.result.Trace, sim.Entry{
			Step: len(m.result.Trace), AtMS: m.now, Actor: actor, Action: action,
			Token: token, Outcome: outcome, Detail: detail, State: m.state,
		})
	}
}

func (m *machine) enqueue(at int, message Envelope) {
	m.nextID++
	message.ID = m.nextID
	message.Key = "invoice-001"
	m.pending = append(m.pending, Event{AtMS: at, Message: message})
}

func (m *machine) send(kind, from, to string, token int) {
	message := Envelope{Kind: kind, From: from, To: to, Token: token, Key: "invoice-001"}
	delay, copies, spacing := 0, 1, 0
	local := m.now
	if from == "worker-a" {
		local += m.scenario.ClockSkewMS[0]
	} else if from == "worker-b" {
		local += m.scenario.ClockSkewMS[1]
	}
	m.record("network", "message_sent", token, "info", fmt.Sprintf("%s: %s -> %s; key %s; sender clock %d (not lease authority).", kind, from, to, message.Key, local))
	for _, f := range m.scenario.Faults {
		if f.Kind != kind || (f.From != "" && f.From != from) || (f.To != "" && f.To != to) || (f.Token != 0 && f.Token != token) {
			continue
		}
		m.record("network", "fault_"+f.Action, token, "fault", fmt.Sprintf("%s -> %s: %s %s; delay %d ms.", from, to, f.Action, kind, f.DelayMS))
		switch f.Action {
		case "drop":
			return
		case "delay":
			delay = f.DelayMS
		case "duplicate":
			copies, spacing = 2, f.DelayMS
		}
		break // Fault rules are ordered: first matching rule wins.
	}
	for i := 0; i < copies; i++ {
		at := m.now + m.scenario.LatencyMS + delay + i*spacing
		initial := at
		for {
			prior := at
			for _, p := range m.scenario.Partitions {
				if p.From == from && p.To == to && at >= p.StartMS && at < p.EndMS {
					at = p.EndMS
				}
			}
			if prior == at {
				break
			}
		}
		if at != initial {
			m.record("network", "partition_held", token, "fault", fmt.Sprintf("%s -> %s: delivery held until authoritative t=%d.", from, to, at))
		}
		m.enqueue(at, message)
	}
}

func (m *machine) claim() {
	m.issued++
	m.record("authority", "epoch_issued", m.issued, "info", "Reserve a new token. Issuance alone does not activate ownership in the barrier protocol.")
	if m.scenario.Policy == sim.LeaseOnly {
		m.activate(m.issued)
		return
	}
	m.send("fence", "authority", "store", m.issued)
	if m.scenario.Protocol == "eager" {
		m.activate(m.issued)
	}
}

func (m *machine) activate(token int) {
	if token != m.issued || m.dispatched[token] {
		return
	}
	m.dispatched[token] = true
	m.state.Epoch, m.state.Owner = token, worker(token)
	m.record("authority", "ownership_activated", token, "info", "Ownership becomes active; start the authoritative lease and dispatch this attempt.")
	m.send("dispatch", "authority", worker(token), token)
	m.enqueue(m.now+m.scenario.LeaseMS, Envelope{Kind: "expire", From: "authority", To: "authority", Token: token})
}

// Only earliest-time events are enabled. Equal-time deliveries and timers are
// separate choices; the search never moves time backwards or delivers early.
func (m *machine) enabled() []int {
	at := int(^uint(0) >> 1)
	for _, e := range m.pending {
		if e.AtMS < at {
			at = e.AtMS
		}
	}
	ids := []int{}
	for _, e := range m.pending {
		if e.AtMS == at {
			ids = append(ids, e.Message.ID)
		}
	}
	slices.Sort(ids)
	return ids
}

func (m *machine) advance(id int) error {
	if !slices.Contains(m.enabled(), id) {
		return fmt.Errorf("event %d is not enabled at the current virtual time", id)
	}
	index := slices.IndexFunc(m.pending, func(e Event) bool { return e.Message.ID == id })
	e := m.pending[index]
	m.pending = slices.Delete(m.pending, index, index+1)
	m.now = e.AtMS
	msg := e.Message
	if m.capture {
		m.result.Replay.Decisions = append(m.result.Replay.Decisions, id)
	}
	switch msg.Kind {
	case "partition", "heal":
		m.record("network", msg.Kind, 0, "fault", msg.From+" -> "+msg.To+" directional link "+msg.Kind+".")
	case "expire":
		if !m.state.Completed && msg.Token == m.state.Epoch {
			m.record("authority", "lease_expired", msg.Token, "fault", "Expiry cannot recall in-flight messages; at most two attempts are modeled.")
			if m.issued < 2 {
				m.claim()
			}
		}
	case "work", "retry":
		if !m.acked[msg.Token] {
			m.record(worker(msg.Token), msg.Kind, msg.Token, "info", "Request the same logical effect using this attempt's token.")
			m.send("write", worker(msg.Token), "store", msg.Token)
			if msg.Kind == "work" {
				m.enqueue(m.now+m.scenario.LeaseMS/2, Envelope{Kind: "retry", From: worker(msg.Token), To: worker(msg.Token), Token: msg.Token})
			}
		}
	default:
		m.record("network", "message_delivered", msg.Token, "info", fmt.Sprintf("Message #%d %s: %s -> %s; key %s.", msg.ID, msg.Kind, msg.From, msg.To, msg.Key))
		switch msg.Kind {
		case "fence":
			m.state.StorageFence = max(m.state.StorageFence, msg.Token)
			m.record("store", "fence_installed", msg.Token, "info", "Raise the storage fence monotonically. Acknowledgment travels separately.")
			m.send("fence-ack", "store", "authority", msg.Token)
		case "fence-ack":
			if m.scenario.Protocol == "barrier" {
				m.activate(msg.Token)
			}
		case "dispatch":
			if !m.started[msg.Token] {
				m.started[msg.Token] = true
				m.record(worker(msg.Token), "started", msg.Token, "info", "Dispatch received; duplicate dispatches for this token are ignored.")
				m.enqueue(m.now+m.scenario.WorkMS, Envelope{Kind: "work", From: msg.To, To: msg.To, Token: msg.Token})
			}
		case "write":
			m.write(msg)
		case "result":
			m.acked[msg.Token] = true
			if msg.Token == m.state.Epoch && msg.Token == m.issued {
				m.state.Completed = true
				m.record("authority", "job_completed", msg.Token, "accepted", "A current-attempt result confirms the effect.")
			} else {
				m.record("authority", "old_result_ignored", msg.Token, "rejected", "A replacement is pending or active; the old result cannot complete it.")
			}
		default:
			return fmt.Errorf("unknown event kind %q", msg.Kind)
		}
	}
	return nil
}

func (m *machine) write(msg Envelope) {
	if m.scenario.Policy != sim.LeaseOnly && msg.Token < m.state.StorageFence {
		m.result.Summary.RejectedWrites++
		m.record("store", "stale_rejected", msg.Token, "rejected", "Storage rejects a token below the installed fence.")
		return
	}
	if m.scenario.Policy == sim.Idempotent && len(m.result.Effects) > 0 {
		m.result.Summary.Deduplicated++
		m.record("store", "effect_deduplicated", msg.Token, "deduplicated", "Return the existing logical result without a new effect.")
		m.send("result", "store", "authority", msg.Token)
		return
	}
	m.result.Effects = append(m.result.Effects, sim.Effect{AtMS: m.now, Worker: msg.From, Token: msg.Token, CurrentEpoch: m.state.Epoch, Key: msg.Key})
	m.state.Writes++
	stale, duplicate := msg.Token < m.state.Epoch, m.state.Writes > 1
	outcome := "accepted"
	if stale || duplicate {
		outcome = "violation"
	}
	m.record("store", "effect_committed", msg.Token, outcome, "Atomic effect committed; any result message is a separate delivery.")
	for _, condition := range []struct {
		failed bool
		kind   string
	}{{stale, "stale-write"}, {duplicate, "duplicate-effect"}} {
		if condition.failed {
			m.result.Violations = append(m.result.Violations, sim.Violation{
				Step: len(m.result.Trace) - 1, AtMS: m.now, Kind: condition.kind,
				Detail: "Committed effect violates " + condition.kind + " at the active-ownership boundary.",
			})
		}
	}
	if stale {
		m.result.Summary.StaleWrites++
	}
	if duplicate {
		m.result.Summary.DuplicateWrites++
	}
	m.send("result", "store", "authority", msg.Token)
}

func (m *machine) finish(reason string) Result {
	m.result.Summary.AcceptedWrites = len(m.result.Effects)
	m.result.Summary.Safe = len(m.result.Violations) == 0
	m.result.Summary.Completed = m.state.Completed
	m.result.Pending = slices.Clone(m.pending)
	if m.result.Pending == nil {
		m.result.Pending = []Event{}
	}
	m.result.HaltReason = reason
	return m.result
}

func Run(ctx context.Context, s Scenario) (Result, error) {
	if err := s.Validate(); err != nil {
		return Result{}, err
	}
	m := newMachine(s, true)
	for step := 0; len(m.pending) > 0; step++ {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		if step == MaxEvents {
			return m.finish("event-limit"), nil
		}
		if err := m.advance(m.enabled()[0]); err != nil {
			return Result{}, err
		}
	}
	return m.finish("quiescent"), nil
}

func Replay(ctx context.Context, replay ReplayFile) (Result, error) {
	if replay.Version != Version || len(replay.Decisions) > MaxEvents {
		return Result{}, fmt.Errorf("invalid replay version or more than %d decisions", MaxEvents)
	}
	if err := replay.Scenario.Validate(); err != nil {
		return Result{}, err
	}
	m := newMachine(replay.Scenario, true)
	for _, id := range replay.Decisions {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		if err := m.advance(id); err != nil {
			return Result{}, err
		}
	}
	reason := "prefix"
	if len(m.pending) == 0 {
		reason = "quiescent"
	}
	return m.finish(reason), nil
}
