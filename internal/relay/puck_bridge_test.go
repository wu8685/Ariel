package relay

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPuckBridgeWhitelistsMethodsAndInjectsTrustedOrigin(t *testing.T) {
	r, err := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, PuckState: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(r.Handler())
	defer server.Close()
	agent := dialTest(t, server.URL, "")
	hello := agentHello()
	hello["capabilities"].(map[string]bool)["puckBrain"] = true
	hello["adapterVersion"] = "mock-1+puck-toolset-2"
	sendJSON(t, agent, hello)
	readJSON(t, agent)

	bridge := r.PuckBridge()
	if bridge == nil {
		t.Fatal("ready Relay has no Puck bridge")
	}
	if devices := bridge.Devices(); len(devices) != 1 || devices[0]["puckToolsetVersion"] != 2 {
		t.Fatalf("devices=%+v", devices)
	}
	if got := bridge.Forward(context.Background(), Origin{Kind: "puck", AssistantID: "assistant-a", Trigger: "tool"}, "mock-mac", "thread.archive.update", json.RawMessage(`{}`)); got.Error == nil || got.Error.Code != "UNAUTHORIZED" {
		t.Fatalf("forbidden method: %+v", got)
	}
	done := make(chan PuckResponse, 1)
	go func() {
		done <- bridge.Forward(context.Background(), Origin{Kind: "puck", AssistantID: "assistant-a", Trigger: "tool"}, "mock-mac", "thread.list", json.RawMessage(`{"limit":1}`))
	}()
	request := readJSON(t, agent)
	origin := request["origin"].(map[string]any)
	if origin["kind"] != "puck" || origin["assistantId"] != "assistant-a" || origin["trigger"] != "tool" {
		t.Fatalf("origin = %+v", origin)
	}
	sendJSON(t, agent, map[string]any{"type": "response", "v": 1, "requestId": request["requestId"], "outcome": "accepted", "data": map[string]any{"threads": []any{}}})
	select {
	case response := <-done:
		if response.Outcome != "accepted" {
			t.Fatalf("response: %+v", response)
		}
	case <-time.After(time.Second):
		t.Fatal("Puck bridge did not complete")
	}
}

func TestPuckBridgeRoutesBrainEventsByConnectionIdentity(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, PuckState: "ready"})
	server := httptest.NewServer(r.Handler())
	defer server.Close()
	agent := dialTest(t, server.URL, "")
	hello := agentHello()
	hello["capabilities"].(map[string]bool)["puckBrain"] = true
	sendJSON(t, agent, hello)
	readJSON(t, agent)
	bridge := r.PuckBridge()
	bridge.BrainThreads(map[BrainThread]string{{DeviceID: "mock-mac", ThreadID: "brain-a"}: "assistant-a"})
	sendJSON(t, agent, map[string]any{"type": "event", "v": 1, "event": "puck.brain.toolCall", "deviceId": "mock-mac", "threadId": "brain-a", "callId": "call-a", "tool": "list_devices", "arguments": map[string]any{}})
	select {
	case event := <-bridge.BrainEvents():
		if event.AssistantID != "assistant-a" || event.DeviceID != "mock-mac" || event.ThreadID != "brain-a" {
			t.Fatalf("event identity: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("brain event not routed")
	}
}

func TestPuckBridgeFailsClosedWhenExecutionPolicyIsUnverified(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, PuckState: "ready"})
	server := httptest.NewServer(r.Handler())
	defer server.Close()
	agent := dialTest(t, server.URL, "")
	hello := agentHello()
	hello["capabilities"].(map[string]bool)["executionPolicy"] = false
	sendJSON(t, agent, hello)
	readJSON(t, agent)
	response := r.PuckBridge().Forward(context.Background(), Origin{Kind: "puck", AssistantID: "assistant-a", Trigger: "tool"}, "mock-mac", "thread.create", json.RawMessage(`{"cwd":"/tmp/puck-fixture"}`))
	if response.Error == nil || response.Error.Code != "EXECUTION_POLICY_UNENFORCEABLE" {
		t.Fatalf("response=%+v", response)
	}
	if executionPolicyUnenforceable("thread.create", Origin{Kind: "puck", AssistantID: "assistant-a", Trigger: "user_decision"}, map[string]bool{"executionPolicy": false}) {
		t.Fatal("consumed user decision must permit the exact one-time action")
	}
}

func TestPuckBridgeForwardsAtomicThreadBootstrap(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, PuckState: "ready"})
	server := httptest.NewServer(r.Handler())
	defer server.Close()
	agent := dialTest(t, server.URL, "")
	hello := agentHello()
	hello["capabilities"].(map[string]bool)["threadBootstrap"] = true
	hello["capabilities"].(map[string]bool)["executionPolicy"] = true
	sendJSON(t, agent, hello)
	readJSON(t, agent)
	done := make(chan PuckResponse, 1)
	go func() {
		done <- r.PuckBridge().Forward(context.Background(), Origin{Kind: "puck", AssistantID: "assistant-a", Trigger: "tool"}, "mock-mac", "thread.bootstrap", json.RawMessage(`{"cwd":"/fixture","clientMessageId":"00000000-0000-4000-8000-000000000001","text":"run"}`))
	}()
	request := readJSON(t, agent)
	if request["method"] != "thread.bootstrap" || request["origin"].(map[string]any)["assistantId"] != "assistant-a" {
		t.Fatalf("request=%+v", request)
	}
	sendJSON(t, agent, map[string]any{"type": "response", "v": 1, "requestId": request["requestId"], "outcome": "accepted", "data": map[string]any{"threadId": "thread-a", "turnId": "turn-a"}})
	if result := <-done; result.Outcome != "accepted" {
		t.Fatalf("result=%+v", result)
	}
}

func TestWebCannotCallPuckBrainMethods(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, PuckState: "ready"})
	server := httptest.NewServer(r.Handler())
	defer server.Close()
	web := dialTest(t, server.URL, testOrigin)
	sendJSON(t, web, webHello())
	readJSON(t, web)
	sendJSON(t, web, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000130", "deviceId": "mock-mac", "method": "puck.brain.ensure", "params": map[string]any{"assistantId": "assistant-a", "name": "Puck", "cwd": "/tmp/puck", "developerInstructions": "fixture", "toolsetVersion": 1}})
	response := readJSON(t, web)
	if response["outcome"] != "rejected" || response["error"].(map[string]any)["code"] != "UNAUTHORIZED" {
		t.Fatalf("brain method response: %+v", response)
	}
}

func TestPuckBridgeRejectsBrainToolsetNewerThanAgent(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, PuckState: "ready"})
	server := httptest.NewServer(r.Handler())
	defer server.Close()
	agent := dialTest(t, server.URL, "")
	hello := agentHello()
	hello["capabilities"].(map[string]bool)["puckBrain"] = true
	hello["adapterVersion"] = "mock-1+puck-toolset-1"
	sendJSON(t, agent, hello)
	readJSON(t, agent)
	response := r.PuckBridge().Forward(context.Background(), Origin{Kind: "puck", AssistantID: "assistant-a", Trigger: "tool"}, "mock-mac", "puck.brain.ensure", json.RawMessage(`{"assistantId":"assistant-a","name":"Puck","cwd":"/fixture","developerInstructions":"guard","toolsetVersion":2}`))
	if response.Error == nil || response.Error.Code != "PROTOCOL_UNSUPPORTED" {
		t.Fatalf("response=%+v", response)
	}
}

func TestPuckToolsetVersionUsesBackwardCompatibleAdapterVersion(t *testing.T) {
	for adapter, expected := range map[string]int{
		"desktop-ipc-0.160.0":                      0,
		"desktop-ipc-0.160.0+puck-toolset-2":       2,
		"desktop-ipc-0.160.0+puck-toolset-invalid": 0,
		"desktop-ipc-0.160.0+puck-toolset-2-extra": 0,
	} {
		if actual := parsePuckToolsetVersion(adapter); actual != expected {
			t.Fatalf("adapter=%q actual=%d expected=%d", adapter, actual, expected)
		}
	}
}

func TestPuckWatchSharesRelayCacheFollowerAndRejectsBrainThread(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, PuckState: "ready"})
	bridge := r.PuckBridge()
	origin := Origin{Kind: "puck", AssistantID: "assistant-a", Trigger: "event"}
	bridge.BrainThreads(map[BrainThread]string{{DeviceID: "mock-mac", ThreadID: "brain-a"}: "assistant-a"})
	if _, _, err := bridge.Watch(context.Background(), origin, "mock-mac", "brain-a"); err == nil || err.Code != "RECURSION_DENIED" {
		t.Fatalf("brain watch error = %+v", err)
	}
	r.mu.Lock()
	r.cache.watch(cacheKey{"mock-mac", "thread-a"}, "web-watch", &peer{}, time.Now())
	r.mu.Unlock()
	ctx, cancelContext := context.WithCancel(context.Background())
	events, cancel, err := bridge.Watch(ctx, origin, "mock-mac", "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	if first := <-events; first["watchId"] == "" {
		t.Fatalf("initial event: %+v", first)
	}
	r.mu.Lock()
	watchers := len(r.cache.entries[cacheKey{"mock-mac", "thread-a"}].watchers)
	promoting := r.cachePromoting[cacheKey{"mock-mac", "thread-a"}]
	r.mu.Unlock()
	if watchers != 2 || promoting {
		t.Fatalf("watchers=%d promoting=%v", watchers, promoting)
	}
	cancel()
	cancelContext()
}

func TestPuckBridgeHasIndependentBoundedWriteQuota(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, PuckState: "ready"})
	bridge := r.PuckBridge()
	origin := Origin{Kind: "puck", AssistantID: "assistant-a", Trigger: "tool"}
	for i := 0; i < 120; i++ {
		response := bridge.Forward(context.Background(), origin, "offline", "turn.start", json.RawMessage(`{"threadId":"thread-a","clientMessageId":"00000000-0000-4000-8000-000000000130","text":"fixture"}`))
		if response.Error == nil || response.Error.Code != "DEVICE_OFFLINE" {
			t.Fatalf("attempt %d: %+v", i, response)
		}
	}
	if response := bridge.Forward(context.Background(), origin, "offline", "turn.start", json.RawMessage(`{"threadId":"thread-a","clientMessageId":"00000000-0000-4000-8000-000000000130","text":"fixture"}`)); response.Error == nil || response.Error.Code != "OVERLOADED" {
		t.Fatalf("quota response: %+v", response)
	}
	if response := bridge.Forward(context.Background(), origin, "offline", "thread.list", json.RawMessage(`{"limit":1}`)); response.Error == nil || response.Error.Code != "DEVICE_OFFLINE" {
		t.Fatalf("read used write quota: %+v", response)
	}
}

func TestPuckInteractionResponseRequiresSingleUseWebDecision(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, PuckState: "ready"})
	server := httptest.NewServer(r.Handler())
	defer server.Close()
	web := dialTest(t, server.URL, testOrigin)
	sendJSON(t, web, webHello())
	readJSON(t, web)
	sendJSON(t, web, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000131", "deviceId": "relay", "method": "puck.decision.record", "params": map[string]any{"interactionId": "interaction-a", "decision": "accept_once"}})
	recorded := readJSON(t, web)
	decisionID := recorded["data"].(map[string]any)["userDecisionId"].(string)
	origin := Origin{Kind: "puck", AssistantID: "assistant-a", Trigger: "tool"}
	params := json.RawMessage(`{"threadId":"thread-a","interactionId":"interaction-a","decision":"accept_once","userDecisionId":"` + decisionID + `"}`)
	if response := r.PuckBridge().Forward(context.Background(), origin, "offline", "interaction.respond", params); response.Error == nil || response.Error.Code != "DEVICE_OFFLINE" {
		t.Fatalf("valid decision: %+v", response)
	}
	if response := r.PuckBridge().Forward(context.Background(), origin, "offline", "interaction.respond", params); response.Error == nil || response.Error.Code != "UNAUTHORIZED" {
		t.Fatalf("replayed decision: %+v", response)
	}
	changed := json.RawMessage(`{"threadId":"thread-a","interactionId":"interaction-a","decision":"deny","userDecisionId":"` + decisionID + `"}`)
	if response := r.PuckBridge().Forward(context.Background(), origin, "offline", "interaction.respond", changed); response.Error == nil || response.Error.Code != "UNAUTHORIZED" {
		t.Fatalf("changed decision: %+v", response)
	}
}
