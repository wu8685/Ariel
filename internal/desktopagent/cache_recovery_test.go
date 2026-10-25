package desktopagent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// Exercise the production cache subscription and sender, with only the native
// owner replaced by an isolated fixture. No live Codex thread is mutated.
func newCacheRecoveryAgent(t *testing.T, attach LiveFactory) (*agent, *websocket.Conn, context.Context) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	stop := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		accepted <- conn
		<-stop
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	peer, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		cancel()
		close(stop)
		server.Close()
		t.Fatal(err)
	}
	conn := <-accepted
	service := NewService(fakeHistory{}, attach)
	a := &agent{
		conn: conn, deviceID: "mac", agentEpoch: "epoch", service: service,
		cacheSubs: map[string]*cacheSubscription{}, cacheLoad: make(chan struct{}, 1),
		cacheWarmGrace: time.Minute, cachePublishInterval: time.Millisecond,
		cacheRecoveryGuardDelay: time.Hour, cacheEntryMaxBytes: defaultCacheEntryMaxBytes,
		cachePending: map[string]cacheOutbound{}, cacheWake: make(chan struct{}, 1),
	}
	senderDone := make(chan struct{})
	go func() { defer close(senderDone); a.runCacheSender(ctx) }()
	t.Cleanup(func() {
		cancel()
		peer.CloseNow()
		conn.CloseNow()
		<-senderDone
		a.releaseCacheSubscriptions()
		service.Close()
		close(stop)
		server.Close()
	})
	return a, peer, ctx
}

func readCacheRecoveryEvent(t *testing.T, ctx context.Context, peer *websocket.Conn) map[string]any {
	t.Helper()
	var event map[string]any
	if err := wsjson.Read(ctx, peer, &event); err != nil {
		t.Fatal(err)
	}
	return event
}

func TestCacheNativeFollowerFailureAllowsExplicitPromotion(t *testing.T) {
	first := &fakeLive{busy: true, updates: make(chan struct{}, 1), done: make(chan struct{})}
	second := &fakeLive{updates: make(chan struct{}, 1), done: make(chan struct{})}
	var attaches atomic.Int32
	a, peer, ctx := newCacheRecoveryAgent(t, func(context.Context, string, string) (Live, error) {
		if attaches.Add(1) == 1 {
			return first, nil
		}
		return second, nil
	})
	if err := a.touchCache(ctx, "thread", true); err != nil {
		t.Fatal(err)
	}
	initial := readCacheRecoveryEvent(t, ctx, peer)
	first.Close()
	failed := readCacheRecoveryEvent(t, ctx, peer)
	if failed["event"] != "thread.cache.status" || failed["freshness"] != "error" {
		t.Fatalf("native failure was not reported: %v", failed)
	}
	if err := a.touchCache(ctx, "thread", true); err != nil {
		t.Fatal(err)
	}
	if attaches.Load() != 2 {
		t.Fatal("promotion reused a dead cache subscription instead of attaching a new native follower")
	}
	recovered := readCacheRecoveryEvent(t, ctx, peer)
	if recovered["cacheStreamId"] == initial["cacheStreamId"] || recovered["thread"].(map[string]any)["runtime"] != "idle" {
		t.Fatalf("promotion did not replace the stale running view: %v", recovered)
	}
}

type blockedRecoveryLive struct {
	*fakeLive
	started chan struct{}
	release chan struct{}
}

func (l *blockedRecoveryLive) Refresh(context.Context) (json.RawMessage, error) {
	close(l.started)
	<-l.release
	return nil, errors.New("late refresh failure")
}

func TestCacheLateRecoveryFailureCannotOverwriteTerminalUpdate(t *testing.T) {
	live := &blockedRecoveryLive{
		fakeLive: &fakeLive{busy: true, updates: make(chan struct{}, 1)},
		started:  make(chan struct{}), release: make(chan struct{}),
	}
	a, peer, ctx := newCacheRecoveryAgent(t, func(context.Context, string, string) (Live, error) { return live, nil })
	if err := a.touchCache(ctx, "thread", true); err != nil {
		t.Fatal(err)
	}
	readCacheRecoveryEvent(t, ctx, peer)
	a.cacheMu.Lock()
	state := a.cacheSubs["thread"]
	a.cacheMu.Unlock()
	finished := make(chan struct{})
	go func() { defer close(finished); a.runCacheRecoveryGuard("thread", state) }()
	<-live.started
	live.mu.Lock()
	live.busy = false
	live.mu.Unlock()
	live.updates <- struct{}{}
	terminal := readCacheRecoveryEvent(t, ctx, peer)
	if terminal["event"] != "thread.cache.snapshot" || terminal["thread"].(map[string]any)["runtime"] != "idle" {
		t.Fatalf("terminal update missing before refresh finishes: %v", terminal)
	}
	close(live.release)
	<-finished
	quiet, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	var stale map[string]any
	if err := wsjson.Read(quiet, peer, &stale); err == nil {
		t.Fatalf("late recovery failure invalidated a newer terminal snapshot: %v", stale)
	}
}

func TestCacheSnapshotMailboxRejectsOutOfOrderSnapshot(t *testing.T) {
	a := &agent{cachePending: map[string]cacheOutbound{}, cacheWake: make(chan struct{}, 1)}
	a.enqueueCacheSnapshot("thread", "stream", 3, map[string]any{"runtime": "idle"})
	a.enqueueCacheSnapshot("thread", "stream", 2, map[string]any{"runtime": "inProgress"})
	if got := a.cachePending["thread"]; got.seq != 3 || got.thread["runtime"] != "idle" {
		t.Fatalf("late timer snapshot replaced the terminal mailbox entry: %+v", got)
	}
}

func TestCacheExplicitRetryRefreshesFailedRecoveryWithoutReattaching(t *testing.T) {
	live := &refreshableFakeLive{fakeLive: &fakeLive{busy: true, updates: make(chan struct{}, 1)}, refreshErr: errors.New("injected refresh failure")}
	var attaches atomic.Int32
	a, peer, ctx := newCacheRecoveryAgent(t, func(context.Context, string, string) (Live, error) {
		attaches.Add(1)
		return live, nil
	})
	if err := a.touchCache(ctx, "thread", true); err != nil {
		t.Fatal(err)
	}
	readCacheRecoveryEvent(t, ctx, peer)
	a.cacheMu.Lock()
	state := a.cacheSubs["thread"]
	a.cacheMu.Unlock()
	a.runCacheRecoveryGuard("thread", state)
	readCacheRecoveryEvent(t, ctx, peer)
	live.refreshErr = nil
	live.onRefresh = func() {
		live.mu.Lock()
		live.busy = false
		live.mu.Unlock()
		live.updates <- struct{}{}
	}
	if err := a.touchCache(ctx, "thread", true); err != nil {
		t.Fatal(err)
	}
	if live.refreshCalls.Load() != 2 || attaches.Load() != 1 {
		t.Fatalf("explicit retry did not refresh the existing failed follower: refreshes=%d attaches=%d", live.refreshCalls.Load(), attaches.Load())
	}
	if recovered := readCacheRecoveryEvent(t, ctx, peer); recovered["event"] != "thread.cache.snapshot" || recovered["thread"].(map[string]any)["runtime"] != "idle" {
		t.Fatalf("explicit retry failed to publish recovered terminal state: %v", recovered)
	}
}
