package relay

import (
	"context"
	"encoding/json"
	"sort"
	"time"
)

func (s *Server) restoreAgentCacheFollowers(ctx context.Context, agent *peer) {
	type candidate struct {
		key      cacheKey
		watching bool
		lastUsed time.Time
	}
	s.mu.Lock()
	candidates := make([]candidate, 0)
	for key, entry := range s.cache.entries {
		if key.deviceID != agent.deviceID || len(entry.thread) == 0 {
			continue
		}
		candidates = append(candidates, candidate{key: key, watching: len(entry.watchers) > 0, lastUsed: entry.lastUsed})
		s.cachePromoting[key] = true
	}
	s.mu.Unlock()
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].watching != candidates[j].watching {
			return candidates[i].watching
		}
		return candidates[i].lastUsed.After(candidates[j].lastUsed)
	})
	for _, candidate := range candidates {
		if err := sendCacheTouch(ctx, agent, candidate.key, true); err != nil {
			return
		}
		if !candidate.watching {
			if err := sendCacheTouch(ctx, agent, candidate.key, false); err != nil {
				return
			}
		}
	}
}

func cacheRequestReference(raw json.RawMessage) (threadID, watchID string, ok bool) {
	var params struct {
		ThreadID string `json:"threadId"`
		WatchID  string `json:"watchId"`
	}
	if json.Unmarshal(raw, &params) != nil {
		return "", "", false
	}
	return params.ThreadID, params.WatchID, true
}

func (s *Server) handleCacheRequest(ctx context.Context, web *peer, requestID, deviceID, method string, params json.RawMessage) bool {
	threadID, watchID, ok := cacheRequestReference(params)
	if !ok {
		_ = web.send(ctx, responseError(requestID, "rejected", "INVALID_ARGUMENT", "invalid cache request"))
		return true
	}
	now := s.now()
	s.mu.Lock()
	switch method {
	case "thread.cache.read":
		key := cacheKey{deviceID, threadID}
		entry := s.cache.entries[key]
		agent := s.agents[deviceID]
		data := s.cacheDataLocked(entry, agent)
		s.mu.Unlock()
		_ = web.send(ctx, map[string]any{"type": "response", "v": 1, "requestId": requestID, "outcome": "accepted", "data": data})
		return true
	case "thread.cache.watch":
		count := 0
		for _, entry := range s.cache.entries {
			for _, watcher := range entry.watchers {
				if watcher.web == web {
					count++
				}
			}
		}
		if count >= maxWebSubscriptions {
			s.mu.Unlock()
			_ = web.send(ctx, responseError(requestID, "rejected", "OVERLOADED", "too many active cache watchers"))
			return true
		}
		watchID = randCacheID()
		key := cacheKey{deviceID, threadID}
		firstWatcher := s.cache.entries[key] == nil || len(s.cache.entries[key].watchers) == 0
		entry := s.cache.watch(key, watchID, web, now)
		agent := s.agents[deviceID]
		data := s.cacheDataLocked(entry, agent)
		needsPromotion := data["freshness"] != "fresh"
		if firstWatcher && agent != nil && needsPromotion {
			s.cachePromoting[key] = true
		}
		data["watchId"] = watchID
		s.mu.Unlock()
		_ = web.send(ctx, map[string]any{"type": "response", "v": 1, "requestId": requestID, "outcome": "accepted", "data": data})
		if firstWatcher && agent != nil {
			_ = sendCacheTouch(ctx, agent, key, true)
		}
		return true
	case "thread.cache.unwatch":
		for key, entry := range s.cache.entries {
			watcher, found := entry.watchers[watchID]
			if found && watcher.web == web {
				s.cache.unwatch(key, watchID, now)
				lastWatcher := len(entry.watchers) == 0
				agent := s.agents[key.deviceID]
				s.mu.Unlock()
				_ = web.send(ctx, map[string]any{"type": "response", "v": 1, "requestId": requestID, "outcome": "accepted", "data": map[string]any{}})
				if lastWatcher && agent != nil {
					_ = sendCacheTouch(ctx, agent, key, false)
				}
				return true
			}
		}
		s.mu.Unlock()
		_ = web.send(ctx, responseError(requestID, "rejected", "INVALID_ARGUMENT", "unknown cache watcher"))
		return true
	case "thread.cache.promote":
		key := cacheKey{deviceID, threadID}
		entry := s.cache.entries[key]
		watching := false
		if entry != nil {
			for _, watcher := range entry.watchers {
				watching = watching || watcher.web == web
			}
		}
		if !watching {
			s.mu.Unlock()
			_ = web.send(ctx, responseError(requestID, "rejected", "INVALID_ARGUMENT", "cache promotion requires an active watcher"))
			return true
		}
		agent := s.agents[deviceID]
		if agent == nil {
			s.mu.Unlock()
			_ = web.send(ctx, responseError(requestID, "rejected", "DEVICE_OFFLINE", "device offline"))
			return true
		}
		already := s.cachePromoting[key]
		if !already {
			s.cachePromoting[key] = true
		}
		s.mu.Unlock()
		if !already {
			if err := sendCacheTouch(ctx, agent, key, true); err != nil {
				s.mu.Lock()
				delete(s.cachePromoting, key)
				s.mu.Unlock()
				_ = web.send(ctx, responseError(requestID, "rejected", "DEVICE_OFFLINE", "cache promotion could not reach agent"))
				return true
			}
		}
		_ = web.send(ctx, map[string]any{"type": "response", "v": 1, "requestId": requestID, "outcome": "accepted", "data": map[string]any{"promoting": !already}})
		return true
	default:
		s.mu.Unlock()
		return false
	}
}

func sendCacheTouch(ctx context.Context, agent *peer, key cacheKey, watching bool) error {
	return agent.send(ctx, map[string]any{"type": "request", "v": 1, "requestId": newRequestID(), "deviceId": key.deviceID, "method": "thread.cache.touch", "params": map[string]any{"threadId": key.threadID, "watching": watching}})
}

func randCacheID() string { return "watch-" + newRequestID() }

func (s *Server) cacheDataLocked(entry *cacheEntry, agent *peer) map[string]any {
	data := map[string]any{"freshness": "missing", "agentOnline": agent != nil}
	if agent != nil {
		data["agentEpoch"] = agent.agentEpoch
	}
	if entry == nil || len(entry.thread) == 0 {
		// An empty entry can still carry the terminal result of a failed
		// promotion. Preserve it so Web can stop the background-sync spinner
		// instead of treating every empty cache as indefinitely pending.
		if entry != nil && entry.staleReason != "" {
			data["staleReason"] = entry.staleReason
		}
		return data
	}
	fresh := agent != nil && agent.agentEpoch == entry.agentEpoch && !entry.stale
	if fresh {
		data["freshness"] = "fresh"
	} else {
		data["freshness"] = "stale"
		reason := entry.staleReason
		if reason == "" {
			reason = "agent_epoch_changed"
		}
		data["staleReason"] = reason
	}
	data["agentEpoch"] = entry.agentEpoch
	data["cacheStreamId"] = entry.streamID
	data["seq"] = entry.seq
	data["capturedAt"] = entry.capturedAt.UTC().Format(time.RFC3339Nano)
	data["thread"] = json.RawMessage(entry.thread)
	return data
}

func (s *Server) cacheUpdateMessagesLocked(key cacheKey) []cacheDelivery {
	entry := s.cache.entries[key]
	if entry == nil {
		return nil
	}
	agent := s.agents[key.deviceID]
	base := s.cacheDataLocked(entry, agent)
	deliveries := make([]cacheDelivery, 0, len(entry.watchers))
	for _, watcher := range entry.watchers {
		msg := map[string]any{"type": "event", "v": 1, "event": "thread.cache.update", "deviceId": key.deviceID, "threadId": key.threadID, "watchId": watcher.id}
		for name, value := range base {
			msg[name] = value
		}
		deliveries = append(deliveries, cacheDelivery{web: watcher.web, message: msg})
	}
	return deliveries
}

type cacheDelivery struct {
	web     *peer
	message map[string]any
}

func sendCacheDeliveries(ctx context.Context, deliveries []cacheDelivery) {
	for _, delivery := range deliveries {
		_ = delivery.web.send(ctx, delivery.message)
	}
}

func (s *Server) handleCacheAgentEvent(ctx context.Context, agent *peer, body []byte) bool {
	var envelope struct {
		Event         string          `json:"event"`
		DeviceID      string          `json:"deviceId"`
		ThreadID      string          `json:"threadId"`
		AgentEpoch    string          `json:"agentEpoch"`
		CacheStreamID string          `json:"cacheStreamId"`
		Seq           int64           `json:"seq"`
		CapturedAt    time.Time       `json:"capturedAt"`
		PayloadBytes  int64           `json:"payloadBytes"`
		Thread        json.RawMessage `json:"thread"`
		Freshness     string          `json:"freshness"`
		Reason        string          `json:"reason"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.DeviceID != agent.deviceID || envelope.AgentEpoch != agent.agentEpoch {
		return false
	}
	key := cacheKey{agent.deviceID, envelope.ThreadID}
	s.mu.Lock()
	var deliveries []cacheDelivery
	switch envelope.Event {
	case "thread.cache.snapshot":
		err := s.cache.put(cacheSnapshot{deviceID: agent.deviceID, threadID: envelope.ThreadID, agentEpoch: envelope.AgentEpoch, streamID: envelope.CacheStreamID, seq: envelope.Seq, capturedAt: envelope.CapturedAt, thread: envelope.Thread, bytes: envelope.PayloadBytes}, s.now())
		delete(s.cachePromoting, key)
		if err != nil {
			if entry := s.cache.entries[key]; entry != nil {
				entry.stale = true
				entry.staleReason = err.Error()
			}
		}
		deliveries = s.cacheUpdateMessagesLocked(key)
	case "thread.cache.status":
		delete(s.cachePromoting, key)
		if entry := s.cache.entries[key]; entry != nil {
			entry.stale = envelope.Freshness != "fresh"
			entry.staleReason = envelope.Reason
		}
		deliveries = s.cacheUpdateMessagesLocked(key)
	case "thread.cache.remove":
		delete(s.cachePromoting, key)
		if entry := s.cache.entries[key]; entry != nil {
			s.cache.totalBytes -= entry.retainedBytes
			entry.thread = nil
			entry.bytes = 0
			entry.retainedBytes = 0
			entry.stale = true
			entry.staleReason = "removed"
		}
		deliveries = s.cacheUpdateMessagesLocked(key)
	default:
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()
	sendCacheDeliveries(ctx, deliveries)
	return true
}

func isCacheEvent(body []byte) bool {
	var envelope struct {
		Event string `json:"event"`
	}
	return json.Unmarshal(body, &envelope) == nil && (envelope.Event == "thread.cache.snapshot" || envelope.Event == "thread.cache.status" || envelope.Event == "thread.cache.remove")
}
