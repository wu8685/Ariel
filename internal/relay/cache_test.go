package relay

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

func cacheFixture(threadID string, textBytes int) cacheSnapshot {
	thread := map[string]any{
		"threadId": threadID, "title": threadID, "cwd": "/fixture", "updatedAt": "2026-10-25T00:00:00Z",
		"runtime": "idle", "pendingInteractions": []any{},
		"turns": []any{
			map[string]any{
				"turnId": "turn-1", "status": "completed",
				"items": []any{map[string]any{"itemId": "item-1", "role": "assistant", "text": string(make([]byte, textBytes))}},
			},
		},
	}
	body, _ := json.Marshal(thread)
	return cacheSnapshot{deviceID: "device-a", threadID: threadID, agentEpoch: "epoch-a", streamID: "stream-" + threadID, seq: 1, capturedAt: time.Unix(1, 0), thread: body, bytes: int64(len(body))}
}

func TestCacheStoreKeepsFiveDistinctWatchedThreadsWithoutCountThrash(t *testing.T) {
	store := newCacheStore(1<<20, 1<<20, time.Minute)
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("thread-%d", i)
		store.watch(cacheKey{"device-a", id}, fmt.Sprintf("watch-%d", i), nil, time.Unix(1, 0))
		if err := store.put(cacheFixture(id, 32), time.Unix(2, 0)); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
	}
	if len(store.entries) != 5 {
		t.Fatalf("fixed-count eviction remains: got %d entries", len(store.entries))
	}
	for key, entry := range store.entries {
		if len(entry.watchers) != 1 || len(entry.thread) == 0 {
			t.Fatalf("cache entry %v was copied or lost: watchers=%d bytes=%d", key, len(entry.watchers), len(entry.thread))
		}
	}
}

func TestCacheStoreEvictsUnwatchedLRUByBytesAndNeverUsesWatchedEntryAsPressureValve(t *testing.T) {
	first := cacheFixture("first", 200)
	second := cacheFixture("second", 200)
	budget := first.bytes + second.bytes + 2*cacheEntryMetadataBytes - 1
	store := newCacheStore(budget, 1<<20, 0)
	if err := store.put(first, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := store.put(second, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.entries[cacheKey{"device-a", "first"}]; ok {
		t.Fatal("oldest unwatched entry was not evicted")
	}
	store.watch(cacheKey{"device-a", "second"}, "watch", nil, time.Unix(3, 0))
	third := cacheFixture("third", 200)
	if err := store.put(third, time.Unix(4, 0)); !errors.Is(err, errCacheCapacity) {
		t.Fatalf("wanted CACHE_CAPACITY with all resident bytes watched, got %v", err)
	}
	if _, ok := store.entries[cacheKey{"device-a", "second"}]; !ok {
		t.Fatal("watched entry was evicted")
	}
}

func TestCacheStoreRejectsOversizedAndNonMonotonicSnapshots(t *testing.T) {
	large := cacheFixture("large", 256)
	first := cacheFixture("thread", 1)
	store := newCacheStore(1<<20, first.bytes, time.Minute)
	if err := store.put(large, time.Now()); !errors.Is(err, errCacheOversized) {
		t.Fatalf("oversized: %v", err)
	}
	if err := store.put(first, time.Now()); err != nil {
		t.Fatal(err)
	}
	first.seq = 1
	if err := store.put(first, time.Now()); !errors.Is(err, errCacheSequence) {
		t.Fatalf("sequence: %v", err)
	}
}

func TestCacheStoreExpiresOnlyUnwatchedStaleBodiesAfterTwentyFourHours(t *testing.T) {
	store := newCacheStore(1<<20, 1<<20, time.Minute)
	now := time.Unix(1, 0)
	stale := cacheFixture("stale", 32)
	if err := store.put(stale, now); err != nil {
		t.Fatal(err)
	}
	store.markDeviceStale("device-a", "offline")
	store.pruneExpired(now.Add(cacheStaleTTL - time.Second))
	if len(store.entries) != 1 {
		t.Fatal("stale cache expired before TTL")
	}
	store.pruneExpired(now.Add(cacheStaleTTL + time.Second))
	if len(store.entries) != 0 || store.totalBytes != 0 {
		t.Fatal("expired stale cache body remained resident")
	}
}
