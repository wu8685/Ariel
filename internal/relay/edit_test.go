package relay

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEditingRequiresOptInAndRoutesToCurrentAgent(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	server := httptest.NewServer(r.Handler())
	defer server.Close()
	a := dialTest(t, server.URL, "")
	hello := agentHello()
	caps := hello["capabilities"].(map[string]bool)
	caps["turnEditing"] = true
	caps["executionSettings"] = true
	sendJSON(t, a, hello)
	readJSON(t, a)
	w := dialTest(t, server.URL, testOrigin)
	sendJSON(t, w, webHello())
	readJSON(t, w)
	req := map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000001", "deviceId": "mock-mac", "method": "turn.edit", "params": map[string]any{"threadId": "thread", "agentEpoch": "epoch-a", "expectedTurnId": "old", "expectedItemId": "user", "fingerprint": strings.Repeat("a", 64), "operationId": "op", "text": "replacement"}}
	sendJSON(t, w, req)
	if readJSON(t, w)["outcome"] != "rejected" {
		t.Fatal("edit without opt-in accepted")
	}
	sendJSON(t, w, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000002", "deviceId": "mock-mac", "method": "model.catalog.read", "params": map[string]any{}})
	forwarded := readJSON(t, a)
	sendJSON(t, a, map[string]any{"type": "response", "v": 1, "requestId": forwarded["requestId"], "outcome": "accepted", "data": map[string]any{"editSupported": true}})
	readJSON(t, w)
	req["requestId"] = "00000000-0000-4000-8000-000000000003"
	sendJSON(t, w, req)
	if readJSON(t, a)["method"] != "turn.edit" {
		t.Fatal("edit not forwarded")
	}
	if !isLongRead("turn.edit.read") || isLongRead("turn.edit") {
		t.Fatal("edit timeout classification unsafe")
	}
}
