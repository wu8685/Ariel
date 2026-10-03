package desktopipc

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"
)

func TestObserveFollowsExactOwnerAndUnsubscribes(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	c := NewClient(a, Options{})
	defer c.Close()
	type result struct {
		s   Observation
		err error
	}
	done := make(chan result, 1)
	go func() {
		s, err := c.Observe(context.Background(), "fixture-thread", 20*time.Millisecond)
		done <- result{s, err}
	}()
	m := readMessage(t, b)
	if stringField(m, "method") != "thread-owner-discovery" {
		t.Fatal("missing discovery")
	}
	sendReply(t, b, stringField(m, "requestId"), "success", map[string]any{})
	follow := readMessage(t, b)
	if stringField(follow, "method") != "thread-stream-following-changed" || string(follow["targetClientIds"]) != `["owner"]` {
		t.Fatalf("bad subscription: %s", follow)
	}
	load := readMessage(t, b)
	if stringField(load, "method") != "thread-follower-load-complete-history" || stringField(load, "targetClientId") != "owner" {
		t.Fatal("bad load target")
	}
	event := json.RawMessage(`{"type":"broadcast","sourceClientId":"owner","method":"thread-stream-state-changed","version":11,"params":{"hostId":"local","conversationId":"fixture-thread","change":{"type":"snapshot","revision":1,"conversationState":{"requests":[],"turns":[]}}}}`)
	if err := WriteFrame(b, event, 4096); err != nil {
		t.Fatal(err)
	}
	sendReply(t, b, stringField(load, "requestId"), "success", map[string]any{"revision": 1})
	unfollow := readMessage(t, b)
	if stringField(unfollow, "method") != "thread-stream-following-changed" {
		t.Fatal("no cleanup")
	}
	var params struct {
		Following bool `json:"following"`
	}
	json.Unmarshal(unfollow["params"], &params)
	if params.Following {
		t.Fatal("subscription leaked")
	}
	r := <-done
	if r.err != nil || !r.s.SnapshotSeen || r.s.Snapshots != 1 || !r.s.InteractionStateKnown {
		t.Fatalf("result: %+v, %v", r.s, r.err)
	}
}

func TestObserverRejectsWrongOwnerOrVersion(t *testing.T) {
	for _, body := range []string{
		`{"type":"broadcast","sourceClientId":"other","method":"thread-stream-state-changed","version":11,"params":{"hostId":"local","conversationId":"fixture","change":{"type":"snapshot","revision":1,"conversationState":{}}}}`,
		`{"type":"broadcast","sourceClientId":"owner","method":"thread-stream-state-changed","version":999,"params":{"hostId":"local","conversationId":"fixture","change":{"type":"snapshot","revision":1,"conversationState":{}}}}`,
	} {
		o := newObservationState("fixture", "owner")
		if err := o.apply(json.RawMessage(body)); err == nil {
			t.Fatal("untrusted snapshot accepted")
		}
	}
}
