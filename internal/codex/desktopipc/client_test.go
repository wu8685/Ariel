package desktopipc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"
)

func readMessage(t *testing.T, c net.Conn) map[string]json.RawMessage {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(time.Second))
	b, err := ReadFrame(c, 4096)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func stringField(m map[string]json.RawMessage, k string) string {
	var s string
	json.Unmarshal(m[k], &s)
	return s
}

func sendReply(t *testing.T, c net.Conn, id, resultType string, result any) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"type": "response", "requestId": id, "resultType": resultType, "handledByClientId": "owner", "result": result})
	if err := WriteFrame(c, b, 4096); err != nil {
		t.Fatal(err)
	}
}

func TestInitializeIdentifiesProductionAgentHonestly(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	c := NewClient(local, Options{ClientType: "ariel-desktop-agent"})
	defer c.Close()
	done := make(chan error, 1)
	go func() { done <- c.Initialize(context.Background()) }()
	m := readMessage(t, peer)
	var params map[string]string
	if json.Unmarshal(m["params"], &params) != nil || params["clientType"] != "ariel-desktop-agent" {
		t.Fatalf("identity: %s", m["params"])
	}
	sendReply(t, peer, stringField(m, "requestId"), "success", map[string]string{"clientId": "agent-client"})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestClientCorrelatesOutOfOrderResponses(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	c := NewClient(local, Options{})
	defer c.Close()
	type result struct {
		method string
		value  string
		err    error
	}
	outputs := make(chan result, 2)
	for _, name := range []string{"first", "second"} {
		go func() {
			r, err := c.Call(context.Background(), Request{Method: name, Params: map[string]any{}, Version: 1})
			outputs <- result{name, string(r.Result), err}
		}()
	}
	a, b := readMessage(t, peer), readMessage(t, peer)
	if stringField(a, "requestId") == stringField(b, "requestId") {
		t.Fatal("duplicate IDs")
	}
	sendReply(t, peer, stringField(b, "requestId"), "success", stringField(b, "method"))
	sendReply(t, peer, stringField(a, "requestId"), "success", stringField(a, "method"))
	seen := map[string]bool{}
	for range 2 {
		select {
		case r := <-outputs:
			if r.err != nil {
				t.Fatal(r.err)
			}
			var returned string
			json.Unmarshal([]byte(r.value), &returned)
			if returned != r.method {
				t.Fatalf("reply for %s delivered to %s", returned, r.method)
			}
			seen[r.value] = true
		case <-time.After(time.Second):
			t.Fatal("calls stuck")
		}
	}
	if len(seen) != 2 {
		t.Fatalf("responses mixed: %v", seen)
	}
}

func TestClientDoesNotTreatWrongOwnerErrorAsRejection(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	c := NewClient(a, Options{})
	defer c.Close()
	result := make(chan error, 1)
	go func() {
		_, err := c.Call(context.Background(), Request{Method: "mutate", Mutating: true, TargetClientID: "expected-owner"})
		result <- err
	}()
	m := readMessage(t, b)
	sendReply(t, b, stringField(m, "requestId"), "error", nil)
	assertOutcome(t, <-result, "unknown")
	if !errors.Is(c.Err(), ErrProtocol) {
		t.Fatal("wrong-owner response did not invalidate connection")
	}
}

func assertOutcome(t *testing.T, err error, want string) {
	t.Helper()
	var failure *CallError
	if !errors.As(err, &failure) || failure.Outcome != want {
		t.Fatalf("got %#v; want outcome %s", err, want)
	}
}

func TestClientExpiredMutationNeverWrites(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	c := NewClient(local, Options{})
	defer c.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Call(ctx, Request{Method: "mutate", Mutating: true})
	assertOutcome(t, err, "not_submitted")
	peer.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
	var b [1]byte
	if n, _ := peer.Read(b[:]); n != 0 {
		t.Fatal("expired mutation written")
	}
}

func TestClientMutationTimeoutIsUnknownAndNotRetried(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	c := NewClient(local, Options{})
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := c.Call(ctx, Request{Method: "mutate", Mutating: true}); result <- err }()
	readMessage(t, peer)
	assertOutcome(t, <-result, "unknown")
	peer.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
	var b [1]byte
	if n, _ := peer.Read(b[:]); n != 0 {
		t.Fatal("mutation retried")
	}
}

func TestClientDisconnectSettlesPending(t *testing.T) {
	local, peer := net.Pipe()
	c := NewClient(local, Options{})
	defer c.Close()
	result := make(chan error, 1)
	go func() {
		_, err := c.Call(context.Background(), Request{Method: "mutate", Mutating: true})
		result <- err
	}()
	readMessage(t, peer)
	peer.Close()
	select {
	case err := <-result:
		assertOutcome(t, err, "unknown")
	case <-time.After(time.Second):
		t.Fatal("pending call leaked")
	}
	_, err := c.Call(context.Background(), Request{Method: "mutate", Mutating: true})
	assertOutcome(t, err, "not_submitted")
}

func TestClientPendingLimit(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	c := NewClient(local, Options{MaxPending: 1})
	defer c.Close()
	result := make(chan error, 1)
	go func() { _, err := c.Call(context.Background(), Request{Method: "first"}); result <- err }()
	readMessage(t, peer)
	_, err := c.Call(context.Background(), Request{Method: "second", Mutating: true})
	assertOutcome(t, err, "not_submitted")
	if !errors.Is(err, ErrOverloaded) {
		t.Fatalf("got %v", err)
	}
	c.Close()
	<-result
}

func TestClientRemoteRejection(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	c := NewClient(local, Options{})
	defer c.Close()
	result := make(chan error, 1)
	go func() {
		_, err := c.Call(context.Background(), Request{Method: "mutate", Mutating: true})
		result <- err
	}()
	m := readMessage(t, peer)
	sendReply(t, peer, stringField(m, "requestId"), "error", nil)
	assertOutcome(t, <-result, "rejected")
}

func TestClientEventsOverflowInvalidatesConnection(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	c := NewClient(local, Options{MaxEvents: 1})
	defer c.Close()
	for range 2 {
		if err := WriteFrame(peer, json.RawMessage(`{"type":"broadcast","method":"update"}`), 4096); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-c.Done():
		if !errors.Is(c.Err(), ErrOverloaded) {
			t.Fatal(c.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("overflow silently dropped")
	}
}

func TestClientNeverClaimsThreadOwnership(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	c := NewClient(local, Options{})
	defer c.Close()
	if err := WriteFrame(peer, json.RawMessage(`{"type":"client-discovery-request","requestId":"discovery"}`), 4096); err != nil {
		t.Fatal(err)
	}
	m := readMessage(t, peer)
	if stringField(m, "type") != "client-discovery-response" || stringField(m, "requestId") != "discovery" || string(m["response"]) != `{"canHandle":false}` {
		t.Fatalf("unexpected discovery answer: %s", m)
	}
}
