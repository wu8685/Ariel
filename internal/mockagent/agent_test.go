package mockagent

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/wu8685/Ariel/internal/relay"
)

func TestAgentRelayEndToEnd(t *testing.T) {
	r, err := relay.New(relay.Config{Token: "test-secret", WebPIN: "012345", AllowedOrigins: []string{"http://localhost:5173"}})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(r.Handler())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Config{URL: url, Token: "test-secret", DeviceID: "mock-device", DeviceName: "Mock Device", Store: NewStore(StoreConfig{StepInterval: 10 * time.Millisecond})})
	}()

	wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
	defer wcancel()
	web, _, err := websocket.Dial(wctx, url, &websocket.DialOptions{HTTPHeader: map[string][]string{"Origin": {"http://localhost:5173"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer web.CloseNow()
	writeJSON(t, wctx, web, map[string]any{"type": "hello", "v": 1, "role": "web", "token": "012345"})
	if m := readJSON(t, wctx, web); m["type"] != "hello.ok" {
		t.Fatalf("hello: %v", m)
	}
	request := func(method string, params any) map[string]any {
		id := newTestID()
		writeJSON(t, wctx, web, map[string]any{"type": "request", "v": 1, "requestId": id, "deviceId": "mock-device", "method": method, "params": params})
		for {
			m := readJSON(t, wctx, web)
			if m["type"] == "response" && m["requestId"] == id {
				return m
			}
		}
	}
	var listed map[string]any
	for i := 0; i < 20; i++ {
		listed = request("thread.list", map[string]any{"limit": 1})
		if listed["outcome"] == "accepted" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if listed["outcome"] != "accepted" {
		t.Fatalf("list: %v", listed)
	}
	data := listed["data"].(map[string]any)
	if len(data["threads"].([]any)) != 1 || data["nextCursor"] == "" {
		t.Fatalf("list page: %v", data)
	}
	if m := request("thread.read", map[string]any{"threadId": "mock-thread-a"}); m["outcome"] != "accepted" {
		t.Fatalf("read: %v", m)
	}

	id := newTestID()
	writeJSON(t, wctx, web, map[string]any{"type": "request", "v": 1, "requestId": id, "deviceId": "mock-device", "method": "thread.subscribe", "params": map[string]any{"threadId": "mock-thread-a"}})
	var sub map[string]any
	for sub == nil {
		m := readJSON(t, wctx, web)
		if m["type"] == "event" && m["event"] == "thread.snapshot" {
			t.Fatalf("snapshot preceded response: %v", m)
		}
		if m["type"] == "response" && m["requestId"] == id {
			sub = m
		}
	}
	if sub["outcome"] != "accepted" {
		t.Fatalf("subscribe: %v", sub)
	}
	snapshot := readJSON(t, wctx, web)
	if snapshot["event"] != "thread.snapshot" || snapshot["seq"] != float64(1) {
		t.Fatalf("snapshot: %v", snapshot)
	}
	startID := newTestID()
	writeJSON(t, wctx, web, map[string]any{"type": "request", "v": 1, "requestId": startID, "deviceId": "mock-device", "method": "turn.start", "params": map[string]any{"threadId": "mock-thread-a", "clientMessageId": newTestID(), "text": "hello"}})
	var start map[string]any
	for start == nil {
		m := readJSON(t, wctx, web)
		if m["type"] == "response" && m["requestId"] == startID {
			start = m
		}
	}
	if start["outcome"] != "accepted" {
		t.Fatalf("start: %v", start)
	}
	turnID := start["data"].(map[string]any)["turnId"].(string)
	if m := request("turn.interrupt", map[string]any{"threadId": "mock-thread-a", "expectedTurnId": "old-turn"}); m["outcome"] != "rejected" {
		t.Fatalf("stale interrupt: %v", m)
	}
	if m := request("turn.interrupt", map[string]any{"threadId": "mock-thread-a", "expectedTurnId": turnID}); m["outcome"] != "accepted" {
		t.Fatalf("interrupt: %v", m)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("agent did not stop")
	}
}

func writeJSON(t *testing.T, ctx context.Context, conn *websocket.Conn, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
}

func readJSON(t *testing.T, ctx context.Context, conn *websocket.Conn) map[string]any {
	t.Helper()
	_, b, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func newTestID() string { return "f1787c09-8c19-4e8e-bf3a-0be39bf8d2c1" }
