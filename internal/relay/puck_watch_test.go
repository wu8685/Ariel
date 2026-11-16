package relay

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type watchPuckHandler struct {
	events   chan PuckResponse
	canceled chan struct{}
}

func (h *watchPuckHandler) Handle(context.Context, string, json.RawMessage) PuckResponse {
	return PuckResponse{Outcome: "accepted", Data: map[string]any{}}
}

func (h *watchPuckHandler) Watch(context.Context, string) (<-chan PuckResponse, func(), *PuckError) {
	return h.events, func() {
		select {
		case <-h.canceled:
		default:
			close(h.canceled)
		}
	}, nil
}

func TestRelayStreamsAndCancelsPuckConversationWatch(t *testing.T) {
	r, err := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, PuckState: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	handler := &watchPuckHandler{events: make(chan PuckResponse, 1), canceled: make(chan struct{})}
	r.SetPuckHandler(handler)
	server := httptest.NewServer(r.Handler())
	defer server.Close()
	web := dialTest(t, server.URL, testOrigin)
	sendJSON(t, web, webHello())
	readJSON(t, web)

	sendJSON(t, web, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000130", "deviceId": "relay", "method": "puck.watch", "params": map[string]any{"assistantId": "assistant-a"}})
	watch := readJSON(t, web)
	data, _ := watch["data"].(map[string]any)
	watchID, _ := data["watchId"].(string)
	if watch["outcome"] != "accepted" || watchID == "" {
		t.Fatalf("watch response=%+v", watch)
	}

	handler.events <- PuckResponse{Outcome: "accepted", Data: map[string]any{"messages": []any{map[string]any{"id": "message-a", "text": "durable"}}}}
	event := readJSON(t, web)
	if event["event"] != "puck.event" || event["assistantId"] != "assistant-a" || event["kind"] != "history" || event["seq"] != float64(1) {
		t.Fatalf("watch event=%+v", event)
	}

	sendJSON(t, web, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000131", "deviceId": "relay", "method": "puck.unwatch", "params": map[string]any{"watchId": watchID}})
	if response := readJSON(t, web); response["outcome"] != "accepted" {
		t.Fatalf("unwatch response=%+v", response)
	}
	select {
	case <-handler.canceled:
	case <-time.After(time.Second):
		t.Fatal("watch was not canceled")
	}
}

// Unwatch overlaps an in-flight event write. The socket must stay open:
// coder/websocket closes a connection when the write context is canceled.
func TestUnwatchOverlappingPuckEventDoesNotDropWebSocket(t *testing.T) {
	payload := map[string]any{"messages": []any{map[string]any{"id": "message-a", "text": strings.Repeat("durable", 4000)}}}
	for attempt := 0; attempt < 20; attempt++ {
		r, err := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, PuckState: "ready"})
		if err != nil {
			t.Fatal(err)
		}
		handler := &watchPuckHandler{events: make(chan PuckResponse, 1), canceled: make(chan struct{})}
		r.SetPuckHandler(handler)
		server := httptest.NewServer(r.Handler())
		web := dialTest(t, server.URL, testOrigin)
		sendJSON(t, web, webHello())
		readJSON(t, web)
		sendJSON(t, web, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000130", "deviceId": "relay", "method": "puck.watch", "params": map[string]any{"assistantId": "assistant-a"}})
		watch := readJSON(t, web)
		data, _ := watch["data"].(map[string]any)
		watchID, _ := data["watchId"].(string)
		if watch["outcome"] != "accepted" || watchID == "" {
			server.Close()
			t.Fatalf("watch response=%+v", watch)
		}
		handler.events <- PuckResponse{Outcome: "accepted", Data: payload}
		sendJSON(t, web, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000131", "deviceId": "relay", "method": "puck.unwatch", "params": map[string]any{"watchId": watchID}})
		sawUnwatch := false
		for reads := 0; reads < 2 && !sawUnwatch; reads++ {
			msg := readJSON(t, web)
			if msg["requestId"] == "00000000-0000-4000-8000-000000000131" {
				if msg["outcome"] != "accepted" {
					server.Close()
					t.Fatalf("attempt %d unwatch response=%+v", attempt, msg)
				}
				sawUnwatch = true
			}
		}
		server.Close()
		if !sawUnwatch {
			t.Fatalf("attempt %d: unwatch response missing", attempt)
		}
		select {
		case <-handler.canceled:
		case <-time.After(time.Second):
			t.Fatal("watch was not canceled")
		}
	}
}
