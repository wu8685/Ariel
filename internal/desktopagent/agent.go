package desktopagent

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/codex/desktopipc"
	"github.com/wu8685/Ariel/protocol"
)

type Config struct {
	URL, Token, DeviceID, DeviceName, Binary, Socket, CWD string
	HeartbeatInterval, HeartbeatTimeout                   time.Duration
}
type agent struct {
	conn     *websocket.Conn
	deviceID string
	service  *Service
	writeMu  sync.Mutex
}

func Run(ctx context.Context, cfg Config) error {
	if cfg.Binary == "" || cfg.Socket == "" {
		return errors.New("Codex binary and Desktop IPC socket are required")
	}
	check := func(ctx context.Context) error { return checkDesktop(ctx, cfg.Socket) }
	if err := check(ctx); err != nil {
		return err
	}
	cwd := cfg.CWD
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	process, err := appserver.Start(ctx, cfg.Binary, cwd)
	if err != nil {
		return err
	}
	defer process.Close()
	service := NewService(appserver.HistoryReader{RPC: process.Session}, func(ctx context.Context, id, cwd string) (Live, error) { return OpenFollower(ctx, cfg.Socket, id, cwd) })
	defer service.Close()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	healthResult := make(chan error, 1)
	go func() {
		err := monitorDesktop(runCtx, 5*time.Second, check)
		healthResult <- err
		if err != nil {
			cancel()
		}
	}()
	err = serveWithAppServer(runCtx, process.Session.Done(), func(ctx context.Context) error {
		return RunWithService(ctx, cfg, service)
	})
	cancel()
	select {
	case healthErr := <-healthResult:
		if healthErr != nil {
			return healthErr
		}
	default:
	}
	return err
}

func RunWithService(ctx context.Context, cfg Config, service *Service) error {
	if cfg.URL == "" || cfg.Token == "" || cfg.DeviceID == "" || cfg.DeviceName == "" || service == nil {
		return errors.New("invalid Desktop Agent configuration")
	}
	conn, _, err := websocket.Dial(ctx, cfg.URL, nil)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(8 << 20)
	a := &agent{conn: conn, deviceID: cfg.DeviceID, service: service}
	requestCtx, cancelRequests := context.WithCancel(ctx)
	var requests sync.WaitGroup
	defer func() {
		cancelRequests()
		requests.Wait()
	}()
	queue := make(chan struct{}, 32)
	hello := map[string]any{"type": "hello", "v": 1, "role": "agent", "token": cfg.Token, "deviceId": cfg.DeviceID, "deviceName": cfg.DeviceName, "agentEpoch": rand.Text(), "adapterVersion": "desktop-ipc-0.160.0", "capabilities": map[string]bool{"autoLoad": true, "codexReady": true, "history": true, "send": true, "interrupt": true, "interaction": true}}
	if err := a.send(ctx, hello); err != nil {
		return err
	}
	_, body, err := conn.Read(ctx)
	if err != nil {
		return err
	}
	var ack struct {
		Type string `json:"type"`
	}
	if protocol.Validate(body) != nil || json.Unmarshal(body, &ack) != nil || ack.Type != "hello.ok" {
		return errors.New("invalid Relay acknowledgement")
	}
	heartCtx, stopHeartbeat := context.WithCancel(ctx)
	defer stopHeartbeat()
	interval, timeout := cfg.HeartbeatInterval, cfg.HeartbeatTimeout
	if interval <= 0 {
		interval = 20 * time.Second
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	go heartbeatRelay(heartCtx, conn, interval, timeout)
	for {
		_, body, err = conn.Read(ctx)
		if err != nil {
			return err
		}
		if err := protocol.Validate(body); err != nil {
			return err
		}
		var req struct {
			Type      string          `json:"type"`
			RequestID string          `json:"requestId"`
			Method    string          `json:"method"`
			Params    json.RawMessage `json:"params"`
		}
		if json.Unmarshal(body, &req) != nil || req.Type != "request" {
			return errors.New("unexpected Relay message")
		}
		select {
		case queue <- struct{}{}:
			requests.Add(1)
			go func() {
				defer requests.Done()
				defer func() { <-queue }()
				a.handle(requestCtx, req.RequestID, req.Method, req.Params)
			}()
		default:
			a.reply(requestCtx, req.RequestID, nil, errors.New("OVERLOADED"))
		}
	}
}

func heartbeatRelay(ctx context.Context, conn *websocket.Conn, interval, timeout time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, timeout)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				conn.CloseNow()
				return
			}
		}
	}
}

func (a *agent) send(ctx context.Context, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := protocol.Validate(body); err != nil {
		return fmt.Errorf("invalid outbound agent message: %w", err)
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return a.conn.Write(writeCtx, websocket.MessageText, body)
}

func (a *agent) handle(ctx context.Context, id, method string, raw json.RawMessage) {
	var p struct {
		ThreadID        string              `json:"threadId"`
		SubscriptionID  string              `json:"subscriptionId"`
		Cursor          string              `json:"cursor"`
		Limit           int                 `json:"limit"`
		ClientMessageID string              `json:"clientMessageId"`
		Text            string              `json:"text"`
		ExpectedTurnID  string              `json:"expectedTurnId"`
		InteractionID   string              `json:"interactionId"`
		Decision        string              `json:"decision"`
		Answers         map[string][]string `json:"answers"`
	}
	if json.Unmarshal(raw, &p) != nil {
		a.reply(ctx, id, nil, errors.New("INVALID_ARGUMENT"))
		return
	}
	var data map[string]any
	var err error
	switch method {
	case "thread.list":
		if p.Limit == 0 {
			p.Limit = 50
		}
		var list []map[string]any
		var cursor string
		list, cursor, err = a.service.List(ctx, p.Limit, p.Cursor)
		data = map[string]any{"threads": list, "nextCursor": cursor}
	case "thread.read":
		var thread map[string]any
		thread, err = a.service.Read(ctx, p.ThreadID)
		data = map[string]any{"thread": thread}
	case "thread.subscribe":
		loadCtx, cancel := context.WithTimeout(ctx, 28*time.Second)
		defer cancel()
		var subID, streamID string
		var thread map[string]any
		var activate func()
		subID, streamID, thread, activate, err = a.service.Subscribe(loadCtx, p.ThreadID, func(event map[string]any) {
			event["type"] = "event"
			event["v"] = 1
			event["deviceId"] = a.deviceID
			_ = a.send(ctx, event)
		})
		if err == nil {
			data = map[string]any{"subscriptionId": subID, "streamId": streamID}
			a.reply(ctx, id, data, nil)
			_ = a.send(ctx, map[string]any{"type": "event", "v": 1, "event": "thread.snapshot", "deviceId": a.deviceID, "threadId": p.ThreadID, "subscriptionId": subID, "streamId": streamID, "seq": 1, "thread": thread})
			activate()
			return
		}
	case "thread.unsubscribe":
		if !a.service.Unsubscribe(p.SubscriptionID) {
			err = errors.New("INVALID_ARGUMENT")
		} else {
			data = map[string]any{}
		}
	case "turn.start":
		var turnID string
		turnID, err = a.service.Start(ctx, p.ThreadID, p.ClientMessageID, p.Text)
		data = map[string]any{"turnId": turnID}
	case "turn.interrupt":
		err = a.service.Interrupt(ctx, p.ThreadID, p.ExpectedTurnID)
		data = map[string]any{}
	case "interaction.respond":
		err = a.service.Respond(ctx, p.ThreadID, p.InteractionID, p.Decision, p.Answers)
		data = map[string]any{}
	default:
		err = errors.New("INVALID_ARGUMENT")
	}
	a.reply(ctx, id, data, err)
}

func (a *agent) reply(ctx context.Context, id string, data map[string]any, err error) {
	msg := map[string]any{"type": "response", "v": 1, "requestId": id}
	if err == nil {
		msg["outcome"] = "accepted"
		msg["data"] = data
	} else {
		outcome, code := "rejected", err.Error()
		var callErr *desktopipc.CallError
		if errors.As(err, &callErr) {
			outcome = callErr.Outcome
			if outcome == "unknown" {
				code = "OUTCOME_UNKNOWN"
			} else if outcome == "not_submitted" {
				code = "DEVICE_OFFLINE"
			} else {
				code = "INVALID_ARGUMENT"
			}
		}
		switch code {
		case "INVALID_ARGUMENT", "NOT_FOUND", "TURN_BUSY", "STALE_TURN", "STALE_INTERACTION", "INTERACTION_UNSUPPORTED", "OUTCOME_UNKNOWN", "DEVICE_OFFLINE", "RESYNC_REQUIRED", "NATIVE_STATE_UNCERTAIN", "HISTORY_TOO_LARGE", "OVERLOADED":
		default:
			code = "RESYNC_REQUIRED"
		}
		msg["outcome"] = outcome
		msg["error"] = map[string]string{"code": code, "message": code}
	}
	_ = a.send(ctx, msg)
}
