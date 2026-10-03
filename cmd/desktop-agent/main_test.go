package main

import "testing"

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
