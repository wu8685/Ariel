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
		active   bool
		lastUsed time.Time
	}
	s.mu.Lock()
	candidates := make([]candidate, 0)
	for key, entry := range s.cache.entries {
		active := cacheThreadActive(entry.thread)
		// A Web client may install its watcher just before the Agent finishes
		// reconnecting. Even when no snapshot exists yet, that watcher is demand
		// for a follower and must be promoted once the Agent becomes available.
		if key.deviceID != agent.deviceID || (len(entry.watchers) == 0 && !active) {
			continue
		}
		candidates = append(candidates, candidate{key: key, watching: len(entry.watchers) > 0, active: active, lastUsed: entry.lastUsed})
		s.beginCachePromotionLocked(key)
	}
	s.mu.Unlock()
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].active != candidates[j].active {
			return candidates[i].active
		}
		if candidates[i].watching != candidates[j].watching {
			return candidates[i].watching
		}
		return candidates[i].lastUsed.After(candidates[j].lastUsed)
	})
	for _, candidate := range candidates {
		requestID := s.promotionRequestID(candidate.key)
		if err := sendCacheTouch(ctx, agent, candidate.key, true, requestID); err != nil {
			s.mu.Lock()
			s.finishCachePromotionLocked(candidate.key)
			s.mu.Unlock()
			return
		}
	}
}

func cacheThreadActive(raw json.RawMessage) bool {
	var thread struct {
		Runtime string `json:"runtime"`
	}
	if json.Unmarshal(raw, &thread) != nil {
		return false
	}
	switch thread.Runtime {
	case "running", "processing", "inProgress":
		return true
	}
	return false
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
		promotionID := ""
		if firstWatcher && agent != nil && needsPromotion {
			promotionID = s.beginCachePromotionLocked(key)
		}
		data["watchId"] = watchID
		s.mu.Unlock()
		_ = web.send(ctx, map[string]any{"type": "response", "v": 1, "requestId": requestID, "outcome": "accepted", "data": data})
		if promotionID != "" {
			if err := sendCacheTouch(ctx, agent, key, true, promotionID); err != nil {
				s.failCachePromotion(key, "DEVICE_OFFLINE")
			}
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
					_ = sendCacheTouch(ctx, agent, key, false, newRequestID())
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
		promotionID := ""
		if !already {
			promotionID = s.beginCachePromotionLocked(key)
		}
		s.mu.Unlock()
		if !already {
			if err := sendCacheTouch(ctx, agent, key, true, promotionID); err != nil {
				s.failCachePromotion(key, "DEVICE_OFFLINE")
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

func sendCacheTouch(ctx context.Context, agent *peer, key cacheKey, watching bool, requestID string) error {
	if requestID == "" {
		requestID = newRequestID()
	}
	return agent.send(ctx, map[string]any{"type": "request", "v": 1, "requestId": requestID, "deviceId": key.deviceID, "method": "thread.cache.touch", "params": map[string]any{"threadId": key.threadID, "watching": watching}})
}

func (s *Server) beginCachePromotionLocked(key cacheKey) string {
	if s.cachePromoting[key] {
		return ""
	}
	requestID := newRequestID()
	s.cachePromoting[key] = true
	s.cachePromotionIDs[key] = requestID
	s.cachePromotionRequests[requestID] = key
	time.AfterFunc(s.cfg.SubscriptionTimeout, func() {
		s.mu.Lock()
		if s.cachePromotionIDs[key] != requestID {
			s.mu.Unlock()
			return
		}
		s.finishCachePromotionLocked(key)
		if entry := s.cache.entries[key]; entry != nil {
			entry.stale = true
			entry.staleReason = "TIMEOUT"
		}
		deliveries := s.cacheUpdateMessagesLocked(key)
		s.mu.Unlock()
		sendCacheDeliveries(context.Background(), deliveries)
	})
	return requestID
}

func (s *Server) promotionRequestID(key cacheKey) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cachePromotionIDs[key]
}

func (s *Server) finishCachePromotionLocked(key cacheKey) {
	if requestID := s.cachePromotionIDs[key]; requestID != "" {
		delete(s.cachePromotionRequests, requestID)
	}
	delete(s.cachePromotionIDs, key)
	delete(s.cachePromoting, key)
}

func (s *Server) failCachePromotion(key cacheKey, reason string) {
	s.mu.Lock()
	s.finishCachePromotionLocked(key)
	if entry := s.cache.entries[key]; entry != nil {
		entry.stale = true
		entry.staleReason = reason
	}
	deliveries := s.cacheUpdateMessagesLocked(key)
	s.mu.Unlock()
	sendCacheDeliveries(context.Background(), deliveries)
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
	fresh := agent != nil && agent.agentEpoch == entry.agentEpoch && !entry.stale && s.now().Before(entry.leaseValidUntil)
	if fresh {
		data["freshness"] = "fresh"
	} else {
		data["freshness"] = "stale"
		reason := entry.staleReason
		if reason == "" {
			if agent != nil && agent.agentEpoch == entry.agentEpoch && !s.now().Before(entry.leaseValidUntil) {
				reason = "follower_lease_expired"
			} else {
				reason = "agent_epoch_changed"
			}
		}
		data["staleReason"] = reason
	}
	data["agentEpoch"] = entry.agentEpoch
	data["cacheStreamId"] = entry.streamID
	data["seq"] = entry.seq
	data["capturedAt"] = entry.capturedAt.UTC().Format(time.RFC3339Nano)
	data["leaseValidUntil"] = entry.leaseValidUntil.UTC().Format(time.RFC3339Nano)
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
		deliveries = append(deliveries, cacheDelivery{web: watcher.web, key: watcher.id, message: msg})
	}
	return deliveries
}

type cacheDelivery struct {
	web     *peer
	key     string
	message map[string]any
}

func sendCacheDeliveries(ctx context.Context, deliveries []cacheDelivery) {
	_ = ctx
	for _, delivery := range deliveries {
		delivery.web.enqueueCacheDelivery(delivery.key, delivery.message)
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
	if s.agents[agent.deviceID] != agent {
		// The connection was already replaced by a newer authenticated
		// transport. A stable process-level agentEpoch intentionally survives
		// reconnects, so transport identity is the final fence for late frames.
		s.mu.Unlock()
		return true
	}
	var deliveries []cacheDelivery
	switch envelope.Event {
	case "thread.cache.snapshot":
		err := s.cache.put(cacheSnapshot{deviceID: agent.deviceID, threadID: envelope.ThreadID, agentEpoch: envelope.AgentEpoch, streamID: envelope.CacheStreamID, seq: envelope.Seq, capturedAt: envelope.CapturedAt, thread: envelope.Thread, bytes: envelope.PayloadBytes}, s.now())
		s.finishCachePromotionLocked(key)
		if err != nil {
			if entry := s.cache.entries[key]; entry != nil {
				entry.stale = true
				entry.staleReason = err.Error()
			}
		}
		deliveries = s.cacheUpdateMessagesLocked(key)
	case "thread.cache.status":
		s.finishCachePromotionLocked(key)
		if entry := s.cache.entries[key]; entry != nil {
			entry.stale = envelope.Freshness != "fresh"
			entry.staleReason = envelope.Reason
			if envelope.Freshness == "fresh" {
				entry.leaseValidUntil = s.now().Add(s.cfg.CacheFreshness)
			}
		}
		deliveries = s.cacheUpdateMessagesLocked(key)
	case "thread.cache.remove":
		s.finishCachePromotionLocked(key)
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
