package main

import (
	"testing"
	"time"
)

func TestDesktopAgentConfigurationRequiresAllBoundaries(t *testing.T) {
	if _, err := configFrom("", "secret", "mac", "Mac", "/Applications/ChatGPT.app", "/tmp/ipc.sock"); err == nil {
		t.Fatal("missing URL accepted")
	}
	if _, err := configFrom("ws://localhost:8080/ws", "", "mac", "Mac", "/Applications/ChatGPT.app", "/tmp/ipc.sock"); err == nil {
		t.Fatal("missing token accepted")
	}
	if _, err := configFrom("ws://localhost:8080/ws", "secret", "mac", "Mac", "/Applications/ChatGPT.app", ""); err == nil {
		t.Fatal("missing socket accepted")
	}
	if _, err := configFrom("ws://localhost:8080/ws", "secret", "mac", "Mac", "/Applications/ChatGPT.app", "/tmp/ipc.sock"); err != nil {
		t.Fatal(err)
	}
}

func TestReconnectBackoffResetsAfterACompletedHandshake(t *testing.T) {
	b := newReconnectBackoff()
	if got := b.nextDelay(0.5); got != time.Second {
		t.Fatalf("first delay = %v", got)
	}
	if got := b.nextDelay(0.5); got != 2*time.Second {
		t.Fatalf("second consecutive delay = %v", got)
	}
	for i, want := range []time.Duration{4 * time.Second, 8 * time.Second, 15 * time.Second, 15 * time.Second} {
		if got := b.nextDelay(0.5); got != want {
			t.Fatalf("continued delay %d = %v, want %v", i, got, want)
		}
	}
	b.connected()
	if got := b.nextDelay(0.5); got != time.Second {
		t.Fatalf("delay after a successful hello = %v", got)
	}
}
