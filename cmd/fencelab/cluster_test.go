package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/PoojaAgarwal2003/FenceLab/internal/cluster"
)

func clusterAddress(t *testing.T) string {
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

type clusterChild struct {
	cmd    *exec.Cmd
	done   chan error
	output *os.File
}

func (c *clusterChild) stop(t *testing.T) {
	t.Helper()
	if c.cmd == nil {
		return
	}
	select {
	case <-c.done:
	default:
		if err := c.cmd.Process.Kill(); err != nil {
			t.Error(err)
		}
		<-c.done
	}
	if err := c.output.Close(); err != nil {
		t.Error(err)
	}
	c.cmd = nil
}

func TestClusterProcesses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	root := t.TempDir()
	executable := filepath.Join(root, "fencelab.exe")
	t.Cleanup(func() {
		deadline := time.Now().Add(5 * time.Second)
		for {
			err := os.Remove(executable)
			if err == nil || os.IsNotExist(err) {
				return
			}
			if time.Now().After(deadline) {
				t.Error(err)
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
	if output, err := exec.CommandContext(ctx, "go", "build", "-o", executable, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, output)
	}
	pki := filepath.Join(root, "pki")
	if output, err := exec.CommandContext(ctx, executable, "cluster-pki", "-dir", pki).CombinedOutput(); err != nil {
		t.Fatalf("PKI: %v: %s", err, output)
	}
	var peers, endpoints []string
	var raftAddresses, apiAddresses [3]string
	for i := range raftAddresses {
		raftAddresses[i] = clusterAddress(t)
		apiAddresses[i] = clusterAddress(t)
		peers = append(peers, fmt.Sprintf("node%d=%s", i+1, raftAddresses[i]))
		endpoints = append(endpoints, "https://"+apiAddresses[i])
	}
	var children []*clusterChild
	start := func(name string, args ...string) *clusterChild {
		output, err := os.CreateTemp(root, name+"-*.log")
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, executable, args...)
		cmd.Dir = root
		cmd.Stdout = output
		cmd.Stderr = output
		if err := cmd.Start(); err != nil {
			output.Close()
			t.Fatal(err)
		}
		child := &clusterChild{cmd: cmd, done: make(chan error, 1), output: output}
		go func() { child.done <- cmd.Wait() }()
		children = append(children, child)
		return child
	}
	t.Cleanup(func() {
		for _, child := range children {
			child.stop(t)
		}
		if t.Failed() {
			files, _ := filepath.Glob(filepath.Join(root, "*.log"))
			for _, file := range files {
				data, _ := os.ReadFile(file)
				if len(data) > 6000 {
					data = data[len(data)-6000:]
				}
				t.Log(filepath.Base(file), string(data))
			}
		}
	})
	startNode := func(i int) *clusterChild {
		id := fmt.Sprintf("node%d", i+1)
		args := []string{"cluster-node", "-id", id, "-dir", filepath.Join(root, id), "-credentials", filepath.Join(pki, id),
			"-raft-listen", raftAddresses[i], "-api-listen", apiAddresses[i], "-peers", strings.Join(peers, ",")}
		if i == 0 {
			args = append(args, "-bootstrap")
		}
		return start(id, args...)
	}
	var nodes [3]*clusterChild
	for i := range nodes {
		nodes[i] = startNode(i)
	}
	newClient := func(role string, addresses []string) *cluster.Client {
		config, err := cluster.LoadTLS(filepath.Join(pki, role), "client", false)
		if err != nil {
			t.Fatal(err)
		}
		client, err := cluster.NewClient(addresses, config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(client.Close)
		return client
	}
	admin := newClient("admin", endpoints)
	await := func(label string, condition func() bool) {
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if condition() {
				return
			}
			time.Sleep(30 * time.Millisecond)
		}
		t.Fatal("timeout:", label)
	}
	await("initial quorum", func() bool { r, e := admin.Call(ctx, "status", cluster.Command{}); return e == nil && r.Status == "ok" })
	call := func(client *cluster.Client, op string, c cluster.Command) cluster.Reply {
		r, err := client.Call(ctx, op, c)
		if err != nil {
			t.Fatal(op, err)
		}
		return r
	}
	spec := cluster.Spec{Key: "interrupted", Payload: "durable-work", WorkMS: 500, MaxAttempts: 3}
	if r := call(admin, "enqueue", cluster.Command{Spec: spec}); r.Status != "enqueued" {
		t.Fatal(r)
	}
	startWorker := func(id string) *clusterChild {
		return start(id, "cluster-worker", "-credentials", filepath.Join(pki, id), "-endpoints", strings.Join(endpoints, ","), "-lease-ms", "2000")
	}
	firstWorker := startWorker("worker1")
	await("worker1 claim", func() bool {
		r, e := admin.Call(ctx, "status", cluster.Command{Key: spec.Key})
		return e == nil && r.Job != nil && r.Job.Status == "leased" && r.Job.Worker == "worker1"
	})
	firstWorker.stop(t)
	secondWorker := startWorker("worker2")
	var recovered cluster.Job
	await("lease recovery", func() bool {
		r, e := admin.Call(ctx, "status", cluster.Command{Key: spec.Key})
		if e == nil && r.Job != nil && r.Job.Status == "completed" {
			recovered = *r.Job
			return true
		}
		return false
	})
	if recovered.Generation != 2 || recovered.Worker != "worker2" {
		t.Fatal(recovered)
	}
	oldWorkerClient := newClient("worker1", endpoints)
	stale := call(oldWorkerClient, "complete", cluster.Command{Key: spec.Key, Generation: 1, Result: cluster.Digest(spec.Payload)})
	if stale.Status != "stale" {
		t.Fatal(stale)
	}
	if _, err := oldWorkerClient.Call(ctx, "enqueue", cluster.Command{Spec: cluster.Spec{Key: "unauthorized", MaxAttempts: 1}}); err == nil {
		t.Fatal("worker could enqueue")
	}
	if _, err := oldWorkerClient.Call(ctx, "claim", cluster.Command{Worker: "worker2", Request: "spoof", LeaseMS: 2000}); err == nil {
		t.Fatal("worker identity spoof accepted")
	}
	firstWorker = startWorker("worker1")
	for i := 0; i < 8; i++ {
		r := call(admin, "enqueue", cluster.Command{Spec: cluster.Spec{Key: fmt.Sprintf("parallel-%d", i), Payload: fmt.Sprint(i), Class: i % 2, WorkMS: 400, MaxAttempts: 3}})
		if r.Status != "enqueued" {
			t.Fatal(r)
		}
	}
	await("two simultaneous remote claims", func() bool {
		r, e := admin.Call(ctx, "status", cluster.Command{})
		return e == nil && r.Counts.Leased == 2
	})
	await("nine completed jobs", func() bool {
		r, e := admin.Call(ctx, "status", cluster.Command{})
		return e == nil && r.Counts.Completed == 9
	})
	firstWorker.stop(t)
	secondWorker.stop(t)
	var jobs []cluster.Job
	owners := map[string]bool{}
	for i := 0; i < 8; i++ {
		r := call(admin, "status", cluster.Command{Key: fmt.Sprintf("parallel-%d", i)})
		if r.Job == nil || r.Job.Result != cluster.Digest(fmt.Sprint(i)) {
			t.Fatal(r)
		}
		jobs = append(jobs, *r.Job)
		owners[r.Job.Worker] = true
	}
	if len(owners) != 2 {
		t.Fatal("only one process completed parallel work", owners)
	}
	singles := make([]*cluster.Client, 3)
	for i := range singles {
		singles[i] = newClient("admin", []string{endpoints[i]})
	}
	findLeader := func() int {
		found := -1
		await("leader identification", func() bool {
			for i, client := range singles {
				if nodes[i] != nil {
					if _, err := client.Call(ctx, "ready", cluster.Command{}); err == nil {
						found = i
						return true
					}
				}
			}
			return false
		})
		return found
	}
	leader := findLeader()
	nodes[leader].stop(t)
	nodes[leader] = nil
	await("leader failover preserves results", func() bool {
		r, e := admin.Call(ctx, "status", cluster.Command{})
		return e == nil && r.Counts.Completed == 9
	})
	if r := call(admin, "enqueue", cluster.Command{Spec: spec}); r.Status != "existing" {
		t.Fatal("submission duplicated across failover", r)
	}
	next := findLeader()
	nodes[next].stop(t)
	nodes[next] = nil
	time.Sleep(1500 * time.Millisecond)
	if _, err := admin.Call(ctx, "enqueue", cluster.Command{Spec: cluster.Spec{Key: "minority", MaxAttempts: 1}}); err == nil {
		t.Fatal("minority acknowledged submission")
	}
	for i, node := range nodes {
		if node != nil {
			node.stop(t)
		}
		nodes[i] = startNode(i)
	}
	var final cluster.Reply
	await("all three restart from disks", func() bool {
		r, e := admin.Call(ctx, "status", cluster.Command{})
		if e == nil && r.Counts.Completed == 9 {
			final = r
			return true
		}
		return false
	})
	if final.Counts.Retained != 9 {
		t.Fatal("rejected minority write or duplicate became a new job", final)
	}
	if target := os.Getenv("FENCELAB_CLUSTER_EVIDENCE"); target != "" {
		report := struct {
			Version          string        `json:"version"`
			Recorded         string        `json:"recorded"`
			Environment      string        `json:"environment"`
			Voters           int           `json:"voters"`
			WorkerProcesses  int           `json:"worker_processes"`
			ConcurrentClaims int           `json:"observed_simultaneous_claims"`
			Recovered        cluster.Job   `json:"recovered_worker_job"`
			Stale            cluster.Reply `json:"old_generation_completion"`
			Jobs             []cluster.Job `json:"parallel_jobs"`
			Final            cluster.Reply `json:"after_quorum_loss_and_full_restart"`
		}{"fencelab/cluster-evidence-v1", time.Now().UTC().Format(time.RFC3339), runtime.GOOS + "/" + runtime.GOARCH + " " + runtime.Version(), 3, 2, 2, recovered, stale, jobs, final}
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := file.Write(append(data, '\n'))
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			t.Fatal(writeErr, closeErr)
		}
	}
}

func TestClusterCLIValidation(t *testing.T) {
	for _, command := range []string{"cluster-pki", "cluster-node", "cluster-client", "cluster-worker"} {
		var out, stderr bytes.Buffer
		if code := run(context.Background(), []string{command, "-help"}, &out, &stderr); code != 0 {
			t.Fatal(command, code)
		}
		if code := run(context.Background(), []string{command}, &out, &stderr); code != 2 {
			t.Fatal(command, code)
		}
	}
}
