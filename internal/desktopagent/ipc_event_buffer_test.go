package desktopagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wu8685/Ariel/internal/codex/desktopipc"
)

// A discovery response is an in-order reader barrier: no consumer runs until
// all broadcasts have been queued. Only then issue a normal RPC, so race's
// large-payload processing time is not charged to a production RPC timeout.
func queuedIPCBurst(t *testing.T, opts desktopipc.Options, count, eventBytes int) (*desktopipc.Client, error) {
	t.Helper()
	local, peer := net.Pipe()
	c := desktopipc.NewClient(local, opts)
	t.Cleanup(func() { c.Close(); peer.Close() })
	_ = peer.SetDeadline(time.Now().Add(3 * time.Minute))
	queued := make(chan error, 1)
	served := make(chan error, 1)
	go func() {
		var err error
		for i := 1; i <= count; i++ {
			prefix := fmt.Sprintf(`{"type":"broadcast","method":"thread-stream-state-changed","version":11,"sourceClientId":"fixture-owner","params":{"hostId":"local","conversationId":"fixture-thread","change":{"type":"patches","baseRevision":%d,"revision":%d,"patches":[]}},"padding":"`, i-1, i)
			body := append([]byte(prefix), bytes.Repeat([]byte("x"), max(0, eventBytes-len(prefix)-2))...)
			body = append(body, '"', '}')
			if err = desktopipc.WriteFrame(peer, body, opts.MaxFrameBytes); err != nil {
				queued <- err
				served <- err
				return
			}
		}
		err = desktopipc.WriteFrame(peer, json.RawMessage(`{"type":"client-discovery-request","requestId":"queue-barrier"}`), 4096)
		if err == nil {
			var response json.RawMessage
			response, err = desktopipc.ReadFrame(peer, 4096)
			var receipt struct{ Type, RequestID string }
			if err == nil && (json.Unmarshal(response, &receipt) != nil || receipt.Type != "client-discovery-response" || receipt.RequestID != "queue-barrier") {
				err = desktopipc.ErrProtocol
			}
		}
		queued <- err
		if err != nil {
			served <- err
			return
		}
		request, err := desktopipc.ReadFrame(peer, 4096)
		if err != nil {
			served <- err
			return
		}
		var m struct {
			RequestID string `json:"requestId"`
		}
		if err = json.Unmarshal(request, &m); err != nil {
			served <- err
			return
		}
		reply, _ := json.Marshal(map[string]any{"type": "response", "requestId": m.RequestID, "resultType": "success", "result": map[string]any{}})
		served <- desktopipc.WriteFrame(peer, reply, 4096)
	}()
	if err := <-queued; err != nil {
		c.Close()
		<-served
		return c, c.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.Call(ctx, desktopipc.Request{Method: "fixture-reader-barrier"})
	if err != nil {
		c.Close()
	}
	if serverErr := <-served; err == nil && serverErr != nil {
		t.Fatal(serverErr)
	}
	return c, err
}

func TestProductionLargeIPCBurstPreservesEveryQueuedEvent(t *testing.T) {
	for _, tc := range []struct {
		name        string
		count, size int
	}{
		{"event-count-boundary", 1024, 512},
		{"two-40MiB-events", 2, 40 << 20},
		{"128MiB-byte-boundary", 2, 64 << 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := queuedIPCBurst(t, productionLargeIPCOptions("fixture-thread"), tc.count, tc.size)
			if err != nil {
				t.Fatalf("legal burst invalidated IPC before draining: %v", err)
			}
			if used, limit := largeFollowerEventBudget.Usage(); used != tc.count*tc.size || limit != 512<<20 {
				t.Fatalf("production shared accounting=%d/%d, want %d queued bytes", used, limit, tc.count*tc.size)
			}
			for revision := 1; revision <= tc.count; revision++ {
				body, ok := c.NextEvent()
				if !ok {
					t.Fatalf("missing event %d: %v", revision, c.Err())
				}
				var m struct {
					Params struct {
						Change struct {
							Revision int `json:"revision"`
						} `json:"change"`
					} `json:"params"`
				}
				if len(body) != tc.size || json.Unmarshal(body, &m) != nil || m.Params.Change.Revision != revision {
					t.Fatalf("event bytes/order changed at %d", revision)
				}
			}
			if _, ok := c.NextEvent(); ok || c.Err() != nil {
				t.Fatalf("unexpected leftover event/connection failure: %v", c.Err())
			}
			if used, _ := largeFollowerEventBudget.Usage(); used != 0 {
				t.Fatalf("drained queue retained %d bytes", used)
			}
			t.Logf("queued and drained %d ordered events, %d retained event bytes; RPC reader barrier succeeded", tc.count, tc.count*tc.size)
		})
	}
}

func TestLargeIPCBurstStillRejectsOverloadAndReproducesOldLimits(t *testing.T) {
	for _, tc := range []struct {
		name        string
		count, size int
		old         bool
	}{
		{"old-count-limit", 65, 512, true},
		{"old-byte-limit", 2, 40 << 20, true},
		{"new-count-limit", 1025, 512, false},
		{"new-byte-limit", 3, 48 << 20, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := productionLargeIPCOptions("fixture-thread")
			if tc.old {
				opts.MaxEvents = 64
				opts.MaxEventBytes = 64 << 20
			}
			c, err := queuedIPCBurst(t, opts, tc.count, tc.size)
			if !errors.Is(err, desktopipc.ErrOverloaded) {
				t.Fatalf("overload did not fail closed: %v", err)
			}
			if _, ok := c.NextEvent(); ok {
				t.Fatal("invalidated queue remains usable")
			}
		})
	}
}

func TestProductionFollowerInitializationFailureReleasesEventBudget(t *testing.T) {
	dir, err := os.MkdirTemp("", "a28-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "ipc.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	finished := make(chan struct{})
	defer close(finished)
	served := make(chan error, 1)
	go func() {
		peer, err := listener.Accept()
		if err != nil {
			served <- err
			return
		}
		defer peer.Close()
		body, err := desktopipc.ReadFrame(peer, 4096)
		if err != nil {
			served <- err
			return
		}
		var request struct {
			RequestID string `json:"requestId"`
		}
		if err = json.Unmarshal(body, &request); err != nil {
			served <- err
			return
		}
		event := json.RawMessage(`{"type":"broadcast","method":"thread-stream-state-changed","version":11,"params":{"conversationId":"fixture-init"}}`)
		if err = desktopipc.WriteFrame(peer, event, 4096); err != nil {
			served <- err
			return
		}
		reply, _ := json.Marshal(map[string]any{"type": "response", "requestId": request.RequestID, "resultType": "error"})
		served <- desktopipc.WriteFrame(peer, reply, 4096)
		select {
		case <-finished:
		case <-ctx.Done():
		}
	}()
	opts := productionLargeIPCOptions("fixture-init")
	_, err = openFollower(ctx, path, "fixture-init", "/fixture", opts)
	if !errors.Is(err, desktopipc.ErrRemoteRejected) {
		t.Fatalf("unexpected initialize failure: %v", err)
	}
	if err := <-served; err != nil {
		t.Fatal(err)
	}
	if used, _ := opts.EventBudget.Usage(); used != 0 {
		t.Fatalf("failed production open retained %d queue bytes", used)
	}
}
