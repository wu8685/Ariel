package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestPasskeyModeAcceptsOnlyVerifiedCookieForWebAndKeepsAgentToken(t *testing.T) {
	r, err := New(Config{
		Token:          "agent-secret",
		WebAuthMode:    WebAuthPasskey,
		AllowedOrigins: []string{testOrigin},
		WebSessionVerifier: func(req *http.Request) bool {
			cookie, err := req.Cookie("ariel-test-session")
			return err == nil && cookie.Value == "valid"
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(r.Handler())
	defer s.Close()

	tryWeb := func(cookie, token string) bool {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		header := http.Header{"Origin": {testOrigin}}
		if cookie != "" {
			header.Set("Cookie", "ariel-test-session="+cookie)
		}
		c, _, err := websocket.Dial(ctx, strings.Replace(s.URL, "http://", "ws://", 1)+"/ws", &websocket.DialOptions{HTTPHeader: header})
		if err != nil {
			return false
		}
		defer c.CloseNow()
		if err := wsjson.Write(ctx, c, map[string]any{"type": "hello", "v": 1, "role": "web", "token": token}); err != nil {
			return false
		}
		var got map[string]any
		return wsjson.Read(ctx, c, &got) == nil && got["type"] == "hello.ok"
	}
	if tryWeb("", "012345") || tryWeb("", "s_"+strings.Repeat("a", 64)) || tryWeb("", "p_"+strings.Repeat("a", 64)) {
		t.Fatal("passkey mode accepted a legacy Web credential")
	}
	if !tryWeb("valid", "cookie") {
		t.Fatal("verified Web session cookie was rejected")
	}

	agent := dialTest(t, s.URL, "")
	sendJSON(t, agent, map[string]any{"type": "hello", "v": 1, "role": "agent", "token": "agent-secret", "deviceId": "mac", "deviceName": "Mac", "agentEpoch": "epoch", "adapterVersion": "test", "capabilities": map[string]bool{"autoLoad": false, "codexReady": true}})
	if got := readJSON(t, agent); got["type"] != "hello.ok" {
		t.Fatalf("Agent token rejected: %+v", got)
	}
}

func TestWebPINIsSeparateFromAgentToken(t *testing.T) {
	if _, err := New(Config{Token: "012345", WebPIN: "012345", AllowedOrigins: []string{testOrigin}}); err == nil {
		t.Fatal("identical credentials accepted")
	}
	r, err := New(Config{Token: "agent-secret", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	for _, tc := range []struct {
		role, token string
		accepted    bool
	}{
		{"web", "012345", true},
		{"web", "agent-secret", false},
		{"agent", "agent-secret", true},
		{"agent", "012345", false},
	} {
		c := dialTest(t, s.URL, testOrigin)
		hello := map[string]any{"type": "hello", "v": 1, "role": tc.role, "token": tc.token}
		if tc.role == "agent" {
			hello["deviceId"] = "mac"
			hello["deviceName"] = "Mac"
			hello["agentEpoch"] = "epoch"
			hello["adapterVersion"] = "test"
			hello["capabilities"] = map[string]bool{"autoLoad": false, "codexReady": false}
		}
		sendJSON(t, c, hello)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		var got map[string]any
		err := wsjson.Read(ctx, c, &got)
		cancel()
		if tc.accepted != (err == nil && got["type"] == "hello.ok") {
			t.Fatalf("%s with %q: got %v, %v", tc.role, tc.token, got, err)
		}
		c.CloseNow()
	}
}

func TestWebPINLocksAfterTenFailuresUntilNewRelay(t *testing.T) {
	cfg := Config{Token: "agent-secret", WebPIN: "012345", AllowedOrigins: []string{testOrigin}}
	r, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	existing := dialTest(t, s.URL, testOrigin)
	sendJSON(t, existing, map[string]any{"type": "hello", "v": 1, "role": "web", "token": "012345"})
	if got := readJSON(t, existing); got["type"] != "hello.ok" {
		t.Fatalf("existing Web: %v", got)
	}
	tryPIN := func(token string) bool {
		c := dialTest(t, s.URL, testOrigin)
		sendJSON(t, c, map[string]any{"type": "hello", "v": 1, "role": "web", "token": token})
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		var got map[string]any
		err := wsjson.Read(ctx, c, &got)
		c.CloseNow()
		return err == nil && got["type"] == "hello.ok"
	}
	for i := 0; i < 9; i++ {
		if tryPIN("999999") {
			t.Fatal("wrong PIN accepted")
		}
	}
	badAgent := dialTest(t, s.URL, testOrigin)
	sendJSON(t, badAgent, map[string]any{"type": "hello", "v": 1, "role": "agent", "token": "wrong", "deviceId": "mac", "deviceName": "Mac", "agentEpoch": "epoch", "adapterVersion": "test", "capabilities": map[string]bool{"autoLoad": false, "codexReady": false}})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	var denied map[string]any
	if err := wsjson.Read(ctx, badAgent, &denied); err == nil {
		t.Fatal("bad Agent accepted")
	}
	cancel()
	if !tryPIN("012345") {
		t.Fatal("Agent failure consumed Web PIN attempt")
	}
	if tryPIN("999999") {
		t.Fatal("tenth wrong PIN accepted")
	}
	if tryPIN("012345") {
		t.Fatal("PIN accepted after lock")
	}
	sendJSON(t, existing, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000001", "deviceId": "relay", "method": "device.list", "params": map[string]any{}})
	if got := readJSON(t, existing); got["outcome"] != "accepted" {
		t.Fatalf("existing Web disconnected by PIN lock: %v", got)
	}
	agent := dialTest(t, s.URL, testOrigin)
	sendJSON(t, agent, map[string]any{"type": "hello", "v": 1, "role": "agent", "token": "agent-secret", "deviceId": "mac", "deviceName": "Mac", "agentEpoch": "epoch", "adapterVersion": "test", "capabilities": map[string]bool{"autoLoad": false, "codexReady": false}})
	if got := readJSON(t, agent); got["type"] != "hello.ok" {
		t.Fatalf("Agent blocked by Web PIN lock: %v", got)
	}
	fresh, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s2 := httptest.NewServer(fresh.Handler())
	defer s2.Close()
	c := dialTest(t, s2.URL, testOrigin)
	sendJSON(t, c, map[string]any{"type": "hello", "v": 1, "role": "web", "token": "012345"})
	if got := readJSON(t, c); got["type"] != "hello.ok" {
		t.Fatalf("fresh relay: %v", got)
	}
}
