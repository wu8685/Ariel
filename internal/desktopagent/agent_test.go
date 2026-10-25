package desktopagent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/wu8685/Ariel/internal/relay"
	"github.com/wu8685/Ariel/internal/wsheartbeat"
)

func TestProductionThreadListTimeoutBudgets(t *testing.T) {
	if threadListAttemptTimeout != 15*time.Second {
		t.Fatalf("thread/list attempt timeout = %s, want 15s", threadListAttemptTimeout)
	}
	if threadListRequestTimeout != 30*time.Second {
		t.Fatalf("thread/list request timeout = %s, want 30s", threadListRequestTimeout)
	}
	if 2*threadListAttemptTimeout != threadListRequestTimeout {
		t.Fatalf("two attempts = %s, request budget = %s", 2*threadListAttemptTimeout, threadListRequestTimeout)
	}
}

func TestProductionSubscriptionLoadBudget(t *testing.T) {
	if threadSubscribeLoadTimeout != 45*time.Second {
		t.Fatalf("thread subscribe load timeout = %s, want 45s", threadSubscribeLoadTimeout)
	}
}

func TestCacheSnapshotWriteCannotConsumeHeartbeatDeadline(t *testing.T) {
	if cacheSnapshotWriteTimeout >= wsheartbeat.DefaultTimeout {
		t.Fatalf("cache snapshot write timeout %s must stay below heartbeat timeout %s", cacheSnapshotWriteTimeout, wsheartbeat.DefaultTimeout)
	}
}

func TestCacheSnapshotMailboxKeepsOnlyLatestPerThread(t *testing.T) {
	a := &agent{cachePending: map[string]cacheOutbound{}, cacheWake: make(chan struct{}, 1)}
	a.enqueueCacheSnapshot("thread", "stream", 1, map[string]any{"revision": 1})
	a.enqueueCacheSnapshot("thread", "stream", 2, map[string]any{"revision": 2})
	if len(a.cachePending) != 1 || a.cachePending["thread"].seq != 2 {
		t.Fatalf("cache mailbox did not coalesce to latest snapshot: %+v", a.cachePending)
	}
}

func TestRestartRecoveryGuardRefreshesSilentInProgressFollowerOnlyOnce(t *testing.T) {
	live := &refreshableFakeLive{fakeLive: &fakeLive{busy: true, updates: make(chan struct{}, 2)}}
	service := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) { return live, nil })
	defer service.Close()
	_, _, _, activate, err := service.Subscribe(context.Background(), "thread", func(map[string]any) {})
	if err != nil {
		t.Fatal(err)
	}
	activate()
	state := &cacheSubscription{runtime: "inProgress"}
	a := &agent{
		service: service, cacheSubs: map[string]*cacheSubscription{"thread": state},
		cacheRecoveryGuardDelay: 20 * time.Millisecond,
	}
	a.armCacheRecoveryGuard("thread", state)
	deadline := time.After(time.Second)
	for live.refreshCalls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("silent in-progress follower was not refreshed")
		case <-time.After(5 * time.Millisecond):
		}
	}
	time.Sleep(50 * time.Millisecond)
	if got := live.refreshCalls.Load(); got != 1 {
		t.Fatalf("recovery guard refreshed %d times, want exactly once", got)
	}
}

func TestRestartRecoveryGuardCancelsWhenFollowerBecomesTerminal(t *testing.T) {
	live := &refreshableFakeLive{fakeLive: &fakeLive{busy: true, updates: make(chan struct{}, 2)}}
	service := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) { return live, nil })
	defer service.Close()
	_, _, _, activate, err := service.Subscribe(context.Background(), "thread", func(map[string]any) {})
	if err != nil {
		t.Fatal(err)
	}
	activate()
	state := &cacheSubscription{runtime: "inProgress"}
	a := &agent{
		service: service, cacheSubs: map[string]*cacheSubscription{"thread": state},
		cacheRecoveryGuardDelay: 40 * time.Millisecond,
	}
	a.armCacheRecoveryGuard("thread", state)
	a.observeCacheRecoveryState("thread", state, "idle")
	time.Sleep(80 * time.Millisecond)
	if got := live.refreshCalls.Load(); got != 0 {
		t.Fatalf("terminal follower still triggered %d recovery refreshes", got)
	}
}

func TestRestartRecoveryGuardResetsQuietWindowOnFollowerUpdate(t *testing.T) {
	live := &refreshableFakeLive{fakeLive: &fakeLive{busy: true, updates: make(chan struct{}, 2)}}
	service := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) { return live, nil })
	defer service.Close()
	_, _, _, activate, err := service.Subscribe(context.Background(), "thread", func(map[string]any) {})
	if err != nil {
		t.Fatal(err)
	}
	activate()
	state := &cacheSubscription{runtime: "inProgress"}
	a := &agent{
		service: service, cacheSubs: map[string]*cacheSubscription{"thread": state},
		cacheRecoveryGuardDelay: 60 * time.Millisecond,
	}
	a.armCacheRecoveryGuard("thread", state)
	time.Sleep(40 * time.Millisecond)
	a.observeCacheRecoveryState("thread", state, "inProgress")
	time.Sleep(35 * time.Millisecond)
	if got := live.refreshCalls.Load(); got != 0 {
		t.Fatalf("ordinary follower update did not reset the quiet window: refreshes=%d", got)
	}
	deadline := time.After(time.Second)
	for live.refreshCalls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("guard did not refresh after the reset quiet window elapsed")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestThreadListRequestContextBudgetsOnlyMenuRefresh(t *testing.T) {
	parent := context.Background()
	listCtx, cancel := threadListRequestContext(parent, "")
	defer cancel()
	deadline, ok := listCtx.Deadline()
	remaining := time.Until(deadline)
	if !ok || remaining < 29*time.Second || remaining > 30*time.Second {
		t.Fatalf("menu refresh deadline remaining = %s, present=%v", remaining, ok)
	}
	searchCtx, stopSearch := threadListRequestContext(parent, "needle")
	defer stopSearch()
	if searchCtx != parent {
		t.Fatal("thread search unexpectedly received the menu refresh budget")
	}
	if _, ok := searchCtx.Deadline(); ok {
		t.Fatal("thread search unexpectedly received a deadline")
	}
}

func TestRealAgentRoutesHistoryAndSnapshotThroughRelay(t *testing.T) {
	r, _ := relay.New(relay.Config{Token: "test-secret", WebPIN: "012345", AllowedOrigins: []string{"http://localhost:5173"}})
	srv := httptest.NewServer(r.Handler())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	live := &fakeLive{updates: make(chan struct{}, 1)}
	s := NewService(pagedHistory{}, func(context.Context, string, string) (Live, error) {
		return live, nil
	})
	// Spec 0086 keeps the follower warm after the last Web subscription
	// disappears; use a short grace so this test still observes the eventual
	// close once the Web side disconnects.
	s.idleGrace = 50 * time.Millisecond
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go RunWithService(ctx, Config{URL: url, Token: "test-secret", DeviceID: "real-mac", DeviceName: "Real Mac"}, s)
	wctx, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	w, _, err := websocket.Dial(wctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"http://localhost:5173"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer w.CloseNow()
	if err := wsjson.Write(wctx, w, map[string]any{"type": "hello", "v": 1, "role": "web", "token": "012345"}); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := wsjson.Read(wctx, w, &got); err != nil {
		t.Fatal(err)
	}
	requestID := "00000000-0000-4000-8000-000000000001"
	for i := 0; i < 10; i++ {
		if err := wsjson.Write(wctx, w, map[string]any{"type": "request", "v": 1, "requestId": requestID, "deviceId": "real-mac", "method": "thread.list", "params": map[string]any{"limit": 10}}); err != nil {
			t.Fatal(err)
		}
		if err := wsjson.Read(wctx, w, &got); err != nil {
			t.Fatal(err)
		}
		if got["type"] == "event" {
			if err := wsjson.Read(wctx, w, &got); err != nil {
				t.Fatal(err)
			}
		}
		if got["outcome"] == "accepted" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got["outcome"] != "accepted" {
		t.Fatalf("list: %v", got)
	}
	data := got["data"].(map[string]any)
	if len(data["threads"].([]any)) != 1 {
		t.Fatalf("history: %v", data)
	}
	requestID = "00000000-0000-4000-8000-000000000005"
	if err := wsjson.Write(wctx, w, map[string]any{"type": "request", "v": 1, "requestId": requestID, "deviceId": "real-mac", "method": "thread.create", "params": map[string]any{"cwd": t.TempDir()}}); err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Read(wctx, w, &got); err != nil {
		t.Fatal(err)
	}
	if got["outcome"] != "rejected" || got["error"].(map[string]any)["code"] != "PROTOCOL_UNSUPPORTED" {
		t.Fatalf("deprecated create thread route: response=%v", got)
	}
	requestID = "00000000-0000-4000-8000-000000000004"
	if err := wsjson.Write(wctx, w, map[string]any{"type": "request", "v": 1, "requestId": requestID, "deviceId": "real-mac", "method": "thread.history", "params": map[string]any{"threadId": "thread", "limit": 10}}); err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Read(wctx, w, &got); err != nil {
		t.Fatal(err)
	}
	if got["type"] != "response" || got["outcome"] != "accepted" || len(got["data"].(map[string]any)["turns"].([]any)) != 10 {
		t.Fatalf("paged history response: %v", got)
	}
	requestID = "00000000-0000-4000-8000-000000000002"
	if err := wsjson.Write(wctx, w, map[string]any{"type": "request", "v": 1, "requestId": requestID, "deviceId": "real-mac", "method": "thread.subscribe", "params": map[string]any{"threadId": "thread", "updateMode": "item-delta-v1"}}); err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Read(wctx, w, &got); err != nil {
		t.Fatal(err)
	}
	if got["type"] != "response" || got["outcome"] != "accepted" {
		t.Fatalf("subscription response: %v", got)
	}
	if err := wsjson.Read(wctx, w, &got); err != nil {
		t.Fatal(err)
	}
	if got["event"] != "thread.snapshot" || got["seq"] != float64(1) {
		t.Fatalf("snapshot: %v", got)
	}
	// A change originating at the owner, with no Web mutation request, must
	// reach the already subscribed browser through Agent and Relay.
	live.mu.Lock()
	live.busy = true
	live.mu.Unlock()
	live.updates <- struct{}{}
	for {
		if err := wsjson.Read(wctx, w, &got); err != nil {
			t.Fatalf("owner update did not reach Web: %v", err)
		}
		if got["event"] == "thread.update" {
			break
		}
	}
	patch := got["patch"].(map[string]any)
	if got["seq"] != float64(2) || got["baseSeq"] != float64(1) || patch["threadState"].(map[string]any)["runtime"] != "inProgress" || len(patch["turnPatches"].([]any)) != 1 {
		t.Fatalf("owner update: %v", got)
	}
	w.CloseNow()
	deadline := time.After(2 * time.Second)
	for {
		live.mu.Lock()
		closed := live.closed
		live.mu.Unlock()
		if closed {
			break
		}
		select {
		case <-deadline:
			t.Fatal("Web disconnect left Agent follower subscribed")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestAgentCacheTouchPublishesTwoTurnSnapshotWithoutWebSubscription(t *testing.T) {
	r, _ := relay.New(relay.Config{Token: "test-secret", WebPIN: "012345", AllowedOrigins: []string{"http://localhost:5173"}})
	srv := httptest.NewServer(r.Handler())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	live := &fakeLive{updates: make(chan struct{}, 2)}
	service := NewService(pagedHistory{}, func(context.Context, string, string) (Live, error) { return live, nil })
	defer service.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go RunWithService(ctx, Config{URL: url, Token: "test-secret", DeviceID: "real-mac", DeviceName: "Real Mac", CacheWarmGrace: 30 * time.Millisecond}, service)
	wctx, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	web, _, err := websocket.Dial(wctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"http://localhost:5173"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer web.CloseNow()
	if err := wsjson.Write(wctx, web, map[string]any{"type": "hello", "v": 1, "role": "web", "token": "012345"}); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := wsjson.Read(wctx, web, &got); err != nil {
		t.Fatal(err)
	}
	for {
		if err := wsjson.Write(wctx, web, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000091", "deviceId": "relay", "method": "device.list", "params": map[string]any{}}); err != nil {
			t.Fatal(err)
		}
		got = nil
		if err := wsjson.Read(wctx, web, &got); err != nil {
			t.Fatal(err)
		}
		if got["type"] == "event" {
			got = nil
			if err := wsjson.Read(wctx, web, &got); err != nil {
				t.Fatal(err)
			}
		}
		if got["requestId"] == "00000000-0000-4000-8000-000000000091" && len(got["data"].(map[string]any)["devices"].([]any)) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := wsjson.Write(wctx, web, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000092", "deviceId": "real-mac", "method": "thread.cache.watch", "params": map[string]any{"threadId": "thread"}}); err != nil {
		t.Fatal(err)
	}
	got = nil
	for got["requestId"] != "00000000-0000-4000-8000-000000000092" {
		got = nil
		if err := wsjson.Read(wctx, web, &got); err != nil {
			t.Fatalf("watch: %v err=%v", got, err)
		}
	}
	if got["data"].(map[string]any)["freshness"] != "missing" {
		t.Fatalf("watch: %v", got)
	}
	watchID := got["data"].(map[string]any)["watchId"].(string)
	if err := wsjson.Write(wctx, web, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000093", "deviceId": "real-mac", "method": "thread.cache.promote", "params": map[string]any{"threadId": "thread"}}); err != nil {
		t.Fatal(err)
	}
	for {
		got = nil
		if err := wsjson.Read(wctx, web, &got); err != nil {
			t.Fatal(err)
		}
		if got["event"] == "thread.cache.update" {
			break
		}
	}
	thread := got["thread"].(map[string]any)
	if got["freshness"] != "fresh" || len(thread["turns"].([]any)) > 2 {
		t.Fatalf("cache snapshot: %+v", got)
	}
	live.mu.Lock()
	live.busy = true
	live.mu.Unlock()
	live.updates <- struct{}{}
	for {
		got = nil
		if err := wsjson.Read(wctx, web, &got); err != nil {
			t.Fatal(err)
		}
		if got["event"] == "thread.cache.update" && got["seq"] == float64(2) {
			break
		}
	}
	if got["thread"].(map[string]any)["runtime"] != "inProgress" {
		t.Fatalf("streaming replacement did not reach Relay cache: %+v", got)
	}
	// A second update with identical structure is pure streaming growth. It is
	// superseded by the following structural completion instead of creating an
	// intermediate Relay update.
	live.updates <- struct{}{}
	time.Sleep(50 * time.Millisecond)
	live.mu.Lock()
	live.busy = false
	live.mu.Unlock()
	live.updates <- struct{}{}
	for {
		got = nil
		if err := wsjson.Read(wctx, web, &got); err != nil {
			t.Fatal(err)
		}
		if got["event"] == "thread.cache.update" {
			break
		}
	}
	if got["seq"] != float64(4) {
		t.Fatalf("pure streaming update was not coalesced to the latest structural snapshot: %+v", got)
	}
	if err := wsjson.Write(wctx, web, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000094", "deviceId": "real-mac", "method": "thread.cache.unwatch", "params": map[string]any{"watchId": watchID}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	live.mu.Lock()
	closed := live.closed
	live.mu.Unlock()
	if closed {
		t.Fatal("cache follower was eagerly closed after warm grace instead of remaining available until byte-capacity pressure")
	}
}

func TestAgentCacheOversizeReportsTerminalStatusWithoutDisconnecting(t *testing.T) {
	r, _ := relay.New(relay.Config{Token: "test-secret", WebPIN: "012345", AllowedOrigins: []string{"http://localhost:5173"}})
	srv := httptest.NewServer(r.Handler())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	service := NewService(pagedHistory{}, func(context.Context, string, string) (Live, error) {
		return &fakeLive{updates: make(chan struct{}, 1)}, nil
	})
	defer service.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go RunWithService(ctx, Config{
		URL: url, Token: "test-secret", DeviceID: "real-mac", DeviceName: "Real Mac",
		CacheEntryMaxBytes: 64,
	}, service)
	wctx, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	web, _, err := websocket.Dial(wctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"http://localhost:5173"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer web.CloseNow()
	if err := wsjson.Write(wctx, web, map[string]any{"type": "hello", "v": 1, "role": "web", "token": "012345"}); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := wsjson.Read(wctx, web, &got); err != nil {
		t.Fatal(err)
	}
	for {
		if err := wsjson.Write(wctx, web, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000095", "deviceId": "relay", "method": "device.list", "params": map[string]any{}}); err != nil {
			t.Fatal(err)
		}
		if err := wsjson.Read(wctx, web, &got); err != nil {
			t.Fatal(err)
		}
		if got["type"] == "event" {
			if err := wsjson.Read(wctx, web, &got); err != nil {
				t.Fatal(err)
			}
		}
		if got["requestId"] == "00000000-0000-4000-8000-000000000095" && len(got["data"].(map[string]any)["devices"].([]any)) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := wsjson.Write(wctx, web, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000094", "deviceId": "real-mac", "method": "thread.read", "params": map[string]any{"threadId": "thread"}}); err != nil {
		t.Fatal(err)
	}
	for {
		got = nil
		if err := wsjson.Read(wctx, web, &got); err != nil {
			t.Fatal(err)
		}
		if got["requestId"] == "00000000-0000-4000-8000-000000000094" {
			break
		}
	}
	if got["outcome"] != "rejected" || got["error"].(map[string]any)["code"] != "HISTORY_TOO_LARGE" {
		t.Fatalf("oversized progressive read crossed the Agent boundary: %+v", got)
	}
	if err := wsjson.Write(wctx, web, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000096", "deviceId": "real-mac", "method": "thread.cache.watch", "params": map[string]any{"threadId": "thread"}}); err != nil {
		t.Fatal(err)
	}
	for {
		got = nil
		if err := wsjson.Read(wctx, web, &got); err != nil {
			t.Fatal(err)
		}
		if got["event"] == "thread.cache.update" && got["staleReason"] == "CACHE_OVERSIZED" {
			break
		}
	}
	if got["freshness"] != "missing" || got["thread"] != nil {
		t.Fatalf("oversized cache leaked a payload: %+v", got)
	}
	if err := wsjson.Write(wctx, web, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000097", "deviceId": "relay", "method": "device.list", "params": map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	for {
		got = nil
		if err := wsjson.Read(wctx, web, &got); err != nil {
			t.Fatal(err)
		}
		if got["requestId"] == "00000000-0000-4000-8000-000000000097" {
			break
		}
	}
	if len(got["data"].(map[string]any)["devices"].([]any)) != 1 {
		t.Fatalf("oversized cache disconnected the agent: %+v", got)
	}
}

func TestAgentCacheRewatchReusesFreshWarmFollower(t *testing.T) {
	r, _ := relay.New(relay.Config{Token: "test-secret", WebPIN: "012345", AllowedOrigins: []string{"http://localhost:5173"}})
	srv := httptest.NewServer(r.Handler())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	first := &fakeLive{updates: make(chan struct{}, 1)}
	second := &fakeLive{updates: make(chan struct{}, 1), busy: true}
	var opens int
	service := NewService(pagedHistory{}, func(context.Context, string, string) (Live, error) {
		opens++
		if opens == 1 {
			return first, nil
		}
		return second, nil
	})
	defer service.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go RunWithService(ctx, Config{URL: url, Token: "test-secret", DeviceID: "real-mac", DeviceName: "Real Mac"}, service)
	wctx, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	web, _, err := websocket.Dial(wctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"http://localhost:5173"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer web.CloseNow()
	if err := wsjson.Write(wctx, web, map[string]any{"type": "hello", "v": 1, "role": "web", "token": "012345"}); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := wsjson.Read(wctx, web, &got); err != nil {
		t.Fatal(err)
	}
	const firstWatchRequest = "00000000-0000-4000-8001-999999999997"
	if err := wsjson.Write(wctx, web, map[string]any{"type": "request", "v": 1, "requestId": firstWatchRequest, "deviceId": "real-mac", "method": "thread.cache.watch", "params": map[string]any{"threadId": "thread"}}); err != nil {
		t.Fatal(err)
	}
	var watchID string
	for {
		got = nil
		if err := wsjson.Read(wctx, web, &got); err != nil {
			t.Fatal(err)
		}
		if got["requestId"] == firstWatchRequest {
			watchID, _ = got["data"].(map[string]any)["watchId"].(string)
		}
		if got["event"] == "thread.cache.update" && got["freshness"] == "fresh" {
			break
		}
	}
	if watchID == "" {
		t.Fatal("first cache watch did not return a watch ID")
	}
	if err := wsjson.Write(wctx, web, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8001-999999999998", "deviceId": "real-mac", "method": "thread.cache.unwatch", "params": map[string]any{"watchId": watchID}}); err != nil {
		t.Fatal(err)
	}
	for {
		got = nil
		if err := wsjson.Read(wctx, web, &got); err != nil {
			t.Fatal(err)
		}
		if got["requestId"] == "00000000-0000-4000-8001-999999999998" {
			break
		}
	}

	if err := wsjson.Write(wctx, web, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8001-999999999999", "deviceId": "real-mac", "method": "thread.cache.watch", "params": map[string]any{"threadId": "thread"}}); err != nil {
		t.Fatal(err)
	}
	for {
		got = nil
		if err := wsjson.Read(wctx, web, &got); err != nil {
			t.Fatal(err)
		}
		if got["requestId"] == "00000000-0000-4000-8001-999999999999" {
			data := got["data"].(map[string]any)
			if data["freshness"] != "fresh" {
				t.Fatalf("rewatch did not reuse fresh Relay cache: %+v", got)
			}
			break
		}
	}
	if opens != 1 {
		t.Fatalf("fresh warm follower was rebound: opens=%d", opens)
	}
	first.mu.Lock()
	closed := first.closed
	first.mu.Unlock()
	if closed {
		t.Fatal("fresh warm follower was closed after rewatch")
	}
}

func TestAgentPrewarmsMostRecentThreadWithoutWebWatcher(t *testing.T) {
	r, _ := relay.New(relay.Config{Token: "test-secret", WebPIN: "012345", AllowedOrigins: []string{"http://localhost:5173"}})
	srv := httptest.NewServer(r.Handler())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	live := &fakeLive{updates: make(chan struct{}, 1)}
	service := NewService(pagedHistory{}, func(context.Context, string, string) (Live, error) { return live, nil })
	defer service.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go RunWithService(ctx, Config{URL: url, Token: "test-secret", DeviceID: "real-mac", DeviceName: "Real Mac", CachePrewarmRecent: true}, service)
	wctx, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	web, _, err := websocket.Dial(wctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"http://localhost:5173"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer web.CloseNow()
	if err := wsjson.Write(wctx, web, map[string]any{"type": "hello", "v": 1, "role": "web", "token": "012345"}); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := wsjson.Read(wctx, web, &got); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 100; attempt++ {
		requestID := fmt.Sprintf("00000000-0000-4000-8000-%012d", attempt+200)
		if err := wsjson.Write(wctx, web, map[string]any{"type": "request", "v": 1, "requestId": requestID, "deviceId": "real-mac", "method": "thread.cache.read", "params": map[string]any{"threadId": "thread"}}); err != nil {
			t.Fatal(err)
		}
		for {
			got = nil
			if err := wsjson.Read(wctx, web, &got); err != nil {
				t.Fatal(err)
			}
			if got["requestId"] == requestID {
				break
			}
		}
		if got["outcome"] == "accepted" {
			data := got["data"].(map[string]any)
			if data["freshness"] == "fresh" {
				thread := data["thread"].(map[string]any)
				if len(thread["turns"].([]any)) > 2 {
					t.Fatalf("prewarm retained more than two turns: %+v", thread)
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("most recent thread was not prewarmed: %+v", got)
}

func TestAgentHeartbeatDropsSilentRelay(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		var hello map[string]any
		if wsjson.Read(ctx, conn, &hello) != nil {
			return
		}
		_ = wsjson.Write(ctx, conn, map[string]any{"type": "hello.ok", "v": 1, "connectionId": "c", "relayEpoch": "e"})
		<-ctx.Done() // Intentionally no Reader for ping/pong after hello.
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ready, notReady := false, false
	err := RunWithService(ctx, Config{URL: "ws" + strings.TrimPrefix(srv.URL, "http"), Token: "test-secret", DeviceID: "real-mac", DeviceName: "Real Mac", HeartbeatInterval: 30 * time.Millisecond, HeartbeatTimeout: 30 * time.Millisecond,
		OnReady: func() error { ready = true; return nil }, OnNotReady: func() { notReady = true },
	}, NewService(fakeHistory{}, nil))
	if err == nil || ctx.Err() != nil {
		t.Fatalf("silent Relay did not cause bounded disconnect: %v", err)
	}
	if !strings.Contains(err.Error(), "heartbeat") {
		t.Fatalf("heartbeat failure lost its cause: %v", err)
	}
	if !ready || !notReady {
		t.Fatalf("handshake readiness lifecycle: ready=%v notReady=%v", ready, notReady)
	}
}

func TestTransportReconnectKeepsCodexFollowerWarm(t *testing.T) {
	var connections atomic.Int32
	var opens atomic.Int32
	live := &fakeLive{updates: make(chan struct{}, 1)}
	service := NewService(pagedHistory{}, func(context.Context, string, string) (Live, error) { opens.Add(1); return live, nil })
	defer service.Close()
	secondSnapshot := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		connection := connections.Add(1)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var message map[string]any
		if wsjson.Read(ctx, conn, &message) != nil {
			return
		}
		if err := wsjson.Write(ctx, conn, map[string]any{"type": "hello.ok", "v": 1, "connectionId": fmt.Sprintf("c-%d", connection), "relayEpoch": "relay"}); err != nil {
			return
		}
		requestID := fmt.Sprintf("00000000-0000-4000-8000-%012d", 700+connection)
		if err := wsjson.Write(ctx, conn, map[string]any{"type": "request", "v": 1, "requestId": requestID, "deviceId": "real-mac", "method": "thread.cache.touch", "params": map[string]any{"threadId": "thread", "watching": true}}); err != nil {
			return
		}
		for {
			message = nil
			if wsjson.Read(ctx, conn, &message) != nil {
				return
			}
			if message["event"] == "thread.cache.snapshot" {
				break
			}
		}
		if connection == 1 {
			_ = conn.Close(websocket.StatusGoingAway, "fixture reconnect")
			return
		}
		select {
		case <-secondSnapshot:
		default:
			close(secondSnapshot)
		}
		<-ctx.Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runTransportLoop(ctx, Config{URL: "ws" + strings.TrimPrefix(srv.URL, "http"), Token: "test-secret", DeviceID: "real-mac", DeviceName: "Real Mac", AgentEpoch: "process-epoch", HeartbeatInterval: time.Hour}, service)
	}()
	select {
	case <-secondSnapshot:
	case <-time.After(5 * time.Second):
		t.Fatal("Agent did not reconnect with a warm follower")
	}
	cancel()
	<-done
	if connections.Load() < 2 {
		t.Fatalf("connections=%d, want reconnect", connections.Load())
	}
	if opens.Load() != 1 {
		t.Fatalf("transport reconnect cold-attached Codex owner: opens=%d", opens.Load())
	}
}

func TestAgentAcceptsInterruptWhileStartReceiptPending(t *testing.T) {
	r, _ := relay.New(relay.Config{Token: "test-secret", WebPIN: "012345", AllowedOrigins: []string{"http://localhost:5173"}})
	srv := httptest.NewServer(r.Handler())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	live := &slowLive{fakeLive: &fakeLive{updates: make(chan struct{}, 2)}, started: make(chan struct{}), release: make(chan struct{}), stopped: make(chan struct{})}
	defer func() {
		select {
		case <-live.release:
		default:
			close(live.release)
		}
	}()
	s := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) { return live, nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go RunWithService(ctx, Config{URL: url, Token: "test-secret", DeviceID: "real-mac", DeviceName: "Real Mac"}, s)
	wctx, stop := context.WithTimeout(ctx, 4*time.Second)
	defer stop()
	w, _, err := websocket.Dial(wctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"http://localhost:5173"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer w.CloseNow()
	if err := wsjson.Write(wctx, w, map[string]any{"type": "hello", "v": 1, "role": "web", "token": "012345"}); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := wsjson.Read(wctx, w, &got); err != nil {
		t.Fatal(err)
	}
	// Poll the Relay registry; device.status may have preceded this Web client.
	for registered := false; !registered; {
		if err := wsjson.Write(wctx, w, map[string]any{"type": "request", "v": 1, "requestId": "00000000-0000-4000-8000-000000000010", "deviceId": "relay", "method": "device.list", "params": map[string]any{}}); err != nil {
			t.Fatal(err)
		}
		for {
			if err := wsjson.Read(wctx, w, &got); err != nil {
				t.Fatal(err)
			}
			if got["requestId"] == "00000000-0000-4000-8000-000000000010" {
				break
			}
		}
		for _, raw := range got["data"].(map[string]any)["devices"].([]any) {
			if raw.(map[string]any)["deviceId"] == "real-mac" {
				registered = true
			}
		}
		if !registered {
			time.Sleep(10 * time.Millisecond)
		}
	}
	request := func(id, method string, params map[string]any) {
		t.Helper()
		if err := wsjson.Write(wctx, w, map[string]any{"type": "request", "v": 1, "requestId": id, "deviceId": "real-mac", "method": method, "params": params}); err != nil {
			t.Fatal(err)
		}
	}
	request("00000000-0000-4000-8000-000000000011", "thread.subscribe", map[string]any{"threadId": "thread"})
	for i := 0; i < 2; i++ {
		if err := wsjson.Read(wctx, w, &got); err != nil {
			t.Fatal(err)
		}
	}
	request("00000000-0000-4000-8000-000000000012", "turn.start", map[string]any{"threadId": "thread", "clientMessageId": "00000000-0000-4000-8000-000000000014", "text": "hello"})
	select {
	case <-live.started:
	case <-time.After(time.Second):
		t.Fatal("start did not reach live owner")
	}
	request("00000000-0000-4000-8000-000000000013", "turn.interrupt", map[string]any{"threadId": "thread", "expectedTurnId": "one"})
	interruptCtx, interruptCancel := context.WithTimeout(wctx, time.Second)
	defer interruptCancel()
	for {
		if err := wsjson.Read(interruptCtx, w, &got); err != nil {
			t.Fatalf("interrupt blocked behind start receipt: %v", err)
		}
		if got["requestId"] == "00000000-0000-4000-8000-000000000013" {
			if got["outcome"] != "accepted" {
				t.Fatalf("interrupt: %v", got)
			}
			break
		}
	}
	close(live.release)
}

func TestTwoWebClientsCannotQueueStartsThroughRelayAndAgent(t *testing.T) {
	r, _ := relay.New(relay.Config{Token: "test-secret", WebPIN: "012345", AllowedOrigins: []string{"http://localhost:5173"}})
	srv := httptest.NewServer(r.Handler())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	live := &fakeLive{updates: make(chan struct{}, 1)}
	service := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) { return live, nil })
	defer service.Close()
	// A selected history thread retains one follower, as it does in the Web UI.
	if _, _, _, _, err := service.Subscribe(context.Background(), "thread", func(map[string]any) {}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go RunWithService(ctx, Config{URL: url, Token: "test-secret", DeviceID: "real-mac", DeviceName: "Real Mac"}, service)
	wctx, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	openWeb := func() *websocket.Conn {
		t.Helper()
		web, _, err := websocket.Dial(wctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"http://localhost:5173"}}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = web.CloseNow() })
		if err := wsjson.Write(wctx, web, map[string]any{"type": "hello", "v": 1, "role": "web", "token": "012345"}); err != nil {
			t.Fatal(err)
		}
		var ack map[string]any
		if err := wsjson.Read(wctx, web, &ack); err != nil || ack["type"] != "hello.ok" {
			t.Fatalf("web hello: %+v %v", ack, err)
		}
		return web
	}
	web1, web2 := openWeb(), openWeb()
	readReply := func(web *websocket.Conn, id string) map[string]any {
		t.Helper()
		for {
			var got map[string]any
			if err := wsjson.Read(wctx, web, &got); err != nil {
				t.Fatal(err)
			}
			if got["requestId"] == id {
				return got
			}
		}
	}
	for ready := false; !ready; {
		constID := "00000000-0000-4000-8000-000000000070"
		if err := wsjson.Write(wctx, web1, map[string]any{"type": "request", "v": 1, "requestId": constID, "deviceId": "relay", "method": "device.list", "params": map[string]any{}}); err != nil {
			t.Fatal(err)
		}
		list := readReply(web1, constID)
		for _, item := range list["data"].(map[string]any)["devices"].([]any) {
			if item.(map[string]any)["deviceId"] == "real-mac" {
				ready = true
			}
		}
		if !ready {
			time.Sleep(10 * time.Millisecond)
		}
	}
	clients := []struct {
		web *websocket.Conn
		id  string
		mid string
	}{{web1, "00000000-0000-4000-8000-000000000071", "00000000-0000-4000-8000-000000000073"}, {web2, "00000000-0000-4000-8000-000000000072", "00000000-0000-4000-8000-000000000074"}}
	start := make(chan struct{})
	writeErrors := make(chan error, 2)
	for _, client := range clients {
		go func() {
			<-start
			writeErrors <- wsjson.Write(wctx, client.web, map[string]any{"type": "request", "v": 1, "requestId": client.id, "deviceId": "real-mac", "method": "turn.start", "params": map[string]any{"threadId": "thread", "clientMessageId": client.mid, "text": "isolated fixture"}})
		}()
	}
	close(start)
	for range clients {
		if err := <-writeErrors; err != nil {
			t.Fatal(err)
		}
	}
	accepted, busy := 0, 0
	for _, client := range clients {
		reply := readReply(client.web, client.id)
		if reply["outcome"] == "accepted" {
			accepted++
		} else if reply["outcome"] == "rejected" && reply["error"].(map[string]any)["code"] == "TURN_BUSY" {
			busy++
		} else {
			t.Fatalf("unexpected concurrent start result: %+v", reply)
		}
	}
	if accepted != 1 || busy != 1 {
		t.Fatalf("two Web starts accepted=%d busy=%d", accepted, busy)
	}
}
