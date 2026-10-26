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
	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/probe"
	"github.com/wu8685/Ariel/internal/relay"
)

// Called only with the fixture created and authorized by the real settings gate.
// This uses real WebSockets, Relay, Agent Service, follower and Codex owner.
func verifyNativeSettingsWire(t *testing.T, parent context.Context, m probe.Manifest, live Live, rpc appserver.RPC, effort string) {
	t.Helper()
	if m.Authorize(true, m.ThreadID, m.Workspace) != nil {
		t.Fatal("settings wire fixture guard")
	}
	service := NewService(singleFixtureHistory{thread: appserver.Thread{ID: m.ThreadID, CWD: m.Workspace}}, func(context.Context, string, string) (Live, error) { return live, nil }, appserver.QueueReader{RPC: rpc})
	defer service.Close()
	service.SetModelReader(appserver.ModelReader{RPC: rpc})
	_, _, _, activate, err := service.Subscribe(parent, m.ThreadID, func(map[string]any) {})
	if err != nil {
		t.Fatal(err)
	}
	activate()
	r, err := relay.New(relay.Config{Token: "isolated-settings-agent", WebPIN: "012345", AllowedOrigins: []string{"http://localhost:5173"}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(r.Handler())
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(parent, 25*time.Second)
	defer cancel()
	ready := make(chan struct{})
	stopped := make(chan error, 1)
	go func() {
		stopped <- RunWithService(ctx, Config{URL: url, Token: "isolated-settings-agent", DeviceID: "settings-fixture", DeviceName: "Settings fixture", AgentEpoch: "settings-fixture-epoch", OnReady: func() error { close(ready); return nil }}, service)
	}()
	select {
	case <-ready:
	case err := <-stopped:
		t.Fatal("fixture Agent failed", err)
	case <-ctx.Done():
		t.Fatal("fixture Agent registration timeout")
	}
	web, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"http://localhost:5173"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer web.CloseNow()
	if err = wsjson.Write(ctx, web, map[string]any{"type": "hello", "v": 1, "role": "web", "token": "012345"}); err != nil {
		t.Fatal(err)
	}
	read := func() map[string]any {
		var out map[string]any
		if e := wsjson.Read(ctx, web, &out); e != nil {
			t.Fatal("settings wire response", e)
		}
		return out
	}
	read()
	send := func(id, method string, params map[string]any) {
		if e := wsjson.Write(ctx, web, map[string]any{"type": "request", "v": 1, "requestId": id, "deviceId": "settings-fixture", "method": method, "params": params}); e != nil {
			t.Fatal(e)
		}
	}
	response := func(id string) map[string]any {
		for {
			msg := read()
			if msg["type"] == "response" && msg["requestId"] == id {
				if msg["outcome"] != "accepted" {
					t.Fatal("native settings wire rejected", msg["error"])
				}
				return msg["data"].(map[string]any)
			}
		}
	}
	catalogID := fixtureMessageID(t)
	send(catalogID, "model.catalog.read", map[string]any{})
	response(catalogID)
	watchID := fixtureMessageID(t)
	send(watchID, "thread.settings.watch", map[string]any{"threadId": m.ThreadID})
	baseline := response(watchID)
	revision := baseline["settings"].(map[string]any)["revision"]
	writeID := fixtureMessageID(t)
	send(writeID, "thread.settings.update", map[string]any{"threadId": m.ThreadID, "expectedRevision": revision, "operationId": fixtureMessageID(t), "patch": map[string]any{"effort": effort}})
	accepted, pushed := false, false
	for !accepted || !pushed {
		msg := read()
		if msg["type"] == "response" && msg["requestId"] == writeID {
			if msg["outcome"] != "accepted" {
				t.Fatal("native settings wire write rejected", msg["error"])
			}
			accepted = true
		}
		if msg["event"] == "thread.settings.update" && msg["subscriptionId"] == baseline["subscriptionId"] {
			values := msg["settings"].(map[string]any)["values"].(map[string]any)
			pushed = values["effort"] == effort
		}
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("fixture Agent shutdown timeout")
	}
	t.Log("verified real Relay/Agent/native write and follower-to-Web settings push")
}
