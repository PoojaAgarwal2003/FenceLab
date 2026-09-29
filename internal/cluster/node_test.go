package cluster

import (
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/hashicorp/raft"
)

func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func awaitLeader(t *testing.T, nodes []*Node, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for i, node := range nodes {
			if node != nil && node.Raft.State() == raft.Leader {
				if err := node.Raft.Barrier(time.Second).Error(); err == nil {
					return i
				}
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("no writable quorum leader")
	return -1
}

func TestThreeVoterFailoverAndDiskRestart(t *testing.T) {
	root := t.TempDir()
	pki := filepath.Join(root, "pki")
	if err := GeneratePKI(pki); err != nil {
		t.Fatal(err)
	}
	peers := []Peer{{ID: "node1", Address: freeAddress(t)}, {ID: "node2", Address: freeAddress(t)}, {ID: "node3", Address: freeAddress(t)}}
	nodes := make([]*Node, 3)
	configs := make([]Config, 3)
	t.Cleanup(func() {
		for _, node := range nodes {
			if node != nil {
				if err := node.Close(); err != nil {
					t.Error(err)
				}
			}
		}
	})
	for i, peer := range peers {
		tlsConfig, err := LoadTLS(filepath.Join(pki, peer.ID), "raft", true)
		if err != nil {
			t.Fatal(err)
		}
		configs[i] = Config{ID: peer.ID, Directory: filepath.Join(root, peer.ID), Listen: peer.Address, Peers: peers, Bootstrap: i == 0, TLS: tlsConfig, Log: io.Discard}
		nodes[i], err = Open(configs[i])
		if err != nil {
			t.Fatal(err)
		}
	}
	leader := awaitLeader(t, nodes, 10*time.Second)
	r, err := nodes[leader].Apply(Command{Operation: "enqueue", Spec: Spec{Key: "durable-job", Payload: "quorum", MaxAttempts: 3}})
	if err != nil || r.Status != "enqueued" {
		t.Fatal(r, err)
	}
	r, err = nodes[leader].Apply(Command{Operation: "claim", Worker: "worker1", Request: "claim1", LeaseMS: 1000})
	if err != nil || r.Status != "claimed" {
		t.Fatal(r, err)
	}
	if err := nodes[leader].Raft.Snapshot().Error(); err != nil {
		t.Fatal(err)
	}
	if err := nodes[leader].Close(); err != nil {
		t.Fatal(err)
	}
	nodes[leader] = nil
	next := awaitLeader(t, nodes, 10*time.Second)
	time.Sleep(1100 * time.Millisecond)
	r, err = nodes[next].Apply(Command{Operation: "claim", Worker: "worker2", Request: "claim2", LeaseMS: 1000})
	if err != nil || r.Job == nil || r.Job.Generation != 2 {
		t.Fatal(r, err)
	}
	r, err = nodes[next].Apply(Command{Operation: "complete", Worker: "worker2", Key: "durable-job", Generation: 2, Result: Digest("quorum")})
	if err != nil || r.Status != "completed" {
		t.Fatal(r, err)
	}
	nodes[leader], err = Open(configs[leader])
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for nodes[leader].FSM.View("").Counts.Completed != 1 && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	if nodes[leader].FSM.View("").Counts.Completed != 1 {
		t.Fatal("restarted node failed to catch up")
	}
	for i, node := range nodes {
		if err := node.Close(); err != nil {
			t.Fatal(err)
		}
		nodes[i] = nil
	}
	for i := range nodes {
		nodes[i], err = Open(configs[i])
		if err != nil {
			t.Fatal(err)
		}
	}
	leader = awaitLeader(t, nodes, 10*time.Second)
	r, err = nodes[leader].Query("durable-job")
	if err != nil || r.Job == nil || r.Job.Result != Digest("quorum") {
		t.Fatal("full disk restart lost effect", r, err)
	}
	for i, node := range nodes {
		if i != leader {
			if err := node.Close(); err != nil {
				t.Fatal(err)
			}
			nodes[i] = nil
		}
	}
	time.Sleep(1500 * time.Millisecond)
	if _, err := nodes[leader].Apply(Command{Operation: "enqueue", Spec: Spec{Key: "no-quorum", MaxAttempts: 1}}); err == nil {
		t.Fatal("minority acknowledged a write")
	}
}
