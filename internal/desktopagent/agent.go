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
	OnReady                                               func() error
	OnNotReady                                            func()
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
	readOnlyRPC, err := appserver.NewRestartingRPC(ctx, func(parent context.Context) (appserver.ReadOnlyEndpoint, error) {
		return appserver.Start(parent, cfg.Binary, cwd)
	})
	if err != nil {
		return err
	}
	defer readOnlyRPC.Close()
	queueReader := appserver.QueueReader{RPC: readOnlyRPC}
	queueSupported, err := queueReader.Supported(ctx)
	if err != nil {
		return err
	}
	var queues []FollowUpQueue
	if queueSupported {
		queues = append(queues, queueReader)
	}
	service := NewService(appserver.HistoryReader{RPC: readOnlyRPC}, func(ctx context.Context, id, cwd string) (Live, error) { return OpenFollower(ctx, cfg.Socket, id, cwd) }, queues...)
	service.SetThreadCreator(appserver.NewThreadCreator(cfg.Binary))
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
	err = RunWithService(runCtx, cfg, service)
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
	hello := map[string]any{"type": "hello", "v": 1, "role": "agent", "token": cfg.Token, "deviceId": cfg.DeviceID, "deviceName": cfg.DeviceName, "agentEpoch": rand.Text(), "adapterVersion": "desktop-ipc-0.160.0", "capabilities": map[string]bool{"autoLoad": true, "codexReady": true, "history": true, "send": true, "interrupt": true, "interaction": true, "queue": service.QueueEnabled(), "threadCreate": service.ThreadCreationEnabled()}}
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
	if cfg.OnReady != nil {
		if err := cfg.OnReady(); err != nil {
			return err
		}
		if cfg.OnNotReady != nil {
			defer cfg.OnNotReady()
		}
	}
	heartCtx, stopHeartbeat := context.WithCancel(ctx)
	defer stopHeartbeat()
	heartbeatFailure := make(chan error, 1)
	interval, timeout := cfg.HeartbeatInterval, cfg.HeartbeatTimeout
	if interval <= 0 {
		interval = 20 * time.Second
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	go func() {
		if err := heartbeatRelay(heartCtx, conn, interval, timeout); err != nil {
			heartbeatFailure <- err
			conn.CloseNow()
		}
	}()
	for {
		_, body, err = conn.Read(ctx)
		if err != nil {
			select {
			case heartbeatErr := <-heartbeatFailure:
				return fmt.Errorf("Relay heartbeat failed: %w", heartbeatErr)
			default:
			}
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

func heartbeatRelay(ctx context.Context, conn *websocket.Conn, interval, timeout time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, timeout)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return err
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
		TurnID          string              `json:"turnId"`
		SubscriptionID  string              `json:"subscriptionId"`
		Cursor          string              `json:"cursor"`
		SearchTerm      string              `json:"searchTerm"`
		Limit           int                 `json:"limit"`
		ClientMessageID string              `json:"clientMessageId"`
		Text            string              `json:"text"`
		Images          []string            `json:"images"`
		ItemID          string              `json:"itemId"`
		ImageIndex      int                 `json:"imageIndex"`
		ExpectedTurnID  string              `json:"expectedTurnId"`
		QueueID         string              `json:"queueId"`
		QueueIDs        []string            `json:"queueIds"`
		InteractionID   string              `json:"interactionId"`
		Decision        string              `json:"decision"`
		Answers         map[string][]string `json:"answers"`
		CWD             string              `json:"cwd"`
	}
	if json.Unmarshal(raw, &p) != nil {
		a.reply(ctx, id, nil, errors.New("INVALID_ARGUMENT"))
		return
	}
	var data map[string]any
	var err error
	switch method {
	case "thread.create":
		var thread map[string]any
		thread, err = a.service.CreateThread(ctx, p.CWD)
		data = map[string]any{"thread": thread}
	case "thread.list":
		if p.Limit == 0 {
			p.Limit = 50
		}
		var list []map[string]any
		var cursor string
		if p.SearchTerm != "" {
			list, cursor, err = a.service.Search(ctx, p.SearchTerm, p.Limit, p.Cursor)
		} else {
			list, cursor, err = a.service.List(ctx, p.Limit, p.Cursor)
		}
		data = map[string]any{"threads": list, "nextCursor": cursor}
	case "thread.read":
		var thread map[string]any
		thread, err = a.service.Read(ctx, p.ThreadID)
		data = map[string]any{"thread": thread}
	case "thread.history":
		if p.Limit == 0 {
			p.Limit = 10
		}
		data, err = a.service.History(ctx, p.ThreadID, p.Cursor, p.Limit)
	case "thread.history.items":
		if p.Limit == 0 {
			p.Limit = 100
		}
		data, err = a.service.HistoryItems(ctx, p.ThreadID, p.TurnID, p.Cursor, p.Limit)
	case "thread.image":
		var uri string
		uri, err = a.service.Image(ctx, p.ThreadID, p.TurnID, p.ItemID, p.ImageIndex)
		data = map[string]any{"dataUri": uri}
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
		turnID, err = a.service.StartWithImages(ctx, p.ThreadID, p.ClientMessageID, p.Text, p.Images)
		data = map[string]any{"turnId": turnID}
	case "turn.interrupt":
		err = a.service.Interrupt(ctx, p.ThreadID, p.ExpectedTurnID)
		data = map[string]any{}
	case "queue.add":
		var queued []map[string]any
		queued, err = a.service.QueueAdd(ctx, p.ThreadID, p.ClientMessageID, p.Text, p.Images)
		data = map[string]any{"queuedMessages": queued}
	case "queue.update":
		var queued []map[string]any
		queued, err = a.service.QueueUpdate(ctx, p.ThreadID, p.QueueID, p.Text, p.Images)
		data = map[string]any{"queuedMessages": queued}
	case "queue.delete":
		var queued []map[string]any
		queued, err = a.service.QueueDelete(ctx, p.ThreadID, p.QueueID)
		data = map[string]any{"queuedMessages": queued}
	case "queue.reorder":
		var queued []map[string]any
		queued, err = a.service.QueueReorder(ctx, p.ThreadID, p.QueueIDs)
		data = map[string]any{"queuedMessages": queued}
	case "queue.steer":
		var queued []map[string]any
		queued, err = a.service.QueueSteer(ctx, p.ThreadID, p.QueueID, p.ExpectedTurnID)
		data = map[string]any{"queuedMessages": queued}
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
		outcome, code := "rejected", requestErrorCode(err)
		if errors.Is(err, appserver.ErrOutcomeUnknown) {
			outcome = "unknown"
			code = "OUTCOME_UNKNOWN"
		}
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
		msg["outcome"] = outcome
		msg["error"] = map[string]string{"code": code, "message": code}
	}
	_ = a.send(ctx, msg)
}

func requestErrorCode(err error) string {
	switch {
	case errors.Is(err, desktopipc.ErrFrameTooLarge), errors.Is(err, appserver.ErrResponseTooLarge):
		return "HISTORY_TOO_LARGE"
	case errors.Is(err, desktopipc.ErrOverloaded):
		return "OVERLOADED"
	case errors.Is(err, ErrNativeShape), errors.Is(err, appserver.ErrMethodUnavailable), errors.Is(err, desktopipc.ErrProtocol):
		return "PROTOCOL_UNSUPPORTED"
	}
	switch code := err.Error(); code {
	case "INVALID_ARGUMENT", "NOT_FOUND", "TURN_BUSY", "STALE_TURN", "STALE_INTERACTION", "INTERACTION_UNSUPPORTED", "OUTCOME_UNKNOWN", "DEVICE_OFFLINE", "RESYNC_REQUIRED", "NATIVE_STATE_UNCERTAIN", "HISTORY_TOO_LARGE", "OVERLOADED", "PROTOCOL_UNSUPPORTED":
		return code
	default:
		return "RESYNC_REQUIRED"
	}
}
