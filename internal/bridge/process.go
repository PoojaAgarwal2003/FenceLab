package bridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"github.com/PoojaAgarwal2003/FenceLab/internal/model"
	"github.com/PoojaAgarwal2003/FenceLab/internal/protocol"
)

type child struct {
	cmd      *exec.Cmd
	in       io.WriteCloser
	out      *bufio.Scanner
	stderr   bytes.Buffer
	sequence int
	stopped  bool
}

type processes struct {
	mu       sync.Mutex
	children map[string]*child
	history  []protocol.Entry
}

func startChild(ctx context.Context, executable, role, wal string, s model.Scenario) (*child, error) {
	cmd := exec.CommandContext(ctx, executable, "actor", "-role", role, "-policy", string(s.Policy), "-protocol", s.Protocol)
	if wal != "" {
		cmd.Args = append(cmd.Args, "-wal", wal)
	}
	c := &child{cmd: cmd}
	cmd.Stderr = &c.stderr
	var err error
	c.in, err = cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errors.Join(err, c.in.Close())
	}
	c.out = bufio.NewScanner(pipe)
	c.out.Buffer(make([]byte, 4096), model.MaxInput)
	if err := cmd.Start(); err != nil {
		return nil, errors.Join(err, c.in.Close(), pipe.Close())
	}
	if _, err := c.call(protocol.Request{To: role, Operation: "inspect"}); err != nil {
		stopErr := c.stop(true)
		return nil, fmt.Errorf("actor %s startup: %w; stderr=%s", role, errors.Join(err, stopErr), c.stderr.String())
	}
	return c, nil
}

func (c *child) call(request protocol.Request) (protocol.Response, error) {
	if c.stopped {
		return protocol.Response{}, fmt.Errorf("actor has stopped")
	}
	c.sequence++
	command := protocol.Command{Version: protocol.Version, Sequence: c.sequence, Request: request}
	if err := json.NewEncoder(c.in).Encode(command); err != nil {
		return protocol.Response{}, err
	}
	if !c.out.Scan() {
		return protocol.Response{}, errors.Join(fmt.Errorf("actor closed response stream"), c.out.Err())
	}
	var reply protocol.Reply
	if err := model.Decode(bytes.NewReader(c.out.Bytes()), &reply); err != nil {
		return protocol.Response{}, err
	}
	if reply.Version != protocol.Version || reply.Sequence != c.sequence {
		return protocol.Response{}, fmt.Errorf("actor reply version or sequence mismatch")
	}
	if reply.Error != "" {
		return protocol.Response{}, fmt.Errorf("actor rejected request: %s", reply.Error)
	}
	return reply.Response, nil
}

func (c *child) stop(kill bool) error {
	if c.stopped {
		return nil
	}
	c.stopped = true
	if kill {
		// Kill before waiting; the WAL lock is released by the OS, not a defer.
		killErr := c.cmd.Process.Kill()
		_ = c.in.Close()
		waitErr := c.cmd.Wait()
		var exit *exec.ExitError
		if killErr != nil || !errors.As(waitErr, &exit) {
			return fmt.Errorf("actor termination: kill=%v wait=%v", killErr, waitErr)
		}
		return nil
	}
	closeErr := c.in.Close()
	return errors.Join(closeErr, c.cmd.Wait())
}

func (p *processes) Call(ctx context.Context, r protocol.Request) (protocol.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return protocol.Response{}, err
	}
	child := p.children[r.To]
	if child == nil {
		return protocol.Response{}, fmt.Errorf("unknown actor %q", r.To)
	}
	result, err := child.call(r)
	if err == nil {
		p.history = append(p.history, protocol.Entry{Sequence: len(p.history) + 1, Request: r, Response: result})
	}
	return result, err
}

func (p *processes) close() error {
	var errs []error
	for _, role := range []string{"authority", "worker-a", "worker-b", "store"} {
		if child := p.children[role]; child != nil {
			if err := child.stop(false); err != nil {
				errs = append(errs, fmt.Errorf("stop %s: %w; stderr=%s", role, err, child.stderr.String()))
			}
		}
	}
	return errors.Join(errs...)
}
