package mockagent

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/wu8685/Ariel/protocol"
)

type Config struct {
	URL, Token, DeviceID, DeviceName string
	Store                            *Store
}

type agent struct {
	deviceID string
	store    *Store
	conn     *websocket.Conn
	writeMu  sync.Mutex
}

func Run(ctx context.Context, cfg Config) error {
	if cfg.URL == "" || cfg.Token == "" || cfg.DeviceID == "" || cfg.DeviceName == "" {
		return errors.New("mock agent requires URL, token, device ID and name")
	}
	if cfg.Store == nil {
		cfg.Store = NewStore(StoreConfig{})
	}
	conn, _, err := websocket.Dial(ctx, cfg.URL, nil)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(8 << 20)
	a := &agent{deviceID: cfg.DeviceID, store: cfg.Store, conn: conn}
	hello := map[string]any{"type": "hello", "v": 1, "role": "agent", "token": cfg.Token, "deviceId": cfg.DeviceID, "deviceName": cfg.DeviceName, "agentEpoch": rand.Text(), "adapterVersion": "mock-v1", "capabilities": map[string]bool{"autoLoad": false, "codexReady": false, "history": true, "send": true, "interrupt": true, "interaction": true}}
	if err := a.send(ctx, hello); err != nil {
		return err
	}
	_, b, err := conn.Read(ctx)
	if err != nil {
		return err
	}
	var ack struct {
		Type string `json:"type"`
	}
	if protocol.Validate(b) != nil || json.Unmarshal(b, &ack) != nil || ack.Type != "hello.ok" {
		return errors.New("invalid relay hello acknowledgement")
	}
	for {
		_, b, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		if err := protocol.Validate(b); err != nil {
			return err
		}
		var req struct {
			Type      string          `json:"type"`
			RequestID string          `json:"requestId"`
			Method    string          `json:"method"`
			Params    json.RawMessage `json:"params"`
		}
		if json.Unmarshal(b, &req) != nil || req.Type != "request" {
			return errors.New("unexpected relay message")
		}
		a.handle(ctx, req.RequestID, req.Method, req.Params)
	}
}

func (a *agent) send(ctx context.Context, msg any) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if err := protocol.Validate(b); err != nil {
		return fmt.Errorf("invalid mock message: %w", err)
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return a.conn.Write(writeCtx, websocket.MessageText, b)
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
	if err := json.Unmarshal(raw, &p); err != nil {
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
		var threads []map[string]any
		var cursor string
		threads, cursor, err = a.store.List(p.Limit, p.Cursor)
		data = map[string]any{"threads": threads, "nextCursor": cursor}
	case "thread.read":
		var thread map[string]any
		thread, err = a.store.Read(p.ThreadID)
		data = map[string]any{"thread": thread}
	case "thread.subscribe":
		var subID, streamID string
		var thread map[string]any
		subID, streamID, thread, err = a.store.Subscribe(p.ThreadID, func(event map[string]any) {
			event["type"] = "event"
			event["v"] = 1
			event["deviceId"] = a.deviceID
			_ = a.send(ctx, event)
		})
		if err == nil {
			data = map[string]any{"subscriptionId": subID, "streamId": streamID}
			a.reply(ctx, id, data, nil)
			_ = a.send(ctx, map[string]any{"type": "event", "v": 1, "event": "thread.snapshot", "deviceId": a.deviceID, "threadId": p.ThreadID, "subscriptionId": subID, "streamId": streamID, "seq": 1, "thread": thread})
			return
		}
	case "thread.unsubscribe":
		if !a.store.Unsubscribe(p.SubscriptionID) {
			err = errors.New("INVALID_ARGUMENT")
		} else {
			data = map[string]any{}
		}
	case "turn.start":
		var turnID string
		turnID, err = a.store.Start(p.ThreadID, p.ClientMessageID, p.Text)
		data = map[string]any{"turnId": turnID}
	case "turn.interrupt":
		err = a.store.Interrupt(p.ThreadID, p.ExpectedTurnID)
		data = map[string]any{}
	case "interaction.respond":
		err = a.store.Respond(p.ThreadID, p.InteractionID, p.Decision, p.Answers)
		data = map[string]any{}
	default:
		err = errors.New("INVALID_ARGUMENT")
	}
	a.reply(ctx, id, data, err)
	if method == "interaction.respond" && err == nil {
		_ = a.send(ctx, map[string]any{"type": "event", "v": 1, "event": "interaction.resolved", "deviceId": a.deviceID, "threadId": p.ThreadID, "interactionId": p.InteractionID})
	}
}

func (a *agent) reply(ctx context.Context, id string, data map[string]any, err error) {
	msg := map[string]any{"type": "response", "v": 1, "requestId": id}
	if err == nil {
		msg["outcome"] = "accepted"
		msg["data"] = data
	} else {
		code := err.Error()
		switch code {
		case "INVALID_ARGUMENT", "NOT_FOUND", "TURN_BUSY", "STALE_TURN", "INTERACTION_UNSUPPORTED":
		default:
			code = "INVALID_ARGUMENT"
		}
		msg["outcome"] = "rejected"
		msg["error"] = map[string]string{"code": code, "message": code}
	}
	_ = a.send(ctx, msg)
}
