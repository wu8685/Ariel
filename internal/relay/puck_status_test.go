package relay

import (
	"net/http/httptest"
	"testing"
)

func TestPuckStatusIsOptionalAndDisabledMethodsStayRelayLocal(t *testing.T) {
	r, err := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, PuckState: "unavailable", PuckReason: "PUCK_REQUIRES_PASSKEY_HTTPS"})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	web := dialTest(t, s.URL, testOrigin)
	sendJSON(t, web, webHello())
	ack := readJSON(t, web)
	puck, _ := ack["puck"].(map[string]any)
	if puck["state"] != "unavailable" || puck["reason"] != "PUCK_REQUIRES_PASSKEY_HTTPS" {
		t.Fatalf("hello Puck status: %+v", ack)
	}
	id := "00000000-0000-4000-8000-000000000091"
	sendJSON(t, web, map[string]any{"type": "request", "v": 1, "requestId": id, "deviceId": "relay", "method": "puck.list", "params": map[string]any{}})
	got := readJSON(t, web)
	errorBody, _ := got["error"].(map[string]any)
	if got["outcome"] != "rejected" || errorBody["code"] != "FEATURE_DISABLED" || errorBody["reason"] != "PUCK_REQUIRES_PASSKEY_HTTPS" {
		t.Fatalf("disabled Puck response: %+v", got)
	}
}

func TestLegacyHelloOmitsPuckStatusWhenNotConfigured(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	web := dialTest(t, s.URL, testOrigin)
	sendJSON(t, web, webHello())
	if ack := readJSON(t, web); ack["puck"] != nil {
		t.Fatalf("legacy hello shape changed: %+v", ack)
	}
}
