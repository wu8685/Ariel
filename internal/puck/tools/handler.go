package tools

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

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
}

type Event struct {
	AssistantID, Kind string
	Payload           json.RawMessage
}

type Orchestrator interface {
	Devices() []map[string]any
	Forward(context.Context, Origin, string, string, json.RawMessage) Result
}

type Backend interface {
	Authorize(context.Context, string, Action) (string, error)
	PermissionMode(context.Context, string) string
	SaveTask(context.Context, Task) error
	Task(context.Context, string, string) (Task, error)
	TaskBySourceMessage(context.Context, string, string) (Task, error)
	Emit(context.Context, Event) error
}

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
	case "request_authorization":
		result = h.requestAuthorization(ctx, call)
	case "ask_user", "share_result", "react":
		if err := h.backend.Emit(ctx, Event{AssistantID: call.AssistantID, Kind: call.Tool, Payload: append(json.RawMessage(nil), call.Arguments...)}); err != nil {
			result = toolError("PUCK_UNAVAILABLE", err.Error())
		} else {
			result = ToolResult{Success: true}
		}
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
	input, details := toolcontract.DecodeStartTask(call.Arguments)
	if len(details) != 0 {
		return invalidArguments("invalid start_task arguments", details)
	}
	if call.SourceMessageID != "" {
		if existing, err := h.backend.TaskBySourceMessage(ctx, call.AssistantID, call.SourceMessageID); err == nil {
			return ToolResult{Success: true, Data: taskData(existing)}
		}
		input.ClientTaskID = stableTaskID(call.AssistantID, call.SourceMessageID)
	}
	if existing, err := h.backend.Task(ctx, call.AssistantID, input.ClientTaskID); err == nil {
		return ToolResult{Success: true, Data: taskData(existing)}
	}
	online, supported := h.deviceCapability(input.DeviceID, "threadBootstrap")
	if !online {
		return toolError("DEVICE_OFFLINE", "target device is offline")
	}
	if !supported {
		return toolError("PROTOCOL_UNSUPPORTED", "target device does not support atomic thread bootstrap")
	}
	if !authorized {
		effectiveArguments, _ := json.Marshal(input)
		authorizationID, err := h.backend.Authorize(ctx, call.AssistantID, Action{Tool: call.Tool, SourceMessageID: call.SourceMessageID, Arguments: effectiveArguments})
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
	created := h.orchestrator.Forward(ctx, requestOrigin, input.DeviceID, "thread.bootstrap", mustJSON(map[string]any{"cwd": input.CWD, "clientMessageId": input.ClientTaskID, "text": input.Prompt}))
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

func stableTaskID(assistantID, sourceMessageID string) string {
	digest := sha256.Sum256([]byte(assistantID + "\x00" + sourceMessageID))
	digest[6] = digest[6]&0x0f | 0x50
	digest[8] = digest[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", digest[:4], digest[4:6], digest[6:8], digest[8:10], digest[10:16])
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
	params := mustJSON(map[string]any{"threadId": threadID, "clientMessageId": input.ClientMessageID, "text": input.Text})
	return fromResult(h.orchestrator.Forward(ctx, requestOrigin, deviceID, "turn.start", params))
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
	return ToolResult{Success: true, Data: taskData(task)}
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
