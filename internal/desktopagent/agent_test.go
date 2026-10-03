package desktopagent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/wu8685/Ariel/internal/relay"
)

func TestRealAgentRoutesHistoryAndSnapshotThroughRelay(t *testing.T) {
	r, _ := relay.New(relay.Config{Token: "test-secret", AllowedOrigins: []string{"http://localhost:5173"}})
	srv := httptest.NewServer(r.Handler())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	s := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) {
		return &fakeLive{updates: make(chan struct{}, 1)}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go RunWithService(ctx, Config{URL: url, Token: "test-secret", DeviceID: "real-mac", DeviceName: "Real Mac"}, s)
	wctx, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	w, _, err := websocket.Dial(wctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"http://localhost:5173"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer w.CloseNow()
	if err := wsjson.Write(wctx, w, map[string]any{"type": "hello", "v": 1, "role": "web", "token": "test-secret"}); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := wsjson.Read(wctx, w, &got); err != nil {
		t.Fatal(err)
	}
	requestID := "00000000-0000-4000-8000-000000000001"
	for i := 0; i < 10; i++ {
		if err := wsjson.Write(wctx, w, map[string]any{"type": "request", "v": 1, "requestId": requestID, "deviceId": "real-mac", "method": "thread.list", "params": map[string]any{"limit": 10}}); err != nil {
			t.Fatal(err)
		}
		if err := wsjson.Read(wctx, w, &got); err != nil {
			t.Fatal(err)
		}
		if got["type"] == "event" {
			if err := wsjson.Read(wctx, w, &got); err != nil {
				t.Fatal(err)
			}
		}
		if got["outcome"] == "accepted" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got["outcome"] != "accepted" {
		t.Fatalf("list: %v", got)
	}
	data := got["data"].(map[string]any)
	if len(data["threads"].([]any)) != 1 {
		t.Fatalf("history: %v", data)
	}
	requestID = "00000000-0000-4000-8000-000000000002"
	if err := wsjson.Write(wctx, w, map[string]any{"type": "request", "v": 1, "requestId": requestID, "deviceId": "real-mac", "method": "thread.subscribe", "params": map[string]any{"threadId": "thread"}}); err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Read(wctx, w, &got); err != nil {
		t.Fatal(err)
	}
	if got["type"] != "response" || got["outcome"] != "accepted" {
		t.Fatalf("subscription response: %v", got)
	}
	if err := wsjson.Read(wctx, w, &got); err != nil {
		t.Fatal(err)
	}
	if got["event"] != "thread.snapshot" || got["seq"] != float64(1) {
		t.Fatalf("snapshot: %v", got)
	}
}

func TestAgentHeartbeatDropsSilentRelay(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		var hello map[string]any
		if wsjson.Read(ctx, conn, &hello) != nil {
			return
		}
		_ = wsjson.Write(ctx, conn, map[string]any{"type": "hello.ok", "v": 1, "connectionId": "c", "relayEpoch": "e"})
		<-ctx.Done() // Intentionally no Reader for ping/pong after hello.
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := RunWithService(ctx, Config{URL: "ws" + strings.TrimPrefix(srv.URL, "http"), Token: "test-secret", DeviceID: "real-mac", DeviceName: "Real Mac", HeartbeatInterval: 30 * time.Millisecond, HeartbeatTimeout: 30 * time.Millisecond}, NewService(fakeHistory{}, nil))
	if err == nil || ctx.Err() != nil {
		t.Fatalf("silent Relay did not cause bounded disconnect: %v", err)
	}
}

func TestAgentAcceptsInterruptWhileStartReceiptPending(t *testing.T) {
	r, _ := relay.New(relay.Config{Token: "test-secret", AllowedOrigins: []string{"http://localhost:5173"}})
	srv := httptest.NewServer(r.Handler())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	live := &slowLive{fakeLive: &fakeLive{updates: make(chan struct{}, 2)}, started: make(chan struct{}), release: make(chan struct{}), stopped: make(chan struct{})}
	defer func() {
		select {
		case <-live.release:
		default:
			close(live.release)
		}
	}()
	s := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) { return live, nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go RunWithService(ctx, Config{URL: url, Token: "test-secret", DeviceID: "real-mac", DeviceName: "Real Mac"}, s)
	wctx, stop := context.WithTimeout(ctx, 4*time.Second)
	defer stop()
	w, _, err := websocket.Dial(wctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"http://localhost:5173"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer w.CloseNow()
	if err := wsjson.Write(wctx, w, map[string]any{"type": "hello", "v": 1, "role": "web", "token": "test-secret"}); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := wsjson.Read(wctx, w, &got); err != nil {
		t.Fatal(err)
	}
	// Poll the Relay registry; device.status may have preceded this Web client.
	for registered := false; !registered; {
		if err := wsjson.Write(wctx, w, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000010", "deviceId": "relay", "method": "device.list", "params": map[string]any{}}); err != nil {
			t.Fatal(err)
		}
		for {
			if err := wsjson.Read(wctx, w, &got); err != nil {
				t.Fatal(err)
			}
			if got["requestId"] == "00000000-0000-4000-8000-000000000010" {
				break
			}
		}
		for _, raw := range got["data"].(map[string]any)["devices"].([]any) {
			if raw.(map[string]any)["deviceId"] == "real-mac" {
				registered = true
			}
		}
		if !registered {
			time.Sleep(10 * time.Millisecond)
		}
	}
	request := func(id, method string, params map[string]any) {
		t.Helper()
		if err := wsjson.Write(wctx, w, map[string]any{"type": "request", "v": 1, "requestId": id, "deviceId": "real-mac", "method": method, "params": params}); err != nil {
			t.Fatal(err)
		}
	}
	request("00000000-0000-4000-8000-000000000011", "thread.subscribe", map[string]any{"threadId": "thread"})
	for i := 0; i < 2; i++ {
		if err := wsjson.Read(wctx, w, &got); err != nil {
			t.Fatal(err)
		}
	}
	request("00000000-0000-4000-8000-000000000012", "turn.start", map[string]any{"threadId": "thread", "clientMessageId": "00000000-0000-4000-8000-000000000014", "text": "hello"})
	select {
	case <-live.started:
	case <-time.After(time.Second):
		t.Fatal("start did not reach live owner")
	}
	request("00000000-0000-4000-8000-000000000013", "turn.interrupt", map[string]any{"threadId": "thread", "expectedTurnId": "one"})
	interruptCtx, interruptCancel := context.WithTimeout(wctx, time.Second)
	defer interruptCancel()
	for {
		if err := wsjson.Read(interruptCtx, w, &got); err != nil {
			t.Fatalf("interrupt blocked behind start receipt: %v", err)
		}
		if got["requestId"] == "00000000-0000-4000-8000-000000000013" {
			if got["outcome"] != "accepted" {
				t.Fatalf("interrupt: %v", got)
			}
			break
		}
	}
	close(live.release)
}
