package appserver

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"
)

func TestInteractiveSessionCorrelatesResponsesAndDeliversIncomingFrames(t *testing.T) {
	client, server := net.Pipe()
	session := NewInteractiveSession(client, 4096)
	defer session.Close()
	defer server.Close()

	go func() {
		decoder := json.NewDecoder(server)
		var request map[string]any
		if err := decoder.Decode(&request); err != nil {
			return
		}
		_, _ = server.Write([]byte("{\"method\":\"turn/started\",\"params\":{\"threadId\":\"thread-a\"}}\n"))
		_, _ = server.Write([]byte("{\"id\":77,\"method\":\"item/commandExecution/requestApproval\",\"params\":{\"threadId\":\"thread-a\"}}\n"))
		_, _ = server.Write([]byte("{\"id\":1,\"result\":{\"ok\":true}}\n"))
		_ = decoder.Decode(&request)
	}()

	var result struct {
		OK bool `json:"ok"`
	}
	if err := session.Call(context.Background(), "thread/read", map[string]any{}, &result); err != nil || !result.OK {
		t.Fatalf("Call() = %#v, %v", result, err)
	}
	first := <-session.Incoming()
	second := <-session.Incoming()
	if first.Method != "turn/started" || first.IsRequest() {
		t.Fatalf("first incoming = %#v", first)
	}
	if second.Method != "item/commandExecution/requestApproval" || !second.IsRequest() || string(second.ID) != "77" {
		t.Fatalf("second incoming = %#v", second)
	}
	if err := session.Reply(context.Background(), second.ID, map[string]any{"decision": "decline"}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestInteractiveSessionFailsClosedWhenIncomingConsumerIsSlow(t *testing.T) {
	client, server := net.Pipe()
	session := NewInteractiveSession(client, 4096)
	defer server.Close()
	for i := 0; i < interactiveIncomingLimit+1; i++ {
		_, _ = server.Write([]byte("{\"method\":\"item/updated\",\"params\":{}}\n"))
	}
	select {
	case <-session.Done():
		if session.Err() != ErrCapacity {
			t.Fatalf("Err() = %v", session.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("slow incoming consumer did not close session")
	}
}

func TestInteractiveSessionRejectsOversizeUnsolicitedFrame(t *testing.T) {
	client, server := net.Pipe()
	session := NewInteractiveSession(client, 64)
	defer server.Close()
	_, _ = server.Write([]byte("{\"method\":\"item/updated\",\"params\":{\"text\":\"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\"}}\n"))
	select {
	case <-session.Done():
		if session.Err() != ErrProtocol {
			t.Fatalf("Err() = %v", session.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("oversize frame did not close session")
	}
}
