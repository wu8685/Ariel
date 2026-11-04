package relay

import (
	"encoding/json"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/wu8685/Ariel/protocol"
)

func TestPuckStatusIsReadOverHTTPAndDisabledMethodsStayRelayLocal(t *testing.T) {
	r, err := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, PuckState: "unavailable", PuckReason: "PUCK_REQUIRES_PASSKEY_HTTPS"})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	web := dialTest(t, s.URL, testOrigin)
	sendJSON(t, web, webHello())
	ack := readJSON(t, web)
	if _, exists := ack["puck"]; exists {
		t.Fatalf("hello.ok must not carry unnegotiated Puck status: %+v", ack)
	}
	id := "00000000-0000-4000-8000-000000000091"
	sendJSON(t, web, map[string]any{"type": "request", "v": 1, "requestId": id, "deviceId": "relay", "method": "puck.list", "params": map[string]any{}})
	got := readJSON(t, web)
	errorBody, _ := got["error"].(map[string]any)
	if got["outcome"] != "rejected" || errorBody["code"] != "FEATURE_DISABLED" || errorBody["reason"] != "PUCK_REQUIRES_PASSKEY_HTTPS" {
		t.Fatalf("disabled Puck response: %+v", got)
	}
}

func TestHelloOKPreservesLegacyV1ContractForUnnegotiatedPeers(t *testing.T) {
	states := []struct {
		name, state, reason string
	}{
		{name: "empty"},
		{name: "off", state: "off"},
		{name: "ready", state: "ready"},
		{name: "unavailable", state: "unavailable", reason: "PUCK_REQUIRES_PASSKEY_HTTPS"},
		{name: "degraded", state: "degraded", reason: "PUCK_BRAIN_UNAVAILABLE"},
	}
	roles := []struct {
		name, origin string
		hello        func() map[string]any
	}{
		{name: "agent", hello: agentHello},
		{name: "web", origin: testOrigin, hello: webHello},
	}
	for _, state := range states {
		for _, role := range roles {
			t.Run(state.name+"/"+role.name, func(t *testing.T) {
				r, err := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, PuckState: state.state, PuckReason: state.reason})
				if err != nil {
					t.Fatal(err)
				}
				s := httptest.NewServer(r.Handler())
				defer s.Close()
				peer := dialTest(t, s.URL, role.origin)
				sendJSON(t, peer, role.hello())
				ack := readJSON(t, peer)
				assertLegacyHelloOK(t, ack, role.name == "web")
			})
		}
	}
}

// assertLegacyHelloOK freezes the strict helloOK shape from
// protocol/v1.schema.json at 911a45b1e8d8684eb3de4fa359ab62d1cab971b1.
func assertLegacyHelloOK(t *testing.T, ack map[string]any, wantSessionToken bool) {
	t.Helper()
	allowed := []string{"connectionId", "relayEpoch", "sessionToken", "type", "v"}
	for key := range ack {
		if !slices.Contains(allowed, key) {
			t.Fatalf("legacy helloOK additionalProperties=false rejects %q: %+v", key, ack)
		}
	}
	if ack["type"] != "hello.ok" || ack["v"] != float64(1) || ack["connectionId"] == "" || ack["relayEpoch"] == "" {
		t.Fatalf("legacy helloOK required fields changed: %+v", ack)
	}
	_, hasSessionToken := ack["sessionToken"]
	if hasSessionToken != wantSessionToken {
		t.Fatalf("sessionToken presence=%v, want %v: %+v", hasSessionToken, wantSessionToken, ack)
	}
	raw, err := json.Marshal(ack)
	if err != nil {
		t.Fatal(err)
	}
	if err := protocol.Validate(raw); err != nil {
		t.Fatalf("current receiver rejected legacy acknowledgement: %v", err)
	}
}
