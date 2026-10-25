package relay

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/wu8685/Ariel/internal/wsheartbeat"
	"github.com/wu8685/Ariel/protocol"
)

type Config struct {
	TasksEnabled        bool
	TaskDevices         map[string]TaskDeviceCredential
	Token               string
	WebPIN              string
	WebAuthMode         string
	WebSessionVerifier  func(*http.Request) bool
	AllowedOrigins      []string
	HelloTimeout        time.Duration
	MaxFrameBytes       int64
	RequestTimeout      time.Duration
	ReadTimeout         time.Duration
	SubscriptionTimeout time.Duration
	MutationTimeout     time.Duration
	HeartbeatInterval   time.Duration
	HeartbeatTimeout    time.Duration
	CacheMaxBytes       int64
	CacheEntryMaxBytes  int64
	CacheWarmGrace      time.Duration
}

const (
	WebAuthPIN     = "pin"
	WebAuthPasskey = "passkey"
)

const maxWebSubscriptions = 32
const maxWebSessions = 32
const maxWebPairings = 8
const webSessionLifetime = 24 * time.Hour
const webPairingLifetime = 2 * time.Minute
const defaultCacheMaxBytes = 512 << 20
const defaultCacheEntryMaxBytes = 8 << 20

type Server struct {
	cfg            Config
	epoch          string
	now            func() time.Time
	origins        map[string]struct{}
	mu             sync.Mutex
	deviceStatusMu sync.Mutex
	webPINFailures int
	webSessions    map[[32]byte]time.Time
	sessionOrder   [][32]byte
	pairings       map[[32]byte]pairing
	pairingByWeb   map[*peer][32]byte
	agents         map[string]*peer
	webs           map[*peer]struct{}
	routes         map[string]*route
	byWeb          map[*peer]map[string]string
	subs           map[*peer]map[string]*subscription
	taskRoutes     map[string]*taskRoute
	cache          *cacheStore
	cachePromoting map[cacheKey]bool
}

type pairing struct {
	creator   *peer
	expiresAt time.Time
}

type route struct {
	web      *peer
	agent    *peer
	webID    string
	method   string
	deviceID string
	timer    *time.Timer
	threadID string
	subID    string
}

type subscription struct {
	web      *peer
	agent    *peer
	deviceID string
	threadID string
	streamID string
}

type peer struct {
	taskState      atomic.Pointer[taskPeerState]
	taskStoreID    string
	taskCredential *TaskDeviceCredential
	conn           *websocket.Conn
	id             string
	role           string
	deviceID       string
	deviceName     string
	agentEpoch     string
	adapterVersion string
	capabilities   map[string]bool
	writeMu        sync.Mutex
}

func New(cfg Config) (*Server, error) {
	if cfg.TasksEnabled {
		if err := validateTaskDevices(cfg.TaskDevices, cfg.Token); err != nil {
			return nil, err
		}
	}
	if cfg.Token == "" || len(cfg.AllowedOrigins) == 0 {
		return nil, errors.New("relay requires token and web origin allowlist")
	}
	if cfg.WebAuthMode == "" {
		cfg.WebAuthMode = WebAuthPIN
	}
	switch cfg.WebAuthMode {
	case WebAuthPIN:
		if !ValidWebPIN(cfg.WebPIN) {
			return nil, errors.New("ARIEL_WEB_PIN must be exactly 6 ASCII digits")
		}
		if cfg.Token == cfg.WebPIN {
			return nil, errors.New("Agent token and Web PIN must differ")
		}
	case WebAuthPasskey:
		if cfg.WebPIN != "" {
			return nil, errors.New("ARIEL_WEB_PIN is forbidden in passkey mode")
		}
		if cfg.WebSessionVerifier == nil {
			return nil, errors.New("passkey mode requires a Web session verifier")
		}
	default:
		return nil, errors.New("unknown Web authentication mode")
	}
	origins := make(map[string]struct{}, len(cfg.AllowedOrigins))
	for _, origin := range cfg.AllowedOrigins {
		if origin == "" {
			return nil, errors.New("empty origin is not allowed")
		}
		origins[origin] = struct{}{}
	}
	if cfg.HelloTimeout <= 0 {
		cfg.HelloTimeout = 5 * time.Second
	}
	maxFrameConfigured := cfg.MaxFrameBytes > 0
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 10 * time.Second
	}
	if cfg.ReadTimeout <= 0 {
		cfg.ReadTimeout = 35 * time.Second
	}
	if cfg.SubscriptionTimeout <= 0 {
		cfg.SubscriptionTimeout = 50 * time.Second
	}
	if cfg.MutationTimeout <= 0 {
		cfg.MutationTimeout = 45 * time.Second
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = wsheartbeat.DefaultInterval
	}
	if cfg.HeartbeatTimeout <= 0 {
		cfg.HeartbeatTimeout = wsheartbeat.DefaultTimeout
	}
	if cfg.CacheMaxBytes <= 0 {
		cfg.CacheMaxBytes = defaultCacheMaxBytes
	}
	if cfg.CacheEntryMaxBytes <= 0 {
		cfg.CacheEntryMaxBytes = defaultCacheEntryMaxBytes
	}
	if !maxFrameConfigured {
		cfg.MaxFrameBytes = cfg.CacheEntryMaxBytes + (1 << 20)
	} else if cfg.MaxFrameBytes <= cfg.CacheEntryMaxBytes {
		return nil, errors.New("WebSocket frame limit must exceed cache entry limit")
	}
	if cfg.CacheWarmGrace <= 0 {
		cfg.CacheWarmGrace = 90 * time.Second
	}
	if cfg.CacheEntryMaxBytes > cfg.CacheMaxBytes {
		return nil, errors.New("cache entry limit exceeds cache budget")
	}
	return &Server{cfg: cfg, epoch: rand.Text(), now: time.Now, origins: origins, webSessions: map[[32]byte]time.Time{}, pairings: map[[32]byte]pairing{}, pairingByWeb: map[*peer][32]byte{}, agents: map[string]*peer{}, webs: map[*peer]struct{}{}, routes: map[string]*route{}, byWeb: map[*peer]map[string]string{}, subs: map[*peer]map[string]*subscription{}, cache: newCacheStore(cfg.CacheMaxBytes, cfg.CacheEntryMaxBytes, cfg.CacheWarmGrace), cachePromoting: map[cacheKey]bool{}}, nil
}

func ValidWebPIN(pin string) bool {
	if len(pin) != 6 {
		return false
	}
	for i := 0; i < len(pin); i++ {
		if pin[i] < '0' || pin[i] > '9' {
			return false
		}
	}
	return true
}

func pairingKey(token string) ([32]byte, bool) {
	if len(token) != 66 || !strings.HasPrefix(token, "p_") {
		return [32]byte{}, false
	}
	if _, err := hex.DecodeString(token[2:]); err != nil {
		return [32]byte{}, false
	}
	return sha256.Sum256([]byte(token)), true
}

func (s *Server) removePairingLocked(key [32]byte) {
	invitation, ok := s.pairings[key]
	if !ok {
		return
	}
	delete(s.pairings, key)
	if current, ok := s.pairingByWeb[invitation.creator]; ok && current == key {
		delete(s.pairingByWeb, invitation.creator)
	}
}

func (s *Server) cleanExpiredPairingsLocked() {
	now := s.now()
	for key, invitation := range s.pairings {
		if !now.Before(invitation.expiresAt) {
			s.removePairingLocked(key)
		}
	}
}

func (s *Server) issueWebSessionLocked() (string, bool) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", false
	}
	sessionToken := "s_" + hex.EncodeToString(bytes)
	key := sha256.Sum256([]byte(sessionToken))
	active := s.sessionOrder[:0]
	for _, prior := range s.sessionOrder {
		if _, ok := s.webSessions[prior]; ok {
			active = append(active, prior)
		}
	}
	s.sessionOrder = active
	s.webSessions[key] = s.now().Add(webSessionLifetime)
	s.sessionOrder = append(s.sessionOrder, key)
	for len(s.webSessions) > maxWebSessions {
		oldest := s.sessionOrder[0]
		s.sessionOrder = s.sessionOrder[1:]
		delete(s.webSessions, oldest)
	}
	return sessionToken, true
}

func (s *Server) authorize(role, token, deviceID string, passkeySession bool) (string, *peer, bool) {
	if role == "agent" {
		return "", nil, s.authorizeAgent(deviceID, token)
	}
	if role != "web" {
		return "", nil, false
	}
	if s.cfg.WebAuthMode == WebAuthPasskey {
		return "", nil, passkeySession
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.HasPrefix(token, "p_") {
		key, valid := pairingKey(token)
		if !valid {
			return "", nil, false
		}
		invitation, ok := s.pairings[key]
		if !ok || !s.now().Before(invitation.expiresAt) {
			s.removePairingLocked(key)
			return "", nil, false
		}
		if _, active := s.webs[invitation.creator]; !active {
			s.removePairingLocked(key)
			return "", nil, false
		}
		s.removePairingLocked(key)
		sessionToken, ok := s.issueWebSessionLocked()
		return sessionToken, invitation.creator, ok
	}
	if s.webPINFailures >= 10 {
		return "", nil, false
	}
	if strings.HasPrefix(token, "s_") {
		if len(token) != 66 {
			return "", nil, false
		}
		if _, err := hex.DecodeString(token[2:]); err != nil {
			return "", nil, false
		}
		key := sha256.Sum256([]byte(token))
		if expiry, ok := s.webSessions[key]; ok && s.now().Before(expiry) {
			return token, nil, true
		}
		delete(s.webSessions, key)
		return "", nil, false
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.WebPIN)) == 1 {
		sessionToken, ok := s.issueWebSessionLocked()
		return sessionToken, nil, ok
	}
	s.webPINFailures++
	return "", nil, false
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.serveWS)
	return mux
}

func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin != "" {
		if _, ok := s.origins[origin]; !ok {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
	}
	passkeySession := false
	if s.cfg.WebAuthMode == WebAuthPasskey {
		passkeySession = s.cfg.WebSessionVerifier(r)
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	conn.SetReadLimit(s.cfg.MaxFrameBytes)
	defer conn.CloseNow()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	helloCtx, helloCancel := context.WithTimeout(ctx, s.cfg.HelloTimeout)
	_, raw, err := conn.Read(helloCtx)
	helloCancel()
	if err != nil || protocol.Validate(raw) != nil {
		conn.Close(websocket.StatusPolicyViolation, "invalid hello")
		return
	}
	var hello struct {
		Type           string          `json:"type"`
		Role           string          `json:"role"`
		Token          string          `json:"token"`
		DeviceID       string          `json:"deviceId"`
		DeviceName     string          `json:"deviceName"`
		AgentEpoch     string          `json:"agentEpoch"`
		AdapterVersion string          `json:"adapterVersion"`
		Capabilities   map[string]bool `json:"capabilities"`
		TaskStoreID    string          `json:"taskStoreId"`
	}
	if json.Unmarshal(raw, &hello) != nil || hello.Type != "hello" || (hello.Role == "web" && origin == "") {
		conn.Close(websocket.StatusPolicyViolation, "unauthorized")
		return
	}
	sessionToken, pairingCreator, authorized := s.authorize(hello.Role, hello.Token, hello.DeviceID, passkeySession)
	if !authorized {
		conn.Close(websocket.StatusPolicyViolation, "unauthorized")
		return
	}
	p := &peer{conn: conn, id: rand.Text(), role: hello.Role, deviceID: hello.DeviceID, deviceName: hello.DeviceName, agentEpoch: hello.AgentEpoch, adapterVersion: hello.AdapterVersion, capabilities: hello.Capabilities}
	if p.role == "agent" {
		credential, ok := s.cfg.TaskDevices[p.deviceID]
		if s.cfg.TasksEnabled && ok {
			p.taskCredential = &credential
			if containsTaskValue(credential.TaskStoreIDs, hello.TaskStoreID) {
				p.taskStoreID = hello.TaskStoreID
			} else {
				p.capabilities["taskManagement"] = false
			}
			if !credential.Execute {
				p.capabilities["taskExecution"] = false
				p.capabilities["taskExecutionAuthorized"] = false
				p.capabilities["taskTracking"] = false
			}
		} else {
			delete(p.capabilities, "taskManagement")
			delete(p.capabilities, "taskExecution")
			delete(p.capabilities, "taskExecutionAuthorized")
			delete(p.capabilities, "taskTracking")
		}
	}
	s.mu.Lock()
	if p.role == "agent" {
		if s.agents[p.deviceID] != nil {
			s.mu.Unlock()
			conn.Close(websocket.StatusPolicyViolation, "duplicate agent")
			return
		}
		s.agents[p.deviceID] = p
	} else {
		s.webs[p] = struct{}{}
	}
	s.mu.Unlock()
	defer s.removePeer(p)
	ack := map[string]any{"type": "hello.ok", "v": 1, "connectionId": p.id, "relayEpoch": s.epoch}
	if p.role == "web" && sessionToken != "" {
		ack["sessionToken"] = sessionToken
	}
	if err := p.send(ctx, ack); err != nil {
		return
	}
	if pairingCreator != nil {
		_ = pairingCreator.send(ctx, map[string]any{"type": "event", "v": 1, "event": "auth.pair.consumed"})
	}
	if p.role == "agent" {
		s.broadcastDevice(p.deviceID, p)
		go s.restoreAgentCacheFollowers(ctx, p)
	}
	go s.heartbeat(ctx, p)
	for {
		_, body, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if protocol.Validate(body) != nil {
			conn.Close(websocket.StatusPolicyViolation, "invalid message")
			return
		}
		if !s.handleMessage(ctx, p, body) {
			conn.Close(websocket.StatusPolicyViolation, "invalid role or message")
			return
		}
	}
}

func (s *Server) heartbeat(ctx context.Context, p *peer) {
	if err := wsheartbeat.Run(ctx, s.cfg.HeartbeatInterval, s.cfg.HeartbeatTimeout, wsheartbeat.DefaultMaxConsecutiveFailures, p.conn.Ping); err != nil {
		p.conn.CloseNow()
	}
}

func (p *peer) send(ctx context.Context, msg any) error {
	body, err := json.Marshal(msg)
	if err != nil || protocol.Validate(body) != nil {
		return errors.New("invalid outbound protocol message")
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	writeCtx, cancel := context.WithTimeout(ctx, wsheartbeat.DefaultWriteTimeout)
	defer cancel()
	if err := p.conn.Write(writeCtx, websocket.MessageText, body); err != nil {
		// A slow consumer must lose its subscription and resync, not retain an
		// unbounded backlog or stall every other client behind it.
		p.conn.CloseNow()
		return err
	}
	return nil
}

func (s *Server) removePeer(p *peer) {
	s.removeTaskPeer(p)
	s.mu.Lock()
	failed := make([]*route, 0)
	cacheDeliveries := make([]cacheDelivery, 0)
	type orphan struct {
		agent *peer
		id    string
	}
	orphans := make([]orphan, 0)
	type cacheRelease struct {
		agent *peer
		key   cacheKey
	}
	cacheReleases := make([]cacheRelease, 0)
	for id, pending := range s.routes {
		if pending.web == p || pending.agent == p {
			delete(s.routes, id)
			delete(s.byWeb[pending.web], pending.webID)
			pending.timer.Stop()
			if pending.agent == p && pending.web != p {
				failed = append(failed, pending)
			}
		}
	}
	for agent, byID := range s.subs {
		for id, sub := range byID {
			if sub.web == p || sub.agent == p {
				if sub.web == p && sub.agent != p && s.agents[sub.agent.deviceID] == sub.agent {
					orphans = append(orphans, orphan{agent: sub.agent, id: id})
				}
				delete(byID, id)
			}
		}
		if len(byID) == 0 {
			delete(s.subs, agent)
		}
	}
	if p.role == "agent" {
		if s.agents[p.deviceID] == p {
			delete(s.agents, p.deviceID)
			s.cache.markDeviceStale(p.deviceID, "agent_offline")
			for key := range s.cache.entries {
				if key.deviceID == p.deviceID {
					cacheDeliveries = append(cacheDeliveries, s.cacheUpdateMessagesLocked(key)...)
				}
			}
		}
	} else {
		if key, ok := s.pairingByWeb[p]; ok {
			s.removePairingLocked(key)
		}
		delete(s.webs, p)
		delete(s.byWeb, p)
		for _, key := range s.cache.removeWeb(p, s.now()) {
			if agent := s.agents[key.deviceID]; agent != nil {
				cacheReleases = append(cacheReleases, cacheRelease{agent: agent, key: key})
			}
		}
	}
	s.mu.Unlock()
	sendCacheDeliveries(context.Background(), cacheDeliveries)
	cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCleanup()
	for _, sub := range orphans {
		_ = sendUnsubscribe(cleanupCtx, sub.agent, sub.id)
	}
	for _, release := range cacheReleases {
		_ = sendCacheTouch(cleanupCtx, release.agent, release.key, false)
	}
	for _, pending := range failed {
		if pending.method == "thread.subscribe" {
			pending.web.send(context.Background(), responseError(pending.webID, "rejected", "RESYNC_REQUIRED", "agent disconnected during subscription"))
			continue
		}
		pending.web.send(context.Background(), responseError(pending.webID, "unknown", "OUTCOME_UNKNOWN", "agent disconnected after forwarding"))
	}
	if p.role == "agent" {
		s.broadcastDevice(p.deviceID, p)
	}
}

func (s *Server) createPairing(ctx context.Context, web *peer, requestID string) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		_ = web.send(ctx, responseError(requestID, "unknown", "OUTCOME_UNKNOWN", "pairing credential generation failed"))
		return
	}
	credential := "p_" + hex.EncodeToString(bytes)
	key := sha256.Sum256([]byte(credential))
	s.mu.Lock()
	s.cleanExpiredPairingsLocked()
	if previous, ok := s.pairingByWeb[web]; ok {
		s.removePairingLocked(previous)
	}
	if len(s.pairings) >= maxWebPairings {
		s.mu.Unlock()
		_ = web.send(ctx, responseError(requestID, "rejected", "OVERLOADED", "too many pending pairing invitations"))
		return
	}
	expiresAt := s.now().Add(webPairingLifetime)
	s.pairings[key] = pairing{creator: web, expiresAt: expiresAt}
	s.pairingByWeb[web] = key
	s.mu.Unlock()
	_ = web.send(ctx, map[string]any{
		"type": "response", "v": 1, "requestId": requestID, "outcome": "accepted",
		"data": map[string]any{"credential": credential, "expiresAt": expiresAt.UTC().Format(time.RFC3339Nano)},
	})
}

func (s *Server) cancelPairing(ctx context.Context, web *peer, requestID string, params json.RawMessage) {
	var request struct {
		Credential string `json:"credential"`
	}
	if json.Unmarshal(params, &request) != nil {
		_ = web.send(ctx, responseError(requestID, "rejected", "INVALID_ARGUMENT", "invalid pairing credential"))
		return
	}
	key, valid := pairingKey(request.Credential)
	cancelled := false
	if valid {
		s.mu.Lock()
		if invitation, ok := s.pairings[key]; ok && invitation.creator == web {
			s.removePairingLocked(key)
			cancelled = true
		}
		s.mu.Unlock()
	}
	_ = web.send(ctx, map[string]any{
		"type": "response", "v": 1, "requestId": requestID, "outcome": "accepted",
		"data": map[string]any{"cancelled": cancelled},
	})
}

func sendUnsubscribe(ctx context.Context, agent *peer, subscriptionID string) error {
	return agent.send(ctx, map[string]any{"type": "request", "v": 1, "requestId": newRequestID(), "deviceId": agent.deviceID, "method": "thread.unsubscribe", "params": map[string]string{"subscriptionId": subscriptionID}})
}

func (s *Server) currentDeviceStatus(deviceID string, departed *peer) map[string]any {
	s.mu.Lock()
	current := s.agents[deviceID]
	s.mu.Unlock()
	if current != nil {
		status := map[string]any{"type": "event", "v": 1, "event": "device.status", "deviceId": deviceID, "agentEpoch": current.agentEpoch, "agentOnline": true, "codexReady": current.capabilities["codexReady"], "capabilities": current.currentCapabilities()}
		if store := current.currentTaskStoreID(); store != "" {
			status["taskStoreId"] = store
		}
		return status
	}
	status := map[string]any{"type": "event", "v": 1, "event": "device.status", "deviceId": deviceID, "agentOnline": false, "codexReady": false}
	if departed != nil && departed.agentEpoch != "" {
		status["agentEpoch"] = departed.agentEpoch
	}
	return status
}

func (s *Server) broadcastDevice(deviceID string, departed *peer) {
	s.deviceStatusMu.Lock()
	defer s.deviceStatusMu.Unlock()
	s.mu.Lock()
	webs := make([]*peer, 0, len(s.webs))
	for p := range s.webs {
		webs = append(webs, p)
	}
	s.mu.Unlock()
	status := s.currentDeviceStatus(deviceID, departed)
	for _, p := range webs {
		p.send(context.Background(), status)
	}
}

func (s *Server) handleMessage(ctx context.Context, p *peer, body []byte) bool {
	var msg struct {
		Type      string          `json:"type"`
		RequestID string          `json:"requestId"`
		DeviceID  string          `json:"deviceId"`
		Method    string          `json:"method"`
		Params    json.RawMessage `json:"params"`
	}
	if json.Unmarshal(body, &msg) != nil {
		return false
	}
	if p.role == "agent" {
		if msg.Type == "task.capabilities" {
			return s.handleTaskCapabilities(ctx, p, body)
		}
		if isTaskFrameType(msg.Type) {
			return s.handleTaskFrame(ctx, p, body)
		}
		if msg.Type == "response" {
			s.handleAgentResponse(ctx, p, body, msg.RequestID)
			return true
		}
		if msg.Type == "event" {
			if isCacheEvent(body) {
				return s.handleCacheAgentEvent(ctx, p, body)
			}
			return s.handleAgentEvent(ctx, p, body)
		}
		return false
	}
	if p.role != "web" || msg.Type != "request" {
		return false
	}
	if strings.HasPrefix(msg.Method, "thread.cache.") {
		if msg.Method == "thread.cache.touch" {
			_ = p.send(ctx, responseError(msg.RequestID, "rejected", "UNAUTHORIZED", "cache touch is agent-only"))
			return true
		}
		return s.handleCacheRequest(ctx, p, msg.RequestID, msg.DeviceID, msg.Method, msg.Params)
	}
	if msg.Method == "auth.pair.create" || msg.Method == "auth.pair.cancel" {
		if s.cfg.WebAuthMode != WebAuthPIN {
			_ = p.send(ctx, responseError(msg.RequestID, "rejected", "UNSUPPORTED", "pairing is available only in PIN mode"))
			return true
		}
		if msg.DeviceID != "relay" {
			_ = p.send(ctx, responseError(msg.RequestID, "rejected", "INVALID_ARGUMENT", "pairing requests target relay"))
			return true
		}
		if msg.Method == "auth.pair.create" {
			s.createPairing(ctx, p, msg.RequestID)
		} else {
			s.cancelPairing(ctx, p, msg.RequestID, msg.Params)
		}
		return true
	}
	if msg.Method != "device.list" {
		return s.forwardRequest(ctx, p, msg.RequestID, msg.DeviceID, msg.Method, msg.Params)
	}
	s.mu.Lock()
	devices := make([]map[string]any, 0, len(s.agents))
	for _, agent := range s.agents {
		device := map[string]any{"deviceId": agent.deviceID, "deviceName": agent.deviceName, "agentOnline": true, "codexReady": agent.capabilities["codexReady"], "capabilities": agent.currentCapabilities(), "agentEpoch": agent.agentEpoch, "adapterVersion": agent.adapterVersion}
		if store := agent.currentTaskStoreID(); store != "" {
			device["taskStoreId"] = store
		}
		devices = append(devices, device)
	}
	s.mu.Unlock()
	p.send(ctx, map[string]any{"type": "response", "v": 1, "requestId": msg.RequestID, "outcome": "accepted", "data": map[string]any{"devices": devices}})
	return true
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("cryptographic randomness unavailable")
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func (s *Server) forwardRequest(ctx context.Context, web *peer, webID, deviceID, method string, params json.RawMessage) bool {
	var reference struct {
		ThreadID       string `json:"threadId"`
		SubscriptionID string `json:"subscriptionId"`
		SearchTerm     string `json:"searchTerm"`
	}
	json.Unmarshal(params, &reference)
	s.mu.Lock()
	agent := s.agents[deviceID]
	if agent == nil {
		s.mu.Unlock()
		web.send(ctx, responseError(webID, "rejected", "DEVICE_OFFLINE", "device offline"))
		return true
	}
	if !s.taskPublicAllowed(agent, method, params) {
		s.mu.Unlock()
		web.send(ctx, responseError(webID, "rejected", "UNAUTHORIZED", "task access not authorized"))
		return true
	}
	if method == "thread.unsubscribe" {
		sub := s.subs[agent][reference.SubscriptionID]
		if sub == nil || sub.web != web {
			s.mu.Unlock()
			web.send(ctx, responseError(webID, "rejected", "INVALID_ARGUMENT", "unknown subscription"))
			return true
		}
	}
	if method == "thread.subscribe" {
		count := 0
		for _, byID := range s.subs {
			for _, sub := range byID {
				if sub.web == web {
					count++
				}
			}
		}
		for _, pending := range s.routes {
			if pending.web == web && pending.method == "thread.subscribe" {
				count++
			}
		}
		if count >= maxWebSubscriptions {
			s.mu.Unlock()
			web.send(ctx, responseError(webID, "rejected", "OVERLOADED", "too many active subscriptions"))
			return true
		}
	}
	if s.byWeb[web] == nil {
		s.byWeb[web] = map[string]string{}
	}
	if _, duplicate := s.byWeb[web][webID]; duplicate {
		s.mu.Unlock()
		return false // Ambiguous same-ID replies would be unsafe; close this Web connection.
	}
	if len(s.byWeb[web]) >= 32 {
		s.mu.Unlock()
		web.send(ctx, responseError(webID, "rejected", "OVERLOADED", "too many pending requests"))
		return true
	}
	relayID := newRequestID()
	entry := &route{web: web, agent: agent, webID: webID, method: method, deviceID: deviceID, threadID: reference.ThreadID, subID: reference.SubscriptionID}
	s.routes[relayID] = entry
	s.byWeb[web][webID] = relayID
	timeout := s.cfg.RequestTimeout
	if isLongRead(method) {
		timeout = s.cfg.ReadTimeout
	}
	if method == "thread.subscribe" {
		timeout = s.cfg.SubscriptionTimeout
	}
	if method == "thread.create" || method == "thread.bootstrap" || method == "thread.archive.update" || method == "thread.archive.undo" || method == "thread.name.update" || method == "thread.pin.update" || method == "turn.start" || method == "turn.interrupt" || method == "interaction.respond" || method == "queue.add" || method == "queue.update" || method == "queue.delete" || method == "queue.reorder" || method == "queue.steer" {
		timeout = s.cfg.MutationTimeout
	}
	entry.timer = time.AfterFunc(timeout, func() {
		if isLongRead(method) || method == "thread.subscribe" {
			s.finishRoute(relayID, agent, responseError(webID, "rejected", "TIMEOUT", "agent read response timeout"))
			return
		}
		s.finishRoute(relayID, agent, responseError(webID, "unknown", "OUTCOME_UNKNOWN", "agent response timeout"))
	})
	s.mu.Unlock()
	forwarded := map[string]any{"type": "request", "v": 1, "requestId": relayID, "deviceId": deviceID, "method": method, "params": json.RawMessage(params)}
	if err := agent.send(ctx, forwarded); err != nil {
		s.finishRoute(relayID, agent, responseError(webID, "unknown", "OUTCOME_UNKNOWN", "agent send outcome unknown"))
	}
	return true
}

func isLongRead(method string) bool {
	switch method {
	case "thread.list", "thread.read", "thread.history", "thread.history.items", "thread.image", "file.context.changes", "file.context.read", "file.context.reference.read":
		return true
	default:
		return false
	}
}

func (s *Server) handleAgentResponse(ctx context.Context, agent *peer, body []byte, relayID string) {
	var reply map[string]any
	if json.Unmarshal(body, &reply) != nil {
		return
	}
	s.mu.Lock()
	entry := s.routes[relayID]
	if entry == nil || entry.agent != agent {
		data, _ := reply["data"].(map[string]any)
		subID, _ := data["subscriptionId"].(string)
		orphan := reply["outcome"] == "accepted" && subID != "" && s.subs[agent][subID] == nil && s.agents[agent.deviceID] == agent
		s.mu.Unlock()
		if orphan {
			_ = sendUnsubscribe(ctx, agent, subID)
		}
		return
	}
	reply["requestId"] = entry.webID
	s.mu.Unlock()
	s.finishRoute(relayID, agent, reply)
}

func (s *Server) finishRoute(relayID string, agent *peer, response map[string]any) {
	s.mu.Lock()
	entry := s.routes[relayID]
	if entry == nil || entry.agent != agent {
		s.mu.Unlock()
		return
	}
	delete(s.routes, relayID)
	delete(s.byWeb[entry.web], entry.webID)
	entry.timer.Stop()
	if response["outcome"] == "accepted" {
		if entry.method == "thread.subscribe" {
			data, _ := response["data"].(map[string]any)
			subID, _ := data["subscriptionId"].(string)
			streamID, _ := data["streamId"].(string)
			if subID == "" || streamID == "" || s.subs[agent][subID] != nil {
				response = responseError(entry.webID, "unknown", "RESYNC_REQUIRED", "invalid subscription identity")
			} else {
				if s.subs[agent] == nil {
					s.subs[agent] = map[string]*subscription{}
				}
				s.subs[agent][subID] = &subscription{web: entry.web, agent: agent, deviceID: entry.deviceID, threadID: entry.threadID, streamID: streamID}
			}
		}
		if entry.method == "thread.unsubscribe" {
			delete(s.subs[agent], entry.subID)
		}
	}
	s.mu.Unlock()
	entry.web.send(context.Background(), response)
}

func (s *Server) handleAgentEvent(ctx context.Context, agent *peer, body []byte) bool {
	var event struct {
		Event          string `json:"event"`
		DeviceID       string `json:"deviceId"`
		ThreadID       string `json:"threadId"`
		SubscriptionID string `json:"subscriptionId"`
		StreamID       string `json:"streamId"`
	}
	if json.Unmarshal(body, &event) != nil || event.DeviceID != agent.deviceID || event.Event == "device.status" {
		return false
	}
	s.mu.Lock()
	webs := make([]*peer, 0, 1)
	if event.Event == "interaction.resolved" {
		for _, sub := range s.subs[agent] {
			if sub.threadID == event.ThreadID {
				webs = append(webs, sub.web)
			}
		}
	} else {
		sub := s.subs[agent][event.SubscriptionID]
		if sub != nil && sub.threadID == event.ThreadID && sub.streamID == event.StreamID {
			webs = append(webs, sub.web)
			if event.Event == "thread.error" {
				delete(s.subs[agent], event.SubscriptionID)
			}
		}
	}
	s.mu.Unlock()
	for _, web := range webs {
		var outgoing any
		if json.Unmarshal(body, &outgoing) == nil {
			web.send(ctx, outgoing)
		}
	}
	return true
}

func responseError(id, outcome, code, message string) map[string]any {
	return map[string]any{"type": "response", "v": 1, "requestId": id, "outcome": outcome, "error": map[string]string{"code": code, "message": message}}
}
