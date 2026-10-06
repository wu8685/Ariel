package relay

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func authenticatedPairingWeb(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	web := dialTest(t, url, testOrigin)
	sendJSON(t, web, map[string]any{"type": "hello", "v": 1, "role": "web", "token": "012345"})
	if got := readJSON(t, web); got["type"] != "hello.ok" {
		t.Fatalf("web hello: %+v", got)
	}
	return web
}

func createPairing(t *testing.T, web *websocket.Conn) (string, map[string]any) {
	t.Helper()
	id := newRequestID()
	sendJSON(t, web, map[string]any{"type": "request", "v": 1, "requestId": id, "deviceId": "relay", "method": "auth.pair.create", "params": map[string]any{}})
	response := readJSON(t, web)
	if response["requestId"] != id || response["outcome"] != "accepted" {
		t.Fatalf("pair create: %+v", response)
	}
	data, _ := response["data"].(map[string]any)
	credential, _ := data["credential"].(string)
	if len(credential) != 66 || !strings.HasPrefix(credential, "p_") || data["expiresAt"] == "" {
		t.Fatalf("pair data: %+v", data)
	}
	return credential, data
}

func tryPairingSession(url, token string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, strings.Replace(url, "http://", "ws://", 1)+"/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {testOrigin}}})
	if err != nil {
		return false
	}
	defer c.CloseNow()
	if wsjson.Write(ctx, c, map[string]any{"type": "hello", "v": 1, "role": "web", "token": token}) != nil {
		return false
	}
	var got map[string]any
	return wsjson.Read(ctx, c, &got) == nil && got["type"] == "hello.ok"
}

func TestPairingInvitationIsHashOnlySingleUseAndNotifiesCreator(t *testing.T) {
	r, _ := New(Config{Token: "agent-secret", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	creator := authenticatedPairingWeb(t, s.URL)
	credential, _ := createPairing(t, creator)
	key := sha256.Sum256([]byte(credential))
	r.mu.Lock()
	_, storedByHash := r.pairings[key]
	count := len(r.pairings)
	r.mu.Unlock()
	if !storedByHash || count != 1 {
		t.Fatalf("pairing was not stored by digest: stored=%v count=%d", storedByHash, count)
	}
	ack, ok := webSessionHello(t, s.URL, credential)
	if !ok || !strings.HasPrefix(ack["sessionToken"].(string), "s_") {
		t.Fatalf("pairing was not exchanged for a web session: %+v", ack)
	}
	if event := readJSON(t, creator); event["event"] != "auth.pair.consumed" || len(event) != 3 {
		t.Fatalf("creator event leaked data or was missing: %+v", event)
	}
	if _, ok := webSessionHello(t, s.URL, credential); ok {
		t.Fatal("pairing credential was replayed")
	}
}

func TestPairingRequiresWebConfirmationTokenAndCannotAuthenticateAgent(t *testing.T) {
	r, _ := New(Config{Token: "agent-secret", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	creator := authenticatedPairingWeb(t, s.URL)
	credential, _ := createPairing(t, creator)
	agent := dialTest(t, s.URL, "")
	sendJSON(t, agent, map[string]any{
		"type": "hello", "v": 1, "role": "agent", "token": credential,
		"deviceId": "bad", "deviceName": "Bad", "agentEpoch": "epoch", "adapterVersion": "test",
		"capabilities": map[string]bool{"autoLoad": false, "codexReady": false},
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var got map[string]any
	if err := wsjson.Read(ctx, agent, &got); err == nil {
		t.Fatalf("pairing credential authenticated an Agent: %+v", got)
	}
	if _, ok := webSessionHello(t, s.URL, credential); !ok {
		t.Fatal("Agent rejection consumed the Web pairing credential")
	}
}

func TestPairingExpiresCancelsAndReplacementRevokesPrevious(t *testing.T) {
	r, _ := New(Config{Token: "agent-secret", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	var seconds atomic.Int64
	seconds.Store(1_000_000)
	r.now = func() time.Time { return time.Unix(seconds.Load(), 0) }
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	creator := authenticatedPairingWeb(t, s.URL)
	old, _ := createPairing(t, creator)
	replacement, _ := createPairing(t, creator)
	if _, ok := webSessionHello(t, s.URL, old); ok {
		t.Fatal("replaced pairing remained valid")
	}
	id := newRequestID()
	sendJSON(t, creator, map[string]any{"type": "request", "v": 1, "requestId": id, "deviceId": "relay", "method": "auth.pair.cancel", "params": map[string]any{"credential": replacement}})
	if got := readJSON(t, creator); got["outcome"] != "accepted" {
		t.Fatalf("cancel failed: %+v", got)
	}
	if _, ok := webSessionHello(t, s.URL, replacement); ok {
		t.Fatal("cancelled pairing remained valid")
	}
	expiring, _ := createPairing(t, creator)
	seconds.Add(int64((webPairingLifetime + time.Second).Seconds()))
	if _, ok := webSessionHello(t, s.URL, expiring); ok {
		t.Fatal("expired pairing remained valid")
	}
}

func TestInvalidPairingDoesNotConsumePINAttemptsAndCreatorDisconnectRevokes(t *testing.T) {
	r, _ := New(Config{Token: "agent-secret", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	for i := 0; i < 12; i++ {
		if _, ok := webSessionHello(t, s.URL, "p_"+strings.Repeat(string(rune('a'+i)), 64)); ok {
			t.Fatal("unknown pairing accepted")
		}
	}
	if _, ok := webSessionHello(t, s.URL, "012345"); !ok {
		t.Fatal("invalid pairings consumed PIN attempts")
	}
	creator := authenticatedPairingWeb(t, s.URL)
	credential, _ := createPairing(t, creator)
	creator.CloseNow()
	deadline := time.Now().Add(time.Second)
	for {
		r.mu.Lock()
		remaining := len(r.pairings)
		r.mu.Unlock()
		if remaining == 0 || time.Now().After(deadline) {
			if remaining != 0 {
				t.Fatal("creator disconnect did not revoke pairing")
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, ok := webSessionHello(t, s.URL, credential); ok {
		t.Fatal("pairing survived creator disconnect")
	}
}

func TestPairingCapacityAndConcurrentConsumption(t *testing.T) {
	r, _ := New(Config{Token: "agent-secret", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	creators := make([]*websocket.Conn, 0, maxWebPairings+1)
	for i := 0; i < maxWebPairings; i++ {
		creator := authenticatedPairingWeb(t, s.URL)
		creators = append(creators, creator)
		createPairing(t, creator)
	}
	overflow := authenticatedPairingWeb(t, s.URL)
	id := newRequestID()
	sendJSON(t, overflow, map[string]any{"type": "request", "v": 1, "requestId": id, "deviceId": "relay", "method": "auth.pair.create", "params": map[string]any{}})
	if got := readJSON(t, overflow); got["outcome"] != "rejected" || got["error"].(map[string]any)["code"] != "OVERLOADED" {
		t.Fatalf("capacity overflow was not rejected: %+v", got)
	}
	creators[0].CloseNow()
	deadline := time.Now().Add(time.Second)
	for {
		r.mu.Lock()
		remaining := len(r.pairings)
		r.mu.Unlock()
		if remaining < maxWebPairings || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	credential, _ := createPairing(t, overflow)
	var successes atomic.Int64
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if tryPairingSession(s.URL, credential) {
				successes.Add(1)
			}
		}()
	}
	wait.Wait()
	if successes.Load() != 1 {
		t.Fatalf("concurrent pairing successes = %d", successes.Load())
	}
}
