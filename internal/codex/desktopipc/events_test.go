package desktopipc

import (
	"encoding/json"
	"net"
	"testing"
	"time"
)

func TestUnrelatedBroadcastBurstDoesNotFillThreadQueue(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	c := NewClient(a, Options{ThreadID: "fixture", MaxEvents: 1})
	defer c.Close()
	for range 20 {
		if err := WriteFrame(b, json.RawMessage(`{"type":"broadcast","method":"thread-stream-state-changed","params":{"conversationId":"other-thread"}}`), 4096); err != nil {
			t.Fatal(err)
		}
	}
	if err := WriteFrame(b, json.RawMessage(`{"type":"broadcast","method":"thread-stream-state-changed","params":{"conversationId":"fixture"}}`), 4096); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.EventsReady():
		if _, ok := c.NextEvent(); !ok {
			t.Fatal("target update absent")
		}
	case <-time.After(time.Second):
		t.Fatal("missing target update")
	}
	if c.Err() != nil {
		t.Fatal(c.Err())
	}
}

func TestEventQueueLimitsTotalBytesAndReleasesConsumedBudget(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	c := NewClient(a, Options{MaxEvents: 10, MaxEventBytes: 64})
	defer c.Close()
	frame := json.RawMessage(`{"type":"broadcast","method":"x"}`)
	for range 3 {
		if err := WriteFrame(b, frame, 4096); err != nil {
			t.Fatal(err)
		}
		select {
		case <-c.EventsReady():
			c.NextEvent()
		case <-time.After(time.Second):
			t.Fatal("missing event")
		}
	}
	for range 64/len(frame) + 1 {
		if err := WriteFrame(b, frame, 4096); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-c.Done():
	case <-time.After(time.Second):
		t.Fatal("byte cap not enforced")
	}
}
