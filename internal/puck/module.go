package puck

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wu8685/Ariel/internal/puck/toolcontract"
	pucktools "github.com/wu8685/Ariel/internal/puck/tools"
	"github.com/wu8685/Ariel/internal/relay"
)

type Orchestrator interface {
	Devices() []map[string]any
	Forward(context.Context, relay.Origin, string, string, json.RawMessage) relay.PuckResponse
	Watch(context.Context, relay.Origin, string, string) (<-chan map[string]any, func(), *relay.PuckError)
	BrainThreads(map[relay.BrainThread]string)
	BrainEvents() <-chan relay.BrainEvent
	BrainFailures() <-chan relay.BrainDeliveryFailure
}

const (
	currentToolsetVersion = toolcontract.Version
	MaxActiveAssistants   = 50
)

type Module struct {
	store              *Store
	bridge             Orchestrator
	supervisor         *Supervisor
	tools              *pucktools.Handler
	rootCtx            context.Context
	cancel             context.CancelFunc
	wg                 sync.WaitGroup
	watchWg            sync.WaitGroup
	watchMu            sync.Mutex
	watchCancel        map[string]context.CancelFunc
	watchClosed        bool
	wakeMu             sync.Mutex
	pendingMu          sync.Mutex
	pending            map[string]pendingBrainTurn
	toolCalls          map[string][]observedToolCall
	wakeSkips          map[string][]observedWakeSkip
	lifecycle          sync.RWMutex
	ackDelay           time.Duration
	now                func() time.Time
	watchSnapshotReads atomic.Uint64
}

type observedToolCall struct {
	Tool    string
	Success bool
	Code    string
}

type observedWakeSkip struct {
	TaskKey string
	Reason  string
	At      time.Time
}

type pendingBrainTurn struct {
	threadID, turnID, messageID, responseID, groupID string
	taskEvent                                        string
	spoken                                           map[string]struct{}
	startedAt                                        time.Time
	steps                                            int
	toolFailures                                     map[string]int
	ack                                              bool
	queueKey                                         string
	timer                                            *time.Timer
}

func NewModule(dataDir string, bridge Orchestrator) (*Module, error) {
	store, err := OpenStore(filepath.Join(dataDir, "puck.db"))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Module{store: store, bridge: bridge, supervisor: NewSupervisor(3, time.Minute), rootCtx: ctx, cancel: cancel, watchCancel: map[string]context.CancelFunc{}, pending: map[string]pendingBrainTurn{}, toolCalls: map[string][]observedToolCall{}, wakeSkips: map[string][]observedWakeSkip{}, ackDelay: 3 * time.Second, now: time.Now}
	m.tools = pucktools.New(toolOrchestrator{bridge}, toolBackend{module: m})
	m.syncBrainThreads(context.Background())
	m.wg.Add(2)
	go m.runBrainEvents(ctx)
	go m.runTaskWatches(ctx)
	return m, nil
}

func (m *Module) Close() error {
	if m == nil || m.store == nil {
		return nil
	}
	m.cancel()
	m.stopWatches()
	m.pendingMu.Lock()
	for _, pending := range m.pending {
		if pending.timer != nil {
			pending.timer.Stop()
		}
	}
	m.pending = map[string]pendingBrainTurn{}
	m.pendingMu.Unlock()
	m.wg.Wait()
	m.watchWg.Wait()
	return m.store.Close()
}

// Watch emits the durable conversation snapshot immediately and whenever it
// changes. SQLite remains the source of truth; the relay only forwards these
// bounded snapshots to currently connected browsers.
func (m *Module) Watch(ctx context.Context, assistantID string) (<-chan relay.PuckResponse, func(), *relay.PuckError) {
	if _, err := m.store.Assistant(ctx, assistantID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, &relay.PuckError{Code: "PUCK_ASSISTANT_NOT_FOUND", Message: "assistant not found"}
		}
		return nil, nil, &relay.PuckError{Code: "PUCK_STORE_ERROR", Message: err.Error()}
	}

	watchCtx, cancel := context.WithCancel(ctx)
	events := make(chan relay.PuckResponse, 1)
	var once sync.Once
	stop := func() { once.Do(cancel) }
	go func() {
		defer close(events)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		lastSnapshot := ""
		lastRevision := ^uint64(0)
		emit := func(force bool) (<-chan struct{}, bool) {
			revision, changed := m.store.watchState()
			if !force && revision == lastRevision {
				return changed, true
			}
			m.watchSnapshotReads.Add(1)
			if _, err := m.store.Assistant(watchCtx, assistantID); err != nil {
				return changed, false
			}
			messages, err := m.store.Messages(watchCtx, assistantID, 200)
			if err != nil {
				return changed, false
			}
			encoded, _ := json.Marshal(messages)
			lastRevision = revision
			if string(encoded) == lastSnapshot {
				return changed, true
			}
			lastSnapshot = string(encoded)
			select {
			case events <- accepted(map[string]any{"messages": messages}):
				return changed, true
			case <-watchCtx.Done():
				return changed, false
			}
		}
		changed, ok := emit(true)
		if !ok {
			return
		}
		for {
			select {
			case <-watchCtx.Done():
				return
			case <-changed:
				changed, ok = emit(false)
				if !ok {
					return
				}
			case <-ticker.C:
				changed, ok = emit(false)
				if !ok {
					return
				}
			}
		}
	}()
	return events, stop, nil
}

func (m *Module) runBrainEvents(ctx context.Context) {
	defer m.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-m.bridge.BrainEvents():
			if !ok {
				return
			}
			m.handleBrainEvent(ctx, event)
		case failure, ok := <-m.bridge.BrainFailures():
			if !ok {
				return
			}
			m.handleBrainDeliveryFailure(ctx, failure)
		}
	}
}

func (m *Module) handleBrainDeliveryFailure(ctx context.Context, failure relay.BrainDeliveryFailure) {
	pending := m.clearPending(failure.AssistantID)
	message := Message{ID: newUUID(), AssistantID: failure.AssistantID, Role: "assistant", Kind: "error", Text: "助理事件队列繁忙，本次操作已停止，请重试。"}
	if pending.responseID != "" {
		message.ID = pending.responseID
		if pending.groupID != "" {
			_ = m.store.UpsertGroupMessage(ctx, GroupMessage{ID: message.ID, GroupID: pending.groupID, Role: "assistant", AuthorAssistantID: failure.AssistantID, Kind: "error", Text: message.Text, CreatedAt: pending.startedAt})
			return
		}
		_ = m.store.UpsertMessage(ctx, message)
		return
	}
	_ = m.store.AppendMessage(ctx, message)
}

func (m *Module) handleBrainEvent(ctx context.Context, event relay.BrainEvent) {
	m.lifecycle.RLock()
	defer m.lifecycle.RUnlock()
	a, err := m.store.Assistant(ctx, event.AssistantID)
	if err != nil || a.BrainDeviceID != event.DeviceID || a.BrainThreadID != event.ThreadID {
		return
	}
	if event.Name != "puck.brain.toolCall" {
		m.recordBrainMessage(ctx, event)
		return
	}
	var input struct {
		CallID, Tool string
		Arguments    json.RawMessage
	}
	if json.Unmarshal(event.Payload, &input) != nil || input.CallID == "" || input.Tool == "" {
		return
	}
	m.pendingMu.Lock()
	if pending, ok := m.pending[event.AssistantID]; ok {
		pending.steps++
		m.pending[event.AssistantID] = pending
	}
	m.pendingMu.Unlock()
	sourceMessageID := m.sourceMessageID(ctx, event.AssistantID)
	result := m.tools.Execute(ctx, pucktools.Call{AssistantID: event.AssistantID, SourceMessageID: sourceMessageID, Tool: input.Tool, Arguments: input.Arguments})
	code := result.Code
	if result.Success {
		code = ""
	}
	m.noteToolCall(event.AssistantID, input.Tool, result.Success, code)
	if result.Diagnostic != nil {
		diagnostic, _ := json.Marshal(result.Diagnostic)
		log.Printf("puck structured tool rejected: diagnostic=%s", diagnostic)
		if m.recordStructuredToolRejection(ctx, event.AssistantID, input.Tool, result.Diagnostic) >= 2 {
			result.Message = "arguments still do not match the canonical contract after one retry; do not retry this tool again in this turn; a friendly fallback card was shown"
		}
	} else if result.Success {
		m.clearStructuredToolFailure(event.AssistantID, input.Tool)
	}
	content, _ := json.Marshal(result)
	delivery := m.bridge.Forward(ctx, relay.Origin{Kind: "puck", AssistantID: event.AssistantID, Trigger: "tool"}, event.DeviceID, "puck.brain.toolResult", mustJSON(map[string]any{"threadId": event.ThreadID, "callId": input.CallID, "success": result.Success, "contentItems": []map[string]any{{"type": "inputText", "text": string(content)}}, "truncated": result.Truncated}))
	if delivery.Outcome != "accepted" || delivery.Error != nil {
		code := "OUTCOME_UNKNOWN"
		if delivery.Error != nil && delivery.Error.Code != "" {
			code = delivery.Error.Code
		}
		_ = m.store.Audit(ctx, AuditEntry{ID: randomID("audit"), AssistantID: event.AssistantID, Actor: "relay", Action: "brain_tool_result", Target: input.Tool, Result: code})
	}
}

func (m *Module) sourceMessageID(ctx context.Context, assistantID string) string {
	m.pendingMu.Lock()
	messageID := m.pending[assistantID].messageID
	m.pendingMu.Unlock()
	if messageID != "" {
		return messageID
	}
	messageID, _ = m.store.LatestUserMessageID(ctx, assistantID)
	return messageID
}

func (m *Module) recordStructuredToolRejection(ctx context.Context, assistantID, tool string, diagnostic *toolcontract.Diagnostic) int {
	m.pendingMu.Lock()
	pending, ok := m.pending[assistantID]
	if !ok {
		m.pendingMu.Unlock()
		return 0
	}
	if pending.toolFailures == nil {
		pending.toolFailures = map[string]int{}
	}
	pending.toolFailures[tool]++
	attempt := pending.toolFailures[tool]
	m.pending[assistantID] = pending
	m.pendingMu.Unlock()
	if attempt > 2 {
		return attempt
	}
	field, reason := "$", "invalid arguments"
	if diagnostic != nil && len(diagnostic.Details) != 0 {
		field, reason = safeDiagnosticField(diagnostic.Details[0].Field), diagnostic.Details[0].Reason
	}
	if attempt == 1 {
		text := fmt.Sprintf("%s 参数不兼容（字段 %s：%s）；助理已收到规范说明，正在重试一次。", tool, field, reason)
		_ = m.store.AppendMessage(ctx, Message{ID: newUUID(), AssistantID: assistantID, Role: "assistant", Kind: "notice", Text: text})
		return attempt
	}
	kind, text := structuredToolFallback(tool)
	card, _ := json.Marshal(map[string]any{"cardState": "invalid", "tool": tool})
	_ = m.store.AppendMessage(ctx, Message{ID: newUUID(), AssistantID: assistantID, Role: "assistant", Kind: kind, Text: text, CardJSON: string(card)})
	return attempt
}

func safeDiagnosticField(value string) string {
	if value == "" || len(value) > 256 {
		return "$"
	}
	for _, char := range value {
		if char != '_' && char != '-' && char != '.' && char != '[' && char != ']' && (char < 'A' || char > 'Z') && (char < 'a' || char > 'z') && (char < '0' || char > '9') {
			return "$"
		}
	}
	return value
}

func (m *Module) clearStructuredToolFailure(assistantID, tool string) {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	pending, ok := m.pending[assistantID]
	if !ok || pending.toolFailures == nil {
		return
	}
	delete(pending.toolFailures, tool)
	m.pending[assistantID] = pending
}

func structuredToolFallback(tool string) (string, string) {
	switch tool {
	case "ask_user":
		return "question", "暂时无法生成问题卡，请直接发送消息回答。"
	case "request_authorization":
		return "authorization", "暂时无法生成授权卡；尚未授权任何操作，请直接说明是否继续。"
	case "share_result":
		return "share_result", "暂时无法生成结果卡，请让助理重新整理结果。"
	default:
		return "notice", "助理暂时无法生成交互卡，请直接发送消息继续。"
	}
}

func (m *Module) recordBrainMessage(ctx context.Context, event relay.BrainEvent) {
	var envelope struct {
		Kind    string `json:"kind"`
		Payload struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		} `json:"payload"`
	}
	if json.Unmarshal(event.Payload, &envelope) != nil {
		return
	}
	method := envelope.Kind
	if method == "" {
		method = envelope.Payload.Method
	}
	if method != "item/completed" && method != "item/agentMessage/completed" && method != "turn/completed" {
		return
	}
	var params struct {
		TurnID string                          `json:"turnId"`
		Item   struct{ ID, Type, Text string } `json:"item"`
		Text   string                          `json:"text"`
	}
	if envelope.Payload.Params != nil && json.Unmarshal(envelope.Payload.Params, &params) != nil {
		return
	}
	if method == "turn/completed" {
		var completed struct {
			TurnID string `json:"turnId"`
			Turn   struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		_ = json.Unmarshal(envelope.Payload.Params, &completed)
		turnID := completed.TurnID
		if turnID == "" {
			turnID = completed.Turn.ID
		}
		if turnID == "" {
			return
		}
		m.finishBrainTurn(ctx, event.AssistantID, turnID)
		return
	}
	text := params.Item.Text
	if text == "" {
		text = params.Text
	}
	if text == "" || params.Item.Type != "" && params.Item.Type != "agentMessage" {
		return
	}
	pending := m.pendingTurn(event.AssistantID)
	if pending.taskEvent != "" || brainSilent(text) || turnAlreadySpoke(pending, text) {
		return
	}
	if friendly, ok := friendlyBrainError(text); ok {
		m.finishBrainTurnMessage(ctx, event.AssistantID, params.TurnID, friendly, "error")
		return
	}
	// A confirmation message is not the end of the turn. Codex still emits
	// start_task after that text; clearing busy here lets the next message.send
	// interrupt the turn and leave the tool call without contentItems.
	if pending.responseID != "" && (pending.turnID == "" || pending.turnID == params.TurnID) {
		if pending.groupID != "" {
			_ = m.store.UpsertGroupMessage(ctx, GroupMessage{ID: pending.responseID, GroupID: pending.groupID, Role: "assistant", AuthorAssistantID: event.AssistantID, Kind: "processing", Text: text, CreatedAt: pending.startedAt})
			return
		}
		kind := "processing"
		if current, err := m.store.Message(ctx, event.AssistantID, pending.responseID); err == nil && current.Kind == "greeting_processing" {
			kind = "greeting_processing"
		}
		summary, _ := json.Marshal(map[string]any{"startedAt": pending.startedAt.UTC(), "state": "running", "steps": pending.steps})
		_ = m.store.UpsertMessage(ctx, Message{ID: pending.responseID, AssistantID: event.AssistantID, Role: "assistant", Kind: kind, Text: text, WorkSummary: string(summary), BrainTurnID: params.TurnID})
	}
}

func (m *Module) pendingTurn(assistantID string) pendingBrainTurn {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	return m.pending[assistantID]
}

func (m *Module) finishBrainTurn(ctx context.Context, assistantID, turnID string) {
	pending := m.pendingTurn(assistantID)
	text := ""
	if pending.responseID != "" {
		if pending.groupID != "" {
			if current, err := m.store.GroupMessage(ctx, pending.groupID, pending.responseID); err == nil {
				text = current.Text
			}
		} else if current, err := m.store.Message(ctx, assistantID, pending.responseID); err == nil {
			text = current.Text
		}
	}
	placeholder := text == "" || text == "正在处理…" || text == "收到，我正在处理。" || text == "正在根据任务状态写一条说明…"
	if pending.taskEvent != "" || brainSilent(text) || text == "正在根据任务状态写一条说明…" || turnAlreadySpoke(pending, text) || (len(pending.spoken) > 0 && placeholder) {
		m.finishBrainTurnMessage(ctx, assistantID, turnID, "PUCK_SILENT", "text")
		return
	}
	if text == "" {
		text = "完成。"
	}
	m.finishBrainTurnMessage(ctx, assistantID, turnID, text, "text")
}

func brainSilent(text string) bool {
	trimmed := strings.Trim(strings.TrimSpace(text), "`\"'")
	return trimmed == "PUCK_SILENT"
}

func turnAlreadySpoke(pending pendingBrainTurn, text string) bool {
	if len(pending.spoken) == 0 {
		return false
	}
	_, ok := pending.spoken[strings.TrimSpace(text)]
	return ok
}

func (m *Module) rememberSpoken(assistantID, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	pending, ok := m.pending[assistantID]
	if !ok {
		return
	}
	if pending.spoken == nil {
		pending.spoken = map[string]struct{}{}
	}
	pending.spoken[text] = struct{}{}
	m.pending[assistantID] = pending
}

func (m *Module) finishBrainTurnMessage(ctx context.Context, assistantID, turnID, text, kind string) {
	pending := m.clearPendingForTurn(assistantID, turnID)
	defer m.pumpAssistant(ctx, assistantID)
	defer m.rearmUnassignedInbox(ctx, assistantID, pending.queueKey)
	// The message that just finished is still unassigned until a tool attaches
	// it. Skipping it here left the newest inbox row without a wake.
	defer m.scheduleBotQueue(ctx, assistantID, "")
	if brainSilent(text) {
		if pending.responseID != "" {
			_ = m.store.DeleteMessage(ctx, assistantID, pending.responseID)
		}
		return
	}
	if pending.responseID != "" && (pending.turnID == "" || pending.turnID == turnID) {
		if pending.groupID != "" {
			if text == "" || text == "正在处理…" || text == "收到，我正在处理。" {
				text = "完成。"
			}
			_ = m.store.UpsertGroupMessage(ctx, GroupMessage{ID: pending.responseID, GroupID: pending.groupID, Role: "assistant", AuthorAssistantID: assistantID, Kind: kind, Text: text, CreatedAt: pending.startedAt})
			return
		}
		elapsed := m.now().Sub(pending.startedAt)
		if elapsed < 0 {
			elapsed = 0
		}
		summary, _ := json.Marshal(map[string]any{"elapsedMs": elapsed.Milliseconds(), "steps": pending.steps})
		_ = m.store.UpsertMessage(ctx, Message{ID: pending.responseID, AssistantID: assistantID, Role: "assistant", Kind: kind, Text: text, CardJSON: pending.taskEvent, WorkSummary: string(summary), BrainTurnID: turnID})
		return
	}
	if kind == "error" {
		_ = m.store.AppendMessage(ctx, Message{ID: newUUID(), AssistantID: assistantID, Role: "assistant", Kind: kind, Text: text, BrainTurnID: turnID})
	}
}

func friendlyBrainError(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	switch trimmed {
	case "RESYNC_REQUIRED":
		return "实时连接暂时失效，请重试。", true
	case "BRAIN_UNAVAILABLE", "BRAIN_HOST_OFFLINE":
		return "助理暂时无法思考，请稍后重试。", true
	case "DEVICE_OFFLINE":
		return "目标设备当前离线，请稍后重试。", true
	case "TIMEOUT", "OVERLOADED", "OUTCOME_UNKNOWN", "PROTOCOL_UNSUPPORTED", "HISTORY_TOO_LARGE", "PUCK_UNAVAILABLE":
		return "操作暂时未完成，请重试。", true
	}
	if trimmed == "" || trimmed != strings.ToUpper(trimmed) || !strings.Contains(trimmed, "_") {
		return "", false
	}
	for _, char := range trimmed {
		if char != '_' && (char < 'A' || char > 'Z') && (char < '0' || char > '9') {
			return "", false
		}
	}
	return "操作暂时未完成，请重试。", true
}

type toolOrchestrator struct{ bridge Orchestrator }

func (o toolOrchestrator) Devices() []map[string]any { return o.bridge.Devices() }
func (o toolOrchestrator) CacheSnapshot(deviceID, threadID string) map[string]any {
	type reader interface {
		CacheSnapshot(string, string) map[string]any
	}
	source, ok := o.bridge.(reader)
	if !ok {
		return nil
	}
	return source.CacheSnapshot(deviceID, threadID)
}
func (o toolOrchestrator) Forward(ctx context.Context, origin pucktools.Origin, device, method string, params json.RawMessage) pucktools.Result {
	result := o.bridge.Forward(ctx, relay.Origin{Kind: origin.Kind, AssistantID: origin.AssistantID, Trigger: origin.Trigger, PermissionMode: origin.PermissionMode}, device, method, params)
	out := pucktools.Result{Outcome: result.Outcome, Data: result.Data}
	if result.Error != nil {
		out.Code, out.Message = result.Error.Code, result.Error.Message
	}
	return out
}

type toolBackend struct{ module *Module }

func (b toolBackend) Authorize(ctx context.Context, assistantID string, action pucktools.Action) (string, error) {
	var arguments struct {
		TaskID, DeviceID, CWD, ThreadID, Prompt, Text, Action, Reason, Summary string
		RiskTags                                                               []string
	}
	_ = json.Unmarshal(action.Arguments, &arguments)
	a, err := b.module.store.Assistant(ctx, assistantID)
	if err != nil {
		return "", err
	}
	allowed := map[string][]string{}
	_ = json.Unmarshal([]byte(a.AllowedScope), &allowed)
	ownedThread := false
	if arguments.TaskID != "" {
		task, taskErr := b.module.store.Task(ctx, assistantID, arguments.TaskID)
		if taskErr != nil {
			return "", taskErr
		}
		arguments.DeviceID, arguments.CWD, arguments.ThreadID = task.DeviceID, task.CWD, task.NativeThreadID
		ownedThread = true
	}
	if arguments.Prompt == "" {
		arguments.Prompt = arguments.Text
	}
	enforceable := false
	for _, device := range b.module.bridge.Devices() {
		if device["deviceId"] == arguments.DeviceID {
			caps, _ := device["capabilities"].(map[string]bool)
			enforceable = caps["executionPolicy"]
		}
	}
	decision := EvaluatePolicy(PolicyInput{PermissionMode: permissionMode(a), Tool: action.Tool, DeviceID: arguments.DeviceID, CWD: arguments.CWD, ThreadID: arguments.ThreadID, Prompt: arguments.Prompt, RiskTags: arguments.RiskTags, AllowedScope: allowed, OwnedThread: ownedThread, ExecutionPolicyEnforceable: enforceable, AssistantWorkspaceRoot: "/agent-managed/puck/" + assistantID})
	if decision.DeniedCode != "" {
		return "", errors.New(decision.DeniedCode)
	}
	if !decision.RequiresAuthorization {
		return "", nil
	}
	actionType := action.Tool
	if action.Tool == "request_authorization" {
		specific := strings.ToLower(strings.TrimSpace(arguments.Action))
		if specific != "" && len(specific) <= 192 {
			actionType += ":" + specific
		}
	}
	if matched, matchErr := b.module.store.MatchAuthorizationRule(ctx, assistantID, actionType, arguments.DeviceID, arguments.CWD); matchErr != nil {
		return "", matchErr
	} else if matched != nil {
		text := fmt.Sprintf("已按“以后都允许”自动执行：%s", authorizationActionLabel(action.Tool, arguments.Action))
		card, _ := json.Marshal(map[string]any{"action": actionType, "deviceId": arguments.DeviceID, "cwd": matched.CWD, "ruleId": matched.ID, "cardState": "auto_executed"})
		_ = b.module.store.AppendMessage(ctx, Message{ID: newUUID(), AssistantID: assistantID, Role: "assistant", Kind: "notice", Text: text, CardJSON: string(card)})
		return "", nil
	}
	auth, err := NewAuthorizationService(b.module.store).Request(ctx, assistantID, FrozenAction{Tool: action.Tool, SourceMessageID: action.SourceMessageID, ActionType: actionType, DeviceID: arguments.DeviceID, CWD: filepath.Clean(arguments.CWD), Arguments: action.Arguments}, decision.Rules, 30*time.Minute)
	if err != nil {
		return "", err
	}
	card, _ := json.Marshal(map[string]any{"authorizationId": auth.ID, "rules": decision.Rules, "actionHash": auth.ActionHash, "action": authorizationActionLabel(action.Tool, arguments.Action), "reason": arguments.Reason, "summary": arguments.Summary, "deviceId": arguments.DeviceID, "cwd": arguments.CWD, "riskTags": arguments.RiskTags, "expiresAt": auth.ExpiresAt, "cardState": "pending"})
	text := arguments.Summary
	if text == "" {
		text = "这个操作需要你确认后才能继续。"
	}
	_ = b.module.store.AppendMessage(ctx, Message{ID: "authorization-card:" + auth.ID, AssistantID: assistantID, Role: "assistant", Kind: "authorization", Text: text, CardJSON: string(card)})
	return auth.ID, pucktools.ErrAuthorizationRequired
}

func authorizationActionLabel(tool, specific string) string {
	if strings.TrimSpace(specific) != "" {
		return strings.TrimSpace(specific)
	}
	return tool
}

func (b toolBackend) PermissionMode(ctx context.Context, assistantID string) string {
	a, err := b.module.store.Assistant(ctx, assistantID)
	if err != nil {
		return "ask"
	}
	return permissionMode(a)
}
func (b toolBackend) SaveTask(ctx context.Context, task pucktools.Task) error {
	if err := b.module.store.UpsertTask(ctx, Task{ID: task.ID, AssistantID: task.AssistantID, SourceMessageID: task.SourceMessageID, DeviceID: task.DeviceID, CWD: task.CWD, NativeThreadID: task.NativeThreadID, Goal: task.Goal, Status: task.Status, CreatedBy: "puck", LastTurnID: task.LastTurnID}); err != nil {
		return err
	}
	card, _ := json.Marshal(map[string]any{"taskId": task.ID, "status": task.Status, "cardState": task.Status, "threadId": task.NativeThreadID, "deviceId": task.DeviceID, "cwd": task.CWD})
	if err := b.module.store.UpsertMessage(ctx, Message{ID: "task-card:" + task.ID, AssistantID: task.AssistantID, Role: "assistant", Kind: "task", Text: task.Goal, CardJSON: string(card)}); err != nil {
		return err
	}
	stored, err := b.module.store.Task(ctx, task.AssistantID, task.ID)
	if err != nil {
		return err
	}
	b.module.watchExecutionTask(b.module.rootCtx, stored)
	return nil
}
func (b toolBackend) Task(ctx context.Context, assistantID, taskID string) (pucktools.Task, error) {
	task, err := b.module.store.Task(ctx, assistantID, taskID)
	return pucktools.Task{ID: task.ID, AssistantID: task.AssistantID, SourceMessageID: task.SourceMessageID, DeviceID: task.DeviceID, CWD: task.CWD, NativeThreadID: task.NativeThreadID, Goal: task.Goal, Status: task.Status, LastTurnID: task.LastTurnID}, err
}
func (b toolBackend) TaskBySourceMessage(ctx context.Context, assistantID, sourceMessageID string) (pucktools.Task, error) {
	task, err := b.module.store.TaskBySourceMessage(ctx, assistantID, sourceMessageID)
	return pucktools.Task{ID: task.ID, AssistantID: task.AssistantID, SourceMessageID: task.SourceMessageID, DeviceID: task.DeviceID, CWD: task.CWD, NativeThreadID: task.NativeThreadID, Goal: task.Goal, Status: task.Status, LastTurnID: task.LastTurnID, UpdatedAt: task.UpdatedAt}, err
}
func (b toolBackend) Tasks(ctx context.Context, assistantID string) ([]pucktools.Task, error) {
	tasks, err := b.module.store.TasksByActivity(ctx, assistantID)
	if err != nil {
		return nil, err
	}
	out := make([]pucktools.Task, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, pucktools.Task{ID: task.ID, AssistantID: task.AssistantID, SourceMessageID: task.SourceMessageID, DeviceID: task.DeviceID, CWD: task.CWD, NativeThreadID: task.NativeThreadID, Goal: task.Goal, Status: task.Status, LastTurnID: task.LastTurnID, UpdatedAt: task.UpdatedAt})
	}
	return out, nil
}
func (b toolBackend) ApplyRuntime(ctx context.Context, assistantID string, update pucktools.RuntimeUpdate) error {
	task, err := b.module.store.Task(ctx, assistantID, update.TaskID)
	if err != nil {
		return err
	}
	status := update.Status
	if status == "" {
		status = task.Status
	}
	if err := b.module.store.UpdateTaskObservation(ctx, assistantID, update.TaskID, status, update.TurnID, update.Summary, task.LastSeenRevision); err != nil {
		return err
	}
	return b.module.updateTaskCardDetails(ctx, assistantID, update.TaskID, status, update.Summary)
}

func (b toolBackend) ThreadAccess(ctx context.Context, _, _, threadID string) (string, error) {
	if threadID == "" {
		return "", nil
	}
	brain, err := b.module.store.BrainThreadMatch(ctx, threadID)
	if err != nil {
		return "", err
	}
	if brain {
		return "PUCK_SELF_TARGET", nil
	}
	return "", nil
}

func (b toolBackend) ForeignThread(ctx context.Context, assistantID, deviceID, threadID string) (bool, error) {
	tasks, err := b.module.store.TasksForThread(ctx, deviceID, threadID)
	if err != nil {
		return false, err
	}
	for _, task := range tasks {
		if task.AssistantID != assistantID {
			return true, nil
		}
	}
	return false, nil
}

func (b toolBackend) SaveWatch(ctx context.Context, assistantID, deviceID, threadID, until string) (string, error) {
	existing, err := b.module.store.ThreadWatch(ctx, assistantID, deviceID, threadID)
	id := existing.ID
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
		id = newUUID()
	}
	if id == "" {
		id = newUUID()
	}
	if err := b.module.store.SaveThreadWatch(ctx, ThreadWatch{ID: id, AssistantID: assistantID, DeviceID: deviceID, ThreadID: threadID, Until: until}); err != nil {
		return "", err
	}
	b.module.startWatch(b.module.rootCtx, assistantID, deviceID, threadID, id, true)
	b.module.primeSnapshot(b.module.rootCtx, assistantID, deviceID, threadID, id, true)
	return id, nil
}

func (b toolBackend) PostNotice(ctx context.Context, assistantID, text, clientMessageID string) (string, error) {
	if _, err := b.module.store.Assistant(ctx, assistantID); err != nil {
		return "", err
	}
	id := clientMessageID
	if id == "" {
		id = newUUID()
	} else {
		var owner string
		err := b.module.store.db.QueryRowContext(ctx, `SELECT assistant_id FROM conversation_messages WHERE id=?`, id).Scan(&owner)
		if err == nil && owner != assistantID {
			return "", pucktools.ErrAuthorizationRequired
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}
	card, _ := json.Marshal(map[string]any{"proactive": true, "source": "notify_user"})
	if err := b.module.store.UpsertMessage(ctx, Message{ID: id, AssistantID: assistantID, Role: "assistant", Kind: "text", Text: text, CardJSON: string(card)}); err != nil {
		return "", err
	}
	b.module.rememberSpoken(assistantID, text)
	return id, nil
}

func (b toolBackend) NoteThreadGone(ctx context.Context, _, deviceID, threadID string) error {
	b.module.noteThreadDisappeared(ctx, deviceID, threadID)
	return nil
}

func (b toolBackend) FinishTask(ctx context.Context, assistantID, taskID string, finish pucktools.TaskFinish) (pucktools.TaskFinishResult, error) {
	task, err := b.module.store.Task(ctx, assistantID, taskID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return pucktools.TaskFinishResult{}, pucktools.ErrNotFound
		}
		return pucktools.TaskFinishResult{}, err
	}
	if task.NativeThreadID != "" {
		code, accessErr := b.ThreadAccess(ctx, assistantID, task.DeviceID, task.NativeThreadID)
		if accessErr != nil {
			return pucktools.TaskFinishResult{}, accessErr
		}
		if code == "PUCK_SELF_TARGET" {
			return pucktools.TaskFinishResult{}, pucktools.FinishError{Code: "PUCK_SELF_TARGET"}
		}
	}
	archived, err := b.module.store.ArchiveOwnedTask(ctx, assistantID, taskID, finish.Note)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return pucktools.TaskFinishResult{}, pucktools.ErrNotFound
		}
		return pucktools.TaskFinishResult{}, err
	}
	if err := b.module.updateTaskCardDetails(ctx, assistantID, taskID, "archived", finish.Note); err != nil {
		return pucktools.TaskFinishResult{}, err
	}
	if task.DeviceID != "" && task.NativeThreadID != "" {
		b.module.releaseWatch(task.DeviceID, task.NativeThreadID, assistantID)
	}
	updated, err := b.module.store.Task(ctx, assistantID, taskID)
	if err != nil {
		return pucktools.TaskFinishResult{}, err
	}
	return pucktools.TaskFinishResult{
		Task:            pucktools.Task{ID: updated.ID, AssistantID: updated.AssistantID, SourceMessageID: updated.SourceMessageID, DeviceID: updated.DeviceID, CWD: updated.CWD, NativeThreadID: updated.NativeThreadID, Goal: updated.Goal, Status: updated.Status, LastTurnID: updated.LastTurnID},
		ClosedWatches:   archived.ClosedWatches,
		ClosedWakes:     archived.ClosedWakes,
		AlreadyArchived: archived.AlreadyArchived,
	}, nil
}

func (b toolBackend) ReadOnlyThread(ctx context.Context, assistantID, deviceID, threadID string) (bool, error) {
	if _, err := b.module.store.ThreadWatch(ctx, assistantID, deviceID, threadID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if _, err := b.module.store.OwnedTaskForThread(ctx, assistantID, deviceID, threadID); err == nil {
		return false, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	return true, nil
}

func (b toolBackend) SourceText(ctx context.Context, assistantID, messageID string) (string, error) {
	if messageID == "" {
		return "", nil
	}
	message, err := b.module.store.Message(ctx, assistantID, messageID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if message.Role != "user" {
		return "", nil
	}
	return message.Text, nil
}
func (b toolBackend) Emit(ctx context.Context, event pucktools.Event) error {
	if event.Kind == "react" {
		var input struct{ MessageID, Emoji string }
		if json.Unmarshal(event.Payload, &input) != nil || input.MessageID == "" || !slices.Contains(allowedReactions, input.Emoji) {
			return errors.New("invalid reaction")
		}
		var role string
		if err := b.module.store.db.QueryRowContext(ctx, `SELECT role FROM conversation_messages WHERE assistant_id=? AND id=?`, event.AssistantID, input.MessageID).Scan(&role); err != nil {
			return err
		}
		if role != "user" {
			return errors.New("brain reactions target user messages")
		}
		_, err := b.module.store.UpdateMessageReaction(ctx, event.AssistantID, input.MessageID, "assistant", input.Emoji)
		return err
	}
	if event.Kind == "ask_user" {
		card, text, err := normalizeQuestionCard(event.Payload)
		if err != nil {
			log.Printf("puck structured card normalization failed: assistant=%s kind=question reason=invalid_payload", event.AssistantID)
			return b.module.store.UpsertMessage(ctx, Message{ID: newUUID(), AssistantID: event.AssistantID, Role: "assistant", Kind: "question", Text: unrecognizedQuestionText, CardJSON: `{"cardState":"invalid"}`})
		}
		card.QuestionID = randomID("question")
		encoded, _ := json.Marshal(card)
		return b.module.store.UpsertMessage(ctx, Message{ID: "question-card:" + card.QuestionID, AssistantID: event.AssistantID, Role: "assistant", Kind: "question", Text: text, CardJSON: string(encoded)})
	}
	kind := strings.TrimPrefix(event.Kind, "post_")
	var payload map[string]any
	if json.Unmarshal(event.Payload, &payload) != nil {
		payload = map[string]any{}
	}
	text, _ := payload["text"].(string)
	if text == "" {
		text, _ = payload["question"].(string)
	}
	if text == "" {
		text, _ = payload["summary"].(string)
	}
	if text == "" {
		text = "助手发来了一张暂时无法完整显示的卡片"
		log.Printf("puck structured card text missing: assistant=%s kind=%s reason=missing_text", event.AssistantID, kind)
	}
	messageID := newUUID()
	return b.module.store.UpsertMessage(ctx, Message{ID: messageID, AssistantID: event.AssistantID, Role: "assistant", Kind: kind, Text: text, CardJSON: string(event.Payload)})
}

func (m *Module) Handle(ctx context.Context, method string, params json.RawMessage) relay.PuckResponse {
	if m == nil || m.supervisor.Open() {
		return moduleError("PUCK_UNAVAILABLE", "Puck module unavailable")
	}
	var response relay.PuckResponse
	err := m.supervisor.Call(ctx, func(ctx context.Context) error {
		if method == "puck.delete" {
			m.lifecycle.Lock()
			defer m.lifecycle.Unlock()
		} else {
			m.lifecycle.RLock()
			defer m.lifecycle.RUnlock()
		}
		switch method {
		case "puck.list":
			response = m.list(ctx)
		case "puck.create":
			response = m.create(ctx, params)
		case "puck.update":
			response = m.update(ctx, params)
		case "puck.delete":
			response = m.delete(ctx, params)
		case "puck.message.send":
			response = m.message(ctx, params)
		case "puck.message.get":
			response = m.getMessage(ctx, params)
		case "puck.history":
			response = m.history(ctx, params)
		case "puck.read":
			response = m.read(ctx, params)
		case "puck.message.react":
			response = m.react(ctx, params)
		case "puck.stop":
			response = m.stop(ctx, params)
		case "puck.question.answer":
			response = m.answerQuestion(ctx, params)
		case "puck.authorization.decide":
			response = m.decideAuthorization(ctx, params)
		case "puck.authorization.rule.list":
			response = m.listAuthorizationRules(ctx, params)
		case "puck.authorization.rule.revoke":
			response = m.revokeAuthorizationRule(ctx, params)
		case "puck.group.list":
			response = m.groupList(ctx)
		case "puck.group.create":
			response = m.groupCreate(ctx, params)
		case "puck.group.history":
			response = m.groupHistory(ctx, params)
		case "puck.group.message.send":
			response = m.groupMessage(ctx, params)
		case "puck.section.list":
			response = m.sectionList(ctx)
		case "puck.section.create":
			response = m.sectionCreate(ctx, params)
		case "puck.section.rename":
			response = m.sectionRename(ctx, params)
		case "puck.section.delete":
			response = m.sectionDelete(ctx, params)
		case "puck.section.collapse":
			response = m.sectionCollapse(ctx, params)
		case "puck.section.move":
			response = m.sectionMove(ctx, params)
		default:
			response = moduleError("UNSUPPORTED", "Puck method is not available in P0")
		}
		return nil
	})
	if err != nil {
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	return response
}

func (m *Module) list(ctx context.Context) relay.PuckResponse {
	assistants, err := m.store.Assistants(ctx)
	if err != nil {
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	items := make([]map[string]any, 0, len(assistants))
	for _, a := range assistants {
		items = append(items, assistantData(a))
	}
	return accepted(map[string]any{"assistants": items, "maxActiveAssistants": MaxActiveAssistants})
}

func (m *Module) create(ctx context.Context, raw json.RawMessage) relay.PuckResponse {
	var input struct {
		Name           string         `json:"name"`
		Description    string         `json:"description"`
		CustomContext  string         `json:"customContext"`
		BrainDeviceID  string         `json:"brainDeviceId"`
		AllowedScope   map[string]any `json:"allowedScope"`
		Avatar         map[string]any `json:"avatar"`
		PermissionMode string         `json:"permissionMode"`
	}
	if json.Unmarshal(raw, &input) != nil || input.Name == "" || input.BrainDeviceID == "" {
		return moduleError("INVALID_ARGUMENT", "invalid assistant")
	}
	ready := false
	for _, device := range m.bridge.Devices() {
		if device["deviceId"] == input.BrainDeviceID {
			caps, _ := device["capabilities"].(map[string]bool)
			ready = caps["puckBrainReady"]
		}
	}
	if !ready {
		return moduleError("BRAIN_HOST_OFFLINE", "selected brain host is not ready")
	}
	id := randomID("assistant")
	scope, _ := json.Marshal(input.AllowedScope)
	avatar := ""
	if input.Avatar != nil {
		var err error
		avatar, err = validateAvatar(input.Avatar)
		if err != nil {
			return moduleError("INVALID_ARGUMENT", err.Error())
		}
	}
	if input.PermissionMode == "" {
		input.PermissionMode = "full"
	}
	if input.PermissionMode != "full" && input.PermissionMode != "ask" {
		return moduleError("INVALID_ARGUMENT", "invalid permission mode")
	}
	customContext, contextErr := normalizeCustomContext(input.CustomContext)
	if contextErr != nil {
		return moduleError("INVALID_ARGUMENT", contextErr.Error())
	}
	a := Assistant{ID: id, Name: input.Name, Avatar: avatar, Description: input.Description, Persona: input.Description, CustomContext: customContext, BrainDeviceID: input.BrainDeviceID, BrainCWD: "agent-managed", ToolsetVersion: m.toolsetForHost(input.BrainDeviceID), AllowedScope: string(scope), Status: "provisioning"}
	a.Policy = policyWithPermissionMode(a.Policy, input.PermissionMode)
	if err := m.store.CreateAssistantLimited(ctx, a, MaxActiveAssistants); errors.Is(err, ErrAssistantLimitReached) {
		return moduleError("OVERLOADED", fmt.Sprintf("active assistant limit reached (%d)", MaxActiveAssistants))
	} else if err != nil {
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	result := m.ensureBrain(ctx, a)
	if result.Error != nil {
		return result
	}
	threadID, _ := result.Data["threadId"].(string)
	if threadID == "" {
		_ = m.store.SoftDeleteAssistant(ctx, id)
		return moduleError("BRAIN_UNAVAILABLE", "brain thread identity missing")
	}
	if err := m.store.SetAssistantBrain(ctx, id, threadID, "ready"); err != nil {
		_ = m.store.SoftDeleteAssistant(ctx, id)
		_ = m.bridge.Forward(ctx, relay.Origin{Kind: "puck", AssistantID: id, Trigger: "user_message"}, a.BrainDeviceID, "puck.brain.retire", mustJSON(map[string]any{"threadId": threadID}))
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	a.BrainThreadID, a.Status = threadID, "ready"
	m.syncBrainThreads(ctx)
	greetingID := newUUID()
	startedAt := m.now()
	greeting := m.bridge.Forward(ctx, relay.Origin{Kind: "puck", AssistantID: a.ID, Trigger: "user_message"}, a.BrainDeviceID, "puck.brain.turn.start", mustJSON(map[string]any{"assistantId": a.ID, "threadId": a.BrainThreadID, "clientMessageId": greetingID, "text": brainEnvelope(a, "请用一条简短消息介绍自己、能做什么，并问我想先做什么。不要调用工具。"), "model": a.BrainModel, "effort": "low"}))
	if greeting.Error != nil {
		_ = m.store.AppendMessage(ctx, Message{ID: greetingID, AssistantID: a.ID, Role: "assistant", Kind: "notice", Text: fmt.Sprintf("你好，我是 %s。大脑暂时无法生成问候，你仍可以直接告诉我想做什么。", a.Name)})
		return accepted(map[string]any{"assistant": assistantData(a), "greeting": map[string]any{"state": "fallback", "messageId": greetingID}})
	}
	turnID, _ := greeting.Data["turnId"].(string)
	summary, _ := json.Marshal(map[string]any{"startedAt": startedAt.UTC(), "state": "running"})
	if err := m.store.AppendMessage(ctx, Message{ID: greetingID, AssistantID: a.ID, Role: "assistant", Kind: "greeting_processing", Text: "正在准备…", WorkSummary: string(summary), BrainTurnID: turnID, CreatedAt: startedAt}); err != nil {
		_ = m.bridge.Forward(ctx, relay.Origin{Kind: "puck", AssistantID: a.ID, Trigger: "user_message"}, a.BrainDeviceID, "puck.brain.turn.interrupt", mustJSON(map[string]any{"threadId": a.BrainThreadID, "expectedTurnId": turnID}))
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	m.setPending(a, turnID, greetingID, greetingID, startedAt, false, "")
	return accepted(map[string]any{"assistant": assistantData(a), "greeting": map[string]any{"state": "processing", "messageId": greetingID, "startedAt": startedAt.UTC()}})
}

func (m *Module) update(ctx context.Context, raw json.RawMessage) relay.PuckResponse {
	var input struct {
		AssistantID    string         `json:"assistantId"`
		Name           *string        `json:"name"`
		Description    *string        `json:"description"`
		CustomContext  *string        `json:"customContext"`
		BrainDeviceID  *string        `json:"brainDeviceId"`
		AllowedScope   map[string]any `json:"allowedScope"`
		Avatar         map[string]any `json:"avatar"`
		PermissionMode *string        `json:"permissionMode"`
	}
	if json.Unmarshal(raw, &input) != nil || input.AssistantID == "" {
		return moduleError("INVALID_ARGUMENT", "invalid assistant update")
	}
	a, err := m.store.Assistant(ctx, input.AssistantID)
	if err != nil {
		return notFound()
	}
	if a.ToolsetVersion > currentToolsetVersion {
		return moduleError("BRAIN_UNAVAILABLE", "assistant toolset is newer than this Relay")
	}
	oldDevice, oldThread := a.BrainDeviceID, a.BrainThreadID
	hostID := a.BrainDeviceID
	if input.BrainDeviceID != nil && *input.BrainDeviceID != "" {
		hostID = *input.BrainDeviceID
	}
	toolsetUpgrade := a.ToolsetVersion < currentToolsetVersion && m.toolsetForHost(hostID) >= currentToolsetVersion
	rotate := a.Status == "provisioning" || input.BrainDeviceID != nil || toolsetUpgrade
	if toolsetUpgrade {
		a.ToolsetVersion = currentToolsetVersion
	}
	if input.Name != nil && *input.Name != "" {
		a.Name = *input.Name
	}
	if input.Description != nil {
		a.Description, a.Persona = *input.Description, *input.Description
	}
	if input.CustomContext != nil {
		customContext, contextErr := normalizeCustomContext(*input.CustomContext)
		if contextErr != nil {
			return moduleError("INVALID_ARGUMENT", contextErr.Error())
		}
		a.CustomContext = customContext
	}
	if input.BrainDeviceID != nil && *input.BrainDeviceID != "" {
		a.BrainDeviceID = *input.BrainDeviceID
	}
	if input.AllowedScope != nil {
		scope, _ := json.Marshal(input.AllowedScope)
		a.AllowedScope = string(scope)
	}
	if input.PermissionMode != nil {
		if *input.PermissionMode != "full" && *input.PermissionMode != "ask" {
			return moduleError("INVALID_ARGUMENT", "invalid permission mode")
		}
		a.Policy = policyWithPermissionMode(a.Policy, *input.PermissionMode)
	}
	if input.Avatar != nil {
		avatar, err := validateAvatar(input.Avatar)
		if err != nil {
			return moduleError("INVALID_ARGUMENT", err.Error())
		}
		a.Avatar = avatar
	}
	if !m.brainReady(a.BrainDeviceID) {
		return moduleError("BRAIN_HOST_OFFLINE", "selected brain host is not ready")
	}
	if rotate {
		result := m.ensureBrain(ctx, a)
		if result.Error != nil {
			return result
		}
		threadID, _ := result.Data["threadId"].(string)
		if threadID == "" {
			return moduleError("BRAIN_UNAVAILABLE", "brain thread identity missing")
		}
		if oldThread != "" {
			history := []string{}
			_ = json.Unmarshal([]byte(defaultJSON(a.BrainThreadHistory, "[]")), &history)
			history = append(history, oldThread)
			encoded, _ := json.Marshal(history)
			a.BrainThreadHistory = string(encoded)
		}
		a.BrainThreadID, a.Status = threadID, "ready"
	}
	if err := m.store.UpdateAssistant(ctx, a); err != nil {
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	if rotate && oldThread != "" && oldThread != a.BrainThreadID {
		_ = m.bridge.Forward(ctx, relay.Origin{Kind: "puck", AssistantID: a.ID, Trigger: "user_message"}, oldDevice, "puck.brain.retire", mustJSON(map[string]any{"threadId": oldThread}))
	}
	m.syncBrainThreads(ctx)
	return accepted(map[string]any{"assistant": assistantData(a)})
}

func (m *Module) brainReady(deviceID string) bool {
	for _, device := range m.bridge.Devices() {
		if device["deviceId"] == deviceID {
			caps, _ := device["capabilities"].(map[string]bool)
			return caps["puckBrainReady"]
		}
	}
	return false
}

func advertisedToolset(devices []map[string]any, deviceID string) int {
	for _, device := range devices {
		if device["deviceId"] != deviceID {
			continue
		}
		switch value := device["puckToolsetVersion"].(type) {
		case int:
			return value
		case int64:
			return int(value)
		case float64:
			if value >= 1 && value == float64(int(value)) {
				return int(value)
			}
		}
		return 0
	}
	return 0
}

func (m *Module) toolsetForHost(deviceID string) int {
	advertised := advertisedToolset(m.bridge.Devices(), deviceID)
	if advertised < 1 || advertised > currentToolsetVersion {
		return currentToolsetVersion
	}
	return advertised
}

func (m *Module) ensureBrain(ctx context.Context, a Assistant) relay.PuckResponse {
	request := map[string]any{"assistantId": a.ID, "name": "[Puck] " + a.Name, "cwd": "/agent-managed/puck/" + a.ID, "developerInstructions": brainInstructions(a.ID, a.Name, a.Description, a.CustomContext, a.ToolsetVersion), "toolsetVersion": a.ToolsetVersion}
	return m.bridge.Forward(ctx, relay.Origin{Kind: "puck", AssistantID: a.ID, Trigger: "user_message"}, a.BrainDeviceID, "puck.brain.ensure", mustJSON(request))
}

func (m *Module) ensureCurrentToolset(ctx context.Context, a Assistant) (Assistant, *relay.PuckError) {
	if a.ToolsetVersion == currentToolsetVersion {
		return a, nil
	}
	if a.ToolsetVersion > currentToolsetVersion {
		return a, &relay.PuckError{Code: "BRAIN_UNAVAILABLE", Message: "assistant toolset is newer than this Relay"}
	}
	if m.toolsetForHost(a.BrainDeviceID) < currentToolsetVersion {
		return a, nil
	}
	oldDevice, oldThread := a.BrainDeviceID, a.BrainThreadID
	a.ToolsetVersion = currentToolsetVersion
	result := m.ensureBrain(ctx, a)
	if result.Error != nil {
		return a, result.Error
	}
	threadID, _ := result.Data["threadId"].(string)
	if threadID == "" {
		return a, &relay.PuckError{Code: "BRAIN_UNAVAILABLE", Message: "upgraded brain thread identity missing"}
	}
	if oldThread != "" {
		history := []string{}
		_ = json.Unmarshal([]byte(defaultJSON(a.BrainThreadHistory, "[]")), &history)
		history = append(history, oldThread)
		encoded, _ := json.Marshal(history)
		a.BrainThreadHistory = string(encoded)
	}
	a.BrainThreadID, a.Status = threadID, "ready"
	if err := m.store.UpdateAssistant(ctx, a); err != nil {
		return a, &relay.PuckError{Code: "PUCK_UNAVAILABLE", Message: err.Error()}
	}
	if oldThread != "" && oldThread != threadID {
		_ = m.bridge.Forward(ctx, relay.Origin{Kind: "puck", AssistantID: a.ID, Trigger: "user_message"}, oldDevice, "puck.brain.retire", mustJSON(map[string]any{"threadId": oldThread}))
	}
	m.syncBrainThreads(ctx)
	return a, nil
}

func (m *Module) resumeBrain(ctx context.Context, a Assistant) relay.PuckResponse {
	request := map[string]any{"assistantId": a.ID, "name": "[Puck] " + a.Name, "threadId": a.BrainThreadID, "cwd": "/agent-managed/puck/" + a.ID, "developerInstructions": brainInstructions(a.ID, a.Name, a.Description, a.CustomContext, a.ToolsetVersion), "model": a.BrainModel, "effort": a.BrainReasoning, "toolsetVersion": a.ToolsetVersion}
	return m.bridge.Forward(ctx, relay.Origin{Kind: "puck", AssistantID: a.ID, Trigger: "user_message"}, a.BrainDeviceID, "puck.brain.ensure", mustJSON(request))
}

func retryableBrainRejection(response relay.PuckResponse) bool {
	return response.Outcome == "rejected" && response.Error != nil && (response.Error.Code == "BRAIN_UNAVAILABLE" || response.Error.Code == "RESYNC_REQUIRED")
}

func (m *Module) startBrainTurn(ctx context.Context, a Assistant, request json.RawMessage) relay.PuckResponse {
	return m.startBrainTurnTriggered(ctx, a, request, "user_message")
}

func (m *Module) startBrainTurnTriggered(ctx context.Context, a Assistant, request json.RawMessage, trigger string) relay.PuckResponse {
	if trigger == "" {
		trigger = "user_message"
	}
	origin := relay.Origin{Kind: "puck", AssistantID: a.ID, Trigger: trigger}
	result := m.bridge.Forward(ctx, origin, a.BrainDeviceID, "puck.brain.turn.start", request)
	if !retryableBrainRejection(result) {
		return requireBrainTurnID(result)
	}
	resumed := m.resumeBrain(ctx, a)
	if resumed.Outcome != "accepted" {
		return resumed
	}
	return requireBrainTurnID(m.bridge.Forward(ctx, origin, a.BrainDeviceID, "puck.brain.turn.start", request))
}

func requireBrainTurnID(result relay.PuckResponse) relay.PuckResponse {
	if result.Outcome != "accepted" || result.Error != nil {
		return result
	}
	turnID, _ := result.Data["turnId"].(string)
	if turnID == "" {
		return moduleError("BRAIN_UNAVAILABLE", "brain turn identity missing")
	}
	return result
}

func (m *Module) delete(ctx context.Context, raw json.RawMessage) relay.PuckResponse {
	var input struct {
		AssistantID string `json:"assistantId"`
	}
	if json.Unmarshal(raw, &input) != nil {
		return moduleError("INVALID_ARGUMENT", "invalid assistant")
	}
	a, err := m.store.Assistant(ctx, input.AssistantID)
	if err != nil {
		return notFound()
	}
	if err := m.store.SoftDeleteAssistant(ctx, a.ID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return notFound()
		}
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	m.clearPending(a.ID)
	m.releaseAssistant(a.ID)
	m.syncBrainThreads(ctx)
	if a.BrainThreadID != "" {
		_ = m.bridge.Forward(ctx, relay.Origin{Kind: "puck", AssistantID: a.ID, Trigger: "user_message"}, a.BrainDeviceID, "puck.brain.retire", mustJSON(map[string]any{"threadId": a.BrainThreadID}))
	}
	return accepted(map[string]any{})
}

func (m *Module) message(ctx context.Context, raw json.RawMessage) relay.PuckResponse {
	var input struct {
		AssistantID     string   `json:"assistantId"`
		ClientMessageID string   `json:"clientMessageId"`
		Text            string   `json:"text"`
		Images          []string `json:"images"`
		ReplyTo         string   `json:"replyTo"`
	}
	if json.Unmarshal(raw, &input) != nil || (input.Text == "" && len(input.Images) == 0) || validateMessageImages(input.Images) != nil || len(input.ReplyTo) > 128 {
		return moduleError("INVALID_ARGUMENT", "invalid message")
	}
	a, err := m.store.Assistant(ctx, input.AssistantID)
	if err != nil {
		return notFound()
	}
	a, toolsetError := m.ensureCurrentToolset(ctx, a)
	if toolsetError != nil {
		return relay.PuckResponse{Outcome: "rejected", Error: toolsetError}
	}
	var replyTarget *Message
	if input.ReplyTo != "" {
		target, targetErr := m.store.Message(ctx, a.ID, input.ReplyTo)
		if targetErr != nil {
			return moduleError("INVALID_REPLY_TARGET", "reply target is not available in this assistant")
		}
		replyTarget = &target
	}
	previous := m.clearPending(a.ID)
	if previous.turnID != "" {
		_ = m.bridge.Forward(ctx, relay.Origin{Kind: "puck", AssistantID: a.ID, Trigger: "user_message"}, a.BrainDeviceID, "puck.brain.turn.interrupt", mustJSON(map[string]any{"threadId": previous.threadID, "expectedTurnId": previous.turnID}))
	}
	if err := m.deletePendingResponse(ctx, a.ID, previous); err != nil {
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	cardJSON := ""
	if len(input.Images) > 0 {
		encoded, _ := json.Marshal(map[string]any{"images": input.Images})
		cardJSON = string(encoded)
	}
	if err := m.store.AppendMessage(ctx, Message{ID: input.ClientMessageID, AssistantID: a.ID, Role: "user", Kind: "text", Text: input.Text, CardJSON: cardJSON, ReplyTo: input.ReplyTo}); err != nil {
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	if err := m.store.AddInbox(ctx, a.ID, input.ClientMessageID, m.now()); err != nil {
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	if len(input.Images) == 0 && input.ReplyTo == "" {
		if deviceID, cwd := explicitExecutionTarget(input.Text, m.bridge.Devices()); deviceID != "" {
			dispatched := m.dispatchExplicitExecution(ctx, a, input.ClientMessageID, input.Text, deviceID, cwd)
			// The named directory is not on the execution host. Rejecting the
			// send strands the conversation; the brain can list a real cwd.
			if dispatched.Error == nil || dispatched.Error.Code != "FILE_NOT_FOUND" {
				return dispatched
			}
		}
	}
	turnParams := map[string]any{"assistantId": a.ID, "threadId": a.BrainThreadID, "clientMessageId": input.ClientMessageID, "text": m.trustedEnvelope(ctx, a, input.Text, replyTarget), "model": a.BrainModel, "effort": a.BrainReasoning}
	if len(input.Images) > 0 {
		turnParams["images"] = input.Images
	}
	turnRequest := mustJSON(turnParams)
	result := m.startBrainTurn(ctx, a, turnRequest)
	if result.Error != nil {
		return result
	}
	turnID, _ := result.Data["turnId"].(string)
	startedAt := m.now()
	responseID := input.ClientMessageID + "-processing"
	summary, _ := json.Marshal(map[string]any{"startedAt": startedAt.UTC(), "state": "running"})
	if err := m.store.AppendMessage(ctx, Message{ID: responseID, AssistantID: a.ID, Role: "assistant", Kind: "processing", Text: "正在处理…", WorkSummary: string(summary), BrainTurnID: turnID, CreatedAt: startedAt}); err != nil {
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	m.setPending(a, turnID, input.ClientMessageID, responseID, startedAt, true, "")
	return accepted(map[string]any{"messageId": input.ClientMessageID, "responseId": responseID, "state": "processing", "startedAt": startedAt.UTC(), "templateAfterMs": 3000})
}

func (m *Module) getMessage(ctx context.Context, raw json.RawMessage) relay.PuckResponse {
	var input struct {
		AssistantID string `json:"assistantId"`
		MessageID   string `json:"messageId"`
	}
	if json.Unmarshal(raw, &input) != nil || input.AssistantID == "" || input.MessageID == "" || len(input.MessageID) > 128 {
		return moduleError("INVALID_ARGUMENT", "invalid message lookup")
	}
	if _, err := m.store.Assistant(ctx, input.AssistantID); err != nil {
		return notFound()
	}
	message, err := m.store.Message(ctx, input.AssistantID, input.MessageID)
	if err != nil {
		return moduleError("PUCK_MESSAGE_NOT_FOUND", "message not found")
	}
	return accepted(map[string]any{"message": message})
}

func (m *Module) dispatchExplicitExecution(ctx context.Context, a Assistant, messageID, text, deviceID, cwd string) relay.PuckResponse {
	goalRunes := []rune(strings.TrimSpace(text))
	if len(goalRunes) > 512 {
		goalRunes = goalRunes[:512]
	}
	arguments := mustJSON(map[string]any{
		"deviceId": deviceID, "cwd": cwd, "goal": string(goalRunes), "prompt": text,
		"riskTags": []string{}, "clientTaskId": newUUID(),
	})
	result := m.tools.Execute(ctx, pucktools.Call{AssistantID: a.ID, SourceMessageID: messageID, Tool: "start_task", Arguments: arguments})
	if result.Success {
		return accepted(map[string]any{"messageId": messageID, "state": "dispatched", "result": result.Data})
	}
	if result.Code == "AUTHORIZATION_REQUIRED" && result.AuthorizationID != "" {
		return accepted(map[string]any{"messageId": messageID, "state": "awaiting_authorization", "authorizationId": result.AuthorizationID})
	}
	if result.Code != "FILE_NOT_FOUND" {
		_ = m.store.AppendMessage(ctx, Message{ID: newUUID(), AssistantID: a.ID, Role: "assistant", Kind: "error", Text: result.Message})
	}
	return moduleError(result.Code, result.Message)
}

func (m *Module) markPendingTaskEvent(assistantID, card string) {
	m.pendingMu.Lock()
	pending, ok := m.pending[assistantID]
	if ok {
		pending.taskEvent = card
		m.pending[assistantID] = pending
	}
	m.pendingMu.Unlock()
}

func (m *Module) setPending(a Assistant, turnID, messageID, responseID string, startedAt time.Time, ack bool, groupID string) {
	m.clearPending(a.ID)
	pending := pendingBrainTurn{threadID: a.BrainThreadID, turnID: turnID, messageID: messageID, responseID: responseID, groupID: groupID, startedAt: startedAt, toolFailures: map[string]int{}, ack: ack}
	if !ack {
		m.pendingMu.Lock()
		m.pending[a.ID] = pending
		m.pendingMu.Unlock()
		return
	}
	pending.timer = time.AfterFunc(m.ackDelay, func() {
		m.pendingMu.Lock()
		current, ok := m.pending[a.ID]
		if ok && current.messageID == messageID {
			current.timer = nil
			m.pending[a.ID] = current
		}
		m.pendingMu.Unlock()
		if ok && current.messageID == messageID {
			if current.groupID != "" {
				_ = m.store.UpsertGroupMessage(context.Background(), GroupMessage{ID: responseID, GroupID: current.groupID, Role: "assistant", AuthorAssistantID: a.ID, Kind: "acknowledged", Text: "收到，我正在处理。", CreatedAt: startedAt})
				return
			}
			summary, _ := json.Marshal(map[string]any{"startedAt": startedAt.UTC(), "state": "running"})
			_ = m.store.UpsertMessage(context.Background(), Message{ID: responseID, AssistantID: a.ID, Role: "assistant", Kind: "acknowledged", Text: "收到，我正在处理。", WorkSummary: string(summary), BrainTurnID: turnID})
		}
	})
	m.pendingMu.Lock()
	m.pending[a.ID] = pending
	m.pendingMu.Unlock()
}

func (m *Module) deletePendingResponse(ctx context.Context, assistantID string, pending pendingBrainTurn) error {
	if pending.responseID == "" {
		return nil
	}
	var err error
	if pending.groupID != "" {
		err = m.store.DeleteGroupMessage(ctx, pending.groupID, pending.responseID)
	} else {
		err = m.store.DeleteMessage(ctx, assistantID, pending.responseID)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

func (m *Module) clearPending(assistantID string) pendingBrainTurn {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	p := m.pending[assistantID]
	if p.timer != nil {
		p.timer.Stop()
	}
	delete(m.pending, assistantID)
	return p
}

func (m *Module) clearPendingForTurn(assistantID, turnID string) pendingBrainTurn {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	p, ok := m.pending[assistantID]
	if !ok || p.turnID != "" && turnID != "" && p.turnID != turnID {
		return pendingBrainTurn{}
	}
	if p.timer != nil {
		p.timer.Stop()
	}
	delete(m.pending, assistantID)
	return p
}

func (m *Module) history(ctx context.Context, raw json.RawMessage) relay.PuckResponse {
	var input struct {
		AssistantID string `json:"assistantId"`
	}
	if json.Unmarshal(raw, &input) != nil {
		return moduleError("INVALID_ARGUMENT", "invalid assistant")
	}
	messages, err := m.store.Messages(ctx, input.AssistantID, 200)
	if err != nil {
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	assistant, err := m.store.Assistant(ctx, input.AssistantID)
	if err != nil {
		return notFound()
	}
	return accepted(map[string]any{
		"messages":      messages,
		"systemContext": systemContextDebug(assistant),
		"toolCalls":     m.recentToolCalls(input.AssistantID),
		"wakeSkips":     m.recentWakeSkips(input.AssistantID),
	})
}

func (m *Module) read(ctx context.Context, raw json.RawMessage) relay.PuckResponse {
	var input struct {
		AssistantID string `json:"assistantId"`
	}
	if json.Unmarshal(raw, &input) != nil || input.AssistantID == "" {
		return moduleError("INVALID_ARGUMENT", "invalid assistant")
	}
	if err := m.store.MarkAssistantRead(ctx, input.AssistantID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return notFound()
		}
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	return accepted(map[string]any{"unreadCount": 0})
}

var allowedReactions = []string{"👍", "❤️", "😂", "🎉", "👀"}

func (m *Module) react(ctx context.Context, raw json.RawMessage) relay.PuckResponse {
	var input struct{ AssistantID, MessageID, Emoji string }
	if json.Unmarshal(raw, &input) != nil || input.AssistantID == "" || input.MessageID == "" || !slices.Contains(allowedReactions, input.Emoji) {
		return moduleError("INVALID_ARGUMENT", "invalid reaction")
	}
	reaction, err := m.store.UpdateMessageReaction(ctx, input.AssistantID, input.MessageID, "user", input.Emoji)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return notFound()
		}
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	return accepted(map[string]any{"reaction": reaction})
}

func (m *Module) stop(ctx context.Context, raw json.RawMessage) relay.PuckResponse {
	var input struct {
		AssistantID string `json:"assistantId"`
	}
	if json.Unmarshal(raw, &input) != nil {
		return moduleError("INVALID_ARGUMENT", "invalid assistant")
	}
	a, err := m.store.Assistant(ctx, input.AssistantID)
	if err != nil {
		return notFound()
	}
	brain := m.clearPending(a.ID)
	if err := m.deletePendingResponse(ctx, a.ID, brain); err != nil {
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	results := make([]map[string]any, 0)
	brainStopped := false
	if brain.turnID != "" {
		result := m.bridge.Forward(ctx, relay.Origin{Kind: "puck", AssistantID: a.ID, Trigger: "user_message"}, a.BrainDeviceID, "puck.brain.turn.interrupt", mustJSON(map[string]any{"threadId": brain.threadID, "expectedTurnId": brain.turnID}))
		state := "stopped"
		if result.Error != nil {
			state = "reconciling"
		} else {
			brainStopped = true
		}
		results = append(results, map[string]any{"brainTurnId": brain.turnID, "state": state})
	}
	tasks, err := m.store.Tasks(ctx, a.ID)
	if err != nil {
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	stopped := 0
	for _, task := range tasks {
		if !slices.Contains([]string{"running", "dispatching", "waiting_user", "awaiting_authorization"}, task.Status) {
			continue
		}
		if task.LastTurnID == "" {
			stopped++
			_ = m.store.SetTaskStatus(ctx, a.ID, task.ID, "stopped")
			_ = m.updateTaskCard(ctx, a.ID, task.ID, "stopped")
			m.releaseWatch(task.DeviceID, task.NativeThreadID, a.ID)
			results = append(results, map[string]any{"taskId": task.ID, "state": "stopped"})
			continue
		}
		result := m.bridge.Forward(ctx, relay.Origin{Kind: "puck", AssistantID: a.ID, Trigger: "user_message"}, task.DeviceID, "turn.interrupt", mustJSON(map[string]any{"threadId": task.NativeThreadID, "expectedTurnId": task.LastTurnID}))
		state := "reconciling"
		if result.Error == nil {
			state = "stopped"
			stopped++
			_ = m.store.SetTaskStatus(ctx, a.ID, task.ID, "stopped")
			_ = m.updateTaskCard(ctx, a.ID, task.ID, "stopped")
			m.releaseWatch(task.DeviceID, task.NativeThreadID, a.ID)
		} else {
			_ = m.store.SetTaskStatus(ctx, a.ID, task.ID, "reconciling")
			_ = m.updateTaskCard(ctx, a.ID, task.ID, "reconciling")
		}
		results = append(results, map[string]any{"taskId": task.ID, "state": state})
	}
	summary := "没有可确认停止的工作"
	if brainStopped && stopped > 0 {
		summary = fmt.Sprintf("已停止当前回复，并请求停止 %d 项任务", stopped)
	} else if brainStopped {
		summary = "已停止当前回复"
	} else if stopped > 0 {
		summary = fmt.Sprintf("已请求停止 %d 项", stopped)
	}
	message := Message{ID: newUUID(), AssistantID: a.ID, Role: "assistant", Kind: "stop_summary", Text: summary}
	if err := m.store.AppendMessage(ctx, message); err != nil {
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	return accepted(map[string]any{"assistantId": a.ID, "results": results, "summary": summary, "message": message})
}

func (m *Module) answerQuestion(ctx context.Context, raw json.RawMessage) relay.PuckResponse {
	var input struct {
		AssistantID string           `json:"assistantId"`
		QuestionID  string           `json:"questionId"`
		Answer      string           `json:"answer"`
		Answers     []QuestionAnswer `json:"answers"`
	}
	if json.Unmarshal(raw, &input) != nil || input.AssistantID == "" || input.QuestionID == "" {
		return moduleError("INVALID_ARGUMENT", "invalid question answer")
	}
	a, err := m.store.Assistant(ctx, input.AssistantID)
	if err != nil {
		return notFound()
	}
	a, toolsetError := m.ensureCurrentToolset(ctx, a)
	if toolsetError != nil {
		return relay.PuckResponse{Outcome: "rejected", Error: toolsetError}
	}
	messageID := "question-card:" + input.QuestionID
	var rawCard string
	var card QuestionCard
	cardErr := m.store.db.QueryRowContext(ctx, `SELECT card_json FROM conversation_messages WHERE assistant_id=? AND id=?`, a.ID, messageID).Scan(&rawCard)
	if errors.Is(cardErr, sql.ErrNoRows) && input.Answer != "" {
		card = QuestionCard{Questions: []toolcontract.Question{{ID: input.QuestionID, Options: []toolcontract.QuestionOption{{Label: input.Answer, Value: input.Answer}}}}}
	} else if cardErr != nil {
		return notFound()
	} else if json.Unmarshal([]byte(rawCard), &card) != nil {
		return moduleError("INVALID_ARGUMENT", "question card is invalid")
	} else if len(card.Questions) == 0 && input.Answer != "" {
		card.Questions = []toolcontract.Question{{ID: input.QuestionID, Options: []toolcontract.QuestionOption{{Label: input.Answer, Value: input.Answer}}}}
	} else if len(card.Questions) == 0 {
		return moduleError("INVALID_ARGUMENT", "question card is invalid")
	}
	if len(input.Answers) == 0 && input.Answer != "" {
		if len(card.Questions) != 1 {
			return moduleError("INVALID_ARGUMENT", "legacy answer only supports one question")
		}
		input.Answers = []QuestionAnswer{{QuestionID: card.Questions[0].ID, Values: []string{input.Answer}}}
	}
	if details := validateQuestionAnswers(card, input.Answers); len(details) != 0 {
		return moduleError("INVALID_ARGUMENT", "invalid question answers")
	}
	answerJSON, _ := json.Marshal(map[string]any{"questionId": input.QuestionID, "answers": input.Answers})
	result := m.startBrainTurn(ctx, a, mustJSON(map[string]any{"assistantId": a.ID, "threadId": a.BrainThreadID, "clientMessageId": newUUID(), "text": m.trustedEnvelope(ctx, a, "问题卡回答（JSON）："+string(answerJSON), nil), "model": a.BrainModel, "effort": a.BrainReasoning}))
	if result.Error != nil {
		return result
	}
	extra := map[string]any{"answers": input.Answers}
	if len(input.Answers) == 1 && len(input.Answers[0].Values) == 1 && input.Answers[0].Custom == "" {
		extra["answer"] = input.Answers[0].Values[0]
	}
	if err := m.finalizeCard(ctx, a.ID, messageID, "answered", extra); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	return accepted(map[string]any{})
}

func (m *Module) decideAuthorization(ctx context.Context, raw json.RawMessage) relay.PuckResponse {
	var input struct {
		AssistantID     string `json:"assistantId"`
		AuthorizationID string `json:"authorizationId"`
		Decision        string `json:"decision"`
	}
	if json.Unmarshal(raw, &input) != nil || input.AssistantID == "" || input.AuthorizationID == "" {
		return moduleError("INVALID_ARGUMENT", "invalid authorization decision")
	}
	auth, err := m.store.Authorization(ctx, input.AssistantID, input.AuthorizationID)
	if err != nil {
		return notFound()
	}
	service := NewAuthorizationService(m.store)
	if input.Decision == "approve_always" {
		var action FrozenAction
		if json.Unmarshal([]byte(auth.ActionJSON), &action) != nil || !m.knownDevice(action.DeviceID) {
			return moduleError("AUTHORIZATION_SCOPE_INVALID", "authorization scope is unavailable")
		}
	}
	if err := service.Decide(ctx, input.AssistantID, input.AuthorizationID, input.Decision, auth.ActionHash); err != nil {
		code := "AUTHORIZATION_INVALID"
		if errors.Is(err, ErrAuthorizationScopeInvalid) {
			code = "AUTHORIZATION_SCOPE_INVALID"
		}
		return moduleError(code, err.Error())
	}
	if input.Decision == "reject" {
		_ = m.finalizeCard(ctx, input.AssistantID, "authorization-card:"+input.AuthorizationID, "rejected", nil)
		return accepted(map[string]any{"state": "rejected"})
	}
	action, err := service.Consume(ctx, input.AssistantID, input.AuthorizationID)
	if err != nil {
		return moduleError("AUTHORIZATION_INVALID", err.Error())
	}
	result := m.tools.ExecuteAuthorized(ctx, pucktools.Call{AssistantID: input.AssistantID, SourceMessageID: action.SourceMessageID, Tool: action.Tool, Arguments: action.Arguments})
	if !result.Success {
		_ = m.finalizeCard(ctx, input.AssistantID, "authorization-card:"+input.AuthorizationID, "failed", map[string]any{"error": result.Message})
		_ = m.store.AppendMessage(ctx, Message{ID: newUUID(), AssistantID: input.AssistantID, Role: "assistant", Kind: "error", Text: result.Message})
		return moduleError(result.Code, result.Message)
	}
	_ = m.finalizeCard(ctx, input.AssistantID, "authorization-card:"+input.AuthorizationID, "consumed", map[string]any{"decision": input.Decision})
	return accepted(map[string]any{"state": "consumed", "result": result.Data})
}

func (m *Module) knownDevice(deviceID string) bool {
	for _, device := range m.bridge.Devices() {
		if device["deviceId"] == deviceID {
			return true
		}
	}
	return false
}

func (m *Module) listAuthorizationRules(ctx context.Context, raw json.RawMessage) relay.PuckResponse {
	var input struct {
		AssistantID string `json:"assistantId"`
	}
	if json.Unmarshal(raw, &input) != nil || input.AssistantID == "" {
		return moduleError("INVALID_ARGUMENT", "invalid assistant")
	}
	if _, err := m.store.Assistant(ctx, input.AssistantID); err != nil {
		return notFound()
	}
	rules, err := m.store.AuthorizationRules(ctx, input.AssistantID)
	if err != nil {
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	return accepted(map[string]any{"rules": rules})
}

func (m *Module) revokeAuthorizationRule(ctx context.Context, raw json.RawMessage) relay.PuckResponse {
	var input struct {
		AssistantID string `json:"assistantId"`
		RuleID      string `json:"ruleId"`
	}
	if json.Unmarshal(raw, &input) != nil || input.AssistantID == "" || input.RuleID == "" {
		return moduleError("INVALID_ARGUMENT", "invalid authorization rule")
	}
	if err := m.store.RevokeAuthorizationRule(ctx, input.AssistantID, input.RuleID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return notFound()
		}
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	return accepted(map[string]any{})
}

func (m *Module) syncBrainThreads(ctx context.Context) {
	assistants, err := m.store.Assistants(ctx)
	if err != nil {
		return
	}
	threads := map[relay.BrainThread]string{}
	for _, a := range assistants {
		if a.BrainThreadID != "" {
			threads[relay.BrainThread{DeviceID: a.BrainDeviceID, ThreadID: a.BrainThreadID}] = a.ID
		}
	}
	m.bridge.BrainThreads(threads)
}

func assistantData(a Assistant) map[string]any {
	data := map[string]any{"assistantId": a.ID, "name": a.Name, "avatar": a.Avatar, "description": a.Description, "customContext": a.CustomContext, "brainDeviceId": a.BrainDeviceID, "brainThreadId": a.BrainThreadID, "status": a.Status, "toolsetVersion": a.ToolsetVersion, "permissionMode": permissionMode(a), "unreadCount": a.UnreadCount, "lastMessagePreview": a.LastMessagePreview}
	if !a.LastActivityAt.IsZero() {
		data["lastActivityAt"] = a.LastActivityAt.UTC().Format(time.RFC3339Nano)
	}
	return data
}

func permissionMode(a Assistant) string {
	var policy struct {
		PermissionMode string `json:"permissionMode"`
	}
	if json.Unmarshal([]byte(defaultJSON(a.Policy, "{}")), &policy) == nil && policy.PermissionMode == "ask" {
		return "ask"
	}
	return "full"
}

func policyWithPermissionMode(raw, mode string) string {
	policy := map[string]any{}
	_ = json.Unmarshal([]byte(defaultJSON(raw, "{}")), &policy)
	policy["permissionMode"] = mode
	encoded, _ := json.Marshal(policy)
	return string(encoded)
}

var avatarShapes = []string{"blob", "pebble", "bean", "egg", "squircle", "tablet", "capsule", "cylinder", "hex", "gem", "crystal", "wedge", "shield", "dome", "arch", "cloud", "teardrop", "leaf", "square", "sparkle", "clover", "flower", "heart", "house", "star"}

var legacyAvatarShapes = map[string]string{
	"circle": "blob", "tilted_square": "square", "four_star": "sparkle", "drop": "teardrop",
	"triangle": "wedge", "tilted_rect": "tablet", "burst": "star", "hexagon": "hex", "pumpkin": "dome",
}

var avatarColorHex = map[string]string{
	"black": "#000000", "white": "#ffffff", "brown": "#8c6640", "red": "#ea4045", "orange": "#ed702d", "yellow": "#f19d38",
	"green": "#5ac67a", "cyan": "#54b9a6", "blue": "#3c82f6", "violet": "#895bf5", "magenta": "#eb4698", "gray": "#777777",
}

var avatarPalette = []string{"black", "white", "brown", "red", "orange", "yellow", "green", "cyan", "blue", "violet", "magenta", "gray"}

func canonicalAvatarShape(shape string) (string, bool) {
	if slices.Contains(avatarShapes, shape) {
		return shape, true
	}
	mapped, ok := legacyAvatarShapes[shape]
	return mapped, ok
}

func canonicalAvatarColor(color string) (string, bool) {
	if _, ok := avatarColorHex[color]; ok {
		return color, true
	}
	hex := strings.ToLower(color)
	if len(hex) != 7 || hex[0] != '#' {
		return "", false
	}
	for _, c := range hex[1:] {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return "", false
		}
	}
	best, score := "black", 1<<31
	parse := func(index int) int {
		value, _ := strconv.ParseInt(hex[index:index+2], 16, 0)
		return int(value)
	}
	r, g, b := parse(1), parse(3), parse(5)
	for _, id := range avatarPalette {
		token := avatarColorHex[id]
		tr, _ := strconv.ParseInt(token[1:3], 16, 0)
		tg, _ := strconv.ParseInt(token[3:5], 16, 0)
		tb, _ := strconv.ParseInt(token[5:7], 16, 0)
		distance := (r-int(tr))*(r-int(tr)) + (g-int(tg))*(g-int(tg)) + (b-int(tb))*(b-int(tb))
		if distance < score {
			score, best = distance, id
		}
	}
	return best, true
}

func avatarImage(value any) string {
	image, _ := value.(string)
	image = strings.TrimSpace(image)
	if image == "" || len(image) > 262144 {
		return ""
	}
	lower := strings.ToLower(image)
	if strings.HasPrefix(lower, "https://") && !strings.ContainsAny(image, " \t\r\n") {
		return image
	}
	if strings.HasPrefix(lower, "data:image/png;base64,") || strings.HasPrefix(lower, "data:image/jpeg;base64,") || strings.HasPrefix(lower, "data:image/webp;base64,") {
		return image
	}
	return ""
}

func validateAvatar(value map[string]any) (string, error) {
	shape, _ := value["shape"].(string)
	color, _ := value["color"].(string)
	canonicalShape, ok := canonicalAvatarShape(shape)
	if !ok {
		return "", errors.New("invalid avatar shape")
	}
	canonicalColor, ok := canonicalAvatarColor(color)
	if !ok {
		return "", errors.New("invalid avatar color")
	}
	stored := map[string]string{"shape": canonicalShape, "color": canonicalColor}
	if image := avatarImage(value["image"]); image != "" {
		stored["image"] = image
	}
	encoded, _ := json.Marshal(stored)
	return string(encoded), nil
}

func (m *Module) updateTaskCard(ctx context.Context, assistantID, taskID, status string) error {
	return m.updateTaskCardDetails(ctx, assistantID, taskID, status, "")
}

func (m *Module) updateTaskCardDetails(ctx context.Context, assistantID, taskID, status, summary string) error {
	tasks := []string{"task-card:" + taskID, taskID}
	for _, id := range tasks {
		var raw string
		err := m.store.db.QueryRowContext(ctx, `SELECT card_json FROM conversation_messages WHERE id=?`, id).Scan(&raw)
		if err != nil {
			continue
		}
		var card map[string]any
		_ = json.Unmarshal([]byte(raw), &card)
		card["status"] = status
		card["cardState"] = status
		if summary != "" {
			card["summary"] = summary
		}
		encoded, _ := json.Marshal(card)
		return m.store.UpdateMessageCard(ctx, assistantID, id, string(encoded))
	}
	return nil
}

func (m *Module) finalizeCard(ctx context.Context, assistantID, messageID, state string, extra map[string]any) error {
	var raw string
	if err := m.store.db.QueryRowContext(ctx, `SELECT card_json FROM conversation_messages WHERE assistant_id=? AND id=?`, assistantID, messageID).Scan(&raw); err != nil {
		return err
	}
	var card map[string]any
	if json.Unmarshal([]byte(raw), &card) != nil {
		card = map[string]any{}
	}
	card["cardState"] = state
	for key, value := range extra {
		card[key] = value
	}
	encoded, _ := json.Marshal(card)
	return m.store.UpdateMessageCard(ctx, assistantID, messageID, string(encoded))
}
func brainEnvelope(a Assistant, text string) string {
	return brainEnvelopeWithReply(a, text, nil, nil, nil)
}

func (m *Module) trustedEnvelope(ctx context.Context, a Assistant, text string, reply *Message) string {
	tasks, err := m.store.TasksByActivity(ctx, a.ID)
	if err != nil {
		tasks = nil
	}
	openTasks, err := m.store.RecentOpenBotTasks(ctx, a.ID, 10)
	if err != nil {
		openTasks = nil
	}
	return brainEnvelopeWithReply(a, text, reply, tasks, openTasks)
}

func brainEnvelopeWithReply(a Assistant, text string, reply *Message, tasks []Task, openTasks []BotTask) string {
	context := ""
	if reply != nil {
		author := "用户"
		if reply.Role == "assistant" {
			author = "助理"
		}
		runes := []rune(strings.TrimSpace(reply.Text))
		if len(runes) > 800 {
			runes = runes[:800]
		}
		context = fmt.Sprintf("\n\n引用上下文：用户正在回复消息 %s。\n作者：%s\n类型：%s\n内容：%s", reply.ID, author, reply.Kind, string(runes))
		if reply.Kind == "task" {
			var card struct {
				TaskID string `json:"taskId"`
			}
			if json.Unmarshal([]byte(reply.CardJSON), &card) == nil && card.TaskID != "" {
				context += fmt.Sprintf("\ntaskId：%s\n这是已有任务的跟进；优先调用 send_turn(taskId)，不得新建任务，除非用户明确要求独立任务。", card.TaskID)
			}
		}
	}
	context += reusableTaskContext(tasks)
	context += openBotTaskContext(openTasks)
	return fmt.Sprintf("PUCK_ENVELOPE assistant=%s nonce=%s\n%s%s\n\n用户请求：%s", a.ID, newUUID(), envelopeContext(a), context, text)
}

func openBotTaskContext(tasks []BotTask) string {
	if len(tasks) == 0 {
		return ""
	}
	lines := make([]string, 0, len(tasks))
	for _, task := range tasks {
		title := strings.ReplaceAll(strings.TrimSpace(task.Title), "\n", " ")
		lines = append(lines, fmt.Sprintf("- id=%s title=%s", task.ID, title))
	}
	return "\n\n未完成的 Bot 任务（最近 " + strconv.Itoa(len(lines)) + " 条，只有 id 和 title）：\n" + strings.Join(lines, "\n")
}

func reusableTaskContext(tasks []Task) string {
	lines := make([]string, 0, 8)
	for _, task := range tasks {
		if len(lines) == 8 {
			break
		}
		if task.NativeThreadID == "" || task.Status == "failed_dispatch" || task.Status == "cancelled" || task.Status == "archived" {
			continue
		}
		goal := strings.ReplaceAll(strings.TrimSpace(task.Goal), "\n", " ")
		runes := []rune(goal)
		if len(runes) > 80 {
			goal = string(runes[:80]) + "…"
		}
		activity := ""
		if !task.UpdatedAt.IsZero() {
			activity = " updatedAt=" + task.UpdatedAt.UTC().Format(time.RFC3339)
		}
		lines = append(lines, fmt.Sprintf("- taskId=%s deviceId=%s cwd=%s status=%s goal=%s%s", task.ID, task.DeviceID, task.CWD, task.Status, goal, activity))
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n\n可复用执行线程（仅本助理，按最近活动）：\n" + strings.Join(lines, "\n") + "\n同一设备且同一目录的后续请求优先 send_turn(taskId)，不要 start_task。只有没有可复用线程、设备或目录不同、旧线程已不存在或不可用，或用户明确要求新会话、独立任务、separate/new session 时才新建。目标线程正在执行、等待授权或等待审批时不要另开线程；此时返回 TURN_BUSY，待结束后再续投同一 taskId。"
}

func explicitExecutionTarget(text string, devices []map[string]any) (string, string) {
	lower := strings.ToLower(text)
	intent := false
	for _, marker := range []string{"执行", "运行", "检查", "测试", "构建", "修改", "run", "check", "test", "build"} {
		if strings.Contains(lower, marker) {
			intent = true
			break
		}
	}
	if !intent {
		return "", ""
	}
	fields := strings.Fields(text)
	mentioned := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		mentioned[strings.Trim(field, "，。；;、：:（）()[]{}<>\"'`")] = struct{}{}
	}
	cwd := ""
	for _, field := range fields {
		candidate := strings.Trim(field, "，。；;、：:（）()[]{}<>\"'`")
		if filepath.IsAbs(candidate) && filepath.Clean(candidate) != string(filepath.Separator) {
			cwd = filepath.Clean(candidate)
			break
		}
	}
	if cwd == "" {
		return "", ""
	}
	for _, device := range devices {
		deviceID, _ := device["deviceId"].(string)
		if deviceID == "" {
			continue
		}
		if _, ok := mentioned[deviceID]; !ok {
			continue
		}
		supported := false
		switch caps := device["capabilities"].(type) {
		case map[string]bool:
			supported = caps["threadBootstrap"]
		case map[string]any:
			supported, _ = caps["threadBootstrap"].(bool)
		}
		if supported {
			return deviceID, cwd
		}
	}
	return "", ""
}
func accepted(data map[string]any) relay.PuckResponse {
	return relay.PuckResponse{Outcome: "accepted", Data: data}
}
func moduleError(code, message string) relay.PuckResponse {
	return relay.PuckResponse{Outcome: "rejected", Error: &relay.PuckError{Code: code, Message: message}}
}
func notFound() relay.PuckResponse {
	return moduleError("PUCK_ASSISTANT_NOT_FOUND", "assistant not found")
}
func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
