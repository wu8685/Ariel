package desktopipc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type staleRPC struct {
	called  bool
	receipt bool
}

func (f *staleRPC) Call(ctx context.Context, req Request) (Reply, error) {
	f.called = true
	if req.Method != "thread-follower-submit-user-input" || req.Version != 1 || !req.Mutating || req.TargetClientID != "owner" {
		return Reply{}, errors.New("wrong follower request")
	}
	p := req.Params.(map[string]any)
	if !strings.HasPrefix(p["requestId"].(string), "ariel-nonexistent-") || p["conversationId"] != "fixture" {
		return Reply{}, errors.New("not a guaranteed test request")
	}
	b, _ := json.Marshal(map[string]bool{"ok": f.receipt})
	return Reply{Result: b}, nil
}

func TestStaleInteractionProbeOnlyTargetsEmptyFixtureState(t *testing.T) {
	for _, tc := range []struct {
		state   string
		allowed bool
	}{
		{`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[]}`, true},
		{`{"cwd":"/fixture","threadRuntimeStatus":{"type":"active"},"requests":[{"id":"live"}]}`, false},
		{`{"cwd":"/business","threadRuntimeStatus":{"type":"idle"},"requests":[]}`, false},
	} {
		f := &staleRPC{receipt: true}
		ack, err := sendMissingUserInput(context.Background(), f, "owner", "fixture", "/fixture", json.RawMessage(tc.state))
		if f.called != tc.allowed || (err == nil) != tc.allowed {
			t.Fatalf("%+v: called %v error %v", tc, f.called, err)
		}
		if tc.allowed && !ack {
			t.Fatal("failed to record misleading native receipt")
		}
	}
}
