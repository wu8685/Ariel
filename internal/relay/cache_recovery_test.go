package relay

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestRelayLeaseCannotClearExplicitCacheFailure(t *testing.T) {
	for _, reason := range []string{"RESYNC_REQUIRED", "CACHE_OVERSIZED", "NATIVE_STATE_UNCERTAIN", "TIMEOUT"} {
		t.Run(reason, func(t *testing.T) {
			r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
			agent := &peer{deviceID: "mock-mac", agentEpoch: "epoch-a"}
			r.agents[agent.deviceID] = agent
			applyCacheTestEvent(t, r, agent, cacheSnapshotEvent("thread", "epoch-a", "stream", 1))
			status := map[string]any{"event": "thread.cache.status", "deviceId": "mock-mac", "threadId": "thread", "agentEpoch": "epoch-a", "cacheStreamId": "stream", "freshness": "error", "reason": reason}
			applyCacheTestEvent(t, r, agent, status)
			status["freshness"], status["reason"] = "fresh", ""
			applyCacheTestEvent(t, r, agent, status)
			entry := r.cache.entries[cacheKey{"mock-mac", "thread"}]
			if got := r.cacheDataLocked(entry, agent); got["freshness"] != "stale" || got["staleReason"] != reason {
				t.Fatalf("lease falsely confirmed a failed snapshot as fresh: %v", got)
			}
			applyCacheTestEvent(t, r, agent, cacheSnapshotEvent("thread", "epoch-a", "stream", 2))
			if got := r.cacheDataLocked(entry, agent); got["freshness"] != "fresh" {
				t.Fatalf("new snapshot failed to recover stale cache: %v", got)
			}
		})
	}
}

func TestRelayOldStreamStatusCannotInvalidateReplacement(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	agent := &peer{deviceID: "mock-mac", agentEpoch: "epoch-a"}
	r.agents[agent.deviceID] = agent
	applyCacheTestEvent(t, r, agent, cacheSnapshotEvent("thread", "epoch-a", "old", 1))
	applyCacheTestEvent(t, r, agent, cacheSnapshotEvent("thread", "epoch-a", "new", 1))
	applyCacheTestEvent(t, r, agent, map[string]any{"event": "thread.cache.status", "deviceId": "mock-mac", "threadId": "thread", "agentEpoch": "epoch-a", "cacheStreamId": "old", "freshness": "error", "reason": "RESYNC_REQUIRED"})
	if got := r.cacheDataLocked(r.cache.entries[cacheKey{"mock-mac", "thread"}], agent); got["freshness"] != "fresh" || got["cacheStreamId"] != "new" {
		t.Fatalf("old follower error poisoned replacement cache: %v", got)
	}
}

func TestRelayDuplicateSnapshotDoesNotPoisonTerminalCache(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	agent := &peer{deviceID: "mock-mac", agentEpoch: "epoch-a"}
	r.agents[agent.deviceID] = agent
	applyCacheTestEvent(t, r, agent, cacheSnapshotEvent("thread", "epoch-a", "stream", 3))
	for _, seq := range []int{3, 2} {
		applyCacheTestEvent(t, r, agent, cacheSnapshotEvent("thread", "epoch-a", "stream", seq))
		if got := r.cacheDataLocked(r.cache.entries[cacheKey{"mock-mac", "thread"}], agent); got["freshness"] != "fresh" || got["seq"] != int64(3) {
			t.Fatalf("late seq %d made a valid terminal cache stale: %v", seq, got)
		}
	}
}

func TestRelayMatchingLeaseRestoresTransportStaleCache(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	agent := &peer{deviceID: "mock-mac", agentEpoch: "epoch-a"}
	r.agents[agent.deviceID] = agent
	applyCacheTestEvent(t, r, agent, cacheSnapshotEvent("thread", "epoch-a", "stream", 1))
	entry := r.cache.entries[cacheKey{"mock-mac", "thread"}]
	entry.stale, entry.staleReason, entry.leaseValidUntil = true, "agent_offline", time.Time{}
	applyCacheTestEvent(t, r, agent, map[string]any{"event": "thread.cache.status", "deviceId": "mock-mac", "threadId": "thread", "agentEpoch": "epoch-a", "cacheStreamId": "stream", "freshness": "fresh"})
	if got := r.cacheDataLocked(entry, agent); got["freshness"] != "fresh" {
		t.Fatalf("same follower lease failed to recover a transient transport loss: %v", got)
	}
}

func TestRelayPromotionFailurePreservesItsReasonOverOldDisplayCache(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	agent := &peer{deviceID: "mock-mac", agentEpoch: "epoch-new"}
	r.agents[agent.deviceID] = agent
	if err := r.cache.put(cacheSnapshot{
		deviceID: "mock-mac", threadID: "thread", agentEpoch: "epoch-old", streamID: "old", seq: 1,
		thread: json.RawMessage(`{"threadId":"thread","turns":[]}`),
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	key := cacheKey{"mock-mac", "thread"}
	r.beginCachePromotionLocked(key)
	applyCacheTestEvent(t, r, agent, map[string]any{"event": "thread.cache.status", "deviceId": "mock-mac", "threadId": "thread", "agentEpoch": "epoch-new", "cacheStreamId": "loading-placeholder", "freshness": "error", "reason": "NATIVE_STATE_UNCERTAIN"})
	entry := r.cache.entries[key]
	if r.cachePromoting[key] || entry.staleReason != "NATIVE_STATE_UNCERTAIN" || entry.agentEpoch != "epoch-old" {
		t.Fatalf("new promotion failed without surfacing its reason over retained history: promoting=%v reason=%s epoch=%s", r.cachePromoting[key], entry.staleReason, entry.agentEpoch)
	}
}

func TestRelayOldStreamRemoveCannotEraseReplacement(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	agent := &peer{deviceID: "mock-mac", agentEpoch: "epoch-a"}
	r.agents[agent.deviceID] = agent
	applyCacheTestEvent(t, r, agent, cacheSnapshotEvent("thread", "epoch-a", "new", 1))
	applyCacheTestEvent(t, r, agent, map[string]any{"event": "thread.cache.remove", "deviceId": "mock-mac", "threadId": "thread", "agentEpoch": "epoch-a", "cacheStreamId": "old"})
	if got := r.cacheDataLocked(r.cache.entries[cacheKey{"mock-mac", "thread"}], agent); got["freshness"] != "fresh" {
		t.Fatal("old stream removal erased the current terminal cache")
	}
}

func TestRelayDisconnectCannotEraseExplicitCacheFailure(t *testing.T) {
	r, _ := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	agent := &peer{deviceID: "mock-mac", agentEpoch: "epoch-a"}
	r.agents[agent.deviceID] = agent
	applyCacheTestEvent(t, r, agent, cacheSnapshotEvent("thread", "epoch-a", "stream", 1))
	entry := r.cache.entries[cacheKey{"mock-mac", "thread"}]
	entry.stale, entry.staleReason = true, "RESYNC_REQUIRED"
	r.cache.markDeviceStale("mock-mac", "agent_offline")
	applyCacheTestEvent(t, r, agent, map[string]any{"event": "thread.cache.status", "deviceId": "mock-mac", "threadId": "thread", "agentEpoch": "epoch-a", "cacheStreamId": "stream", "freshness": "fresh"})
	if got := r.cacheDataLocked(entry, agent); got["freshness"] != "stale" || got["staleReason"] != "RESYNC_REQUIRED" {
		t.Fatal("transport reconnect plus lease cleared the explicit content failure")
	}
}

func applyCacheTestEvent(t *testing.T, r *Server, agent *peer, event map[string]any) {
	t.Helper()
	body, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if !r.handleCacheAgentEvent(context.Background(), agent, body) {
		t.Fatal("cache event was not handled")
	}
}

func TestRelayPromotionSendFailureBelongsToItsOperation(t *testing.T) {
	for _, state := range []string{"recovered", "replaced", "current"} {
		t.Run(state, func(t *testing.T) {
			r, agent, _, key := cacheDeliveryFixture(t)
			applyCacheTestEvent(t, r, agent, cacheSnapshotEvent(key.threadID, agent.agentEpoch, "stream", 1))
			r.mu.Lock()
			oldID := r.beginCachePromotionLocked(key)
			r.mu.Unlock()
			currentID := oldID
			if state == "recovered" {
				applyCacheTestEvent(t, r, agent, cacheSnapshotEvent(key.threadID, agent.agentEpoch, "stream", 2))
			} else if state == "replaced" {
				r.mu.Lock()
				r.finishCachePromotionLocked(key)
				currentID = r.beginCachePromotionLocked(key)
				r.mu.Unlock()
			}
			r.failCachePromotion(key, oldID, "DEVICE_OFFLINE")
			r.mu.Lock()
			defer r.mu.Unlock()
			got := r.cacheDataLocked(r.cache.entries[key], agent)
			if state == "current" {
				if got["freshness"] != "stale" || got["staleReason"] != "DEVICE_OFFLINE" || r.cachePromoting[key] {
					t.Fatal("current send failure was not surfaced and retired")
				}
			} else if got["freshness"] != "fresh" || state == "replaced" && r.cachePromotionIDs[key] != currentID {
				t.Fatalf("late send failure from %q invalidated %s state", oldID, state)
			}
			r.finishCachePromotionLocked(key)
		})
	}
}
