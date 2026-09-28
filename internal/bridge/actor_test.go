package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PoojaAgarwal2003/FenceLab/internal/durable"
	"github.com/PoojaAgarwal2003/FenceLab/internal/model"
	"github.com/PoojaAgarwal2003/FenceLab/internal/protocol"
	"github.com/PoojaAgarwal2003/FenceLab/internal/sim"
)

func TestAuthorityRequiresBarrierAndFreshReservation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "authority.wal")
	log, _, err := durable.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := &actor{role: "authority", log: log, barrier: true}
	call := func(operation string, token int) error {
		_, err := a.call(protocol.Request{To: "authority", Operation: operation, Token: token})
		return err
	}
	if err := call("reserve", 1); err != nil {
		t.Fatal(err)
	}
	if err := call("activate", 1); err == nil {
		t.Fatal("activated before barrier acknowledgment")
	}
	if err := call("ack", 1); err != nil {
		t.Fatal(err)
	}
	if err := call("activate", 1); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	var input, output bytes.Buffer
	for i, operation := range []string{"ack", "activate"} {
		command := protocol.Command{Version: protocol.Version, Sequence: i + 1, Request: protocol.Request{To: "authority", Operation: operation, Token: 1}}
		if err := json.NewEncoder(&input).Encode(command); err != nil {
			t.Fatal(err)
		}
	}
	if err := ServeActor(context.Background(), "authority", path, sim.Idempotent, "barrier", &input, &output); err == nil {
		t.Fatal("restarted authority activated a recovered lease")
	}
}

func TestWorkerDispatchAndPrivateProtocol(t *testing.T) {
	a := &actor{role: "worker-a"}
	request := protocol.Request{To: "worker-a", Operation: "work", Token: 1}
	if _, err := a.call(request); err == nil {
		t.Fatal("work before dispatch")
	}
	request.Operation = "dispatch"
	for _, status := range []string{"started", "duplicate"} {
		response, err := a.call(request)
		if err != nil || response.Status != status {
			t.Fatal(response, err)
		}
	}
	request.Token = 2
	if _, err := a.call(request); err == nil {
		t.Fatal("wrong worker epoch")
	}
	for _, input := range []string{
		`{"version":"wrong","sequence":1,"request":{}}`,
		`{"version":"fencelab/actor-v1","sequence":2,"request":{}}`,
		`{"version":"fencelab/actor-v1","sequence":1,"request":{"to":"worker-a","to":"store"}}`,
		strings.Repeat("x", model.MaxInput+1),
	} {
		var output bytes.Buffer
		if err := ServeActor(context.Background(), "worker-a", "", sim.Idempotent, "barrier", strings.NewReader(input+"\n"), &output); err == nil {
			t.Fatalf("accepted malformed command %.80s", input)
		}
	}
}

func TestHistoryOracleRejectsInvalidCountsAndOrdering(t *testing.T) {
	history := []protocol.Entry{
		{Sequence: 1, Request: protocol.Request{Operation: "activate", Token: 2}, Response: protocol.Response{Status: "active"}},
		{Sequence: 2, Request: protocol.Request{Operation: "write", Token: 1, Key: "key"}, Response: protocol.Response{Status: "committed", Effects: 1}},
		{Sequence: 3, Request: protocol.Request{Operation: "write", Token: 2, Key: "key"}, Response: protocol.Response{Status: "committed", Effects: 2}},
	}
	summary, err := Summarize(history)
	if err != nil || summary.Safe || summary.StaleWrites != 1 || summary.DuplicateWrites != 1 {
		t.Fatal(summary, err)
	}
	history[2].Response.Effects = 1
	if _, err := Summarize(history); err == nil {
		t.Fatal("accepted inconsistent effect count")
	}
	history[0].Sequence = 2
	if _, err := Summarize(history); err == nil {
		t.Fatal("accepted reordered history")
	}
}

func TestBridgeStartupAndCancellation(t *testing.T) {
	s := model.Example("barrier")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, "absent", filepath.Join(t.TempDir(), "cancelled"), s, 7, nil); err == nil {
		t.Fatal("ignored cancellation")
	}
	if _, err := Run(context.Background(), filepath.Join(t.TempDir(), "missing-executable"), filepath.Join(t.TempDir(), "startup"), s, 7, nil); err == nil {
		t.Fatal("accepted missing actor executable")
	}
}
