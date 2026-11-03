package relay

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"sync"
	"time"
)

const (
	puckMaxInFlight = 32
	puckEventBuffer = 128
)

type Origin struct {
	Kind        string `json:"kind"`
	AssistantID string `json:"assistantId"`
	Trigger     string `json:"trigger"`
}

type PuckError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type PuckResponse struct {
	Outcome string
	Data    map[string]any
	Error   *PuckError
}

type BrainThread struct {
	DeviceID string
	ThreadID string
}

type BrainEvent struct {
	DeviceID    string
	ThreadID    string
	AssistantID string
	Name        string
	Payload     json.RawMessage
}

type PuckBridge struct {
	server *Server
	sem    chan struct{}
	events chan BrainEvent
	device chan map[string]any

	mu           sync.RWMutex
	brainThreads map[BrainThread]string
	quotaMu      sync.Mutex
	writes       []time.Time
	now          func() time.Time
}

func (b *PuckBridge) Watch(ctx context.Context, origin Origin, deviceID, threadID string) (<-chan map[string]any, func(), *PuckError) {
	if origin.Kind != "puck" || origin.AssistantID == "" {
		return nil, nil, &PuckError{Code: "UNAUTHORIZED", Message: "invalid Puck origin"}
	}
	if _, brain := b.assistantFor(deviceID, threadID); brain {
		return nil, nil, &PuckError{Code: "RECURSION_DENIED", Message: "brain thread cannot be watched"}
	}
	events := make(chan map[string]any, 8)
	watchID := "puck-" + newRequestID()
	key := cacheKey{deviceID: deviceID, threadID: threadID}
	b.server.mu.Lock()
	count := 0
	for _, entry := range b.server.cache.entries {
		for _, watcher := range entry.watchers {
			if watcher.puck != nil {
				count++
			}
		}
	}
	if count >= 256 {
		b.server.mu.Unlock()
		return nil, nil, &PuckError{Code: "OVERLOADED", Message: "too many Puck watches"}
	}
	first := b.server.cache.entries[key] == nil || len(b.server.cache.entries[key].watchers) == 0
	entry := b.server.cache.watchPuck(key, watchID, events, b.server.now())
	agent := b.server.agents[deviceID]
	data := b.server.cacheDataLocked(entry, agent)
	needsPromotion := data["freshness"] != "fresh"
	promotionID := ""
	if first && agent != nil && needsPromotion {
		promotionID = b.server.beginCachePromotionLocked(key)
	}
	b.server.mu.Unlock()
	data["type"], data["v"], data["event"], data["deviceId"], data["threadId"], data["watchId"] = "event", 1, "thread.cache.update", deviceID, threadID, watchID
	events <- data
	if promotionID != "" {
		if err := sendCacheTouch(ctx, agent, key, true, promotionID); err != nil {
			b.server.failCachePromotion(key, promotionID, "DEVICE_OFFLINE")
		}
	}
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.server.mu.Lock()
			entry := b.server.cache.entries[key]
			removed := b.server.cache.unwatch(key, watchID, b.server.now())
			last := removed && (entry == nil || len(entry.watchers) == 0)
			agent := b.server.agents[deviceID]
			b.server.mu.Unlock()
			if last && agent != nil {
				_ = sendCacheTouch(context.Background(), agent, key, false, newRequestID())
			}
			close(events)
		})
	}
	go func() { <-ctx.Done(); cancel() }()
	return events, cancel, nil
}

func newPuckBridge(server *Server) *PuckBridge {
	return &PuckBridge{server: server, sem: make(chan struct{}, puckMaxInFlight), events: make(chan BrainEvent, puckEventBuffer), device: make(chan map[string]any, 64), brainThreads: map[BrainThread]string{}, now: time.Now}
}

func (s *Server) PuckBridge() *PuckBridge { return s.puckBridge }

func (b *PuckBridge) BrainEvents() <-chan BrainEvent { return b.events }

func (b *PuckBridge) DeviceStatus() <-chan map[string]any { return b.device }

func (b *PuckBridge) BrainThreads(threads map[BrainThread]string) {
	copy := make(map[BrainThread]string, len(threads))
	for key, assistantID := range threads {
		copy[key] = assistantID
	}
	b.mu.Lock()
	b.brainThreads = copy
	b.mu.Unlock()
}

func (b *PuckBridge) assistantFor(deviceID, threadID string) (string, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	assistantID, ok := b.brainThreads[BrainThread{DeviceID: deviceID, ThreadID: threadID}]
	return assistantID, ok
}

func (b *PuckBridge) Devices() []map[string]any {
	b.server.mu.Lock()
	defer b.server.mu.Unlock()
	devices := make([]map[string]any, 0, len(b.server.agents))
	for _, agent := range b.server.agents {
		devices = append(devices, map[string]any{"deviceId": agent.deviceID, "deviceName": agent.deviceName, "agentOnline": true, "codexReady": agent.capabilities["codexReady"], "capabilities": agent.currentCapabilities(), "agentEpoch": agent.agentEpoch})
	}
	return devices
}

func (b *PuckBridge) Forward(ctx context.Context, origin Origin, deviceID, method string, params json.RawMessage) PuckResponse {
	if origin.Kind != "puck" || origin.AssistantID == "" || !puckMethodAllowed(method) {
		return puckRejected("UNAUTHORIZED", "Puck method is not allowed")
	}
	var reference struct {
		ThreadID string `json:"threadId"`
	}
	_ = json.Unmarshal(params, &reference)
	if assistantID, brain := b.assistantFor(deviceID, reference.ThreadID); brain && (!isPuckBrainMethod(method) || assistantID != origin.AssistantID) {
		return puckRejected("RECURSION_DENIED", "brain thread cannot be used as an execution thread")
	}
	if method == "interaction.respond" && !b.server.consumePuckDecision(params) {
		return puckRejected("UNAUTHORIZED", "interaction response requires a current user decision")
	}
	if puckWriteMethod(method) && !b.takeWriteQuota() {
		return puckRejected("OVERLOADED", "Puck write quota exceeded")
	}
	select {
	case b.sem <- struct{}{}:
		defer func() { <-b.sem }()
	default:
		return puckRejected("OVERLOADED", "too many Puck requests")
	}
	return b.server.forwardPuck(ctx, origin, deviceID, method, params)
}

func puckDecisionDigest(params json.RawMessage) (string, [32]byte, bool) {
	var input struct {
		InteractionID  string         `json:"interactionId"`
		Decision       string         `json:"decision"`
		Answers        map[string]any `json:"answers"`
		UserDecisionID string         `json:"userDecisionId"`
	}
	if json.Unmarshal(params, &input) != nil || input.InteractionID == "" || input.Decision == "" {
		return "", [32]byte{}, false
	}
	canonical, _ := json.Marshal(struct {
		InteractionID string         `json:"interactionId"`
		Decision      string         `json:"decision"`
		Answers       map[string]any `json:"answers,omitempty"`
	}{input.InteractionID, input.Decision, input.Answers})
	return input.UserDecisionID, sha256.Sum256(canonical), true
}

func (s *Server) recordPuckDecision(ctx context.Context, web *peer, requestID string, params json.RawMessage) {
	_, digest, ok := puckDecisionDigest(params)
	if !ok {
		_ = web.send(ctx, responseError(requestID, "rejected", "INVALID_ARGUMENT", "invalid user decision"))
		return
	}
	id := newRequestID()
	s.mu.Lock()
	now := s.now()
	for existing, decision := range s.puckDecisions {
		if !now.Before(decision.expiresAt) {
			delete(s.puckDecisions, existing)
		}
	}
	s.puckDecisions[id] = puckDecision{digest: digest, expiresAt: now.Add(10 * time.Minute)}
	s.mu.Unlock()
	_ = web.send(ctx, map[string]any{"type": "response", "v": 1, "requestId": requestID, "outcome": "accepted", "data": map[string]any{"userDecisionId": id, "expiresAt": now.Add(10 * time.Minute).UTC().Format(time.RFC3339Nano)}})
}

func (s *Server) consumePuckDecision(params json.RawMessage) bool {
	id, digest, ok := puckDecisionDigest(params)
	if !ok || id == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	decision, exists := s.puckDecisions[id]
	if !exists || !s.now().Before(decision.expiresAt) || decision.digest != digest {
		return false
	}
	delete(s.puckDecisions, id)
	return true
}

func (b *PuckBridge) takeWriteQuota() bool {
	b.quotaMu.Lock()
	defer b.quotaMu.Unlock()
	cutoff := b.now().Add(-time.Minute)
	kept := b.writes[:0]
	for _, at := range b.writes {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	b.writes = kept
	if len(b.writes) >= 120 {
		return false
	}
	b.writes = append(b.writes, b.now())
	return true
}

func puckWriteMethod(method string) bool {
	switch method {
	case "thread.create", "turn.start", "turn.interrupt", "interaction.respond", "puck.brain.ensure", "puck.brain.turn.start", "puck.brain.turn.interrupt", "puck.brain.toolResult", "puck.brain.retire":
		return true
	default:
		return false
	}
}

func puckMethodAllowed(method string) bool {
	switch method {
	case "device.list", "thread.list", "thread.read", "thread.history", "thread.cache.read", "thread.cache.watch", "thread.cache.unwatch", "thread.cache.promote", "thread.cache.touch", "thread.create", "turn.start", "turn.interrupt", "interaction.respond",
		"puck.brain.ensure", "puck.brain.turn.start", "puck.brain.turn.interrupt", "puck.brain.toolResult", "puck.brain.status", "puck.brain.retire":
		return true
	default:
		return false
	}
}

func isPuckBrainMethod(method string) bool {
	return len(method) > len("puck.brain.") && method[:len("puck.brain.")] == "puck.brain."
}

func puckRejected(code, message string) PuckResponse {
	return PuckResponse{Outcome: "rejected", Error: &PuckError{Code: code, Message: message}}
}

func decodePuckResponse(response map[string]any) PuckResponse {
	result := PuckResponse{Outcome: stringValue(response["outcome"])}
	result.Data, _ = response["data"].(map[string]any)
	if raw, ok := response["error"].(map[string]any); ok {
		result.Error = &PuckError{Code: stringValue(raw["code"]), Message: stringValue(raw["message"])}
	}
	return result
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func (b *PuckBridge) routeBrainEvent(deviceID string, body []byte) bool {
	var envelope struct {
		Event    string `json:"event"`
		ThreadID string `json:"threadId"`
	}
	if json.Unmarshal(body, &envelope) != nil || !isPuckBrainMethod(envelope.Event) {
		return false
	}
	assistantID, ok := b.assistantFor(deviceID, envelope.ThreadID)
	if !ok {
		return true
	}
	event := BrainEvent{DeviceID: deviceID, ThreadID: envelope.ThreadID, AssistantID: assistantID, Name: envelope.Event, Payload: append(json.RawMessage(nil), body...)}
	select {
	case b.events <- event:
	default:
	}
	return true
}

func (b *PuckBridge) routeDeviceStatus(status map[string]any) {
	copy := make(map[string]any, len(status))
	for key, value := range status {
		copy[key] = value
	}
	select {
	case b.device <- copy:
	default:
	}
}

func (s *Server) forwardPuck(ctx context.Context, origin Origin, deviceID, method string, params json.RawMessage) PuckResponse {
	s.mu.Lock()
	agent := s.agents[deviceID]
	if agent == nil {
		s.mu.Unlock()
		return puckRejected("DEVICE_OFFLINE", "device offline")
	}
	if isPuckBrainMethod(method) && !agent.capabilities["puckBrain"] {
		s.mu.Unlock()
		return puckRejected("FEATURE_DISABLED", "Puck brain is disabled on target device")
	}
	relayID := newRequestID()
	result := make(chan map[string]any, 1)
	entry := &route{agent: agent, method: method, deviceID: deviceID, internal: result}
	s.routes[relayID] = entry
	entry.timer = time.AfterFunc(s.cfg.MutationTimeout, func() {
		s.finishRoute(relayID, agent, responseError(relayID, "unknown", "OUTCOME_UNKNOWN", "agent response timeout"))
	})
	s.mu.Unlock()
	request := map[string]any{"type": "request", "v": 1, "requestId": relayID, "deviceId": deviceID, "method": method, "params": json.RawMessage(params), "origin": origin}
	if err := agent.send(ctx, request); err != nil {
		s.finishRoute(relayID, agent, responseError(relayID, "unknown", "OUTCOME_UNKNOWN", "agent send outcome unknown"))
	}
	select {
	case response := <-result:
		return decodePuckResponse(response)
	case <-ctx.Done():
		s.finishRoute(relayID, agent, responseError(relayID, "unknown", "OUTCOME_UNKNOWN", "request cancelled"))
		return puckRejected("OUTCOME_UNKNOWN", "request cancelled")
	}
}
