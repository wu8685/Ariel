package relay

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/wu8685/Ariel/protocol"
)

type Config struct {
	Token               string
	AllowedOrigins      []string
	HelloTimeout        time.Duration
	MaxFrameBytes       int64
	RequestTimeout      time.Duration
	SubscriptionTimeout time.Duration
	MutationTimeout     time.Duration
	HeartbeatInterval   time.Duration
	HeartbeatTimeout    time.Duration
}

type Server struct {
	cfg     Config
	epoch   string
	origins map[string]struct{}
	mu      sync.Mutex
	agents  map[string]*peer
	webs    map[*peer]struct{}
	routes  map[string]*route
	byWeb   map[*peer]map[string]string
	subs    map[*peer]map[string]*subscription
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
	if cfg.Token == "" || len(cfg.AllowedOrigins) == 0 {
		return nil, errors.New("relay requires token and web origin allowlist")
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
	if cfg.MaxFrameBytes <= 0 {
		cfg.MaxFrameBytes = 8 << 20
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 10 * time.Second
	}
	if cfg.SubscriptionTimeout <= 0 {
		cfg.SubscriptionTimeout = 30 * time.Second
	}
	if cfg.MutationTimeout <= 0 {
		cfg.MutationTimeout = 45 * time.Second
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 20 * time.Second
	}
	if cfg.HeartbeatTimeout <= 0 {
		cfg.HeartbeatTimeout = 5 * time.Second
	}
	return &Server{cfg: cfg, epoch: rand.Text(), origins: origins, agents: map[string]*peer{}, webs: map[*peer]struct{}{}, routes: map[string]*route{}, byWeb: map[*peer]map[string]string{}, subs: map[*peer]map[string]*subscription{}}, nil
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
	}
	if json.Unmarshal(raw, &hello) != nil || hello.Type != "hello" || subtle.ConstantTimeCompare([]byte(hello.Token), []byte(s.cfg.Token)) != 1 || (hello.Role == "web" && origin == "") {
		conn.Close(websocket.StatusPolicyViolation, "unauthorized")
		return
	}
	p := &peer{conn: conn, id: rand.Text(), role: hello.Role, deviceID: hello.DeviceID, deviceName: hello.DeviceName, agentEpoch: hello.AgentEpoch, adapterVersion: hello.AdapterVersion, capabilities: hello.Capabilities}
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
	if err := p.send(ctx, map[string]any{"type": "hello.ok", "v": 1, "connectionId": p.id, "relayEpoch": s.epoch}); err != nil {
		return
	}
	if p.role == "agent" {
		s.broadcastDevice(p, true)
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
	ticker := time.NewTicker(s.cfg.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, s.cfg.HeartbeatTimeout)
			err := p.conn.Ping(pingCtx)
			cancel()
			if err != nil {
				p.conn.CloseNow()
				return
			}
		}
	}
}

func (p *peer) send(ctx context.Context, msg any) error {
	body, err := json.Marshal(msg)
	if err != nil || protocol.Validate(body) != nil {
		return errors.New("invalid outbound protocol message")
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
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
	s.mu.Lock()
	failed := make([]*route, 0)
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
		}
	} else {
		delete(s.webs, p)
		delete(s.byWeb, p)
	}
	s.mu.Unlock()
	for _, pending := range failed {
		pending.web.send(context.Background(), responseError(pending.webID, "unknown", "OUTCOME_UNKNOWN", "agent disconnected after forwarding"))
	}
	if p.role == "agent" {
		s.broadcastDevice(p, false)
	}
}

func (s *Server) broadcastDevice(agent *peer, online bool) {
	s.mu.Lock()
	webs := make([]*peer, 0, len(s.webs))
	for p := range s.webs {
		webs = append(webs, p)
	}
	s.mu.Unlock()
	status := map[string]any{"type": "event", "v": 1, "event": "device.status", "deviceId": agent.deviceID, "agentOnline": online, "codexReady": online && agent.capabilities["codexReady"], "capabilities": agent.capabilities}
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
		if msg.Type == "response" {
			s.handleAgentResponse(ctx, p, body, msg.RequestID)
			return true
		}
		if msg.Type == "event" {
			return s.handleAgentEvent(ctx, p, body)
		}
		return false
	}
	if p.role != "web" || msg.Type != "request" {
		return false
	}
	if msg.Method != "device.list" {
		return s.forwardRequest(ctx, p, msg.RequestID, msg.DeviceID, msg.Method, msg.Params)
	}
	s.mu.Lock()
	devices := make([]map[string]any, 0, len(s.agents))
	for _, agent := range s.agents {
		devices = append(devices, map[string]any{"deviceId": agent.deviceID, "deviceName": agent.deviceName, "agentOnline": true, "codexReady": agent.capabilities["codexReady"], "capabilities": agent.capabilities, "agentEpoch": agent.agentEpoch, "adapterVersion": agent.adapterVersion})
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
	}
	json.Unmarshal(params, &reference)
	s.mu.Lock()
	agent := s.agents[deviceID]
	if agent == nil {
		s.mu.Unlock()
		web.send(ctx, responseError(webID, "rejected", "DEVICE_OFFLINE", "device offline"))
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
	if method == "thread.subscribe" {
		timeout = s.cfg.SubscriptionTimeout
	}
	if method == "turn.start" || method == "turn.interrupt" || method == "interaction.respond" {
		timeout = s.cfg.MutationTimeout
	}
	entry.timer = time.AfterFunc(timeout, func() {
		s.finishRoute(relayID, agent, responseError(webID, "unknown", "OUTCOME_UNKNOWN", "agent response timeout"))
	})
	s.mu.Unlock()
	forwarded := map[string]any{"type": "request", "v": 1, "requestId": relayID, "deviceId": deviceID, "method": method, "params": json.RawMessage(params)}
	if err := agent.send(ctx, forwarded); err != nil {
		s.finishRoute(relayID, agent, responseError(webID, "unknown", "OUTCOME_UNKNOWN", "agent send outcome unknown"))
	}
	return true
}

func (s *Server) handleAgentResponse(ctx context.Context, agent *peer, body []byte, relayID string) {
	var reply map[string]any
	if json.Unmarshal(body, &reply) != nil {
		return
	}
	s.mu.Lock()
	entry := s.routes[relayID]
	if entry == nil || entry.agent != agent {
		s.mu.Unlock()
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
