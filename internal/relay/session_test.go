package relay

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket/wsjson"
)

func webSessionHello(t *testing.T, url, token string) (map[string]any, bool) {
	t.Helper()
	c := dialTest(t, url, testOrigin)
	sendJSON(t, c, map[string]any{"type": "hello", "v": 1, "role": "web", "token": token})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var got map[string]any
	err := wsjson.Read(ctx, c, &got)
	c.CloseNow()
	return got, err == nil && got["type"] == "hello.ok"
}

func TestWebSessionIssuedReusedAndInvalidatedByRelayRestart(t *testing.T) {
	cfg := Config{Token: "agent-secret", WebPIN: "012345", AllowedOrigins: []string{testOrigin}}
	r, _ := New(cfg)
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	ack, ok := webSessionHello(t, s.URL, "012345")
	if !ok {
		t.Fatal("PIN rejected")
	}
	token, _ := ack["sessionToken"].(string)
	if !strings.HasPrefix(token, "s_") || len(token) != 66 {
		t.Fatalf("invalid session token shape: %q", token)
	}
	if _, ok := webSessionHello(t, s.URL, token); !ok {
		t.Fatal("session token rejected")
	}
	if _, ok := webSessionHello(t, s.URL, "s_"+strings.Repeat("0", 64)); ok {
		t.Fatal("unknown session accepted")
	}
	fresh, _ := New(cfg)
	s2 := httptest.NewServer(fresh.Handler())
	defer s2.Close()
	if _, ok := webSessionHello(t, s2.URL, token); ok {
		t.Fatal("old session survived Relay restart")
	}
}

func TestWebSessionExpiryCapacityAndPINLock(t *testing.T) {
	r, _ := New(Config{Token: "agent-secret", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	var unixSeconds atomic.Int64
	unixSeconds.Store(1_000_000)
	r.now = func() time.Time { return time.Unix(unixSeconds.Load(), 0) }
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	ack, ok := webSessionHello(t, s.URL, "012345")
	if !ok {
		t.Fatal("PIN rejected")
	}
	first := ack["sessionToken"].(string)
	for range 32 {
		if _, ok := webSessionHello(t, s.URL, "012345"); !ok {
			t.Fatal("PIN session issuance failed")
		}
	}
	if _, ok := webSessionHello(t, s.URL, first); ok {
		t.Fatal("oldest session not evicted at capacity")
	}
	ack, _ = webSessionHello(t, s.URL, "012345")
	latest := ack["sessionToken"].(string)
	unixSeconds.Add(int64((24*time.Hour + time.Second).Seconds()))
	if _, ok := webSessionHello(t, s.URL, latest); ok {
		t.Fatal("expired session accepted")
	}
	ack, _ = webSessionHello(t, s.URL, "012345")
	valid := ack["sessionToken"].(string)
	for range 9 {
		if _, ok := webSessionHello(t, s.URL, "999999"); ok {
			t.Fatal("wrong PIN accepted")
		}
	}
	if _, ok := webSessionHello(t, s.URL, "s_"+strings.Repeat("0", 64)); ok {
		t.Fatal("unknown session accepted")
	}
	if _, ok := webSessionHello(t, s.URL, valid); !ok {
		t.Fatal("invalid session consumed PIN attempt")
	}
	if _, ok := webSessionHello(t, s.URL, "999999"); ok {
		t.Fatal("tenth wrong PIN accepted")
	}
	if _, ok := webSessionHello(t, s.URL, valid); ok {
		t.Fatal("session bypassed PIN lock")
	}
}

func TestWebSessionCannotAuthenticateAsAgent(t *testing.T) {
	r, _ := New(Config{Token: "agent-secret", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	ack, ok := webSessionHello(t, s.URL, "012345")
	if !ok {
		t.Fatal("PIN rejected")
	}
	token := ack["sessionToken"].(string)
	agent := dialTest(t, s.URL, "")
	sendJSON(t, agent, map[string]any{
		"type": "hello", "v": 1, "role": "agent", "token": token,
		"deviceId": "mock-mac", "deviceName": "Test Mac", "agentEpoch": "epoch-a",
		"adapterVersion": "mock-1", "capabilities": map[string]bool{"autoLoad": false},
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var got map[string]any
	if err := wsjson.Read(ctx, agent, &got); err == nil {
		t.Fatalf("Web session authenticated as Agent: %+v", got)
	}
	if _, ok := webSessionHello(t, s.URL, "agent-secret"); ok {
		t.Fatal("Agent token authenticated as Web")
	}
}
