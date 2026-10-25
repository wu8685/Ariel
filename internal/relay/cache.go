package relay

import (
	"encoding/json"
	"errors"
	"sort"
	"time"
)

var (
	errCacheCapacity  = errors.New("CACHE_CAPACITY")
	errCacheOversized = errors.New("CACHE_OVERSIZED")
	errCacheSequence  = errors.New("CACHE_SEQUENCE")
)

const cacheStaleTTL = 24 * time.Hour
const cacheEntryMetadataBytes int64 = 512

type cacheKey struct {
	deviceID string
	threadID string
}

type cacheSnapshot struct {
	deviceID   string
	threadID   string
	agentEpoch string
	streamID   string
	seq        int64
	capturedAt time.Time
	thread     json.RawMessage
	bytes      int64
}

type cacheWatcher struct {
	id  string
	web *peer
}

type cacheEntry struct {
	cacheSnapshot
	watchers        map[string]cacheWatcher
	lastUsed        time.Time
	warmUntil       time.Time
	stale           bool
	staleReason     string
	leaseValidUntil time.Time
	retainedBytes   int64
}

type cacheStore struct {
	maxBytes      int64
	maxEntryBytes int64
	warmGrace     time.Duration
	freshness     time.Duration
	totalBytes    int64
	entries       map[cacheKey]*cacheEntry
}

func newCacheStore(maxBytes, maxEntryBytes int64, warmGrace, freshness time.Duration) *cacheStore {
	return &cacheStore{maxBytes: maxBytes, maxEntryBytes: maxEntryBytes, warmGrace: warmGrace, freshness: freshness, entries: map[cacheKey]*cacheEntry{}}
}

func (s *cacheStore) watch(key cacheKey, watchID string, web *peer, now time.Time) *cacheEntry {
	s.pruneExpired(now)
	entry := s.entries[key]
	if entry == nil {
		entry = &cacheEntry{cacheSnapshot: cacheSnapshot{deviceID: key.deviceID, threadID: key.threadID}, watchers: map[string]cacheWatcher{}}
		s.entries[key] = entry
	}
	entry.watchers[watchID] = cacheWatcher{id: watchID, web: web}
	entry.lastUsed = now
	entry.warmUntil = time.Time{}
	return entry
}

func (s *cacheStore) unwatch(key cacheKey, watchID string, now time.Time) bool {
	entry := s.entries[key]
	if entry == nil {
		return false
	}
	if _, ok := entry.watchers[watchID]; !ok {
		return false
	}
	delete(entry.watchers, watchID)
	entry.lastUsed = now
	if len(entry.watchers) == 0 {
		entry.warmUntil = now.Add(s.warmGrace)
		if len(entry.thread) == 0 {
			delete(s.entries, key)
		}
	}
	return true
}

func (s *cacheStore) removeWeb(web *peer, now time.Time) []cacheKey {
	var released []cacheKey
	for key, entry := range s.entries {
		hadWatchers := len(entry.watchers) > 0
		for id, watcher := range entry.watchers {
			if watcher.web == web {
				delete(entry.watchers, id)
			}
		}
		if len(entry.watchers) == 0 {
			if hadWatchers {
				released = append(released, key)
			}
			entry.warmUntil = now.Add(s.warmGrace)
			if len(entry.thread) == 0 {
				delete(s.entries, key)
			}
		}
	}
	return released
}

func (s *cacheStore) put(snapshot cacheSnapshot, now time.Time) error {
	s.pruneExpired(now)
	if snapshot.bytes <= 0 {
		snapshot.bytes = int64(len(snapshot.thread))
	}
	if snapshot.bytes != int64(len(snapshot.thread)) || snapshot.bytes > s.maxEntryBytes {
		return errCacheOversized
	}
	key := cacheKey{snapshot.deviceID, snapshot.threadID}
	prior := s.entries[key]
	if prior != nil && prior.streamID == snapshot.streamID && prior.agentEpoch == snapshot.agentEpoch && snapshot.seq <= prior.seq {
		return errCacheSequence
	}
	priorBytes := int64(0)
	if prior != nil {
		priorBytes = prior.retainedBytes
	}
	retainedBytes := snapshot.bytes + cacheEntryMetadataBytes
	needed := retainedBytes - priorBytes
	if needed > 0 && !s.makeRoom(needed, key, now) {
		return errCacheCapacity
	}
	if prior == nil {
		prior = &cacheEntry{watchers: map[string]cacheWatcher{}}
		s.entries[key] = prior
	}
	s.totalBytes += retainedBytes - prior.retainedBytes
	prior.cacheSnapshot = snapshot
	prior.retainedBytes = retainedBytes
	prior.lastUsed = now
	prior.stale = false
	prior.staleReason = ""
	prior.leaseValidUntil = now.Add(s.freshness)
	return nil
}

func (s *cacheStore) pruneExpired(now time.Time) {
	for key, entry := range s.entries {
		if !entry.stale || len(entry.watchers) > 0 || now.Sub(entry.lastUsed) < cacheStaleTTL {
			continue
		}
		s.totalBytes -= entry.retainedBytes
		delete(s.entries, key)
	}
}

func (s *cacheStore) makeRoom(needed int64, preserve cacheKey, now time.Time) bool {
	if s.totalBytes+needed <= s.maxBytes {
		return true
	}
	type candidate struct {
		key   cacheKey
		entry *cacheEntry
		rank  int
	}
	candidates := make([]candidate, 0, len(s.entries))
	for key, entry := range s.entries {
		if key == preserve || len(entry.watchers) > 0 || (!entry.warmUntil.IsZero() && now.Before(entry.warmUntil)) {
			continue
		}
		rank := 1
		if entry.stale {
			rank = 0
		}
		candidates = append(candidates, candidate{key: key, entry: entry, rank: rank})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].rank != candidates[j].rank {
			return candidates[i].rank < candidates[j].rank
		}
		return candidates[i].entry.lastUsed.Before(candidates[j].entry.lastUsed)
	})
	for _, candidate := range candidates {
		s.totalBytes -= candidate.entry.retainedBytes
		delete(s.entries, candidate.key)
		if s.totalBytes+needed <= s.maxBytes {
			return true
		}
	}
	return false
}

func (s *cacheStore) markDeviceStale(deviceID, reason string) {
	for key, entry := range s.entries {
		if key.deviceID == deviceID && len(entry.thread) > 0 {
			if entry.stale && !cacheLeaseRestorable(entry.staleReason) {
				continue
			}
			entry.stale = true
			entry.staleReason = reason
		}
	}
}

func cacheLeaseRestorable(reason string) bool {
	switch reason {
	case "", "agent_offline", "follower_lease_expired":
		return true
	default:
		return false
	}
}

func (s *cacheStore) removeDevice(deviceID string) {
	for key, entry := range s.entries {
		if key.deviceID == deviceID {
			s.totalBytes -= entry.retainedBytes
			delete(s.entries, key)
		}
	}
}
