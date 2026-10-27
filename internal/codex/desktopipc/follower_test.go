package desktopipc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestFollowerKeepsLiteralHTMLSnapshotAndPatchesWithinNativeLimit(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	const limit = 4096
	client := NewClient(local, Options{ThreadID: "thread", MaxFrameBytes: limit, RequestTimeout: time.Second})
	defer client.Close()
	patchReady := make(chan struct{})
	defer close(patchReady)
	done := make(chan error, 1)
	sendEvent := func(change any) error {
		var body bytes.Buffer
		encoder := json.NewEncoder(&body)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(map[string]any{"type": "broadcast", "method": "thread-stream-state-changed", "version": 11, "sourceClientId": "owner", "params": map[string]any{"conversationId": "thread", "hostId": "local", "change": change}}); err != nil {
			return err
		}
		return WriteFrame(peer, body.Bytes(), limit)
	}
	go func() {
		readMessage(t, peer)
		request := readMessage(t, peer)
		sendReply(t, peer, stringField(request, "requestId"), "success", map[string]any{})
		state := map[string]any{"cwd": "/fixture", "requests": []any{}, "turns": []any{map[string]any{"turnId": "turn", "status": "completed", "items": []any{}}}, "text": strings.Repeat("<>&", 350)}
		if err := sendEvent(map[string]any{"type": "snapshot", "revision": 1, "conversationState": state}); err != nil {
			done <- err
			return
		}
		<-patchReady
		done <- sendEvent(map[string]any{"type": "patches", "baseRevision": 1, "revision": 2, "patches": []any{map[string]any{"op": "replace", "path": []any{"text"}, "value": strings.Repeat("<>&", 400)}}})
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	f, err := Follow(ctx, client, "thread", "owner", "/fixture")
	if err != nil {
		t.Fatalf("literal JSON failed native follower attachment: %v", err)
	}
	defer f.Close()
	patchReady <- struct{}{}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for {
		state, err := f.Current()
		if err != nil {
			t.Fatalf("literal JSON patch invalidated follower: %v", err)
		}
		var value struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(state, &value) == nil && value.Text == strings.Repeat("<>&", 400) {
			if len(state) > limit {
				t.Fatal("resident owner state exceeded limit")
			}
			break
		}
		select {
		case <-f.Updates():
		case <-ctx.Done():
			t.Fatal("native patch never arrived")
		}
	}
}

func TestFollowerPreservesReducerOverflowCauseWhenIPCCloses(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	client := NewClient(local, Options{ThreadID: "thread", MaxFrameBytes: 2048, RequestTimeout: time.Second})
	defer client.Close()
	go func() {
		readMessage(t, peer)
		request := readMessage(t, peer)
		sendReply(t, peer, stringField(request, "requestId"), "success", map[string]any{})
		// U+2028 remains escaped by the standard JSON encoder even when HTML
		// escaping is off. Genuine normalized overflow must retain its cause.
		body := `{"type":"broadcast","method":"thread-stream-state-changed","version":11,"sourceClientId":"owner","params":{"conversationId":"thread","hostId":"local","change":{"type":"snapshot","revision":1,"conversationState":{"cwd":"/fixture","turns":[{"turnId":"turn","items":[]}],"text":"` + strings.Repeat("\u2028", 400) + `"}}}}`
		_ = WriteFrame(peer, json.RawMessage(body), 2048)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := Follow(ctx, client, "thread", "owner", "/fixture")
	if !errors.Is(err, ErrFrameTooLarge) || !errors.Is(err, ErrResyncRequired) {
		t.Fatalf("native overflow cause was hidden by IPC close: %v", err)
	}
}

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
	if got, want := f.StateBytes(), len(state); got != want {
		t.Fatalf("StateBytes = %d, want latest frame size %d", got, want)
	}
	if _, err := StartProductionTurn(ctx, &recordingOwner{}, "owner", "thread", "/fixture", state, "id", "hello"); err != ErrTurnBusy {
		t.Fatalf("busy: %v", err)
	}
}

func TestFollowerWaitsForAddressableStateAfterCanonicalPlaceholder(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	client := NewClient(local, Options{ThreadID: "thread", RequestTimeout: time.Second})
	defer client.Close()
	done := make(chan error, 1)
	go func() {
		readMessage(t, peer) // following changed
		request := readMessage(t, peer)
		if stringField(request, "method") != "thread-follower-load-complete-history" {
			done <- ErrProtocol
			return
		}
		sendReply(t, peer, stringField(request, "requestId"), "success", map[string]any{})
		for rev, id := range []string{"", "native-turn"} {
			state := map[string]any{"cwd": "/fixture", "threadRuntimeStatus": map[string]any{"type": "active"}, "requests": []any{}, "turnHistory": map[string]any{"kind": "canonical", "history": map[string]any{"islands": []any{map[string]any{"entries": []any{map[string]any{"value": "entry"}}}}, "entitiesByKey": map[string]any{"entry": map[string]any{"turnId": id, "status": "inProgress", "items": []any{}}}}}}
			body, _ := json.Marshal(map[string]any{"type": "broadcast", "method": "thread-stream-state-changed", "version": 11, "sourceClientId": "owner", "params": map[string]any{"conversationId": "thread", "hostId": "local", "change": map[string]any{"type": "snapshot", "revision": rev + 1, "conversationState": state}}})
			if err := WriteFrame(peer, body, DefaultMaxFrameBytes); err != nil {
				done <- err
				return
			}
			if rev == 0 {
				time.Sleep(40 * time.Millisecond)
			}
		}
		done <- nil
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
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
	if err != nil || !nativeCanonicalAddressable(state) {
		t.Fatalf("did not recover addressable state: %v", err)
	}
}

func TestFollowerStartUsesWarmSnapshotAndReturnsNativeReceiptWithoutHistoryRefresh(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	client := NewClient(local, Options{ThreadID: "thread", RequestTimeout: time.Second})
	defer client.Close()
	done := make(chan error, 1)
	go func() {
		readMessage(t, peer) // following changed
		initial := readMessage(t, peer)
		if stringField(initial, "method") != "thread-follower-load-complete-history" {
			done <- ErrProtocol
			return
		}
		sendReply(t, peer, stringField(initial, "requestId"), "success", map[string]any{})
		state := map[string]any{"cwd": "/fixture", "threadRuntimeStatus": map[string]any{"type": "idle"}, "requests": []any{}, "turns": []any{}}
		body, _ := json.Marshal(map[string]any{"type": "broadcast", "method": "thread-stream-state-changed", "version": 11, "sourceClientId": "owner", "params": map[string]any{"conversationId": "thread", "hostId": "local", "change": map[string]any{"type": "snapshot", "revision": 1, "conversationState": state}}})
		if err := WriteFrame(peer, body, DefaultMaxFrameBytes); err != nil {
			done <- err
			return
		}
		start := readMessage(t, peer)
		if stringField(start, "method") != "thread-follower-start-turn" {
			done <- ErrProtocol
			return
		}
		sendReply(t, peer, stringField(start, "requestId"), "success", map[string]any{"result": map[string]any{"turn": map[string]any{"id": "turn-fast"}}})
		_ = peer.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		if _, err := ReadFrame(peer, DefaultMaxFrameBytes); err == nil {
			done <- ErrProtocol
			return
		}
		done <- nil
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	follower, err := Follow(ctx, client, "thread", "owner", "/fixture")
	if err != nil {
		t.Fatal(err)
	}
	defer follower.Close()
	turnID, err := follower.Start(ctx, "00000000-0000-4000-8000-000000000001", "hello")
	if err != nil || turnID != "turn-fast" {
		t.Fatalf("Start = %q, %v", turnID, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
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
		initial := readMessage(t, peer)
		if stringField(initial, "method") != "thread-follower-load-complete-history" {
			t.Errorf("expected initial refresh, got %s", stringField(initial, "method"))
			return
		}
		sendReply(t, peer, stringField(initial, "requestId"), "success", map[string]any{})
		idle := map[string]any{"cwd": "/fixture", "threadRuntimeStatus": map[string]any{"type": "idle"}, "requests": []any{}, "turns": []any{}}
		body, _ := json.Marshal(map[string]any{"type": "broadcast", "method": "thread-stream-state-changed", "version": 11, "sourceClientId": "owner", "params": map[string]any{"conversationId": "thread", "hostId": "local", "change": map[string]any{"type": "snapshot", "revision": 1, "conversationState": idle}}})
		if err := WriteFrame(peer, body, DefaultMaxFrameBytes); err != nil {
			t.Error(err)
			return
		}
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

		refresh := readMessage(t, peer)
		if stringField(refresh, "method") != "thread-follower-load-complete-history" {
			t.Errorf("expected interrupt refresh, got %s", stringField(refresh, "method"))
			return
		}
		sendReply(t, peer, stringField(refresh, "requestId"), "success", map[string]any{})
		running := map[string]any{"cwd": "/fixture", "threadRuntimeStatus": map[string]any{"type": "inProgress"}, "requests": []any{}, "turns": []any{map[string]any{"turnId": "turn-one", "status": "inProgress", "items": []any{}}}}
		body, _ = json.Marshal(map[string]any{"type": "broadcast", "method": "thread-stream-state-changed", "version": 11, "sourceClientId": "owner", "params": map[string]any{"conversationId": "thread", "hostId": "local", "change": map[string]any{"type": "snapshot", "revision": 2, "conversationState": running}}})
		if err := WriteFrame(peer, body, DefaultMaxFrameBytes); err != nil {
			t.Error(err)
			return
		}
		interrupt := readMessage(t, peer)
		if stringField(interrupt, "method") != "thread-follower-interrupt-turn" {
			t.Errorf("expected interrupt, got %s", stringField(interrupt, "method"))
			return
		}
		close(interruptSeen)
		sendReply(t, peer, stringField(interrupt, "requestId"), "success", map[string]any{"ok": true, "interruptedTurnId": "turn-one"})
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
