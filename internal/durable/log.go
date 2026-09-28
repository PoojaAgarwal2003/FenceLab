// Package durable provides a single-writer, synchronous WAL for the laboratory.
// The effect is a ledger entry in this WAL, not an external payment or RPC.
package durable

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"slices"
	"sync"
	"unicode/utf8"

	"github.com/PoojaAgarwal2003/FenceLab/internal/model"
	"github.com/PoojaAgarwal2003/FenceLab/internal/sim"
)

const headerSize = 24
const MaxBytes = 16 << 20
const MaxToken = 1_000_000_000

type Record struct {
	Operation string     `json:"operation"`
	Token     int        `json:"token"`
	Key       string     `json:"key,omitempty"`
	Policy    sim.Policy `json:"policy,omitempty"`
}

type Effect struct {
	Sequence uint64 `json:"sequence"`
	Token    int    `json:"token"`
	Key      string `json:"key"`
}

type State struct {
	Sequence uint64   `json:"sequence"`
	Epoch    int      `json:"epoch"`
	Fence    int      `json:"fence"`
	Effects  []Effect `json:"effects"`
}

type Outcome struct {
	Status string `json:"status"`
	Effect Effect `json:"effect"`
}

type Recovery struct {
	Records        uint64 `json:"records"`
	TruncatedBytes int64  `json:"truncated_bytes"`
}

// Hook is for deliberate crash injection only. It runs with the writer locked.
// A returned error poisons the writer; callers must close and recover it.
type Hook func(point string) error

type Log struct {
	mu       sync.Mutex
	file     *os.File
	state    State
	keys     map[string]Effect
	bytes    int64
	poisoned error
	hook     Hook
}

func Open(name string, hook Hook) (*Log, Recovery, error) {
	file, err := os.OpenFile(name, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, Recovery{}, err
	}
	fail := func(err error) (*Log, Recovery, error) {
		return nil, Recovery{}, errors.Join(err, file.Close())
	}
	if err := lockFile(file); err != nil {
		return fail(fmt.Errorf("WAL already owned or lock unavailable: %w", err))
	}
	l := &Log{file: file, keys: map[string]Effect{}, state: State{Effects: []Effect{}}, hook: hook}
	info, err := file.Stat()
	if err != nil {
		return fail(err)
	}
	if info.Size() > MaxBytes {
		return fail(fmt.Errorf("WAL exceeds %d bytes; compaction is not implemented", MaxBytes))
	}
	recovery := Recovery{}
	for l.bytes < info.Size() {
		header := make([]byte, headerSize)
		n, err := io.ReadFull(file, header)
		if err == io.ErrUnexpectedEOF || err == io.EOF {
			recovery.TruncatedBytes = int64(n)
			break
		}
		if err != nil {
			return fail(err)
		}
		length := binary.LittleEndian.Uint32(header[12:16])
		sequence := binary.LittleEndian.Uint64(header[4:12])
		if string(header[:4]) != "FLW1" || length == 0 || length > 4096 ||
			binary.LittleEndian.Uint32(header[16:20]) != ^length || sequence != l.state.Sequence+1 {
			return fail(fmt.Errorf("corrupt WAL header at byte %d", l.bytes))
		}
		body := make([]byte, length)
		n, err = io.ReadFull(file, body)
		if err == io.ErrUnexpectedEOF || err == io.EOF {
			recovery.TruncatedBytes = int64(headerSize + n)
			break
		}
		if err != nil {
			return fail(err)
		}
		if checksum(header[:20], body) != binary.LittleEndian.Uint32(header[20:24]) {
			return fail(fmt.Errorf("WAL checksum mismatch at byte %d", l.bytes))
		}
		var record Record
		if err := model.Decode(bytes.NewReader(body), &record); err != nil {
			return fail(fmt.Errorf("WAL record at byte %d: %w", l.bytes, err))
		}
		if err := l.validate(record); err != nil {
			return fail(fmt.Errorf("invalid WAL transition at byte %d: %w", l.bytes, err))
		}
		l.apply(record)
		l.bytes += int64(headerSize) + int64(length)
	}
	if recovery.TruncatedBytes != 0 {
		if err := file.Truncate(l.bytes); err != nil {
			return fail(err)
		}
	}
	// Also sync complete unacknowledged frames before exposing recovered state.
	if err := file.Sync(); err != nil {
		return fail(err)
	}
	if _, err := file.Seek(l.bytes, io.SeekStart); err != nil {
		return fail(err)
	}
	recovery.Records = l.state.Sequence
	return l, recovery, nil
}

func checksum(header, body []byte) uint32 {
	crc := crc32.New(crc32.MakeTable(crc32.Castagnoli))
	_, _ = crc.Write(header)
	_, _ = crc.Write(body)
	return crc.Sum32()
}

func (l *Log) validate(r Record) error {
	if r.Token < 1 || r.Token > MaxToken {
		return fmt.Errorf("token must be 1..%d", MaxToken)
	}
	switch r.Operation {
	case "reserve":
		if r.Token != l.state.Epoch+1 || r.Key != "" || r.Policy != "" {
			return fmt.Errorf("reservation must advance the epoch by exactly one")
		}
	case "fence":
		if r.Token <= l.state.Fence || r.Key != "" || r.Policy != "" {
			return fmt.Errorf("logged fence must strictly increase")
		}
	case "write":
		if len(r.Key) < 1 || len(r.Key) > 128 || !utf8.ValidString(r.Key) {
			return fmt.Errorf("effect key must contain 1-128 valid UTF-8 bytes")
		}
		if r.Policy != sim.LeaseOnly && r.Policy != sim.Fenced && r.Policy != sim.Idempotent {
			return fmt.Errorf("invalid storage policy")
		}
		if r.Policy != sim.LeaseOnly && r.Token < l.state.Fence {
			return fmt.Errorf("logged write is fenced out")
		}
		if _, found := l.keys[r.Key]; found && r.Policy == sim.Idempotent {
			return fmt.Errorf("logged write duplicates an idempotency key")
		}
	default:
		return fmt.Errorf("unknown WAL operation %q", r.Operation)
	}
	return nil
}

func (l *Log) apply(r Record) {
	l.state.Sequence++
	switch r.Operation {
	case "reserve":
		l.state.Epoch = r.Token
	case "fence":
		l.state.Fence = r.Token
	case "write":
		e := Effect{Sequence: l.state.Sequence, Token: r.Token, Key: r.Key}
		l.state.Effects = append(l.state.Effects, e)
		if _, found := l.keys[r.Key]; !found {
			l.keys[r.Key] = e
		}
	}
}

func (l *Log) checkpoint(point string) error {
	if l.hook != nil {
		if err := l.hook(point); err != nil {
			l.poisoned = err
			return err
		}
	}
	return nil
}

func (l *Log) append(r Record) error {
	if l.poisoned != nil {
		return fmt.Errorf("WAL unavailable; close and recover: %w", l.poisoned)
	}
	if err := l.validate(r); err != nil {
		return err
	}
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	header := make([]byte, headerSize)
	copy(header, "FLW1")
	binary.LittleEndian.PutUint64(header[4:12], l.state.Sequence+1)
	binary.LittleEndian.PutUint32(header[12:16], uint32(len(body)))
	binary.LittleEndian.PutUint32(header[16:20], ^uint32(len(body)))
	binary.LittleEndian.PutUint32(header[20:24], checksum(header[:20], body))
	if l.bytes+int64(len(header)+len(body)) > MaxBytes {
		return fmt.Errorf("WAL capacity reached; compaction is not implemented")
	}
	if err := l.checkpoint("before-append"); err != nil {
		return err
	}
	for i, data := range [][]byte{header, body} {
		n, err := l.file.Write(data)
		if err == nil && n != len(data) {
			err = io.ErrShortWrite
		}
		if err != nil {
			l.poisoned = err
			return err
		}
		point := "after-header"
		if i == 1 {
			point = "after-append"
		}
		if err := l.checkpoint(point); err != nil {
			return err
		}
	}
	if err := l.file.Sync(); err != nil {
		l.poisoned = err
		return err
	}
	if err := l.checkpoint("after-sync"); err != nil {
		return err
	}
	l.bytes += int64(len(header) + len(body))
	l.apply(r)
	return l.checkpoint("after-apply")
}

func (l *Log) Snapshot() State {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.state
	s.Effects = slices.Clone(s.Effects)
	return s
}

func (l *Log) Reserve() (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	token := l.state.Epoch + 1
	return token, l.append(Record{Operation: "reserve", Token: token})
}

func (l *Log) Fence(token int) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.poisoned != nil {
		return l.poisoned
	}
	if token < 1 || token > MaxToken {
		return fmt.Errorf("invalid fence token")
	}
	if token <= l.state.Fence {
		return nil
	}
	return l.append(Record{Operation: "fence", Token: token})
}

func (l *Log) Write(token int, key string, policy sim.Policy) (Outcome, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.poisoned != nil {
		return Outcome{}, l.poisoned
	}
	r := Record{Operation: "write", Token: token, Key: key, Policy: policy}
	if token < 1 || token > MaxToken || len(key) < 1 || len(key) > 128 || !utf8.ValidString(key) ||
		(policy != sim.LeaseOnly && policy != sim.Fenced && policy != sim.Idempotent) {
		return Outcome{}, fmt.Errorf("invalid effect request")
	}
	if policy != sim.LeaseOnly && token < l.state.Fence {
		return Outcome{Status: "rejected"}, nil
	}
	if e, found := l.keys[key]; found && policy == sim.Idempotent {
		return Outcome{Status: "deduplicated", Effect: e}, nil
	}
	if err := l.append(r); err != nil {
		return Outcome{}, err
	}
	return Outcome{Status: "committed", Effect: l.state.Effects[len(l.state.Effects)-1]}, nil
}

func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.poisoned = os.ErrClosed
	return l.file.Close()
}
