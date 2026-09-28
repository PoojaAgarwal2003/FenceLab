// Package model implements the v2 message-delivery model independently of v1.
package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/PoojaAgarwal2003/FenceLab/internal/sim"
)

const Version = "fencelab/v2"
const MaxInput = 65536
const MaxEvents = 256

type Fault struct {
	Kind    string `json:"kind"`
	From    string `json:"from"`
	To      string `json:"to"`
	Token   int    `json:"token"`
	Action  string `json:"action"`
	DelayMS int    `json:"delay_ms"`
}

type Partition struct {
	From    string `json:"from"`
	To      string `json:"to"`
	StartMS int    `json:"start_ms"`
	EndMS   int    `json:"end_ms"`
}

type Scenario struct {
	Version     string      `json:"version"`
	Name        string      `json:"name"`
	Policy      sim.Policy  `json:"policy"`
	Protocol    string      `json:"protocol"`
	LeaseMS     int         `json:"lease_ms"`
	WorkMS      int         `json:"work_ms"`
	LatencyMS   int         `json:"latency_ms"`
	ClockSkewMS ClockSkew   `json:"clock_skew_ms"`
	Faults      []Fault     `json:"faults"`
	Partitions  []Partition `json:"partitions"`
}

type ClockSkew [2]int

func (s *ClockSkew) UnmarshalJSON(data []byte) error {
	var values []int
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}
	if len(values) != 2 {
		return fmt.Errorf("clock_skew_ms must contain exactly two integers")
	}
	*s = ClockSkew{values[0], values[1]}
	return nil
}

func validActor(s string) bool {
	return s == "authority" || s == "store" || s == "worker-a" || s == "worker-b"
}

func (s Scenario) Validate() error {
	if s.Version != Version {
		return fmt.Errorf("version must be %s", Version)
	}
	if len(s.Name) == 0 || len(s.Name) > 80 {
		return fmt.Errorf("name must contain 1-80 bytes")
	}
	if s.Policy != sim.LeaseOnly && s.Policy != sim.Fenced && s.Policy != sim.Idempotent {
		return fmt.Errorf("invalid policy")
	}
	if s.Protocol != "barrier" && s.Protocol != "eager" {
		return fmt.Errorf("protocol must be barrier or eager")
	}
	if s.LeaseMS < 20 || s.LeaseMS > 1000 || s.WorkMS < 1 || s.WorkMS >= s.LeaseMS || s.LatencyMS < 1 || s.LatencyMS > 100 {
		return fmt.Errorf("lease_ms must be 20-1000, work_ms 1..lease_ms-1, latency_ms 1-100")
	}
	for _, skew := range s.ClockSkewMS {
		if skew < -10000 || skew > 10000 {
			return fmt.Errorf("clock skew must be within +/-10000 ms")
		}
	}
	if len(s.Faults) > 16 || len(s.Partitions) > 16 {
		return fmt.Errorf("at most 16 faults and 16 partitions")
	}
	for i, f := range s.Faults {
		if f.Kind != "fence" && f.Kind != "fence-ack" && f.Kind != "dispatch" && f.Kind != "write" && f.Kind != "result" {
			return fmt.Errorf("fault %d: invalid message kind", i)
		}
		if (f.From != "" && !validActor(f.From)) || (f.To != "" && !validActor(f.To)) || f.Token < 0 || f.Token > 2 {
			return fmt.Errorf("fault %d: invalid endpoint or token", i)
		}
		if f.Action != "delay" && f.Action != "drop" && f.Action != "duplicate" {
			return fmt.Errorf("fault %d: action must be delay, drop, or duplicate", i)
		}
		if f.DelayMS < 0 || f.DelayMS > 10000 || (f.Action == "delay" && f.DelayMS == 0) || (f.Action == "drop" && f.DelayMS != 0) {
			return fmt.Errorf("fault %d: invalid delay_ms", i)
		}
	}
	for i, p := range s.Partitions {
		if !validActor(p.From) || !validActor(p.To) || p.From == p.To || p.StartMS < 0 || p.EndMS <= p.StartMS || p.EndMS > 20000 {
			return fmt.Errorf("partition %d: invalid endpoints or half-open time interval", i)
		}
	}
	return nil
}

// Decode rejects unknown fields, duplicate keys, trailing values, and oversized
// input. The token walk checks duplicate keys at every nested object.
func Decode(r io.Reader, target any) error {
	data, err := io.ReadAll(io.LimitReader(r, MaxInput+1))
	if err != nil {
		return err
	}
	if len(data) > MaxInput {
		return fmt.Errorf("JSON exceeds %d bytes", MaxInput)
	}
	check := json.NewDecoder(bytes.NewReader(data))
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 32 {
			return fmt.Errorf("JSON nesting exceeds 32")
		}
		token, err := check.Token()
		if err != nil {
			return err
		}
		switch token {
		case json.Delim('{'):
			seen := map[string]bool{}
			for check.More() {
				key, err := check.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return fmt.Errorf("duplicate or invalid JSON key %v", key)
				}
				seen[name] = true
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
			_, err = check.Token()
		case json.Delim('['):
			for check.More() {
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
			_, err = check.Token()
		}
		return err
	}
	if err := walk(0); err != nil {
		return err
	}
	if _, err := check.Token(); err != io.EOF {
		return fmt.Errorf("expected exactly one JSON value")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func Example(protocol string) Scenario {
	return Scenario{
		Version: Version, Name: "Delayed replacement fence", Policy: sim.Idempotent,
		Protocol: protocol, LeaseMS: 20, WorkMS: 5, LatencyMS: 1,
		Faults: []Fault{
			{Kind: "write", From: "worker-a", Token: 1, Action: "delay", DelayMS: 20},
			{Kind: "fence", Token: 2, Action: "delay", DelayMS: 20},
		},
		Partitions: []Partition{},
	}
}
