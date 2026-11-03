package desktopagent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wu8685/Ariel/internal/codex/appserver"
)

type brainFrame = appserver.IncomingFrame

type brainProcess interface {
	Call(context.Context, string, any, any) error
	Incoming() <-chan brainFrame
	Reply(context.Context, json.RawMessage, any, any) error
	Done() <-chan struct{}
	Close() error
}

type BrainHostConfig struct {
	Enabled        bool
	Binary         string
	Root           string
	MaxInFlight    int
	ToolTimeout    time.Duration
	RestartBackoff time.Duration
	Start          func(context.Context, string, string) (brainProcess, error)
	Emit           func(BrainHostEvent)
}

type BrainEnsure struct {
	AssistantID, Name, ThreadID, DeveloperInstructions, Model, Effort string
	ToolsetVersion                                                    int
}

type BrainHostEvent struct {
	Kind, AssistantID, ThreadID, TurnID, CallID, Tool string
	Arguments                                         json.RawMessage
	Payload                                           map[string]any
}

type pendingBrainCall struct {
	id     json.RawMessage
	timer  *time.Timer
	thread string
	callID string
}

type BrainHost struct {
	cfg          BrainHostConfig
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.Mutex
	process      brainProcess
	threads      map[string]string
	pending      map[string]pendingBrainCall
	ensures      map[string]BrainEnsure
	failures     []time.Time
	circuitUntil time.Time
}

func NewBrainHost(cfg BrainHostConfig) *BrainHost {
	if cfg.Root == "" {
		cfg.Root = filepath.Join(os.Getenv("HOME"), ".ariel", "puck")
	}
	if cfg.MaxInFlight <= 0 || cfg.MaxInFlight > 4 {
		cfg.MaxInFlight = 4
	}
	if cfg.ToolTimeout <= 0 {
		cfg.ToolTimeout = 60 * time.Second
	}
	if cfg.RestartBackoff <= 0 {
		cfg.RestartBackoff = time.Second
	}
	if cfg.Start == nil {
		cfg.Start = func(ctx context.Context, binary, cwd string) (brainProcess, error) {
			return appserver.StartBrainInteractive(ctx, binary, cwd)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &BrainHost{cfg: cfg, ctx: ctx, cancel: cancel, threads: map[string]string{}, pending: map[string]pendingBrainCall{}, ensures: map[string]BrainEnsure{}}
}

func (h *BrainHost) Ready() bool {
	if h == nil || !h.cfg.Enabled || h.cfg.Binary == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.circuitUntil.IsZero() || time.Now().After(h.circuitUntil)
}

func (h *BrainHost) Ensure(ctx context.Context, request BrainEnsure) (string, error) {
	if !h.Ready() || request.AssistantID == "" || request.Name == "" || request.DeveloperInstructions == "" || request.ToolsetVersion < 1 || strings.ContainsAny(request.AssistantID, `/\\`) {
		return "", errors.New("FEATURE_DISABLED")
	}
	workspace := filepath.Join(h.cfg.Root, request.AssistantID)
	if err := os.MkdirAll(workspace, 0700); err != nil {
		return "", err
	}
	if err := os.Chmod(workspace, 0700); err != nil {
		return "", err
	}
	process, err := h.ensureProcess(ctx)
	if err != nil {
		return "", err
	}
	params := brainThreadParams(workspace, request)
	threadID := request.ThreadID
	if threadID == "" {
		var started struct {
			Thread struct{ ID, CWD string } `json:"thread"`
		}
		if err := process.Call(ctx, "thread/start", params, &started); err != nil || started.Thread.ID == "" || started.Thread.CWD != workspace {
			return "", errors.New("BRAIN_UNAVAILABLE")
		}
		threadID = started.Thread.ID
	} else {
		params["threadId"] = threadID
		if err := process.Call(ctx, "thread/resume", params, nil); err != nil {
			return "", errors.New("BRAIN_UNAVAILABLE")
		}
	}
	h.mu.Lock()
	h.threads[threadID] = request.AssistantID
	request.ThreadID = threadID
	h.ensures[threadID] = request
	h.mu.Unlock()
	return threadID, nil
}

func brainThreadParams(cwd string, request BrainEnsure) map[string]any {
	params := map[string]any{
		"cwd": cwd, "ephemeral": false, "serviceName": "ariel-puck", "threadSource": "chatgpt_hidden",
		"developerInstructions": request.DeveloperInstructions, "dynamicTools": puckDynamicTools(), "approvalPolicy": "never", "sandbox": "read-only",
		"config": map[string]any{"features.code_mode_host": true, "tools.shell": false, "tools.unified_exec": false, "web_search": "disabled", "include_apply_patch_tool": false},
	}
	if request.Model != "" {
		params["model"] = request.Model
	}
	return params
}

func puckDynamicTools() []map[string]any {
	names := []string{"list_devices", "list_threads", "read_thread", "start_task", "send_turn", "interrupt", "respond_interaction", "get_task_status", "request_authorization", "ask_user", "share_result", "react"}
	tools := make([]map[string]any, 0, len(names))
	for _, name := range names {
		tools = append(tools, map[string]any{"name": name, "description": "Ariel Puck tool: " + name, "inputSchema": map[string]any{"type": "object", "additionalProperties": true}})
	}
	return tools
}

func (h *BrainHost) Turn(ctx context.Context, threadID, clientMessageID, text, model, effort string) (string, error) {
	h.mu.Lock()
	process, known := h.process, h.threads[threadID]
	h.mu.Unlock()
	if process == nil || known == "" {
		return "", errors.New("BRAIN_UNAVAILABLE")
	}
	params := map[string]any{"threadId": threadID, "clientUserMessageId": clientMessageID, "input": []map[string]any{{"type": "text", "text": text, "text_elements": []any{}}}, "approvalPolicy": "never", "sandboxPolicy": map[string]any{"type": "readOnly"}}
	if model != "" {
		params["model"] = model
	}
	if effort != "" {
		params["effort"] = effort
	}
	var started struct {
		Turn struct{ ID string } `json:"turn"`
	}
	if err := process.Call(ctx, "turn/start", params, &started); err != nil || started.Turn.ID == "" {
		return "", errors.New("BRAIN_UNAVAILABLE")
	}
	return started.Turn.ID, nil
}

func (h *BrainHost) Interrupt(ctx context.Context, threadID, turnID string) error {
	h.mu.Lock()
	process, known := h.process, h.threads[threadID]
	h.mu.Unlock()
	if process == nil || known == "" {
		return errors.New("BRAIN_UNAVAILABLE")
	}
	return process.Call(ctx, "turn/interrupt", map[string]any{"threadId": threadID, "turnId": turnID}, nil)
}

func (h *BrainHost) ToolResult(ctx context.Context, threadID, callID string, success bool, content []map[string]any) error {
	h.mu.Lock()
	pending, ok := h.pending[callID]
	if ok && pending.thread == threadID {
		delete(h.pending, callID)
	}
	process := h.process
	h.mu.Unlock()
	if !ok || pending.thread != threadID || process == nil {
		return errors.New("INVALID_ARGUMENT")
	}
	pending.timer.Stop()
	return process.Reply(ctx, pending.id, map[string]any{"contentItems": content, "success": success}, nil)
}

func (h *BrainHost) Retire(threadID string) { h.mu.Lock(); delete(h.threads, threadID); h.mu.Unlock() }

func (h *BrainHost) IsBrainThread(threadID string) bool {
	if h == nil || threadID == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.threads[threadID]
	return ok
}

func (h *BrainHost) ensureProcess(ctx context.Context) (brainProcess, error) {
	h.mu.Lock()
	if h.process != nil {
		p := h.process
		h.mu.Unlock()
		return p, nil
	}
	h.mu.Unlock()
	if err := os.MkdirAll(h.cfg.Root, 0700); err != nil {
		return nil, err
	}
	p, err := h.cfg.Start(h.ctx, h.cfg.Binary, h.cfg.Root)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	if h.process != nil {
		existing := h.process
		h.mu.Unlock()
		_ = p.Close()
		return existing, nil
	}
	h.process = p
	h.mu.Unlock()
	go h.run(p)
	return p, nil
}

func (h *BrainHost) run(process brainProcess) {
	for {
		select {
		case frame := <-process.Incoming():
			h.handleFrame(process, frame)
		case <-process.Done():
			h.mu.Lock()
			if h.process == process {
				h.process = nil
			}
			for id, p := range h.pending {
				p.timer.Stop()
				delete(h.pending, id)
			}
			now := time.Now()
			kept := h.failures[:0]
			for _, at := range h.failures {
				if now.Sub(at) < 10*time.Minute {
					kept = append(kept, at)
				}
			}
			h.failures = append(kept, now)
			requests := make([]BrainEnsure, 0, len(h.ensures))
			for _, request := range h.ensures {
				requests = append(requests, request)
			}
			if len(h.failures) >= 5 {
				h.circuitUntil = now.Add(10 * time.Minute)
			}
			circuitOpen := now.Before(h.circuitUntil)
			h.mu.Unlock()
			if h.cfg.Emit != nil {
				for _, request := range requests {
					h.cfg.Emit(BrainHostEvent{Kind: "puck.brain.status", AssistantID: request.AssistantID, ThreadID: request.ThreadID, Payload: map[string]any{"state": "degraded", "reason": "turn.lost"}})
				}
			}
			if !circuitOpen && len(requests) > 0 {
				go h.restart(requests)
			}
			return
		case <-h.ctx.Done():
			return
		}
	}
}

func (h *BrainHost) restart(requests []BrainEnsure) {
	timer := time.NewTimer(h.cfg.RestartBackoff)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-h.ctx.Done():
		return
	}
	for _, request := range requests {
		if _, err := h.Ensure(h.ctx, request); err != nil {
			return
		}
	}
}

func (h *BrainHost) handleFrame(process brainProcess, frame brainFrame) {
	var identity struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		Thread   *struct {
			ID string `json:"id"`
		} `json:"thread"`
		Turn *struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	_ = json.Unmarshal(frame.Params, &identity)
	if identity.ThreadID == "" && identity.Thread != nil {
		identity.ThreadID = identity.Thread.ID
	}
	if identity.TurnID == "" && identity.Turn != nil {
		identity.TurnID = identity.Turn.ID
	}
	h.mu.Lock()
	assistant := h.threads[identity.ThreadID]
	h.mu.Unlock()
	if frame.Method == "item/tool/call" && frame.IsRequest() {
		var call struct {
			ThreadID, CallID, Tool string
			Arguments              json.RawMessage
		}
		if json.Unmarshal(frame.Params, &call) != nil || len(call.Arguments) > 64<<10 {
			_ = process.Reply(context.Background(), frame.ID, map[string]any{"contentItems": []any{}, "success": false}, nil)
			return
		}
		h.mu.Lock()
		assistant := h.threads[call.ThreadID]
		if assistant == "" || len(h.pending) >= h.cfg.MaxInFlight {
			h.mu.Unlock()
			_ = process.Reply(context.Background(), frame.ID, map[string]any{"contentItems": []any{}, "success": false}, nil)
			return
		}
		timer := time.AfterFunc(h.cfg.ToolTimeout, func() { h.timeoutCall(call.CallID) })
		h.pending[call.CallID] = pendingBrainCall{id: append(json.RawMessage(nil), frame.ID...), timer: timer, thread: call.ThreadID, callID: call.CallID}
		h.mu.Unlock()
		if h.cfg.Emit != nil {
			h.cfg.Emit(BrainHostEvent{Kind: "puck.brain.toolCall", AssistantID: assistant, ThreadID: call.ThreadID, CallID: call.CallID, Tool: call.Tool, Arguments: call.Arguments})
		}
		return
	}
	if strings.Contains(frame.Method, "commandExecution") || strings.Contains(frame.Method, "fileChange") || strings.Contains(frame.Method, "collabAgent") {
		if frame.IsRequest() {
			_ = process.Reply(context.Background(), frame.ID, nil, map[string]any{"code": -32600, "message": "Puck local tool denied"})
		}
		if h.cfg.Emit != nil {
			h.cfg.Emit(BrainHostEvent{Kind: "puck.brain.status", AssistantID: assistant, ThreadID: identity.ThreadID, TurnID: identity.TurnID, Payload: map[string]any{"state": "degraded", "reason": "forbidden_local_tool"}})
		}
		return
	}
	if h.cfg.Emit != nil {
		h.cfg.Emit(BrainHostEvent{Kind: "puck.brain.event", AssistantID: assistant, ThreadID: identity.ThreadID, TurnID: identity.TurnID, Payload: map[string]any{"method": frame.Method, "params": json.RawMessage(frame.Params)}})
	}
}

func (h *BrainHost) timeoutCall(callID string) {
	h.mu.Lock()
	pending, ok := h.pending[callID]
	if ok {
		delete(h.pending, callID)
	}
	process := h.process
	h.mu.Unlock()
	if ok && process != nil {
		_ = process.Reply(context.Background(), pending.id, map[string]any{"contentItems": []map[string]any{{"type": "inputText", "text": "Puck tool timed out"}}, "success": false}, nil)
	}
}

func (h *BrainHost) Close() error {
	if h == nil {
		return nil
	}
	h.cancel()
	h.mu.Lock()
	p := h.process
	h.process = nil
	h.mu.Unlock()
	if p != nil {
		return p.Close()
	}
	return nil
}

func (a *agent) handleBrain(ctx context.Context, id, method string, raw json.RawMessage, puckOrigin bool) {
	if !puckOrigin || a.brain == nil || !a.brain.Ready() {
		a.reply(ctx, id, method, nil, errors.New("FEATURE_DISABLED"))
		return
	}
	var input struct {
		AssistantID, Name, ThreadID, DeveloperInstructions, Model, Effort string
		ToolsetVersion                                                    int
		ClientMessageID, Text, TurnID, CallID                             string
		Success                                                           bool
		ContentItems                                                      []map[string]any
	}
	if json.Unmarshal(raw, &input) != nil {
		a.reply(ctx, id, method, nil, errors.New("INVALID_ARGUMENT"))
		return
	}
	data := map[string]any{}
	var err error
	switch method {
	case "puck.brain.ensure":
		input.ThreadID, err = a.brain.Ensure(ctx, BrainEnsure{AssistantID: input.AssistantID, Name: input.Name, ThreadID: input.ThreadID, DeveloperInstructions: input.DeveloperInstructions, Model: input.Model, Effort: input.Effort, ToolsetVersion: input.ToolsetVersion})
		data["threadId"] = input.ThreadID
	case "puck.brain.turn.start":
		input.TurnID, err = a.brain.Turn(ctx, input.ThreadID, input.ClientMessageID, input.Text, input.Model, input.Effort)
		data["turnId"] = input.TurnID
	case "puck.brain.turn.interrupt":
		err = a.brain.Interrupt(ctx, input.ThreadID, input.TurnID)
	case "puck.brain.toolResult":
		err = a.brain.ToolResult(ctx, input.ThreadID, input.CallID, input.Success, input.ContentItems)
	case "puck.brain.status":
		data["ready"] = a.brain.Ready()
	case "puck.brain.retire":
		a.brain.Retire(input.ThreadID)
	default:
		err = errors.New("INVALID_ARGUMENT")
	}
	a.reply(ctx, id, method, data, err)
}

func (a *agent) emitBrainEvent(event BrainHostEvent) {
	if event.Kind == "puck.brain.toolCall" {
		_ = a.send(context.Background(), map[string]any{"type": "event", "v": 1, "event": event.Kind, "deviceId": a.deviceID, "threadId": event.ThreadID, "callId": event.CallID, "tool": event.Tool, "arguments": json.RawMessage(event.Arguments)})
		return
	}
	if event.ThreadID == "" {
		return
	}
	_ = a.send(context.Background(), map[string]any{"type": "event", "v": 1, "event": event.Kind, "deviceId": a.deviceID, "threadId": event.ThreadID, "kind": stringValue(event.Payload["method"]), "payload": event.Payload})
}

func stringValue(value any) string {
	text, _ := value.(string)
	if text == "" {
		return "status"
	}
	return text
}
