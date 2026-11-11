package tools

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/wu8685/Ariel/internal/puck/toolcontract"
)

const MaxResultBytes = 64 << 10

var (
	ErrAuthorizationRequired = errors.New("authorization required")
	ErrNotFound              = errors.New("not found")
)

type Origin struct {
	Kind, AssistantID, Trigger, PermissionMode string
}

type Result struct {
	Outcome string         `json:"outcome"`
	Data    map[string]any `json:"data,omitempty"`
	Code    string         `json:"code,omitempty"`
	Message string         `json:"message,omitempty"`
}

type Call struct {
	AssistantID     string
	SourceMessageID string
	Tool            string
	Arguments       json.RawMessage
}

type ToolResult struct {
	Success         bool                      `json:"success"`
	Code            string                    `json:"code,omitempty"`
	Message         string                    `json:"message,omitempty"`
	Details         []toolcontract.FieldError `json:"details,omitempty"`
	Diagnostic      *toolcontract.Diagnostic  `json:"diagnostic,omitempty"`
	Data            map[string]any            `json:"data,omitempty"`
	AuthorizationID string                    `json:"authorizationId,omitempty"`
	Truncated       bool                      `json:"truncated,omitempty"`
}

type Action struct {
	Tool            string
	SourceMessageID string
	Arguments       json.RawMessage
}

type Task struct {
	ID, AssistantID, SourceMessageID, DeviceID, CWD, NativeThreadID, Goal, Status, LastTurnID string
	UpdatedAt                                                                                 time.Time `json:"-"`
}

type Event struct {
	AssistantID, Kind string
	Payload           json.RawMessage
}

type Orchestrator interface {
	Devices() []map[string]any
	Forward(context.Context, Origin, string, string, json.RawMessage) Result
}

type RuntimeUpdate struct {
	TaskID, Status, TurnID, Summary string
}

type Backend interface {
	Authorize(context.Context, string, Action) (string, error)
	PermissionMode(context.Context, string) string
	SaveTask(context.Context, Task) error
	Task(context.Context, string, string) (Task, error)
	TaskBySourceMessage(context.Context, string, string) (Task, error)
	Tasks(context.Context, string) ([]Task, error)
	SourceText(context.Context, string, string) (string, error)
	Emit(context.Context, Event) error
	ApplyRuntime(context.Context, string, RuntimeUpdate) error
	ThreadAccess(context.Context, string, string, string) (string, error)
	ForeignThread(context.Context, string, string, string) (bool, error)
	SaveWatch(context.Context, string, string, string, string) (string, error)
	ReadOnlyThread(context.Context, string, string, string) (bool, error)
	FinishTask(context.Context, string, string, TaskFinish) (TaskFinishResult, error)
	PostNotice(context.Context, string, string, string) (string, error)
	NoteThreadGone(context.Context, string, string, string) error
	HandoffContext(context.Context, HandoffQuery) (Handoff, error)
}

type TaskFinish struct {
	Outcome, Note string
}

type TaskFinishResult struct {
	Task                       Task
	ClosedWatches, ClosedWakes int
	AlreadyArchived            bool
}

type FinishError struct {
	Code string
}

func (e FinishError) Error() string { return e.Code }

type Handler struct {
	orchestrator Orchestrator
	backend      Backend
}

func New(orchestrator Orchestrator, backend Backend) *Handler {
	return &Handler{orchestrator: orchestrator, backend: backend}
}

func (h *Handler) Execute(ctx context.Context, call Call) ToolResult {
	return h.execute(ctx, call, false)
}

// ExecuteAuthorized executes the exact frozen action consumed by the module.
// Callers must never expose this path directly to a Brain tool call.
func (h *Handler) ExecuteAuthorized(ctx context.Context, call Call) ToolResult {
	return h.execute(ctx, call, true)
}

func (h *Handler) execute(ctx context.Context, call Call, authorized bool) ToolResult {
	if call.AssistantID == "" {
		return ToolResult{Code: "UNAUTHORIZED", Message: "trusted assistant identity is required"}
	}
	if len(call.Arguments) > MaxResultBytes {
		return ToolResult{Code: "INVALID_ARGUMENT", Message: "tool arguments exceed 64 KiB"}
	}
	var result ToolResult
	switch call.Tool {
	case "list_devices":
		result = ToolResult{Success: true, Data: map[string]any{"devices": h.orchestrator.Devices()}}
	case "list_threads":
		result = h.forward(ctx, call, "thread.list")
	case "read_thread":
		result = h.forward(ctx, call, "thread.read")
	case "start_task":
		result = h.startTask(ctx, call, authorized)
	case "send_turn":
		result = h.sendTurn(ctx, call, authorized)
	case "interrupt":
		result = h.interrupt(ctx, call)
	case "respond_interaction":
		result = h.forward(ctx, call, "interaction.respond")
	case "get_task_status":
		result = h.taskStatus(ctx, call)
	case "watch_thread":
		result = h.watchThread(ctx, call)
	case "request_authorization":
		if value, details := toolcontract.DecodeAuthorization(call.Arguments); len(details) != 0 {
			result = invalidStructuredArguments(call.Tool, call.Arguments, details)
		} else if payload, err := json.Marshal(value); err != nil {
			result = toolError("PUCK_UNAVAILABLE", err.Error())
		} else {
			call.Arguments = payload
			result = h.requestAuthorization(ctx, call)
		}
	case "ask_user":
		value, details := toolcontract.DecodeAskUser(call.Arguments)
		if len(details) != 0 {
			result = invalidStructuredArguments(call.Tool, call.Arguments, details)
		} else if payload, err := json.Marshal(value); err != nil {
			result = toolError("PUCK_UNAVAILABLE", err.Error())
		} else if err := h.backend.Emit(ctx, Event{AssistantID: call.AssistantID, Kind: call.Tool, Payload: payload}); err != nil {
			result = toolError("PUCK_UNAVAILABLE", err.Error())
		} else {
			result = ToolResult{Success: true}
		}
	case "share_result":
		if value, details := toolcontract.DecodeShareResult(call.Arguments); len(details) != 0 {
			result = invalidStructuredArguments(call.Tool, call.Arguments, details)
		} else if payload, err := json.Marshal(value); err != nil {
			result = toolError("PUCK_UNAVAILABLE", err.Error())
		} else if err := h.backend.Emit(ctx, Event{AssistantID: call.AssistantID, Kind: call.Tool, Payload: payload}); err != nil {
			result = toolError("PUCK_UNAVAILABLE", err.Error())
		} else {
			result = ToolResult{Success: true}
		}
	case "react":
		if err := h.backend.Emit(ctx, Event{AssistantID: call.AssistantID, Kind: call.Tool, Payload: append(json.RawMessage(nil), call.Arguments...)}); err != nil {
			result = toolError("PUCK_UNAVAILABLE", err.Error())
		} else {
			result = ToolResult{Success: true}
		}
	case "finish_task":
		result = h.finishTask(ctx, call)
	case "send_message":
		result = h.sendMessage(ctx, call)
	default:
		result = toolError("INVALID_ARGUMENT", "unknown Puck tool")
	}
	return boundResult(result)
}

func (h *Handler) forward(ctx context.Context, call Call, method string) ToolResult {
	var arguments map[string]any
	if json.Unmarshal(call.Arguments, &arguments) != nil {
		return toolError("INVALID_ARGUMENT", "invalid tool arguments")
	}
	target, _ := arguments["deviceId"].(string)
	if target == "" {
		return toolError("INVALID_ARGUMENT", "deviceId is required")
	}
	delete(arguments, "deviceId")
	params := mustJSON(arguments)
	result := h.orchestrator.Forward(ctx, origin(call.AssistantID), target, method, params)
	if (method == "thread.list" || method == "thread.read") && result.Outcome == "rejected" && result.Code == "RESYNC_REQUIRED" {
		result = h.orchestrator.Forward(ctx, origin(call.AssistantID), target, method, params)
	}
	return fromResult(result)
}

func (h *Handler) startTask(ctx context.Context, call Call, authorized bool) ToolResult {
	arguments := call.Arguments
	if call.SourceMessageID != "" {
		arguments = withTrustedStartTaskID(arguments, stableTaskID(call.AssistantID, call.SourceMessageID))
	}
	input, details := toolcontract.DecodeStartTask(arguments)
	if len(details) != 0 {
		return invalidArguments("invalid start_task arguments", details)
	}
	if call.SourceMessageID != "" {
		input.ClientTaskID = stableTaskID(call.AssistantID, call.SourceMessageID)
		if existing, err := h.backend.TaskBySourceMessage(ctx, call.AssistantID, call.SourceMessageID); err == nil {
			if sameExecutionTarget(existing, input.DeviceID, input.CWD) {
				return ToolResult{Success: true, Data: taskData(existing)}
			}
			// 0137 binds this message to the task it created. A corrected cwd on
			// that same message still retargets it. A message that only continued
			// an older thread must not overwrite that thread when the cwd changes.
			if existing.ID == input.ClientTaskID {
				return h.reuseStartTask(ctx, call, existing, input, authorized)
			}
			detached := existing
			detached.SourceMessageID = ""
			if err := h.backend.SaveTask(ctx, detached); err != nil {
				return toolError("PUCK_UNAVAILABLE", err.Error())
			}
		}
	}
	if existing, err := h.backend.Task(ctx, call.AssistantID, input.ClientTaskID); err == nil {
		return h.reuseStartTask(ctx, call, existing, input, authorized)
	}
	if !authorized {
		if steered, ok := h.steerReusableStart(ctx, call, input); ok {
			return steered
		}
	}
	online, supported := h.deviceCapability(input.DeviceID, "threadBootstrap")
	if !online {
		return toolError("DEVICE_OFFLINE", "target device is offline")
	}
	if !supported {
		return toolError("PROTOCOL_UNSUPPORTED", "target device does not support atomic thread bootstrap")
	}
	if !authorized {
		if result, ok := h.authorizationResult(ctx, call, input); ok {
			return result
		}
	}
	created := h.bootstrapStart(ctx, call, input, authorized)
	if created.Outcome != "accepted" {
		return fromResult(created)
	}
	threadID, _ := created.Data["threadId"].(string)
	turnID, _ := created.Data["turnId"].(string)
	if threadID == "" || turnID == "" {
		return toolError("OUTCOME_UNKNOWN", "thread bootstrap response lacks authoritative identities")
	}
	task := Task{ID: input.ClientTaskID, AssistantID: call.AssistantID, SourceMessageID: call.SourceMessageID, DeviceID: input.DeviceID, CWD: input.CWD, NativeThreadID: threadID, Goal: input.Goal, Status: "running", LastTurnID: turnID}
	if err := h.backend.SaveTask(ctx, task); err != nil {
		return toolError("PUCK_UNAVAILABLE", err.Error())
	}
	return ToolResult{Success: true, Data: taskData(task)}
}

func (h *Handler) reuseStartTask(ctx context.Context, call Call, existing Task, input toolcontract.StartTaskRequest, authorized bool) ToolResult {
	if sameExecutionTarget(existing, input.DeviceID, input.CWD) {
		return ToolResult{Success: true, Data: taskData(existing)}
	}
	// A different cwd is not the same execution target. Reusing the old task as
	// success skips the host directory check and reports an empty code.
	online, supported := h.deviceCapability(input.DeviceID, "threadBootstrap")
	if !online {
		return toolError("DEVICE_OFFLINE", "target device is offline")
	}
	if !supported {
		return toolError("PROTOCOL_UNSUPPORTED", "target device does not support atomic thread bootstrap")
	}
	if !authorized {
		if result, ok := h.authorizationResult(ctx, call, input); ok {
			return result
		}
	}
	created := h.bootstrapStart(ctx, call, input, authorized)
	if created.Outcome != "accepted" {
		return fromResult(created)
	}
	threadID, _ := created.Data["threadId"].(string)
	turnID, _ := created.Data["turnId"].(string)
	if threadID == "" || turnID == "" {
		return toolError("OUTCOME_UNKNOWN", "thread bootstrap response lacks authoritative identities")
	}
	// The new cwd is the one that actually started. Returning the previous
	// thread and archiving this one reports the wrong task.
	task := existing
	task.DeviceID = input.DeviceID
	task.CWD = input.CWD
	task.NativeThreadID = threadID
	task.Goal = input.Goal
	task.Status = "running"
	task.LastTurnID = turnID
	if err := h.backend.SaveTask(ctx, task); err != nil {
		return toolError("PUCK_UNAVAILABLE", err.Error())
	}
	return ToolResult{Success: true, Data: taskData(task)}
}

func (h *Handler) authorizationResult(ctx context.Context, call Call, input toolcontract.StartTaskRequest) (ToolResult, bool) {
	effectiveArguments, _ := json.Marshal(input)
	authorizationID, err := h.backend.Authorize(ctx, call.AssistantID, Action{Tool: call.Tool, SourceMessageID: call.SourceMessageID, Arguments: effectiveArguments})
	if err != nil {
		if errors.Is(err, ErrAuthorizationRequired) {
			return ToolResult{Code: "AUTHORIZATION_REQUIRED", AuthorizationID: authorizationID}, true
		}
		return toolError("PUCK_UNAVAILABLE", err.Error()), true
	}
	if authorizationID != "" {
		return ToolResult{Code: "AUTHORIZATION_REQUIRED", AuthorizationID: authorizationID}, true
	}
	return ToolResult{}, false
}

func (h *Handler) requestOrigin(ctx context.Context, call Call, authorized bool) Origin {
	requestOrigin := origin(call.AssistantID)
	if authorized {
		requestOrigin.Trigger = "user_decision"
	}
	requestOrigin.PermissionMode = h.backend.PermissionMode(ctx, call.AssistantID)
	return requestOrigin
}

func (h *Handler) bootstrapStart(ctx context.Context, call Call, input toolcontract.StartTaskRequest, authorized bool) Result {
	text, failed := h.handoffText(ctx, call, input.Prompt, input.DeviceID, "", input.IncludeMessages, input.ReferenceThreads)
	if failed.Code != "" {
		return Result{Outcome: "rejected", Code: failed.Code, Message: failed.Message}
	}
	return h.orchestrator.Forward(ctx, h.requestOrigin(ctx, call, authorized), input.DeviceID, "thread.bootstrap", mustJSON(map[string]any{"cwd": input.CWD, "clientMessageId": input.ClientTaskID, "text": text}))
}

func (h *Handler) handoffText(ctx context.Context, call Call, instruction, deviceID, threadID string, includeMessages, referenceThreads []string) (string, ToolResult) {
	handoff, err := h.backend.HandoffContext(ctx, HandoffQuery{
		AssistantID: call.AssistantID, SourceMessageID: call.SourceMessageID,
		TargetDeviceID: deviceID, TargetThreadID: threadID,
		IncludeMessages: includeMessages, ReferenceThreads: referenceThreads,
	})
	if err != nil {
		return "", toolError("PUCK_UNAVAILABLE", err.Error())
	}
	return AppendHandoff(instruction, handoff), ToolResult{}
}

func withTrustedStartTaskID(raw json.RawMessage, taskID string) json.RawMessage {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return raw
	}
	if legacy, ok := fields["clientTaskId"]; ok {
		var value string
		if json.Unmarshal(legacy, &value) != nil || len(value) > 128 {
			return raw
		}
	}
	encodedID, _ := json.Marshal(taskID)
	fields["clientTaskId"] = encodedID
	encoded, err := json.Marshal(fields)
	if err != nil {
		return raw
	}
	return encoded
}

func stableTaskID(assistantID, sourceMessageID string) string {
	digest := sha256.Sum256([]byte(assistantID + "\x00" + sourceMessageID))
	digest[6] = digest[6]&0x0f | 0x50
	digest[8] = digest[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", digest[:4], digest[4:6], digest[6:8], digest[8:10], digest[10:16])
}

func sameExecutionTarget(task Task, deviceID, cwd string) bool {
	return task.DeviceID == deviceID && filepath.Clean(task.CWD) == filepath.Clean(cwd)
}

// steerReusableStart continues one of this assistant's own threads when the
// follow-up names the same device and directory. ok is false when the caller
// should create a thread instead. A frozen, already-authorized start_task must
// not call this.
func (h *Handler) steerReusableStart(ctx context.Context, call Call, input toolcontract.StartTaskRequest) (ToolResult, bool) {
	task, found, err := h.reusableTarget(ctx, call, input)
	if err != nil {
		return toolError("PUCK_UNAVAILABLE", err.Error()), true
	}
	if !found {
		return ToolResult{}, false
	}
	if taskReuseBlocked(task.Status) {
		return blockedReuseResult(task), true
	}
	result := h.continueReusableTask(ctx, call, task, input)
	if result.Code != "NOT_FOUND" {
		return result, true
	}
	gone := task
	gone.Status = "cancelled"
	if err := h.backend.SaveTask(ctx, gone); err != nil {
		return toolError("PUCK_UNAVAILABLE", err.Error()), true
	}
	return ToolResult{}, false
}

func (h *Handler) reusableTarget(ctx context.Context, call Call, input toolcontract.StartTaskRequest) (Task, bool, error) {
	if explicitNewSession(input.Goal, input.Prompt) {
		return Task{}, false, nil
	}
	if call.SourceMessageID != "" {
		text, err := h.backend.SourceText(ctx, call.AssistantID, call.SourceMessageID)
		if err != nil {
			return Task{}, false, err
		}
		if explicitNewSession(text) {
			return Task{}, false, nil
		}
	}
	tasks, err := h.backend.Tasks(ctx, call.AssistantID)
	if err != nil {
		return Task{}, false, err
	}
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].UpdatedAt.Equal(tasks[j].UpdatedAt) {
			return tasks[i].ID > tasks[j].ID
		}
		return tasks[i].UpdatedAt.After(tasks[j].UpdatedAt)
	})
	for _, task := range tasks {
		if task.AssistantID != "" && task.AssistantID != call.AssistantID {
			continue
		}
		if !sameExecutionTarget(task, input.DeviceID, input.CWD) || !taskReusable(task) {
			continue
		}
		return task, true, nil
	}
	return Task{}, false, nil
}

func (h *Handler) continueReusableTask(ctx context.Context, call Call, task Task, input toolcontract.StartTaskRequest) ToolResult {
	tags := input.RiskTags
	if tags == nil {
		tags = []string{}
	}
	payload := map[string]any{
		"taskId":          task.ID,
		"text":            input.Prompt,
		"riskTags":        tags,
		"clientMessageId": reuseClientMessageID(call.AssistantID, call.SourceMessageID, task.ID, input.Prompt),
	}
	if len(input.IncludeMessages) > 0 {
		payload["includeMessages"] = input.IncludeMessages
	}
	if len(input.ReferenceThreads) > 0 {
		payload["referenceThreads"] = input.ReferenceThreads
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return toolError("PUCK_UNAVAILABLE", err.Error())
	}
	result := h.sendTurn(ctx, Call{AssistantID: call.AssistantID, SourceMessageID: call.SourceMessageID, Tool: "send_turn", Arguments: raw}, false)
	if result.Code == "TURN_BUSY" {
		if result.Data == nil {
			result.Data = map[string]any{}
		}
		result.Data["taskId"] = task.ID
		result.Data["deviceId"] = task.DeviceID
		result.Data["threadId"] = task.NativeThreadID
		result.Data["status"] = task.Status
		result.Data["reused"] = true
		if result.Message == "" {
			result.Message = "执行线程正忙，未新建线程。请稍后对同一 taskId 调用 send_turn。"
		}
		return result
	}
	if !result.Success {
		return result
	}
	if turnID, _ := result.Data["turnId"].(string); turnID != "" {
		task.LastTurnID = turnID
	}
	task.Status = "running"
	if call.SourceMessageID != "" {
		task.SourceMessageID = call.SourceMessageID
	}
	if err := h.backend.SaveTask(ctx, task); err != nil {
		return toolError("PUCK_UNAVAILABLE", err.Error())
	}
	data := taskData(task)
	data["reused"] = true
	if task.LastTurnID != "" {
		data["turnId"] = task.LastTurnID
	}
	return ToolResult{Success: true, Data: data}
}

func reuseClientMessageID(assistantID, sourceMessageID, taskID, prompt string) string {
	return stableTaskID(assistantID, sourceMessageID+"\x00"+taskID+"\x00"+prompt)
}

func taskReusable(task Task) bool {
	if task.ID == "" || task.NativeThreadID == "" {
		return false
	}
	switch task.Status {
	case "failed_dispatch", "cancelled", "archived", "orphaned":
		return false
	default:
		return true
	}
}

func taskReuseBlocked(status string) bool {
	switch status {
	case "dispatching", "waiting_user", "awaiting_authorization", "reconciling", "stopping":
		return true
	default:
		return false
	}
}

func blockedReuseResult(task Task) ToolResult {
	return ToolResult{
		Code:    "TURN_BUSY",
		Message: fmt.Sprintf("任务 %s 状态为 %s，未新建执行线程，也未追加 turn。请等该状态结束后再 send_turn；只有用户明确要求新会话时才 start_task。", task.ID, task.Status),
		Data: map[string]any{
			"taskId":   task.ID,
			"deviceId": task.DeviceID,
			"threadId": task.NativeThreadID,
			"status":   task.Status,
			"reused":   false,
		},
	}
}

var newSessionPhrases = []string{
	"新开一个会话", "新开一个线程", "新建一个会话", "新建一个线程",
	"另开一个会话", "另开一个线程", "另起一个会话", "重新开一个会话", "重新开一个线程",
	"新的会话", "新的线程", "独立任务", "独立会话", "独立线程",
	"单独的会话", "单独会话", "单独线程",
	"新开会话", "新开线程", "另开会话", "另开线程", "新建会话", "新建线程",
	"新会话", "新线程",
	"separate session", "separate thread", "fresh session", "start a new session",
	"new session", "new thread",
}

func explicitNewSession(parts ...string) bool {
	text := strings.ToLower(strings.Join(parts, "\n"))
	for _, phrase := range newSessionPhrases {
		from := 0
		for from < len(text) {
			at := strings.Index(text[from:], phrase)
			if at < 0 {
				break
			}
			abs := from + at
			if !newSessionNegated(text, abs) {
				return true
			}
			from = abs + len(phrase)
		}
	}
	return false
}

func newSessionNegated(text string, at int) bool {
	runes := []rune(text[:at])
	if len(runes) > 32 {
		runes = runes[len(runes)-32:]
	}
	prefix := trimReuseFillers(string(runes))
	for _, word := range []string{"不要", "不需要", "不用", "无需", "不是", "don't", "do not", "not"} {
		if strings.HasSuffix(prefix, word) {
			return true
		}
	}
	if strings.HasSuffix(prefix, "别") {
		prefixRunes := []rune(prefix)
		if len(prefixRunes) == 1 {
			return true
		}
		prev := prefixRunes[len(prefixRunes)-2]
		if !unicode.Is(unicode.Han, prev) && !unicode.IsLetter(prev) {
			return true
		}
	}
	return false
}

func trimReuseFillers(prefix string) string {
	fillers := []string{"please", "start", "the", "an", "a", "请", "帮我", "给我", "想", "再", "一个", "开"}
	for {
		trimmed := strings.TrimRight(prefix, " \t\r\n，。；;、：:（）()[]{}<>\"'`")
		stripped := false
		for _, filler := range fillers {
			if strings.HasSuffix(trimmed, filler) {
				trimmed = strings.TrimSuffix(trimmed, filler)
				stripped = true
				break
			}
		}
		prefix = trimmed
		if !stripped {
			return prefix
		}
	}
}

func (h *Handler) sendTurn(ctx context.Context, call Call, authorized bool) ToolResult {
	input, details := toolcontract.DecodeSendTurn(call.Arguments)
	if len(details) != 0 {
		return invalidArguments("invalid send_turn arguments", details)
	}
	deviceID, threadID := input.DeviceID, input.ThreadID
	if input.TaskID != "" {
		task, err := h.backend.Task(ctx, call.AssistantID, input.TaskID)
		if err != nil {
			return toolError("PUCK_ASSISTANT_NOT_FOUND", "task not found")
		}
		deviceID, threadID = task.DeviceID, task.NativeThreadID
	} else if deviceID != "" && threadID != "" {
		readOnly, err := h.backend.ReadOnlyThread(ctx, call.AssistantID, deviceID, threadID)
		if err != nil {
			return toolError("PUCK_UNAVAILABLE", err.Error())
		}
		if readOnly {
			return toolError("WATCH_ONLY", "this thread is watched read-only and cannot receive turns")
		}
	}
	online, supported := h.deviceCapability(deviceID, "send")
	if !online {
		return toolError("DEVICE_OFFLINE", "target device is offline")
	}
	if !supported {
		return toolError("PROTOCOL_UNSUPPORTED", "target device does not support sending turns")
	}
	if !authorized {
		authorizationID, err := h.backend.Authorize(ctx, call.AssistantID, Action{Tool: call.Tool, Arguments: call.Arguments})
		if err != nil {
			if errors.Is(err, ErrAuthorizationRequired) {
				return ToolResult{Code: "AUTHORIZATION_REQUIRED", AuthorizationID: authorizationID}
			}
			return toolError("PUCK_UNAVAILABLE", err.Error())
		}
		if authorizationID != "" {
			return ToolResult{Code: "AUTHORIZATION_REQUIRED", AuthorizationID: authorizationID}
		}
	}
	requestOrigin := origin(call.AssistantID)
	if authorized {
		requestOrigin.Trigger = "user_decision"
	}
	requestOrigin.PermissionMode = h.backend.PermissionMode(ctx, call.AssistantID)
	text, failed := h.handoffText(ctx, call, input.Text, deviceID, threadID, input.IncludeMessages, input.ReferenceThreads)
	if failed.Code != "" {
		return failed
	}
	params := mustJSON(map[string]any{"threadId": threadID, "clientMessageId": input.ClientMessageID, "text": text})
	result := fromResult(h.orchestrator.Forward(ctx, requestOrigin, deviceID, "turn.start", params))
	if result.Code == "NOT_FOUND" && deviceID != "" && threadID != "" {
		_ = h.backend.NoteThreadGone(ctx, call.AssistantID, deviceID, threadID)
	}
	return result
}

func (h *Handler) deviceCapability(deviceID, capability string) (bool, bool) {
	for _, device := range h.orchestrator.Devices() {
		if device["deviceId"] != deviceID {
			continue
		}
		if device["agentOnline"] != true {
			return false, false
		}
		switch capabilities := device["capabilities"].(type) {
		case map[string]bool:
			return true, capabilities[capability]
		case map[string]any:
			supported, _ := capabilities[capability].(bool)
			return true, supported
		}
		return true, false
	}
	return false, false
}

func (h *Handler) interrupt(ctx context.Context, call Call) ToolResult {
	var input struct{ TaskID, TurnID string }
	if json.Unmarshal(call.Arguments, &input) != nil || input.TaskID == "" || input.TurnID == "" {
		return toolError("INVALID_ARGUMENT", "invalid interrupt arguments")
	}
	task, err := h.backend.Task(ctx, call.AssistantID, input.TaskID)
	if err != nil {
		return toolError("PUCK_ASSISTANT_NOT_FOUND", "task not found")
	}
	return fromResult(h.orchestrator.Forward(ctx, origin(call.AssistantID), task.DeviceID, "turn.interrupt", mustJSON(map[string]any{"threadId": task.NativeThreadID, "expectedTurnId": input.TurnID})))
}

func (h *Handler) taskStatus(ctx context.Context, call Call) ToolResult {
	var input struct {
		TaskID string `json:"taskId"`
	}
	if json.Unmarshal(call.Arguments, &input) != nil || input.TaskID == "" {
		return toolError("INVALID_ARGUMENT", "taskId is required")
	}
	task, err := h.backend.Task(ctx, call.AssistantID, input.TaskID)
	if err != nil {
		return toolError("PUCK_ASSISTANT_NOT_FOUND", "task not found")
	}
	data := taskData(task)
	view, fresh := h.liveTask(ctx, call.AssistantID, task)
	if view.Status == "orphaned" {
		data["status"] = "orphaned"
		data["runtime"] = "missing"
		return ToolResult{Success: true, Data: data}
	}
	if !fresh {
		data["runtime"] = "unknown"
		return ToolResult{Success: true, Data: data}
	}
	data["runtime"] = view.Runtime
	if view.TurnStatus != "" {
		data["turnStatus"] = view.TurnStatus
	}
	if view.Summary != "" {
		data["summary"] = view.Summary
	}
	if view.Status != "" {
		outcome := DecideOutcome(view, task.Goal, "", "")
		status := view.Status
		if view.Status == "review" && !outcome.Notify {
			status = task.Status
			if status == "" {
				status = "running"
			}
		} else if outcome.Notify && outcome.Status != "" {
			status = outcome.Status
		}
		data["status"] = status
		if status != task.Status && outcome.Notify {
			_ = h.backend.ApplyRuntime(ctx, call.AssistantID, RuntimeUpdate{TaskID: task.ID, Status: status, TurnID: view.TurnID, Summary: view.Summary})
		}
	}
	return ToolResult{Success: true, Data: data}
}

func (h *Handler) liveTask(ctx context.Context, assistantID string, task Task) (ThreadView, bool) {
	if task.DeviceID == "" || task.NativeThreadID == "" {
		return ThreadView{}, false
	}
	if source, ok := h.orchestrator.(interface {
		CacheSnapshot(string, string) map[string]any
	}); ok {
		if snap := source.CacheSnapshot(task.DeviceID, task.NativeThreadID); snap != nil {
			if view, ok := ClassifySnapshot(snap); ok && view.Freshness == "fresh" && view.Runtime != "" && view.Runtime != "unknown" {
				return view, true
			}
		}
	}
	result := h.orchestrator.Forward(ctx, origin(assistantID), task.DeviceID, "thread.read", mustJSON(map[string]any{"threadId": task.NativeThreadID}))
	if result.Code == "NOT_FOUND" {
		_ = h.backend.NoteThreadGone(ctx, assistantID, task.DeviceID, task.NativeThreadID)
		return ThreadView{Status: "orphaned", Runtime: "missing"}, false
	}
	if result.Outcome != "accepted" {
		return ThreadView{}, false
	}
	view, ok := ClassifySnapshot(result.Data)
	if !ok || view.Runtime == "" || view.Runtime == "unknown" {
		return ThreadView{}, false
	}
	return view, true
}

func (h *Handler) watchThread(ctx context.Context, call Call) ToolResult {
	var input struct {
		DeviceID string `json:"deviceId"`
		ThreadID string `json:"threadId"`
		Until    string `json:"until"`
	}
	if json.Unmarshal(call.Arguments, &input) != nil || strings.TrimSpace(input.DeviceID) == "" || strings.TrimSpace(input.ThreadID) == "" || len(input.DeviceID) > 128 || len(input.ThreadID) > 128 || len([]rune(input.Until)) > 240 {
		return toolError("INVALID_ARGUMENT", "deviceId and threadId are required")
	}
	code, err := h.backend.ThreadAccess(ctx, call.AssistantID, input.DeviceID, input.ThreadID)
	if err != nil {
		return toolError("PUCK_UNAVAILABLE", err.Error())
	}
	if code == "PUCK_SELF_TARGET" {
		return toolError("PUCK_SELF_TARGET", "brain thread cannot be watched")
	}
	foreign, err := h.backend.ForeignThread(ctx, call.AssistantID, input.DeviceID, input.ThreadID)
	if err != nil {
		return toolError("PUCK_UNAVAILABLE", err.Error())
	}
	if foreign {
		return toolError("UNAUTHORIZED", "another assistant owns this thread")
	}
	known := false
	for _, device := range h.orchestrator.Devices() {
		if device["deviceId"] == input.DeviceID {
			known = true
			break
		}
	}
	if !known {
		return toolError("DEVICE_OFFLINE", "target device is offline")
	}
	watchID, err := h.backend.SaveWatch(ctx, call.AssistantID, input.DeviceID, input.ThreadID, strings.TrimSpace(input.Until))
	if err != nil {
		return toolError("PUCK_UNAVAILABLE", err.Error())
	}
	return ToolResult{Success: true, Data: map[string]any{"watchId": watchID, "deviceId": input.DeviceID, "threadId": input.ThreadID, "watching": true, "watchOnly": true}}
}

func (h *Handler) sendMessage(ctx context.Context, call Call) ToolResult {
	input, details := toolcontract.DecodeSendMessage(call.Arguments)
	if len(details) != 0 {
		return invalidArguments("invalid send_message arguments", details)
	}
	id, err := h.backend.PostNotice(ctx, call.AssistantID, input.Text, input.ClientMessageID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return toolError("PUCK_ASSISTANT_NOT_FOUND", "assistant not found")
		}
		if errors.Is(err, ErrAuthorizationRequired) {
			return toolError("UNAUTHORIZED", "message belongs to another assistant")
		}
		return toolError("PUCK_UNAVAILABLE", err.Error())
	}
	return ToolResult{Success: true, Data: map[string]any{"messageId": id, "posted": true}}
}

func (h *Handler) finishTask(ctx context.Context, call Call) ToolResult {
	input, details := toolcontract.DecodeFinishTask(call.Arguments)
	if len(details) != 0 {
		return invalidArguments("invalid finish_task arguments", details)
	}
	finished, err := h.backend.FinishTask(ctx, call.AssistantID, input.TaskID, TaskFinish{Outcome: input.Outcome, Note: input.Note})
	if err != nil {
		var coded FinishError
		if errors.As(err, &coded) {
			return toolError(coded.Code, "finish_task was refused")
		}
		if errors.Is(err, ErrNotFound) {
			return toolError("PUCK_ASSISTANT_NOT_FOUND", "task not found")
		}
		return toolError("PUCK_UNAVAILABLE", "finish_task failed")
	}
	return ToolResult{Success: true, Data: map[string]any{
		"taskId": finished.Task.ID, "status": finished.Task.Status, "outcome": input.Outcome,
		"closedWatches": finished.ClosedWatches, "closedWakes": finished.ClosedWakes, "alreadyArchived": finished.AlreadyArchived,
	}}
}

func (h *Handler) requestAuthorization(ctx context.Context, call Call) ToolResult {
	id, err := h.backend.Authorize(ctx, call.AssistantID, Action{Tool: call.Tool, Arguments: call.Arguments})
	if err != nil && !errors.Is(err, ErrAuthorizationRequired) {
		return toolError("PUCK_UNAVAILABLE", err.Error())
	}
	if id == "" {
		return ToolResult{Success: true, Data: map[string]any{"autoApproved": true}}
	}
	return ToolResult{Code: "AUTHORIZATION_REQUIRED", AuthorizationID: id}
}

func origin(assistantID string) Origin {
	return Origin{Kind: "puck", AssistantID: assistantID, Trigger: "tool"}
}

func taskData(task Task) map[string]any {
	return map[string]any{"taskId": task.ID, "deviceId": task.DeviceID, "threadId": task.NativeThreadID, "status": task.Status}
}

func fromResult(result Result) ToolResult {
	if result.Outcome == "accepted" {
		return ToolResult{Success: true, Data: result.Data}
	}
	code := result.Code
	if code == "" {
		code = "PUCK_UNAVAILABLE"
	}
	return toolError(code, result.Message)
}

func toolError(code, message string) ToolResult { return ToolResult{Code: code, Message: message} }

func invalidArguments(message string, details []toolcontract.FieldError) ToolResult {
	return ToolResult{Code: "INVALID_ARGUMENT", Message: message, Details: details}
}

func invalidStructuredArguments(tool string, raw json.RawMessage, details []toolcontract.FieldError) ToolResult {
	diagnostic := toolcontract.RejectionDiagnostic(tool, raw, details)
	return ToolResult{
		Code:       "INVALID_ARGUMENT",
		Message:    "arguments do not match the canonical contract; correct the listed field paths and retry this tool once",
		Details:    details,
		Diagnostic: &diagnostic,
	}
}

func mustJSON(value any) json.RawMessage {
	encoded, _ := json.Marshal(value)
	return encoded
}

func boundResult(result ToolResult) ToolResult {
	encoded, _ := json.Marshal(result)
	if len(encoded) <= MaxResultBytes {
		return result
	}
	return ToolResult{Success: result.Success, Code: result.Code, Message: fmt.Sprintf("result truncated from %d bytes", len(encoded)), Data: map[string]any{"summary": "Puck tool result exceeded 64 KiB"}, Truncated: true}
}
