package desktopipc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"
)

func TestFollowerPausedConsumerPreservesBurstAndTerminalWithoutReload(t *testing.T) {
	for _, old := range []bool{true, false} {
		name := "new-bounded-profile"
		if old {
			name = "old-64-event-profile"
		}
		t.Run(name, func(t *testing.T) {
			local, peer := net.Pipe()
			defer peer.Close()
			pool := NewEventByteBudget(512 << 20)
			opts := Options{ThreadID: "thread", MaxEvents: 1024, MaxEventBytes: 128 << 20, EventBudget: pool, RequestTimeout: 5 * time.Second}
			if old {
				opts.MaxEvents = 64
				opts.MaxEventBytes = 64 << 20
			}
			c := NewClient(local, opts)
			defer c.Close()
			done := make(chan error, 1)
			go func() {
				send := func(change any) error {
					body, _ := json.Marshal(map[string]any{"type": "broadcast", "method": "thread-stream-state-changed", "version": 11, "sourceClientId": "owner", "params": map[string]any{"conversationId": "thread", "hostId": "local", "change": change}})
					return WriteFrame(peer, body, DefaultMaxFrameBytes)
				}
				// Exactly one follow and one full load. Any mutation or extra load
				// instead of the reader barrier fails this fixture.
				follow := readMessage(t, peer)
				load := readMessage(t, peer)
				if stringField(follow, "method") != "thread-stream-following-changed" || stringField(load, "method") != "thread-follower-load-complete-history" {
					done <- ErrProtocol
					return
				}
				sendReply(t, peer, stringField(load, "requestId"), "success", map[string]any{})
				state := map[string]any{"cwd": "/fixture", "requests": []any{}, "chunks": []any{}, "threadRuntimeStatus": map[string]any{"type": "inProgress"}, "turns": []any{map[string]any{"turnId": "turn", "status": "inProgress", "items": []any{map[string]any{"type": "agentMessage", "id": "reply", "text": "working"}}}}}
				if err := send(map[string]any{"type": "snapshot", "revision": 1, "conversationState": state}); err != nil {
					done <- err
					return
				}
				barrier := readMessage(t, peer)
				if stringField(barrier, "method") != "fixture-burst-barrier" {
					done <- ErrProtocol
					return
				}
				for i := 1; i <= 128; i++ {
					patches := []any{map[string]any{"op": "add", "path": []any{"chunks", i - 1}, "value": i}}
					if i == 128 {
						patches = append(patches, map[string]any{"op": "replace", "path": []any{"turns", 0, "status"}, "value": "completed"}, map[string]any{"op": "replace", "path": []any{"threadRuntimeStatus", "type"}, "value": "idle"}, map[string]any{"op": "replace", "path": []any{"turns", 0, "items", 0, "text"}, "value": "fixture complete"})
					}
					if err := send(map[string]any{"type": "patches", "baseRevision": i, "revision": i + 1, "patches": patches}); err != nil {
						done <- err
						return
					}
				}
				sendReply(t, peer, stringField(barrier, "requestId"), "success", map[string]any{})
				done <- nil
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			f, err := Follow(ctx, c, "thread", "owner", "/fixture")
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			// Pause the reducer, not the IPC reader: receipts must still arrive.
			f.stateMu.Lock()
			_, err = c.Call(ctx, Request{Method: "fixture-burst-barrier"})
			f.stateMu.Unlock()
			serverErr := <-done
			if old {
				if !errors.Is(err, ErrOverloaded) {
					t.Fatalf("old backlog did not reproduce: %v", err)
				}
				return
			}
			if err != nil || serverErr != nil {
				t.Fatalf("buffered burst failed: call=%v peer=%v", err, serverErr)
			}
			for {
				state, err := f.Current()
				if err != nil {
					t.Fatal(err)
				}
				var result struct {
					Chunks []int `json:"chunks"`
					Turns  []struct {
						Status string `json:"status"`
						Items  []struct {
							Text string `json:"text"`
						} `json:"items"`
					} `json:"turns"`
					Runtime struct {
						Type string `json:"type"`
					} `json:"threadRuntimeStatus"`
				}
				if json.Unmarshal(state, &result) != nil {
					t.Fatal("invalid reduced state")
				}
				if len(result.Chunks) == 128 {
					for i, value := range result.Chunks {
						if value != i+1 {
							t.Fatal("patch dropped/reordered")
						}
					}
					if result.Runtime.Type != "idle" || result.Turns[0].Status != "completed" || result.Turns[0].Items[0].Text != "fixture complete" {
						t.Fatal("terminal content/state missing")
					}
					break
				}
				select {
				case <-f.Updates():
				case <-ctx.Done():
					t.Fatal("terminal update missing")
				}
			}
			c.Close()
			if used, _ := pool.Usage(); used != 0 {
				t.Fatalf("retained burst bytes: %d", used)
			}
		})
	}
}
