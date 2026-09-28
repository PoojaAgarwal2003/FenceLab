package model

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/PoojaAgarwal2003/FenceLab/internal/protocol"
)

type brokenTransport struct{ err error }

func (b brokenTransport) Call(context.Context, protocol.Request) (protocol.Response, error) {
	return protocol.Response{}, b.err
}

func TestSeededTransportReplayAndErrors(t *testing.T) {
	for seed := int64(0); seed < 20; seed++ {
		result, err := RunTransport(context.Background(), Example("eager"), seed, nil)
		if err != nil {
			t.Fatal(err)
		}
		replayed, err := Replay(context.Background(), result.Replay)
		if err != nil || !reflect.DeepEqual(result, replayed) {
			t.Fatal("seeded execution and exact replay differ", seed, err)
		}
	}
	for _, transport := range []brokenTransport{{err: errors.New("offline")}, {}} {
		if _, err := RunTransport(context.Background(), Example("barrier"), 7, transport); err == nil {
			t.Fatal("ignored error or mismatched actor response")
		}
	}
}
