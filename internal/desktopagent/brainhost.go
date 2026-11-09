package desktopagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/puck/toolcontract"
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
	turnStarted  map[string]time.Time
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
		cfg.ToolTimeout = 4 * time.Minute
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
	return &BrainHost{cfg: cfg, ctx: ctx, cancel: cancel, threads: map[string]string{}, pending: map[string]pendingBrainCall{}, ensures: map[string]BrainEnsure{}, turnStarted: map[string]time.Time{}}
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
	if !h.Ready() || request.AssistantID == "" || request.Name == "" || request.DeveloperInstructions == "" || request.ToolsetVersion < 1 || request.ToolsetVersion > toolcontract.Version || strings.ContainsAny(request.AssistantID, `/\\`) {
		return "", errors.New("FEATURE_DISABLED")
	}
	workspace := filepath.Join(h.cfg.Root, request.AssistantID)
	if err := os.MkdirAll(workspace, 0700); err != nil {
		return "", err
	}
	if err := os.Chmod(workspace, 0700); err != nil {
		return "", err
	}
	startedAt := time.Now()
	logID := request.ThreadID
	if logID == "" {
		logID = request.AssistantID
	}
	process, err := h.ensureProcess(ctx)
	if err != nil {
		logPhase("brain-thread-ensure", logID, "durationMs", time.Since(startedAt).Milliseconds(), "ok", false, "error", requestErrorCode("puck.brain.ensure", err))
		return "", err
	}
	params := brainThreadParams(workspace, request)
	threadID := request.ThreadID
	if threadID == "" {
		var started struct {
			Thread struct{ ID, CWD string } `json:"thread"`
		}
		if err := process.Call(ctx, "thread/start", params, &started); err != nil || started.Thread.ID == "" || started.Thread.CWD != workspace {
			logPhase("brain-thread-ensure", logID, "durationMs", time.Since(startedAt).Milliseconds(), "ok", false, "error", "BRAIN_UNAVAILABLE")
			return "", errors.New("BRAIN_UNAVAILABLE")
		}
		threadID = started.Thread.ID
	} else {
		params["threadId"] = threadID
		if err := process.Call(ctx, "thread/resume", params, nil); err != nil {
			logPhase("brain-thread-ensure", logID, "durationMs", time.Since(startedAt).Milliseconds(), "ok", false, "error", "BRAIN_UNAVAILABLE")
			return "", errors.New("BRAIN_UNAVAILABLE")
		}
	}
	h.mu.Lock()
	h.threads[threadID] = request.AssistantID
	request.ThreadID = threadID
	h.ensures[threadID] = request
	h.mu.Unlock()
	logPhase("brain-thread-ensure", threadID, "durationMs", time.Since(startedAt).Milliseconds(), "ok", true)
	return threadID, nil
}

func brainThreadParams(cwd string, request BrainEnsure) map[string]any {
	params := map[string]any{
		"cwd": cwd, "ephemeral": false, "serviceName": "ariel-puck", "threadSource": "chatgpt_hidden",
		"developerInstructions": request.DeveloperInstructions, "dynamicTools": puckDynamicTools(request.ToolsetVersion), "approvalPolicy": "never", "approvalsReviewer": "user", "sandbox": "read-only",
		"config": map[string]any{"features.code_mode_host": true, "tools.shell": false, "tools.unified_exec": false, "web_search": "disabled", "include_apply_patch_tool": false},
	}
	if request.Model != "" {
		params["model"] = request.Model
	}
	return params
}

func puckDynamicTools(version int) []map[string]any {
	return toolcontract.DynamicTools(version)
}

func (h *BrainHost) Turn(ctx context.Context, threadID, clientMessageID, text string, images []string, model, effort string) (string, error) {
	startedAt := time.Now()
	h.mu.Lock()
	process, known := h.process, h.threads[threadID]
	h.mu.Unlock()
	if process == nil || known == "" {
		return "", errors.New("BRAIN_UNAVAILABLE")
	}
	input := make([]map[string]any, 0, 1+len(images))
	if text != "" {
		input = append(input, map[string]any{"type": "text", "text": text, "text_elements": []any{}})
	}
	for _, image := range images {
		input = append(input, map[string]any{"type": "image", "url": image})
	}
	params := map[string]any{"threadId": threadID, "clientUserMessageId": clientMessageID, "input": input, "approvalPolicy": "never", "sandboxPolicy": map[string]any{"type": "readOnly"}}
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
		logPhase("brain-turn-start", threadID, "durationMs", time.Since(startedAt).Milliseconds(), "ok", false, "error", "BRAIN_UNAVAILABLE")
		return "", errors.New("BRAIN_UNAVAILABLE")
	}
	h.mu.Lock()
	h.turnStarted[started.Turn.ID] = startedAt
	h.mu.Unlock()
	logPhase("brain-turn-start", threadID, "durationMs", time.Since(startedAt).Milliseconds(), "ok", true, "effort", effort, "model", model, "promptBytes", len(text))
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
	started := time.Now()
	h.mu.Lock()
	pending, ok := h.pending[callID]
	if ok && pending.thread == threadID {
		delete(h.pending, callID)
	}
	process := h.process
	h.mu.Unlock()
	if !ok || pending.thread != threadID || process == nil {
		logPhase("brain-tool-result", threadID, "durationMs", time.Since(started).Milliseconds(), "ok", false, "error", "INVALID_ARGUMENT")
		return errors.New("INVALID_ARGUMENT")
	}
	pending.timer.Stop()
	err := process.Reply(ctx, pending.id, map[string]any{"contentItems": content, "success": success}, nil)
	attrs := []any{"durationMs", time.Since(started).Milliseconds(), "ok", err == nil, "toolSuccess", success}
	if !success {
		if diagnostic, ok := toolResultDiagnostic(content); ok {
			shape, _ := json.Marshal(diagnostic.Shape)
			attrs = append(attrs, "tool", diagnostic.Tool, "rejectedField", diagnostic.Details[0].Field, "reason", diagnostic.Details[0].Reason, "expected", diagnostic.Details[0].Expected, "argumentShape", string(shape))
		}
	}
	if err != nil {
		attrs = append(attrs, "error", requestErrorCode("puck.brain.toolResult", err))
	}
	logPhase("brain-tool-result", threadID, attrs...)
	return err
}

func toolResultDiagnostic(content []map[string]any) (toolcontract.Diagnostic, bool) {
	if len(content) != 1 || content[0]["type"] != "inputText" {
		return toolcontract.Diagnostic{}, false
	}
	text, _ := content[0]["text"].(string)
	if len(text) == 0 || len(text) > 64<<10 {
		return toolcontract.Diagnostic{}, false
	}
	var envelope struct {
		Diagnostic *toolcontract.Diagnostic `json:"diagnostic"`
	}
	if json.Unmarshal([]byte(text), &envelope) != nil || envelope.Diagnostic == nil || envelope.Diagnostic.Tool == "" || len(envelope.Diagnostic.Details) == 0 {
		return toolcontract.Diagnostic{}, false
	}
	shape, _ := json.Marshal(envelope.Diagnostic.Shape)
	if len(shape) > 4096 {
		return toolcontract.Diagnostic{}, false
	}
	return *envelope.Diagnostic, true
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
	started := time.Now()
	p, err := h.cfg.Start(h.ctx, h.cfg.Binary, h.cfg.Root)
	if err != nil {
		logPhase("brain-process-start", "", "durationMs", time.Since(started).Milliseconds(), "ok", false, "error", requestErrorCode("puck.brain.process", err))
		return nil, err
	}
	logPhase("brain-process-start", "", "durationMs", time.Since(started).Milliseconds(), "ok", true)
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
			h.turnStarted = map[string]time.Time{}
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
		logPhase("brain-tool-call", call.ThreadID, "ok", true, "tool", call.Tool, "argumentBytes", len(call.Arguments))
		if h.cfg.Emit != nil {
			h.cfg.Emit(BrainHostEvent{Kind: "puck.brain.toolCall", AssistantID: assistant, ThreadID: call.ThreadID, CallID: call.CallID, Tool: call.Tool, Arguments: call.Arguments})
		}
		return
	}
	if frame.Method == "turn/completed" && identity.TurnID != "" {
		failed := brainTurnCompletionFailed(frame.Params)
		h.mu.Lock()
		started, ok := h.turnStarted[identity.TurnID]
		delete(h.turnStarted, identity.TurnID)
		h.mu.Unlock()
		if ok {
			attrs := []any{"durationMs", time.Since(started).Milliseconds(), "ok", !failed}
			if failed {
				attrs = append(attrs, "error", "BRAIN_UNAVAILABLE")
			}
			logPhase("brain-turn-complete", identity.ThreadID, attrs...)
		}
		if failed {
			if h.cfg.Emit != nil {
				h.cfg.Emit(BrainHostEvent{
					Kind: "puck.brain.event", AssistantID: assistant, ThreadID: identity.ThreadID, TurnID: identity.TurnID,
					Payload: map[string]any{
						"method": "item/agentMessage/completed",
						"params": map[string]any{"threadId": identity.ThreadID, "turnId": identity.TurnID, "item": map[string]any{"id": "brain-failure-" + identity.TurnID, "type": "agentMessage", "text": "BRAIN_UNAVAILABLE"}},
					},
				})
			}
			return
		}
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

func brainTurnCompletionFailed(raw json.RawMessage) bool {
	var completion struct {
		Turn struct {
			Status string          `json:"status"`
			Error  json.RawMessage `json:"error"`
		} `json:"turn"`
	}
	if json.Unmarshal(raw, &completion) != nil {
		return false
	}
	errorValue := bytes.TrimSpace(completion.Turn.Error)
	return completion.Turn.Status == "failed" || len(errorValue) > 0 && string(errorValue) != "null"
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
		Images                                                            []string
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
		input.TurnID, err = a.brain.Turn(ctx, input.ThreadID, input.ClientMessageID, input.Text, input.Images, input.Model, input.Effort)
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
