package desktopagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/wu8685/Ariel/internal/relay"
)

type asyncQuestionFixtureLive struct {
	*fakeLive
	title string
}

func (l *asyncQuestionFixtureLive) Current() (json.RawMessage, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return json.Marshal(map[string]any{"cwd": "/fixture", "threadRuntimeStatus": map[string]string{"type": "inProgress"}, "requests": []any{}, "turns": []any{map[string]any{"turnId": "turn", "status": "inProgress", "items": []any{map[string]any{"id": "message", "type": "agentMessage", "text": "Independent work", "questions": []any{map[string]any{"title": l.title, "options": []string{"Blue", "Green"}}}}}}}})
}

func TestAgentQuestionOptInRepublishesQuietSnapshotWithoutAnotherFollower(t *testing.T) {
	r, err := relay.New(relay.Config{Token: "test-secret", WebPIN: "012345", AllowedOrigins: []string{"http://localhost:5173"}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(r.Handler())
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	live := &asyncQuestionFixtureLive{fakeLive: &fakeLive{updates: make(chan struct{}, 2)}, title: "Choose color"}
	var attaches atomic.Int32
	service := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) { attaches.Add(1); return live, nil })
	defer service.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{})
	go RunWithService(ctx, Config{URL: url, Token: "test-secret", DeviceID: "mac", DeviceName: "fixture", CachePublishInterval: time.Hour, OnReady: func() error { close(ready); return nil }}, service)
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("Agent did not connect")
	}
	connectWeb := func(optIn bool) *websocket.Conn {
		conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"http://localhost:5173"}}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.CloseNow() })
		if err := wsjson.Write(ctx, conn, map[string]any{"type": "hello", "v": 1, "role": "web", "token": "012345"}); err != nil {
			t.Fatal(err)
		}
		var ack map[string]any
		if err := wsjson.Read(ctx, conn, &ack); err != nil {
			t.Fatal(err)
		}
		params := map[string]any{"threadId": "thread"}
		if optIn {
			params["asyncQuestionsVersion"] = 1
		}
		if err := wsjson.Write(ctx, conn, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000071", "deviceId": "mac", "method": "thread.cache.watch", "params": params}); err != nil {
			t.Fatal(err)
		}
		var response map[string]any
		if err := wsjson.Read(ctx, conn, &response); err != nil {
			t.Fatal(err)
		}
		if !optIn && response["data"].(map[string]any)["asyncQuestions"] != nil {
			t.Fatal("legacy Web received sidecar")
		}
		return conn
	}
	web := connectWeb(true)
	readQuestion := func() map[string]any {
		readCtx, stop := context.WithTimeout(ctx, 2*time.Second)
		defer stop()
		for {
			var message map[string]any
			if err := wsjson.Read(readCtx, web, &message); err != nil {
				t.Fatalf("question snapshot not republished after opt-in: %v", err)
			}
			if sidecar, ok := message["asyncQuestions"].(map[string]any); ok {
				return sidecar
			}
		}
	}
	first := readQuestion()
	if len(first["questions"].([]any)) != 1 || first["writable"] != false {
		t.Fatal("incorrect question projection")
	}
	for i := 0; i < 5; i++ {
		connectWeb(false)
	}
	if attaches.Load() != 1 {
		t.Fatalf("per-Web followers created: %d", attaches.Load())
	}
	live.mu.Lock()
	live.title = "New native question"
	live.mu.Unlock()
	live.updates <- struct{}{}
	next := readQuestion()
	if next["questions"].([]any)[0].(map[string]any)["title"] != "New native question" {
		t.Fatal("question change waited for text cadence")
	}
}
