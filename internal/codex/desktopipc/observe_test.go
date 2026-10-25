package desktopipc

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

func TestLargeFollowerReducerAcceptsStateAboveEightMiB(t *testing.T) {
	event := json.RawMessage(`{"type":"broadcast","sourceClientId":"owner","method":"thread-stream-state-changed","version":11,"params":{"hostId":"local","conversationId":"fixture","change":{"type":"snapshot","revision":1,"conversationState":{"requests":[],"large":"` + strings.Repeat("x", 9<<20) + `"}}}}`)
	if err := newObservationState("fixture", "owner").apply(event); err == nil {
		t.Fatal("ordinary follower unexpectedly accepted a large state")
	}
	large := newObservationStateWithLimit("fixture", "owner", 64<<20)
	if err := large.apply(event); err != nil || large.summary.StateBytes <= 8<<20 {
		t.Fatalf("large follower rejected bounded state: bytes=%d err=%v", large.summary.StateBytes, err)
	}
}

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

func TestObservePassiveNeverRequestsCompleteHistory(t *testing.T) {
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
		s, err := c.ObservePassive(context.Background(), "fixture-thread", 20*time.Millisecond)
		done <- result{s, err}
	}()
	discovery := readMessage(t, b)
	if stringField(discovery, "method") != "thread-owner-discovery" {
		t.Fatal("missing discovery")
	}
	sendReply(t, b, stringField(discovery, "requestId"), "success", map[string]any{})
	follow := readMessage(t, b)
	if stringField(follow, "method") != "thread-stream-following-changed" {
		t.Fatalf("bad subscription: %s", follow)
	}
	unfollow := readMessage(t, b)
	if stringField(unfollow, "method") != "thread-stream-following-changed" {
		t.Fatalf("passive observation issued an unexpected request: %s", unfollow)
	}
	var params struct {
		Following bool `json:"following"`
	}
	json.Unmarshal(unfollow["params"], &params)
	if params.Following {
		t.Fatal("subscription leaked")
	}
	r := <-done
	if r.err != nil || !r.s.OwnerFound || r.s.SnapshotSeen || r.s.PatchBatches != 0 {
		t.Fatalf("result: %+v, %v", r.s, r.err)
	}
}

func TestObservePassiveReportsPatchWithoutBaseline(t *testing.T) {
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
		s, err := c.ObservePassive(context.Background(), "fixture-thread", time.Second)
		done <- result{s, err}
	}()
	discovery := readMessage(t, b)
	sendReply(t, b, stringField(discovery, "requestId"), "success", map[string]any{})
	_ = readMessage(t, b) // follow
	event := json.RawMessage(`{"type":"broadcast","sourceClientId":"owner","method":"thread-stream-state-changed","version":11,"params":{"hostId":"local","conversationId":"fixture-thread","change":{"type":"patches","baseRevision":4,"revision":5,"patches":[]}}}`)
	if err := WriteFrame(b, event, 4096); err != nil {
		t.Fatal(err)
	}
	_ = readMessage(t, b) // unfollow
	r := <-done
	if r.err == nil || r.s.EventsSeen != 1 || r.s.FirstChangeType != "patches" || r.s.FirstEventBytes != len(event) {
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
