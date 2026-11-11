package puck

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/wu8685/Ariel/internal/puck/toolcontract"
	"github.com/wu8685/Ariel/internal/relay"
)

// Turn 69: puck.create returned PROTOCOL_UNSUPPORTED in ~7s while the Desktop
// Agent log never recorded that code. Relay rejects puck.brain.ensure locally
// when the connected host advertises an older toolset than the one create
// requests. A host at the current or previous toolset must still create.
func TestPuckCreateAcceptsCurrentAndPreviousBrainToolset(t *testing.T) {
	versions := []int{toolcontract.Version}
	if toolcontract.Version > 1 {
		versions = append(versions, toolcontract.Version-1)
	}
	for _, advertised := range versions {
		t.Run(fmt.Sprintf("advertised-%d", advertised), func(t *testing.T) {
			code, gotVersion, messageCode := createAgainstAdvertisedToolset(t, advertised)
			if code == "PROTOCOL_UNSUPPORTED" {
				t.Fatalf("puck.create returned PROTOCOL_UNSUPPORTED for advertised toolset %d", advertised)
			}
			if code != "" {
				t.Fatalf("puck.create code=%s", code)
			}
			if gotVersion != advertised {
				t.Fatalf("ensure toolsetVersion=%d, want advertised %d", gotVersion, advertised)
			}
			if messageCode != "" {
				t.Fatalf("follow-up message code=%s", messageCode)
			}
		})
	}
}

func createAgainstAdvertisedToolset(t *testing.T, advertised int) (string, int, string) {
	t.Helper()
	const token, pin, origin, deviceID = "fixture-agent-token", "012345", "http://localhost:5173", "brain-host"
	r, err := relay.New(relay.Config{Token: token, WebPIN: pin, WebAuthMode: relay.WebAuthPIN, AllowedOrigins: []string{origin}, PuckState: StateReady, MutationTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	module, err := NewModule(t.TempDir(), r.PuckBridge())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = module.Close() })
	r.SetPuckHandler(module)
	server := httptest.NewServer(r.Handler())
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	wsURL := "ws" + server.URL[len("http"):] + "/ws"

	agent, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { agent.CloseNow() })
	if err := wsjson.Write(ctx, agent, map[string]any{
		"type": "hello", "v": 1, "role": "agent", "token": token,
		"deviceId": deviceID, "deviceName": "Brain Host", "agentEpoch": "epoch-a",
		"adapterVersion": fmt.Sprintf("desktop-ipc-0.160.0+puck-toolset-%d", advertised),
		"capabilities": map[string]bool{
			"autoLoad": false, "codexReady": true, "history": true, "send": true,
			"interrupt": true, "interaction": true, "threadBootstrap": true,
			"executionPolicy": true, "puckBrain": true, "puckBrainReady": true,
		},
	}); err != nil {
		t.Fatal(err)
	}
	var ack map[string]any
	if err := wsjson.Read(ctx, agent, &ack); err != nil || ack["type"] != "hello.ok" {
		t.Fatalf("agent hello: %+v %v", ack, err)
	}
	ensured := make(chan int, 1)
	go func() {
		for {
			var msg map[string]any
			if err := wsjson.Read(ctx, agent, &msg); err != nil {
				return
			}
			if msg["type"] != "request" {
				continue
			}
			method, _ := msg["method"].(string)
			data := map[string]any{}
			switch method {
			case "puck.brain.ensure":
				params, _ := msg["params"].(map[string]any)
				version, _ := params["toolsetVersion"].(float64)
				select {
				case ensured <- int(version):
				default:
				}
				data["threadId"] = "brain-thread"
			case "puck.brain.turn.start":
				data["turnId"] = "brain-turn"
			}
			_ = wsjson.Write(ctx, agent, map[string]any{
				"type": "response", "v": 1, "requestId": msg["requestId"], "outcome": "accepted", "data": data,
			})
		}
	}()

	header := http.Header{"Origin": []string{origin}}
	web, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { web.CloseNow() })
	if err := wsjson.Write(ctx, web, map[string]any{"type": "hello", "v": 1, "role": "web", "token": pin}); err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Read(ctx, web, &ack); err != nil || ack["type"] != "hello.ok" {
		t.Fatalf("web hello: %+v %v", ack, err)
	}
	if err := wsjson.Write(ctx, web, map[string]any{
		"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000001",
		"deviceId": "relay", "method": "puck.create",
		"params": map[string]any{"name": "turn69-fixture", "brainDeviceId": deviceID, "permissionMode": "full"},
	}); err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := wsjson.Read(ctx, web, &response); err != nil {
		t.Fatal(err)
	}
	code := ""
	if problem, ok := response["error"].(map[string]any); ok {
		code, _ = problem["code"].(string)
	}
	if response["outcome"] == "accepted" {
		code = ""
	}
	version := 0
	select {
	case version = <-ensured:
	case <-ctx.Done():
		return code, 0, ""
	}
	if code != "" {
		return code, version, ""
	}
	data, _ := response["data"].(map[string]any)
	assistant, _ := data["assistant"].(map[string]any)
	assistantID, _ := assistant["assistantId"].(string)
	if assistantID == "" {
		t.Fatalf("puck.create accepted without an assistant id: %#v", response)
	}
	if err := wsjson.Write(ctx, web, map[string]any{
		"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000002",
		"deviceId": "relay", "method": "puck.message.send",
		"params": map[string]any{"assistantId": assistantID, "clientMessageId": "00000000-0000-4000-8000-000000000003", "text": "继续"},
	}); err != nil {
		t.Fatal(err)
	}
	var message map[string]any
	if err := wsjson.Read(ctx, web, &message); err != nil {
		t.Fatal(err)
	}
	messageCode := ""
	if message["outcome"] != "accepted" {
		if problem, ok := message["error"].(map[string]any); ok {
			messageCode, _ = problem["code"].(string)
		}
		if messageCode == "" {
			messageCode = "rejected"
		}
	}
	return code, version, messageCode
}
