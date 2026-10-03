package desktopipc

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"
)

func TestFollowerReceivesSnapshotAndRejectsBusyStart(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	client := NewClient(local, Options{ThreadID: "thread", RequestTimeout: time.Second})
	defer client.Close()
	done := make(chan error, 1)
	go func() {
		broadcast := readMessage(t, peer)
		if stringField(broadcast, "method") != "thread-stream-following-changed" {
			done <- ErrProtocol
			return
		}
		request := readMessage(t, peer)
		if stringField(request, "method") != "thread-follower-load-complete-history" {
			done <- ErrProtocol
			return
		}
		sendReply(t, peer, stringField(request, "requestId"), "success", map[string]any{"revision": 1})
		state := map[string]any{"cwd": "/fixture", "threadRuntimeStatus": map[string]any{"type": "inProgress"}, "requests": []any{}, "turns": []any{map[string]any{"turnId": "existing", "status": "inProgress", "items": []any{}}}}
		body, _ := json.Marshal(map[string]any{"type": "broadcast", "method": "thread-stream-state-changed", "version": 11, "sourceClientId": "owner", "params": map[string]any{"conversationId": "thread", "hostId": "local", "change": map[string]any{"type": "snapshot", "revision": 1, "conversationState": state}}})
		if err := WriteFrame(peer, body, DefaultMaxFrameBytes); err != nil {
			done <- err
			return
		}
		done <- nil
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	f, err := Follow(ctx, client, "thread", "owner", "/fixture")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	state, err := f.Current()
	if err != nil || len(state) == 0 {
		t.Fatalf("state: %s %v", state, err)
	}
	if _, err := StartProductionTurn(ctx, &recordingOwner{}, "owner", "thread", "/fixture", state, "id", "hello"); err != ErrTurnBusy {
		t.Fatalf("busy: %v", err)
	}
}

func TestFollowerInterruptCanProceedWhileStartReceiptIsPending(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	client := NewClient(local, Options{ThreadID: "thread", RequestTimeout: 2 * time.Second})
	defer client.Close()
	startSeen := make(chan struct{})
	interruptSeen := make(chan struct{})
	releaseStart := make(chan struct{})
	go func() {
		readMessage(t, peer) // following changed
		for rev := 1; rev <= 3; rev++ {
			req := readMessage(t, peer)
			if stringField(req, "method") != "thread-follower-load-complete-history" {
				t.Errorf("expected refresh, got %s", stringField(req, "method"))
				return
			}
			sendReply(t, peer, stringField(req, "requestId"), "success", map[string]any{})
			status, turns := "idle", []any{}
			if rev == 3 {
				status = "inProgress"
				turns = []any{map[string]any{"turnId": "turn-one", "status": "inProgress", "items": []any{}}}
			}
			state := map[string]any{"cwd": "/fixture", "threadRuntimeStatus": map[string]any{"type": status}, "requests": []any{}, "turns": turns}
			body, _ := json.Marshal(map[string]any{"type": "broadcast", "method": "thread-stream-state-changed", "version": 11, "sourceClientId": "owner", "params": map[string]any{"conversationId": "thread", "hostId": "local", "change": map[string]any{"type": "snapshot", "revision": rev, "conversationState": state}}})
			if err := WriteFrame(peer, body, DefaultMaxFrameBytes); err != nil {
				t.Error(err)
				return
			}
			if rev == 2 {
				start := readMessage(t, peer)
				if stringField(start, "method") != "thread-follower-start-turn" {
					t.Errorf("expected start, got %s", stringField(start, "method"))
					return
				}
				close(startSeen)
				go func(id string) {
					<-releaseStart
					sendReply(t, peer, id, "success", map[string]any{"result": map[string]any{"turn": map[string]any{"id": "turn-one"}}})
				}(stringField(start, "requestId"))
			}
		}
		interrupt := readMessage(t, peer)
		if stringField(interrupt, "method") != "thread-follower-interrupt-turn" {
			t.Errorf("expected interrupt, got %s", stringField(interrupt, "method"))
			return
		}
		close(interruptSeen)
		sendReply(t, peer, stringField(interrupt, "requestId"), "success", map[string]any{"ok": true, "interruptedTurnId": "turn-one"})
		verify := readMessage(t, peer)
		if stringField(verify, "method") != "thread-follower-load-complete-history" {
			t.Errorf("expected post-start verification, got %s", stringField(verify, "method"))
			return
		}
		sendReply(t, peer, stringField(verify, "requestId"), "success", map[string]any{})
		verified := map[string]any{"cwd": "/fixture", "threadRuntimeStatus": map[string]any{"type": "idle"}, "requests": []any{}, "turns": []any{map[string]any{"turnId": "turn-one", "status": "interrupted", "items": []any{map[string]any{"id": "native-item", "clientId": "message-one", "type": "userMessage", "content": []any{map[string]any{"type": "text", "text": "hello"}}}}}}}
		body, _ := json.Marshal(map[string]any{"type": "broadcast", "method": "thread-stream-state-changed", "version": 11, "sourceClientId": "owner", "params": map[string]any{"conversationId": "thread", "hostId": "local", "change": map[string]any{"type": "snapshot", "revision": 4, "conversationState": verified}}})
		if err := WriteFrame(peer, body, DefaultMaxFrameBytes); err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	f, err := Follow(ctx, client, "thread", "owner", "/fixture")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	startDone := make(chan error, 1)
	go func() { _, err := f.Start(ctx, "message-one", "hello"); startDone <- err }()
	<-startSeen
	interruptDone := make(chan error, 1)
	go func() { interruptDone <- f.Interrupt(ctx, "turn-one") }()
	select {
	case <-interruptSeen:
	case <-time.After(time.Second):
		t.Fatal("interrupt could not reach Desktop before start receipt")
	}
	if err := <-interruptDone; err != nil {
		t.Fatal(err)
	}
	close(releaseStart)
	if err := <-startDone; err != nil {
		t.Fatal(err)
	}
}
