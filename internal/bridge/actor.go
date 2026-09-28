// Package bridge executes model-controlled deliveries in separate OS processes.
package bridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/PoojaAgarwal2003/FenceLab/internal/durable"
	"github.com/PoojaAgarwal2003/FenceLab/internal/model"
	"github.com/PoojaAgarwal2003/FenceLab/internal/protocol"
	"github.com/PoojaAgarwal2003/FenceLab/internal/sim"
)

type actor struct {
	role           string
	policy         sim.Policy
	barrier        bool
	log            *durable.Log
	active         int
	ack            int
	started        bool
	completed      bool
	recoveredEpoch int
}

func validRole(role string) bool {
	return role == "authority" || role == "store" || role == "worker-a" || role == "worker-b"
}

// ServeActor consumes bounded newline-delimited commands on a private pipe.
// It exposes no listener, filesystem API, or unauthenticated network service.
func ServeActor(ctx context.Context, role, wal string, policy sim.Policy, mode string, in io.Reader, out io.Writer) (err error) {
	if !validRole(role) || (policy != sim.LeaseOnly && policy != sim.Fenced && policy != sim.Idempotent) ||
		(mode != "barrier" && mode != "eager") {
		return fmt.Errorf("invalid actor role, policy, or protocol")
	}
	a := &actor{role: role, policy: policy, barrier: mode == "barrier" && policy != sim.LeaseOnly}
	if role == "authority" || role == "store" {
		if wal == "" {
			return fmt.Errorf("durable actor requires a WAL")
		}
		a.log, _, err = durable.Open(wal, nil)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, a.log.Close()) }()
		a.recoveredEpoch = a.log.Snapshot().Epoch
	} else if wal != "" {
		return fmt.Errorf("worker must not own a WAL")
	}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), model.MaxInput)
	sequence := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var command protocol.Command
		if err := model.Decode(bytes.NewReader(scanner.Bytes()), &command); err != nil {
			return fmt.Errorf("invalid actor command: %w", err)
		}
		sequence++
		if command.Version != protocol.Version || command.Sequence != sequence {
			return fmt.Errorf("protocol version or sequence mismatch")
		}
		response, callErr := a.call(command.Request)
		reply := protocol.Reply{Version: protocol.Version, Sequence: sequence, Response: response}
		if callErr != nil {
			reply.Error = callErr.Error()
		}
		if err := json.NewEncoder(out).Encode(reply); err != nil {
			return err
		}
		if callErr != nil {
			return callErr // Fail closed; no continuation after ambiguous persistence.
		}
	}
	return scanner.Err()
}

func (a *actor) call(r protocol.Request) (protocol.Response, error) {
	if r.To != a.role || r.AtMS < 0 || r.AtMS > 100000 ||
		r.Token < 0 || r.Token > durable.MaxToken || (r.Operation != "write" && r.Key != "") {
		return protocol.Response{}, fmt.Errorf("invalid actor request")
	}
	if r.Operation == "inspect" {
		if r.Token != 0 {
			return protocol.Response{}, fmt.Errorf("inspection token must be zero")
		}
		response := protocol.Response{Status: "snapshot"}
		if a.log != nil {
			s := a.log.Snapshot()
			response.Token, response.Fence, response.Effects = s.Epoch, s.Fence, len(s.Effects)
		}
		return response, nil
	}
	if r.Token == 0 {
		return protocol.Response{}, fmt.Errorf("operation requires a positive token")
	}
	switch a.role {
	case "authority":
		epoch := a.log.Snapshot().Epoch
		switch r.Operation {
		case "reserve":
			if r.Token != epoch+1 {
				return protocol.Response{}, fmt.Errorf("reservation would reuse or skip an epoch")
			}
			token, err := a.log.Reserve()
			return protocol.Response{Status: "reserved", Token: token}, err
		case "ack":
			if r.Token > epoch {
				return protocol.Response{}, fmt.Errorf("acknowledgment for unreserved epoch")
			}
			a.ack = max(a.ack, r.Token)
			return protocol.Response{Status: "acknowledged", Token: r.Token}, nil
		case "activate":
			if r.Token != epoch || r.Token <= a.active || r.Token <= a.recoveredEpoch || (a.barrier && a.ack < r.Token) {
				return protocol.Response{}, fmt.Errorf("activation lacks current reservation or fence acknowledgment")
			}
			a.active = r.Token
			return protocol.Response{Status: "active", Token: r.Token}, nil
		case "result":
			if r.Token > epoch {
				return protocol.Response{}, fmt.Errorf("result for unreserved epoch")
			}
			if r.Token == a.active && r.Token == epoch {
				a.completed = true
			}
			return protocol.Response{Status: "result", Token: r.Token, Completed: a.completed}, nil
		}
	case "store":
		switch r.Operation {
		case "fence":
			if err := a.log.Fence(r.Token); err != nil {
				return protocol.Response{}, err
			}
			state := a.log.Snapshot()
			return protocol.Response{Status: "fenced", Fence: state.Fence, Effects: len(state.Effects)}, nil
		case "write":
			result, err := a.log.Write(r.Token, r.Key, a.policy)
			if err != nil {
				return protocol.Response{}, err
			}
			state := a.log.Snapshot()
			return protocol.Response{Status: result.Status, Token: result.Effect.Token, Fence: state.Fence, Effects: len(state.Effects)}, nil
		}
	case "worker-a", "worker-b":
		expected := 1
		if a.role == "worker-b" {
			expected = 2
		}
		if r.Token != expected {
			return protocol.Response{}, fmt.Errorf("worker assigned unexpected attempt")
		}
		switch r.Operation {
		case "dispatch":
			status := "started"
			if a.started {
				status = "duplicate"
			}
			a.started = true
			return protocol.Response{Status: status, Token: r.Token}, nil
		case "work":
			if !a.started {
				return protocol.Response{}, fmt.Errorf("work requested before dispatch")
			}
			return protocol.Response{Status: "ready", Token: r.Token}, nil
		}
	}
	return protocol.Response{}, fmt.Errorf("operation %q is not allowed for %s", r.Operation, a.role)
}
