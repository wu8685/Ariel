package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const testOrigin = "http://localhost:5173"

func dialTest(t *testing.T, url, origin string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	opts := &websocket.DialOptions{}
	if origin != "" {
		opts.HTTPHeader = http.Header{"Origin": {origin}}
	}
	c, _, err := websocket.Dial(ctx, strings.Replace(url, "http://", "ws://", 1)+"/ws", opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.CloseNow() })
	return c
}

func sendJSON(t *testing.T, c *websocket.Conn, body any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, c, body); err != nil {
		t.Fatal(err)
	}
}

func readJSON(t *testing.T, c *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var got map[string]any
	if err := wsjson.Read(ctx, c, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func cacheSnapshotEvent(threadID, epoch, stream string, seq int) map[string]any {
	thread := map[string]any{"threadId": threadID, "title": "Cached", "cwd": "/fixture", "updatedAt": "2026-10-25T00:00:00Z", "runtime": "idle", "turns": []any{}, "pendingInteractions": []any{}}
	body, _ := json.Marshal(thread)
	return map[string]any{"type": "event", "v": 1, "event": "thread.cache.snapshot", "deviceId": "mock-mac", "threadId": threadID, "agentEpoch": epoch, "cacheStreamId": stream, "seq": seq, "capturedAt": "2026-10-25T00:00:00Z", "payloadBytes": len(body), "thread": thread}
}

func TestRelayServesHotCacheLocallyAndBroadcastsReplacement(t *testing.T) {
	r, err := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, CacheMaxBytes: 1 << 20, CacheEntryMaxBytes: 1 << 19})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	agent := dialTest(t, s.URL, "")
	sendJSON(t, agent, agentHello())
	readJSON(t, agent)
	sendJSON(t, agent, cacheSnapshotEvent("thread-a", "epoch-a", "cache-a", 1))

	web := dialTest(t, s.URL, testOrigin)
	sendJSON(t, web, webHello())
	readJSON(t, web)
	id := "00000000-0000-4000-8000-000000000081"
	sendJSON(t, web, map[string]any{"type": "request", "v": 1, "requestId": id, "deviceId": "mock-mac", "method": "thread.cache.watch", "params": map[string]any{"threadId": "thread-a"}})
	got := readJSON(t, web)
	data, _ := got["data"].(map[string]any)
	if got["outcome"] != "accepted" || data["freshness"] != "fresh" || data["thread"].(map[string]any)["title"] != "Cached" {
		t.Fatalf("cache watch did not return Relay-local content: %+v", got)
	}
	watchID, _ := data["watchId"].(string)
	probeID := "00000000-0000-4000-8000-000000000089"
	sendJSON(t, web, map[string]any{"type": "request", "v": 1, "requestId": probeID, "deviceId": "mock-mac", "method": "thread.list", "params": map[string]any{"limit": 1}})
	probe := readJSON(t, agent)
	if probe["method"] != "thread.list" {
		t.Fatalf("fresh cache watch unexpectedly reached Agent before probe: %+v", probe)
	}
	relayProbeID := probe["requestId"].(string)
	sendJSON(t, agent, map[string]any{"type": "response", "v": 1, "requestId": relayProbeID, "outcome": "accepted", "data": map[string]any{"threads": []any{}, "cursor": ""}})
	readJSON(t, web)

	sendJSON(t, agent, cacheSnapshotEvent("thread-a", "epoch-a", "cache-a", 2))
	update := readJSON(t, web)
	if update["event"] != "thread.cache.update" || update["watchId"] != watchID || update["seq"].(float64) != 2 {
		t.Fatalf("cache replacement not broadcast: %+v", update)
	}
}

func TestRelayCacheMissPromotesOnceThroughBoundedAgentTouch(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	agent := dialTest(t, s.URL, "")
	sendJSON(t, agent, agentHello())
	readJSON(t, agent)
	web := dialTest(t, s.URL, testOrigin)
	sendJSON(t, web, webHello())
	readJSON(t, web)

	sendJSON(t, web, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000082", "deviceId": "mock-mac", "method": "thread.cache.watch", "params": map[string]any{"threadId": "missing"}})
	watch := readJSON(t, web)
	if watch["data"].(map[string]any)["freshness"] != "missing" {
		t.Fatalf("watch: %+v", watch)
	}
	for _, id := range []string{"00000000-0000-4000-8000-000000000083", "00000000-0000-4000-8000-000000000084"} {
		sendJSON(t, web, map[string]any{"type": "request", "v": 1, "requestId": id, "deviceId": "mock-mac", "method": "thread.cache.promote", "params": map[string]any{"threadId": "missing"}})
		if got := readJSON(t, web); got["outcome"] != "accepted" {
			t.Fatalf("promote: %+v", got)
		}
	}
	touch := readJSON(t, agent)
	if touch["method"] != "thread.cache.touch" || touch["params"].(map[string]any)["threadId"] != "missing" {
		t.Fatalf("touch: %+v", touch)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var duplicate map[string]any
	if err := wsjson.Read(ctx, agent, &duplicate); err == nil {
		t.Fatalf("duplicate promote reached Agent: %+v", duplicate)
	}
}

func TestRelayPreservesTerminalStatusReasonForMissingCache(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	agent := dialTest(t, s.URL, "")
	sendJSON(t, agent, agentHello())
	readJSON(t, agent)
	web := dialTest(t, s.URL, testOrigin)
	sendJSON(t, web, webHello())
	readJSON(t, web)

	sendJSON(t, web, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000085", "deviceId": "mock-mac", "method": "thread.cache.watch", "params": map[string]any{"threadId": "missing-terminal"}})
	watch := readJSON(t, web)
	data := watch["data"].(map[string]any)
	watchID := data["watchId"].(string)
	readJSON(t, agent) // initial bounded cache touch

	sendJSON(t, agent, map[string]any{
		"type": "event", "v": 1, "event": "thread.cache.status",
		"deviceId": "mock-mac", "threadId": "missing-terminal", "agentEpoch": "epoch-a",
		"cacheStreamId": "cache-missing-terminal", "freshness": "error", "reason": "TIMEOUT",
	})
	update := readJSON(t, web)
	if update["event"] != "thread.cache.update" || update["watchId"] != watchID || update["freshness"] != "missing" || update["staleReason"] != "TIMEOUT" {
		t.Fatalf("terminal reason was not preserved for empty cache: %+v", update)
	}
}

func TestRelayRejectedInternalPromotionCanBeRetried(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	agent := dialTest(t, s.URL, "")
	sendJSON(t, agent, agentHello())
	readJSON(t, agent)
	web := dialTest(t, s.URL, testOrigin)
	sendJSON(t, web, webHello())
	readJSON(t, web)

	sendJSON(t, web, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000180", "deviceId": "mock-mac", "method": "thread.cache.watch", "params": map[string]any{"threadId": "retry"}})
	watch := readJSON(t, web)
	touch := readJSON(t, agent)
	sendJSON(t, agent, map[string]any{"type": "response", "v": 1, "requestId": touch["requestId"], "outcome": "rejected", "error": map[string]any{"code": "OVERLOADED", "message": "OVERLOADED"}})
	update := readJSON(t, web)
	if update["event"] != "thread.cache.update" || update["staleReason"] != "OVERLOADED" {
		t.Fatalf("rejected promotion was not surfaced: %+v", update)
	}
	watchID := watch["data"].(map[string]any)["watchId"]
	if watchID == "" {
		t.Fatal("missing watch id")
	}
	sendJSON(t, web, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000181", "deviceId": "mock-mac", "method": "thread.cache.promote", "params": map[string]any{"threadId": "retry"}})
	if got := readJSON(t, web); got["outcome"] != "accepted" || got["data"].(map[string]any)["promoting"] != true {
		t.Fatalf("retry promotion was suppressed: %+v", got)
	}
	if retry := readJSON(t, agent); retry["method"] != "thread.cache.touch" || retry["requestId"] == touch["requestId"] {
		t.Fatalf("retry did not create a fresh internal operation: %+v", retry)
	}
}

func TestRelayCacheFreshnessRequiresLiveFollowerLease(t *testing.T) {
	now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, CacheFreshness: time.Minute})
	r.now = func() time.Time { return now }
	agent := &peer{deviceID: "mock-mac", agentEpoch: "epoch-a"}
	thread := json.RawMessage(`{"threadId":"lease","title":"Lease","cwd":"/fixture","updatedAt":"2026-10-26T00:00:00Z","runtime":"idle","turns":[],"pendingInteractions":[]}`)
	if err := r.cache.put(cacheSnapshot{deviceID: "mock-mac", threadID: "lease", agentEpoch: "epoch-a", streamID: "stream", seq: 1, capturedAt: now, thread: thread, bytes: int64(len(thread))}, now); err != nil {
		t.Fatal(err)
	}
	if got := r.cacheDataLocked(r.cache.entries[cacheKey{"mock-mac", "lease"}], agent); got["freshness"] != "fresh" {
		t.Fatalf("fresh lease: %+v", got)
	} else if _, exposed := got["leaseValidUntil"]; exposed {
		t.Fatalf("Relay-internal follower lease leaked onto the Web wire: %+v", got)
	}
	now = now.Add(time.Minute + time.Nanosecond)
	if got := r.cacheDataLocked(r.cache.entries[cacheKey{"mock-mac", "lease"}], agent); got["freshness"] != "stale" || got["staleReason"] != "follower_lease_expired" {
		t.Fatalf("expired lease: %+v", got)
	}
}

func TestRelayCacheUpdateRemainsCompatibleWithPreviousWebShape(t *testing.T) {
	now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, CacheFreshness: time.Minute})
	r.now = func() time.Time { return now }
	web := &peer{id: "web", cachePending: map[string]map[string]any{}, cacheWake: make(chan struct{}, 1)}
	key := cacheKey{"mock-mac", "legacy"}
	r.cache.watch(key, "watch-legacy", web, now)
	snapshot := cacheFixture("legacy", 8)
	snapshot.deviceID = key.deviceID
	snapshot.agentEpoch = "epoch-a"
	if err := r.cache.put(snapshot, now); err != nil {
		t.Fatal(err)
	}
	r.agents[key.deviceID] = &peer{deviceID: key.deviceID, agentEpoch: "epoch-a"}
	deliveries := r.cacheUpdateMessagesLocked(key)
	if len(deliveries) != 1 {
		t.Fatalf("cache deliveries = %d, want 1", len(deliveries))
	}
	body, err := json.Marshal(deliveries[0].message)
	if err != nil {
		t.Fatal(err)
	}
	var legacy struct {
		Type          string          `json:"type"`
		V             int             `json:"v"`
		Event         string          `json:"event"`
		DeviceID      string          `json:"deviceId"`
		ThreadID      string          `json:"threadId"`
		WatchID       string          `json:"watchId"`
		Freshness     string          `json:"freshness"`
		AgentOnline   bool            `json:"agentOnline"`
		AgentEpoch    string          `json:"agentEpoch"`
		CacheStreamID string          `json:"cacheStreamId"`
		Seq           int             `json:"seq"`
		CapturedAt    string          `json:"capturedAt"`
		Thread        json.RawMessage `json:"thread"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&legacy); err != nil {
		t.Fatalf("current cache update broke the previous Web shape: %v; frame=%s", err, body)
	}
}

func TestRelayDropsCacheEventsFromReplacedAgentTransport(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	old := &peer{deviceID: "mock-mac", agentEpoch: "epoch-a"}
	current := &peer{deviceID: "mock-mac", agentEpoch: "epoch-a"}
	r.agents[old.deviceID] = current

	body, err := json.Marshal(cacheSnapshotEvent("late", "epoch-a", "old-stream", 1))
	if err != nil {
		t.Fatal(err)
	}
	if !r.handleCacheAgentEvent(context.Background(), old, body) {
		t.Fatal("late cache event should be ignored without closing the stale transport twice")
	}
	if cached := r.cache.entries[cacheKey{"mock-mac", "late"}]; cached != nil {
		t.Fatalf("replaced Agent transport mutated Relay cache: %+v", cached)
	}
}

func TestRelayCacheDeliveryQueueKeepsOnlyLatestWatcherUpdate(t *testing.T) {
	web := &peer{cachePending: map[string]map[string]any{}, cacheWake: make(chan struct{}, 1)}
	web.enqueueCacheDelivery("watch-a", map[string]any{"seq": 1})
	web.enqueueCacheDelivery("watch-a", map[string]any{"seq": 2})
	web.cacheSendMu.Lock()
	defer web.cacheSendMu.Unlock()
	if len(web.cachePending) != 1 || web.cachePending["watch-a"]["seq"] != 2 {
		t.Fatalf("cache delivery backlog was not coalesced: %+v", web.cachePending)
	}
	if len(web.cacheWake) != 1 {
		t.Fatalf("cache delivery wakeups were not bounded: %d", len(web.cacheWake))
	}
}

func TestRelayDoesNotRestoreUnwatchedCachedFollowersAfterAgentReconnect(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	first := dialTest(t, s.URL, "")
	sendJSON(t, first, agentHello())
	readJSON(t, first)
	sendJSON(t, first, cacheSnapshotEvent("remembered", "epoch-a", "cache-a", 1))
	first.CloseNow()
	deadline := time.Now().Add(time.Second)
	for {
		r.mu.Lock()
		offline := r.agents["mock-mac"] == nil
		r.mu.Unlock()
		if offline {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first Agent did not disconnect")
		}
		time.Sleep(10 * time.Millisecond)
	}
	second := dialTest(t, s.URL, "")
	hello := agentHello()
	hello["agentEpoch"] = "epoch-b"
	sendJSON(t, second, hello)
	readJSON(t, second)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	var touch map[string]any
	if err := wsjson.Read(ctx, second, &touch); err == nil {
		t.Fatalf("unwatched cache was eagerly restored after reconnect: %+v", touch)
	}
}

func TestDefaultRelayFrameLimitIncludesCacheEnvelopeHeadroom(t *testing.T) {
	r, err := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	if err != nil {
		t.Fatal(err)
	}
	if r.cfg.MaxFrameBytes <= r.cfg.CacheEntryMaxBytes {
		t.Fatalf("frame limit %d must exceed cache body limit %d", r.cfg.MaxFrameBytes, r.cfg.CacheEntryMaxBytes)
	}
}

func TestRelayAuthenticatesRolesAndListsOnlyOnlineDevice(t *testing.T) {
	r, err := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	agent := dialTest(t, s.URL, "")
	sendJSON(t, agent, map[string]any{"type": "hello", "v": 1, "role": "agent", "token": "test-token", "deviceId": "mock-mac", "deviceName": "Test Mac", "agentEpoch": "epoch-a", "adapterVersion": "mock-1", "capabilities": map[string]bool{"autoLoad": false, "codexReady": false}})
	if got := readJSON(t, agent); got["type"] != "hello.ok" || got["relayEpoch"] == "" {
		t.Fatalf("agent hello: %+v", got)
	}
	web := dialTest(t, s.URL, testOrigin)
	sendJSON(t, web, map[string]any{"type": "hello", "v": 1, "role": "web", "token": "012345"})
	if got := readJSON(t, web); got["type"] != "hello.ok" {
		t.Fatalf("web hello: %+v", got)
	}
	id := "00000000-0000-4000-8000-000000000001"
	sendJSON(t, web, map[string]any{"type": "request", "v": 1, "requestId": id, "deviceId": "relay", "method": "device.list", "params": map[string]any{}})
	got := readJSON(t, web)
	if got["type"] != "response" || got["requestId"] != id || got["outcome"] != "accepted" {
		t.Fatalf("device list: %+v", got)
	}
	data, _ := got["data"].(map[string]any)
	devices, _ := data["devices"].([]any)
	if len(devices) != 1 || devices[0].(map[string]any)["deviceId"] != "mock-mac" || devices[0].(map[string]any)["adapterVersion"] != "mock-1" {
		t.Fatalf("devices: %+v", data)
	}
}

func TestRelayRejectsBadOriginTokenAndReplacesAuthenticatedAgent(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, HelloTimeout: 100 * time.Millisecond})
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, strings.Replace(s.URL, "http://", "ws://", 1)+"/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"http://evil.invalid"}}})
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("bad origin accepted: response=%v err=%v", resp, err)
	}
	bad := dialTest(t, s.URL, testOrigin)
	sendJSON(t, bad, map[string]any{"type": "hello", "v": 1, "role": "web", "token": "wrong"})
	readCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	var denied json.RawMessage
	if err := wsjson.Read(readCtx, bad, &denied); err == nil {
		t.Fatal("bad token received a valid reply")
	}
	first := dialTest(t, s.URL, "")
	hello := map[string]any{"type": "hello", "v": 1, "role": "agent", "token": "test-token", "deviceId": "mock-mac", "deviceName": "Mac", "agentEpoch": "epoch-a", "adapterVersion": "mock-1", "capabilities": map[string]bool{"autoLoad": false, "codexReady": false}}
	sendJSON(t, first, hello)
	readJSON(t, first)
	second := dialTest(t, s.URL, "")
	sendJSON(t, second, hello)
	if got := readJSON(t, second); got["type"] != "hello.ok" {
		t.Fatalf("replacement agent was not accepted: %+v", got)
	}
	closedCtx, cancelClosed := context.WithTimeout(context.Background(), time.Second)
	defer cancelClosed()
	if err := wsjson.Read(closedCtx, first, &denied); err == nil {
		t.Fatal("replaced agent connection remained active")
	}
}

func TestRelayRejectsFutureProtocolAgentBeforeItAppearsOnline(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	future := dialTest(t, s.URL, "")
	hello := agentHello()
	hello["v"] = 2
	sendJSON(t, future, hello)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var reply json.RawMessage
	if err := wsjson.Read(ctx, future, &reply); err == nil {
		t.Fatal("future protocol Agent received a successful hello")
	}
	web := dialTest(t, s.URL, testOrigin)
	sendJSON(t, web, webHello())
	readJSON(t, web)
	sendJSON(t, web, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000072", "deviceId": "relay", "method": "device.list", "params": map[string]any{}})
	got := readJSON(t, web)
	data, _ := got["data"].(map[string]any)
	devices, _ := data["devices"].([]any)
	if got["outcome"] != "accepted" || len(devices) != 0 {
		t.Fatalf("future protocol Agent appeared online: %+v", got)
	}
}

func TestRelayRequiresNonemptyTokenAndOriginPolicy(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("relay started without credential")
	}
	if _, err := New(Config{Token: "secret"}); err == nil {
		t.Fatal("relay started without origin allowlist")
	}
}

func TestRelayHeartbeatRemovesUnresponsiveAgent(t *testing.T) {
	r, err := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, HeartbeatInterval: 30 * time.Millisecond, HeartbeatTimeout: 30 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	w := dialTest(t, s.URL, testOrigin)
	sendJSON(t, w, webHello())
	readJSON(t, w)
	a := dialTest(t, s.URL, "")
	sendJSON(t, a, agentHello())
	readJSON(t, a) // From here, no Reader processes the Agent's ping frame.
	if got := readJSON(t, w); got["event"] != "device.status" || got["agentOnline"] != true || got["agentEpoch"] != "epoch-a" {
		t.Fatalf("missing online status: %v", got["event"])
	}
	if got := readJSON(t, w); got["event"] != "device.status" || got["agentOnline"] != false || got["agentEpoch"] != "epoch-a" {
		t.Fatalf("unresponsive Agent remained online: %v", got["event"])
	}
}

func TestRelayStaleAgentCleanupReportsTheCurrentReplacement(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	old := &peer{deviceID: "mock-mac", agentEpoch: "epoch-old", capabilities: map[string]bool{"codexReady": false}}
	replacement := &peer{deviceID: "mock-mac", agentEpoch: "epoch-new", capabilities: map[string]bool{"codexReady": true}}
	r.agents[old.deviceID] = replacement

	status := r.currentDeviceStatus(old.deviceID, old)
	if status["agentOnline"] != true || status["codexReady"] != true || status["agentEpoch"] != "epoch-new" {
		t.Fatalf("stale cleanup described the departed Agent instead of the replacement: %+v", status)
	}
}

func agentHello() map[string]any {
	return map[string]any{"type": "hello", "v": 1, "role": "agent", "token": "test-token", "deviceId": "mock-mac", "deviceName": "Mac", "agentEpoch": "epoch-a", "adapterVersion": "mock-1", "capabilities": map[string]bool{"autoLoad": false, "codexReady": false}}
}

func webHello() map[string]any {
	return map[string]any{"type": "hello", "v": 1, "role": "web", "token": "012345"}
}

func TestRelayBoundsActiveWebSubscriptionsBeforeForwarding(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	a := dialTest(t, s.URL, "")
	sendJSON(t, a, agentHello())
	readJSON(t, a)
	w := dialTest(t, s.URL, testOrigin)
	sendJSON(t, w, webHello())
	readJSON(t, w)
	for i := range 32 {
		id := fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1)
		sendJSON(t, w, map[string]any{"type": "request", "v": 1, "requestId": id, "deviceId": "mock-mac", "method": "thread.subscribe", "params": map[string]any{"threadId": "mock-thread-a"}})
		forwarded := readJSON(t, a)
		sendJSON(t, a, map[string]any{"type": "response", "v": 1, "requestId": forwarded["requestId"], "outcome": "accepted", "data": map[string]any{"subscriptionId": fmt.Sprintf("sub-%d", i), "streamId": fmt.Sprintf("stream-%d", i)}})
		if got := readJSON(t, w); got["outcome"] != "accepted" {
			t.Fatalf("subscribe %d: %v", i, got)
		}
	}
	moreID := "00000000-0000-4000-8000-000000000033"
	sendJSON(t, w, map[string]any{"type": "request", "v": 1, "requestId": moreID, "deviceId": "mock-mac", "method": "thread.subscribe", "params": map[string]any{"threadId": "mock-thread-a"}})
	if got := readJSON(t, w); got["outcome"] != "rejected" || got["error"].(map[string]any)["code"] != "OVERLOADED" {
		t.Fatalf("extra subscription was not rejected locally: %v", got)
	}
	unsubID := "00000000-0000-4000-8000-000000000034"
	sendJSON(t, w, map[string]any{"type": "request", "v": 1, "requestId": unsubID, "deviceId": "mock-mac", "method": "thread.unsubscribe", "params": map[string]any{"subscriptionId": "sub-0"}})
	forwarded := readJSON(t, a)
	if forwarded["method"] != "thread.unsubscribe" {
		t.Fatalf("overflow request reached Agent: %v", forwarded["method"])
	}
	sendJSON(t, a, map[string]any{"type": "response", "v": 1, "requestId": forwarded["requestId"], "outcome": "accepted", "data": map[string]any{"unsubscribed": true}})
	if got := readJSON(t, w); got["outcome"] != "accepted" {
		t.Fatalf("unsubscribe: %v", got)
	}
	sendJSON(t, w, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000035", "deviceId": "mock-mac", "method": "thread.subscribe", "params": map[string]any{"threadId": "mock-thread-a"}})
	if got := readJSON(t, a); got["method"] != "thread.subscribe" {
		t.Fatalf("capacity did not recover: %v", got)
	}
}

func TestRelayReleasesAgentSubscriptionWhenWebDisconnects(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	a := dialTest(t, s.URL, "")
	sendJSON(t, a, agentHello())
	readJSON(t, a)
	w := dialTest(t, s.URL, testOrigin)
	sendJSON(t, w, webHello())
	readJSON(t, w)
	sendJSON(t, w, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000041", "deviceId": "mock-mac", "method": "thread.subscribe", "params": map[string]any{"threadId": "mock-thread-a"}})
	forwarded := readJSON(t, a)
	sendJSON(t, a, map[string]any{"type": "response", "v": 1, "requestId": forwarded["requestId"], "outcome": "accepted", "data": map[string]any{"subscriptionId": "orphan-sub", "streamId": "orphan-stream"}})
	if got := readJSON(t, w); got["outcome"] != "accepted" {
		t.Fatalf("subscribe: %v", got)
	}
	w.CloseNow()
	cleanup := readJSON(t, a)
	if cleanup["method"] != "thread.unsubscribe" || cleanup["params"].(map[string]any)["subscriptionId"] != "orphan-sub" {
		t.Fatalf("missing Agent cleanup: %v", cleanup)
	}
}

func TestRelayReleasesLateAcceptedSubscriptionAfterWebDisconnect(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	a := dialTest(t, s.URL, "")
	sendJSON(t, a, agentHello())
	readJSON(t, a)
	w := dialTest(t, s.URL, testOrigin)
	sendJSON(t, w, webHello())
	readJSON(t, w)
	sendJSON(t, w, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000042", "deviceId": "mock-mac", "method": "thread.subscribe", "params": map[string]any{"threadId": "mock-thread-a"}})
	forwarded := readJSON(t, a)
	w.CloseNow()
	sendJSON(t, a, map[string]any{"type": "response", "v": 1, "requestId": forwarded["requestId"], "outcome": "accepted", "data": map[string]any{"subscriptionId": "late-sub", "streamId": "late-stream"}})
	cleanup := readJSON(t, a)
	if cleanup["method"] != "thread.unsubscribe" || cleanup["params"].(map[string]any)["subscriptionId"] != "late-sub" {
		t.Fatalf("late accepted subscription not cleaned: %v", cleanup)
	}
}

func TestRelayReleasesLateAcceptedSubscriptionAfterTimeout(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, SubscriptionTimeout: 60 * time.Millisecond})
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	a := dialTest(t, s.URL, "")
	sendJSON(t, a, agentHello())
	readJSON(t, a)
	w := dialTest(t, s.URL, testOrigin)
	sendJSON(t, w, webHello())
	readJSON(t, w)
	sendJSON(t, w, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000043", "deviceId": "mock-mac", "method": "thread.subscribe", "params": map[string]any{"threadId": "mock-thread-a"}})
	forwarded := readJSON(t, a)
	if got := readJSON(t, w); got["outcome"] != "rejected" || got["error"].(map[string]any)["code"] != "TIMEOUT" {
		t.Fatalf("subscription did not time out: %v", got)
	}
	sendJSON(t, a, map[string]any{"type": "response", "v": 1, "requestId": forwarded["requestId"], "outcome": "accepted", "data": map[string]any{"subscriptionId": "timeout-sub", "streamId": "timeout-stream"}})
	cleanup := readJSON(t, a)
	if cleanup["method"] != "thread.unsubscribe" || cleanup["params"].(map[string]any)["subscriptionId"] != "timeout-sub" {
		t.Fatalf("timed-out accepted subscription not cleaned: %v", cleanup)
	}
}

func TestRelayDefaultSubscriptionBudgetLeavesTransportMargin(t *testing.T) {
	r, err := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	if err != nil {
		t.Fatal(err)
	}
	if r.cfg.SubscriptionTimeout != 50*time.Second {
		t.Fatalf("subscription timeout = %s, want 50s", r.cfg.SubscriptionTimeout)
	}
}

func TestRelayAllowsNativeSearchToUseTheReadBudget(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, RequestTimeout: 30 * time.Millisecond, ReadTimeout: 300 * time.Millisecond})
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	a := dialTest(t, s.URL, "")
	sendJSON(t, a, agentHello())
	readJSON(t, a)
	w := dialTest(t, s.URL, testOrigin)
	sendJSON(t, w, webHello())
	readJSON(t, w)
	id := "00000000-0000-4000-8000-000000000099"
	sendJSON(t, w, map[string]any{"type": "request", "v": 1, "requestId": id, "deviceId": "mock-mac", "method": "thread.list", "params": map[string]any{"searchTerm": "Ariel", "limit": 50}})
	forwarded := readJSON(t, a)
	time.Sleep(80 * time.Millisecond)
	sendJSON(t, a, map[string]any{"type": "response", "v": 1, "requestId": forwarded["requestId"], "outcome": "accepted", "data": map[string]any{"threads": []any{}, "nextCursor": ""}})
	if got := readJSON(t, w); got["outcome"] != "accepted" {
		t.Fatalf("search timed out as ordinary list: %v", got)
	}
}

func TestRelayAllowsNativeListToUseTheReadBudget(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, RequestTimeout: 30 * time.Millisecond, ReadTimeout: 300 * time.Millisecond})
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	a := dialTest(t, s.URL, "")
	sendJSON(t, a, agentHello())
	readJSON(t, a)
	w := dialTest(t, s.URL, testOrigin)
	sendJSON(t, w, webHello())
	readJSON(t, w)
	id := "00000000-0000-4000-8000-000000000100"
	sendJSON(t, w, map[string]any{"type": "request", "v": 1, "requestId": id, "deviceId": "mock-mac", "method": "thread.list", "params": map[string]any{"limit": 50}})
	forwarded := readJSON(t, a)
	time.Sleep(80 * time.Millisecond)
	sendJSON(t, a, map[string]any{"type": "response", "v": 1, "requestId": forwarded["requestId"], "outcome": "accepted", "data": map[string]any{"threads": []any{}, "nextCursor": ""}})
	if got := readJSON(t, w); got["outcome"] != "accepted" {
		t.Fatalf("ordinary list timed out before the native read budget: %v", got)
	}
}

func TestRelayCorrelatesSameWebRequestIDAcrossTwoConnections(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	agent := dialTest(t, s.URL, "")
	sendJSON(t, agent, agentHello())
	readJSON(t, agent)
	w1, w2 := dialTest(t, s.URL, testOrigin), dialTest(t, s.URL, testOrigin)
	for _, w := range []*websocket.Conn{w1, w2} {
		sendJSON(t, w, webHello())
		readJSON(t, w)
	}
	webID := "00000000-0000-4000-8000-000000000001"
	for _, w := range []*websocket.Conn{w1, w2} {
		sendJSON(t, w, map[string]any{"type": "request", "v": 1, "requestId": webID, "deviceId": "mock-mac", "method": "thread.list", "params": map[string]any{"limit": 20}})
	}
	a, b := readJSON(t, agent), readJSON(t, agent)
	if a["requestId"] == b["requestId"] || a["requestId"] == webID || a["method"] != "thread.list" || b["method"] != "thread.list" {
		t.Fatalf("relay IDs not isolated: %+v %+v", a, b)
	}
	for _, req := range []map[string]any{b, a} {
		sendJSON(t, agent, map[string]any{"type": "response", "v": 1, "requestId": req["requestId"], "outcome": "accepted", "data": map[string]any{"threads": []any{}, "nextCursor": nil}})
	}
	for _, w := range []*websocket.Conn{w1, w2} {
		got := readJSON(t, w)
		if got["type"] != "response" || got["requestId"] != webID || got["outcome"] != "accepted" {
			t.Fatalf("routed response: %+v", got)
		}
	}
}

func TestRelayRejectsOfflineAndClassifiesReadTimeoutAsRetryable(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, RequestTimeout: 80 * time.Millisecond, ReadTimeout: 80 * time.Millisecond})
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	w := dialTest(t, s.URL, testOrigin)
	sendJSON(t, w, webHello())
	readJSON(t, w)
	id := "00000000-0000-4000-8000-000000000002"
	req := map[string]any{"type": "request", "v": 1, "requestId": id, "deviceId": "mock-mac", "method": "thread.list", "params": map[string]any{}}
	sendJSON(t, w, req)
	if got := readJSON(t, w); got["outcome"] != "rejected" || got["error"].(map[string]any)["code"] != "DEVICE_OFFLINE" {
		t.Fatalf("offline route: %+v", got)
	}
	agent := dialTest(t, s.URL, "")
	sendJSON(t, agent, agentHello())
	readJSON(t, agent)
	if got := readJSON(t, w); got["event"] != "device.status" {
		t.Fatalf("missing online event: %+v", got)
	}
	sendJSON(t, w, req)
	readJSON(t, agent)
	got := readJSON(t, w)
	if got["outcome"] != "rejected" || got["error"].(map[string]any)["code"] != "TIMEOUT" {
		t.Fatalf("forwarded timeout misclassified: %+v", got)
	}
}

func TestRelayStillClassifiesMutationTimeoutAsUnknown(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, RequestTimeout: 20 * time.Millisecond, MutationTimeout: 40 * time.Millisecond})
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	agent := dialTest(t, s.URL, "")
	sendJSON(t, agent, agentHello())
	readJSON(t, agent)
	web := dialTest(t, s.URL, testOrigin)
	sendJSON(t, web, webHello())
	readJSON(t, web)
	tests := []struct {
		id, method string
		params     map[string]any
	}{
		{"00000000-0000-4000-8000-000000000072", "turn.start", map[string]any{"threadId": "mock-thread-a", "clientMessageId": "00000000-0000-4000-8000-000000000073", "text": "do not replay"}},
		{"00000000-0000-4000-8000-000000000074", "thread.name.update", map[string]any{"threadId": "mock-thread-a", "name": "Renamed"}},
		{"00000000-0000-4000-8000-000000000075", "thread.pin.update", map[string]any{"threadId": "mock-thread-a", "pinned": true}},
	}
	for _, test := range tests {
		sendJSON(t, web, map[string]any{"type": "request", "v": 1, "requestId": test.id, "deviceId": "mock-mac", "method": test.method, "params": test.params})
		readJSON(t, agent)
		got := readJSON(t, web)
		if got["outcome"] != "unknown" || got["error"].(map[string]any)["code"] != "OUTCOME_UNKNOWN" {
			t.Fatalf("%s timeout lost unknown semantics: %+v", test.method, got)
		}
	}
}

func TestRelayDoesNotReplayForwardedMutationAfterAgentDisconnect(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	agent := dialTest(t, s.URL, "")
	sendJSON(t, agent, agentHello())
	readJSON(t, agent)
	web := dialTest(t, s.URL, testOrigin)
	sendJSON(t, web, webHello())
	readJSON(t, web)
	id := "00000000-0000-4000-8000-000000000061"
	sendJSON(t, web, map[string]any{"type": "request", "v": 1, "requestId": id, "deviceId": "mock-mac", "method": "turn.start", "params": map[string]any{"threadId": "mock-thread-a", "clientMessageId": "00000000-0000-4000-8000-000000000062", "text": "isolated fixture"}})
	forwarded := readJSON(t, agent)
	if forwarded["method"] != "turn.start" {
		t.Fatalf("mutation not forwarded once: %+v", forwarded)
	}
	// The owner may have accepted the mutation before this transport failed.
	agent.CloseNow()
	if got := readJSON(t, web); got["requestId"] != id || got["outcome"] != "unknown" || got["error"].(map[string]any)["code"] != "OUTCOME_UNKNOWN" {
		t.Fatalf("disconnected mutation was not marked unknown: %+v", got)
	}
	if got := readJSON(t, web); got["event"] != "device.status" || got["agentOnline"] != false {
		t.Fatalf("missing offline status: %+v", got)
	}
	replacement := dialTest(t, s.URL, "")
	sendJSON(t, replacement, agentHello())
	readJSON(t, replacement)
	if got := readJSON(t, web); got["event"] != "device.status" || got["agentOnline"] != true {
		t.Fatalf("missing replacement online status: %+v", got)
	}
	r.mu.Lock()
	remaining := len(r.routes)
	r.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("mutation route survived agent replacement: %d", remaining)
	}
}

func TestRelayClassifiesInterruptedSubscriptionAsRecoverable(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	agent := dialTest(t, s.URL, "")
	sendJSON(t, agent, agentHello())
	readJSON(t, agent)
	web := dialTest(t, s.URL, testOrigin)
	sendJSON(t, web, webHello())
	readJSON(t, web)
	id := "00000000-0000-4000-8000-000000000063"
	sendJSON(t, web, map[string]any{"type": "request", "v": 1, "requestId": id, "deviceId": "mock-mac", "method": "thread.subscribe", "params": map[string]any{"threadId": "mock-thread-a"}})
	readJSON(t, agent)
	agent.CloseNow()
	if got := readJSON(t, web); got["requestId"] != id || got["outcome"] != "rejected" || got["error"].(map[string]any)["code"] != "RESYNC_REQUIRED" {
		t.Fatalf("interrupted subscription was not recoverable: %+v", got)
	}
}

func TestRelayAllowsBoundedExtraTimeForAutoLoadSubscription(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, RequestTimeout: 30 * time.Millisecond, SubscriptionTimeout: 300 * time.Millisecond})
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	a := dialTest(t, s.URL, "")
	sendJSON(t, a, agentHello())
	readJSON(t, a)
	w := dialTest(t, s.URL, testOrigin)
	sendJSON(t, w, webHello())
	readJSON(t, w)
	id := "00000000-0000-4000-8000-000000000022"
	sendJSON(t, w, map[string]any{"type": "request", "v": 1, "requestId": id, "deviceId": "mock-mac", "method": "thread.subscribe", "params": map[string]any{"threadId": "mock-thread-a"}})
	forwarded := readJSON(t, a)
	time.Sleep(80 * time.Millisecond)
	sendJSON(t, a, map[string]any{"type": "response", "v": 1, "requestId": forwarded["requestId"], "outcome": "accepted", "data": map[string]any{"subscriptionId": "sub-late", "streamId": "stream-late"}})
	got := readJSON(t, w)
	if got["outcome"] != "accepted" {
		t.Fatalf("auto-load timed out early: %v", got)
	}
}

func TestRelayAllowsBoundedExtraTimeForForwardedMutation(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, RequestTimeout: 30 * time.Millisecond, MutationTimeout: 300 * time.Millisecond})
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	a := dialTest(t, s.URL, "")
	sendJSON(t, a, agentHello())
	readJSON(t, a)
	w := dialTest(t, s.URL, testOrigin)
	sendJSON(t, w, webHello())
	readJSON(t, w)
	id := "00000000-0000-4000-8000-000000000023"
	sendJSON(t, w, map[string]any{"type": "request", "v": 1, "requestId": id, "deviceId": "mock-mac", "method": "thread.create", "params": map[string]any{"cwd": "/mock/workspace"}})
	forwarded := readJSON(t, a)
	time.Sleep(80 * time.Millisecond)
	sendJSON(t, a, map[string]any{"type": "response", "v": 1, "requestId": forwarded["requestId"], "outcome": "accepted", "data": map[string]any{"thread": mockThread()}})
	got := readJSON(t, w)
	if got["outcome"] != "accepted" {
		t.Fatalf("mutation timed out early: %v", got)
	}
}

func mockThread() map[string]any {
	return map[string]any{"threadId": "mock-thread-a", "title": "Draft", "cwd": "/mock/workspace", "updatedAt": "2026-10-04T00:00:00Z", "runtime": "idle", "turns": []any{}, "pendingInteractions": []any{}}
}

// Opt-in because it deliberately fills a loopback TCP receive buffer and waits
// for the production 15-second WebSocket write deadline. It exercises actual
// socket backpressure, not a mocked writer.
func TestRelaySlowWebSocketDoesNotRetainSubscription(t *testing.T) {
	if os.Getenv("ARIEL_TEST_SLOW_SOCKET") != "1" {
		t.Skip("set ARIEL_TEST_SLOW_SOCKET=1 for loopback backpressure test")
	}
	r, err := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, HeartbeatInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	agent := dialTest(t, s.URL, "")
	sendJSON(t, agent, agentHello())
	readJSON(t, agent)
	slow := dialTest(t, s.URL, testOrigin)
	fast := dialTest(t, s.URL, testOrigin)
	for _, web := range []*websocket.Conn{slow, fast} {
		sendJSON(t, web, webHello())
		readJSON(t, web)
	}
	for i, target := range []struct {
		web *websocket.Conn
		sub string
	}{{slow, "slow-sub"}, {fast, "fast-sub"}} {
		requestID := fmt.Sprintf("00000000-0000-4000-8000-%012d", i+71)
		sendJSON(t, target.web, map[string]any{"type": "request", "v": 1, "requestId": requestID, "deviceId": "mock-mac", "method": "thread.subscribe", "params": map[string]string{"threadId": "mock-thread-a"}})
		forwarded := readJSON(t, agent)
		sendJSON(t, agent, map[string]any{"type": "response", "v": 1, "requestId": forwarded["requestId"], "outcome": "accepted", "data": map[string]string{"subscriptionId": target.sub, "streamId": "stream-" + target.sub}})
		if got := readJSON(t, target.web); got["outcome"] != "accepted" {
			t.Fatalf("subscribe %d: %v", i, got["outcome"])
		}
	}
	large := mockThread()
	large["turns"] = []any{map[string]any{"turnId": "large-turn", "status": "inProgress", "items": []any{map[string]any{"itemId": "large-item", "role": "assistant", "text": strings.Repeat("x", (8<<20)-4096)}}}}
	sendJSON(t, agent, map[string]any{"type": "event", "v": 1, "event": "thread.snapshot", "deviceId": "mock-mac", "threadId": "mock-thread-a", "subscriptionId": "slow-sub", "streamId": "stream-slow-sub", "seq": 1, "thread": large})
	deadline := time.Now().Add(20 * time.Second)
	for {
		r.mu.Lock()
		remainingWebs := len(r.webs)
		_, slowSubscribed := r.subs[r.agents["mock-mac"]]["slow-sub"]
		_, fastSubscribed := r.subs[r.agents["mock-mac"]]["fast-sub"]
		r.mu.Unlock()
		if remainingWebs == 1 && !slowSubscribed && fastSubscribed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("slow consumer retained: webs=%d slow=%t fast=%t", remainingWebs, slowSubscribed, fastSubscribed)
		}
		time.Sleep(25 * time.Millisecond)
	}
	sendJSON(t, agent, map[string]any{"type": "event", "v": 1, "event": "thread.snapshot", "deviceId": "mock-mac", "threadId": "mock-thread-a", "subscriptionId": "fast-sub", "streamId": "stream-fast-sub", "seq": 1, "thread": mockThread()})
	if got := readJSON(t, fast); got["event"] != "thread.snapshot" || got["subscriptionId"] != "fast-sub" {
		t.Fatalf("fast subscriber lost update after slow peer closed: event=%v sub=%v", got["event"], got["subscriptionId"])
	}
}

func TestRelaySubscriptionResponsePrecedesSnapshotAndIsIsolated(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	agent := dialTest(t, s.URL, "")
	sendJSON(t, agent, agentHello())
	readJSON(t, agent)
	w1, w2 := dialTest(t, s.URL, testOrigin), dialTest(t, s.URL, testOrigin)
	for _, w := range []*websocket.Conn{w1, w2} {
		sendJSON(t, w, webHello())
		readJSON(t, w)
	}
	for i, w := range []*websocket.Conn{w1, w2} {
		id := "00000000-0000-4000-8000-00000000000" + string(rune('3'+i))
		sendJSON(t, w, map[string]any{"type": "request", "v": 1, "requestId": id, "deviceId": "mock-mac", "method": "thread.subscribe", "params": map[string]any{"threadId": "mock-thread-a"}})
		forwarded := readJSON(t, agent)
		sub := "sub-one"
		if i == 1 {
			sub = "sub-two"
		}
		sendJSON(t, agent, map[string]any{"type": "response", "v": 1, "requestId": forwarded["requestId"], "outcome": "accepted", "data": map[string]any{"subscriptionId": sub, "streamId": "stream-" + sub}})
		sendJSON(t, agent, map[string]any{"type": "event", "v": 1, "event": "thread.snapshot", "deviceId": "mock-mac", "threadId": "mock-thread-a", "subscriptionId": sub, "streamId": "stream-" + sub, "seq": 1, "thread": mockThread()})
		if got := readJSON(t, w); got["type"] != "response" || got["requestId"] != id {
			t.Fatalf("snapshot overtook subscribe reply: %+v", got)
		}
		if got := readJSON(t, w); got["event"] != "thread.snapshot" || got["subscriptionId"] != sub {
			t.Fatalf("snapshot routed incorrectly: %+v", got)
		}
	}
	sendJSON(t, agent, map[string]any{"type": "event", "v": 1, "event": "thread.update", "deviceId": "mock-mac", "threadId": "mock-thread-a", "subscriptionId": "sub-two", "streamId": "stream-sub-two", "baseSeq": 1, "seq": 2, "thread": mockThread()})
	if got := readJSON(t, w2); got["event"] != "thread.update" || got["subscriptionId"] != "sub-two" {
		t.Fatalf("update not routed to second web: %+v", got)
	}
	// First subscriber can unsubscribe without affecting the second.
	sendJSON(t, w1, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000005", "deviceId": "mock-mac", "method": "thread.unsubscribe", "params": map[string]any{"subscriptionId": "sub-one"}})
	forwarded := readJSON(t, agent)
	sendJSON(t, agent, map[string]any{"type": "response", "v": 1, "requestId": forwarded["requestId"], "outcome": "accepted", "data": map[string]any{"unsubscribed": true}})
	if got := readJSON(t, w1); got["outcome"] != "accepted" {
		t.Fatalf("unsubscribe: %+v", got)
	}
	sendJSON(t, agent, map[string]any{"type": "event", "v": 1, "event": "thread.update", "deviceId": "mock-mac", "threadId": "mock-thread-a", "subscriptionId": "sub-two", "streamId": "stream-sub-two", "baseSeq": 2, "seq": 3, "thread": mockThread()})
	if got := readJSON(t, w2); got["seq"] != float64(3) {
		t.Fatalf("second subscription lost: %+v", got)
	}
	sendJSON(t, agent, map[string]any{"type": "event", "v": 1, "event": "thread.error", "deviceId": "mock-mac", "threadId": "mock-thread-a", "subscriptionId": "sub-two", "streamId": "stream-sub-two", "code": "HISTORY_TOO_LARGE"})
	if got := readJSON(t, w2); got["event"] != "thread.error" || got["code"] != "HISTORY_TOO_LARGE" {
		t.Fatalf("stream error not routed: event=%v code=%v", got["event"], got["code"])
	}
}
