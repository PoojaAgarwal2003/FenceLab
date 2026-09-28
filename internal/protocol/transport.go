// Package protocol defines the command boundary shared by the deterministic
// driver and the real-process transport. Timing remains controlled by the driver.
package protocol

import "context"

const Version = "fencelab/actor-v1"

type Envelope struct {
	ID    int    `json:"id"`
	Kind  string `json:"kind"`
	From  string `json:"from"`
	To    string `json:"to"`
	Token int    `json:"token"`
	Key   string `json:"key"`
}

type Request struct {
	AtMS      int    `json:"at_ms"`
	Operation string `json:"operation"`
	To        string `json:"to"`
	Token     int    `json:"token"`
	Key       string `json:"key"`
}

type Response struct {
	Status    string `json:"status"`
	Token     int    `json:"token"`
	Fence     int    `json:"fence"`
	Effects   int    `json:"effects"`
	Completed bool   `json:"completed"`
}

type Transport interface {
	Call(context.Context, Request) (Response, error)
}

type Command struct {
	Version  string  `json:"version"`
	Sequence int     `json:"sequence"`
	Request  Request `json:"request"`
}

type Reply struct {
	Version  string   `json:"version"`
	Sequence int      `json:"sequence"`
	Response Response `json:"response"`
	Error    string   `json:"error,omitempty"`
}

type Entry struct {
	Sequence int      `json:"sequence"`
	Request  Request  `json:"request"`
	Response Response `json:"response"`
}
