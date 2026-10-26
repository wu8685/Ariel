package relay

import (
	"fmt"
	"net/http/httptest"
	"testing"
)

func TestExecutionCapabilitiesNeverLeakIntoLegacyDeviceProjection(t *testing.T) {
	p := &peer{capabilities: map[string]bool{"autoLoad": true, "codexReady": true, "executionSettings": true, "turnEditing": true}}
	if _, ok := p.currentCapabilities()["turnEditing"]; ok {
		t.Fatal("edit capability leaked to legacy Web")
	}
	if _, ok := p.currentCapabilities()["executionSettings"]; ok {
		t.Fatal("new capability breaks strict legacy Web schema")
	}
	if !p.capabilities["executionSettings"] {
		t.Fatal("projection mutated native registration")
	}
}

func TestExecutionSettingsOldAgentIsRejectedWithoutForwardingUnknownMethod(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	server := httptest.NewServer(r.Handler())
	defer server.Close()
	a := dialTest(t, server.URL, "")
	sendJSON(t, a, agentHello())
	readJSON(t, a)
	w := dialTest(t, server.URL, testOrigin)
	sendJSON(t, w, webHello())
	readJSON(t, w)
	request := func(n int, method string, params any) {
		sendJSON(t, w, map[string]any{"type": "request", "v": 1, "requestId": fmt.Sprintf("00000000-0000-4000-8000-%012d", n), "deviceId": "mock-mac", "method": method, "params": params})
	}
	request(1, "model.catalog.read", map[string]any{})
	reply := readJSON(t, w)
	if reply["error"].(map[string]any)["code"] != "PROTOCOL_UNSUPPORTED" {
		t.Fatal(reply)
	}
	request(2, "thread.list", map[string]any{"limit": 1})
	if forwarded := readJSON(t, a); forwarded["method"] != "thread.list" {
		t.Fatal("unknown method reached old Agent", forwarded)
	}
}

func TestExecutionSettingsOnlyReachOptedWatcherAndReleaseOnDisconnect(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	server := httptest.NewServer(r.Handler())
	defer server.Close()
	a := dialTest(t, server.URL, "")
	hello := agentHello()
	hello["capabilities"].(map[string]bool)["executionSettings"] = true
	sendJSON(t, a, hello)
	readJSON(t, a)
	legacy := dialTest(t, server.URL, testOrigin)
	sendJSON(t, legacy, webHello())
	readJSON(t, legacy)
	w := dialTest(t, server.URL, testOrigin)
	sendJSON(t, w, webHello())
	readJSON(t, w)
	sendJSON(t, w, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000101", "deviceId": "mock-mac", "method": "model.catalog.read", "params": map[string]any{}})
	req := readJSON(t, a)
	sendJSON(t, a, map[string]any{"type": "response", "v": 1, "requestId": req["requestId"], "outcome": "accepted", "data": map[string]any{}})
	readJSON(t, w)
	sendJSON(t, w, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000102", "deviceId": "mock-mac", "method": "thread.settings.watch", "params": map[string]any{"threadId": "t"}})
	req = readJSON(t, a)
	sendJSON(t, a, map[string]any{"type": "response", "v": 1, "requestId": req["requestId"], "outcome": "accepted", "data": map[string]any{"subscriptionId": "settings-watch", "streamId": "settings-stream"}})
	readJSON(t, w)
	event := map[string]any{"type": "event", "v": 1, "event": "thread.settings.update", "deviceId": "mock-mac", "agentEpoch": "epoch-a", "threadId": "t", "subscriptionId": "settings-watch", "streamId": "settings-stream", "seq": 2, "settings": map[string]any{"revision": "r", "freshness": "fresh", "writable": true, "values": map[string]any{"model": "m", "effort": "low", "speed": map[string]any{"mode": "standard"}}}}
	sendJSON(t, a, event)
	if got := readJSON(t, w); got["event"] != "thread.settings.update" {
		t.Fatal(got)
	}
	sendJSON(t, legacy, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000103", "deviceId": "relay", "method": "device.list", "params": map[string]any{}})
	if got := readJSON(t, legacy); got["type"] != "response" {
		t.Fatal("new event leaked to legacy Web", got)
	}
	w.CloseNow()
	if cleanup := readJSON(t, a); cleanup["method"] != "thread.settings.unwatch" {
		t.Fatal("settings watch leaked after disconnect", cleanup)
	}
}
