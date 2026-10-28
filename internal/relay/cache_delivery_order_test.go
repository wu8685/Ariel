package relay

import (
	"context"
	"testing"
	"time"
)

// Capture a delivery under the same lock as production, but deliberately hold
// it until a newer authoritative state has already been admitted and queued.
func TestRelayLateCacheDeliveryCannotRollBackWatcher(t *testing.T) {
	for _, tc := range []struct {
		name, initialReason, nextStream string
		nextSeq                         int
		lease, drained                  bool
	}{
		{name: "replacement stream", initialReason: "RESYNC_REQUIRED", nextStream: "new", nextSeq: 1},
		{name: "new snapshot", initialReason: "RESYNC_REQUIRED", nextStream: "old", nextSeq: 2},
		{name: "lease at same seq", initialReason: "agent_offline", lease: true},
		{name: "already drained replacement", initialReason: "RESYNC_REQUIRED", nextStream: "new", nextSeq: 1, drained: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, agent, web, key := cacheDeliveryFixture(t)
			applyCacheTestEvent(t, r, agent, cacheSnapshotEvent(key.threadID, agent.agentEpoch, "old", 1))
			applyCacheTestEvent(t, r, agent, map[string]any{"event": "thread.cache.status", "deviceId": agent.deviceID, "threadId": key.threadID, "agentEpoch": agent.agentEpoch, "cacheStreamId": "old", "freshness": "error", "reason": tc.initialReason})
			r.mu.Lock()
			late := r.cacheUpdateMessagesLocked(key)
			r.mu.Unlock()
			if tc.lease {
				applyCacheTestEvent(t, r, agent, map[string]any{"event": "thread.cache.status", "deviceId": agent.deviceID, "threadId": key.threadID, "agentEpoch": agent.agentEpoch, "cacheStreamId": "old", "freshness": "fresh"})
			} else {
				applyCacheTestEvent(t, r, agent, cacheSnapshotEvent(key.threadID, agent.agentEpoch, tc.nextStream, tc.nextSeq))
			}
			web.cacheSendMu.Lock()
			latest := web.cachePending["watch"]
			if latest["freshness"] != "fresh" {
				t.Fatal("fixture did not recover")
			}
			if tc.drained {
				delete(web.cachePending, "watch")
			}
			web.cacheSendMu.Unlock()
			r.sendCacheDeliveries(context.Background(), late)
			web.cacheSendMu.Lock()
			defer web.cacheSendMu.Unlock()
			got := web.cachePending["watch"]
			if tc.drained {
				if got != nil {
					t.Fatalf("late delivery resurrected a drained update: %v", got)
				}
			} else if got["freshness"] != "fresh" || got["cacheStreamId"] != latest["cacheStreamId"] || got["seq"] != latest["seq"] {
				t.Fatalf("late delivery rolled back recovered state: %v", got)
			}
		})
	}
}

func TestRelayLateFreshDeliveryCannotClearCurrentFailure(t *testing.T) {
	r, agent, web, key := cacheDeliveryFixture(t)
	applyCacheTestEvent(t, r, agent, cacheSnapshotEvent(key.threadID, agent.agentEpoch, "stream", 1))
	r.mu.Lock()
	late := r.cacheUpdateMessagesLocked(key)
	r.mu.Unlock()
	applyCacheTestEvent(t, r, agent, map[string]any{"event": "thread.cache.status", "deviceId": agent.deviceID, "threadId": key.threadID, "agentEpoch": agent.agentEpoch, "cacheStreamId": "stream", "freshness": "error", "reason": "RESYNC_REQUIRED"})
	r.sendCacheDeliveries(context.Background(), late)
	if got := web.cachePending["watch"]; got["freshness"] != "stale" || got["staleReason"] != "RESYNC_REQUIRED" {
		t.Fatalf("late fresh delivery cleared a real current failure: %v", got)
	}
}

func TestRelayLateDeliveryCannotOutliveWatcherOrEntry(t *testing.T) {
	for _, recreate := range []bool{false, true} {
		t.Run(map[bool]string{false: "unwatch", true: "recreated entry"}[recreate], func(t *testing.T) {
			r, agent, web, key := cacheDeliveryFixture(t)
			applyCacheTestEvent(t, r, agent, cacheSnapshotEvent(key.threadID, agent.agentEpoch, "old", 1))
			r.mu.Lock()
			late := r.cacheUpdateMessagesLocked(key)
			r.cache.unwatch(key, "watch", time.Now())
			if recreate {
				delete(r.cache.entries, key)
				// Even a revision collision on a different entry is not authority.
				r.cache.watch(key, "watch", web, time.Now())
				r.cacheUpdateMessagesLocked(key)
				r.cacheUpdateMessagesLocked(key)
			}
			r.mu.Unlock()
			delete(web.cachePending, "watch")
			r.sendCacheDeliveries(context.Background(), late)
			if len(web.cachePending) != 0 {
				t.Fatal("late delivery survived its watcher or entry identity")
			}
		})
	}
}

func TestRelayLateDeliveryCannotCrossWatcherIdentity(t *testing.T) {
	r, agent, web, key := cacheDeliveryFixture(t)
	applyCacheTestEvent(t, r, agent, cacheSnapshotEvent(key.threadID, agent.agentEpoch, "stream", 1))
	r.mu.Lock()
	late := r.cacheUpdateMessagesLocked(key)
	r.cache.entries[key].watchers["watch"] = cacheWatcher{id: "watch", web: &peer{}}
	r.mu.Unlock()
	delete(web.cachePending, "watch")
	r.sendCacheDeliveries(context.Background(), late)
	if len(web.cachePending) != 0 {
		t.Fatal("delivery crossed watcher ownership")
	}
}

func cacheDeliveryFixture(t *testing.T) (*Server, *peer, *peer, cacheKey) {
	t.Helper()
	r, err := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	if err != nil {
		t.Fatal(err)
	}
	agent := &peer{deviceID: "mock-mac", agentEpoch: "epoch-a"}
	r.agents[agent.deviceID] = agent
	web := &peer{cacheWake: make(chan struct{}, 1), cachePending: map[string]map[string]any{}}
	key := cacheKey{agent.deviceID, "thread"}
	r.cache.watch(key, "watch", web, time.Now())
	return r, agent, web, key
}
