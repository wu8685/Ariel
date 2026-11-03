package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

const MaxResultBytes = 64 << 10

var (
	ErrAuthorizationRequired = errors.New("authorization required")
	ErrNotFound              = errors.New("not found")
)

type Origin struct {
	Kind, AssistantID, Trigger string
}

type Result struct {
	Outcome string         `json:"outcome"`
	Data    map[string]any `json:"data,omitempty"`
	Code    string         `json:"code,omitempty"`
	Message string         `json:"message,omitempty"`
}

type Call struct {
	AssistantID string
	Tool        string
	Arguments   json.RawMessage
}

type ToolResult struct {
	Success         bool           `json:"success"`
	Code            string         `json:"code,omitempty"`
	Message         string         `json:"message,omitempty"`
	Data            map[string]any `json:"data,omitempty"`
	AuthorizationID string         `json:"authorizationId,omitempty"`
	Truncated       bool           `json:"truncated,omitempty"`
}

type Action struct {
	Tool      string
	Arguments json.RawMessage
}

type Task struct {
	ID, AssistantID, DeviceID, CWD, NativeThreadID, Goal, Status, LastTurnID string
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
	SaveTask(context.Context, Task) error
	Task(context.Context, string, string) (Task, error)
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
		result = h.sendTurn(ctx, call)
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
	return fromResult(h.orchestrator.Forward(ctx, origin(call.AssistantID), target, method, mustJSON(arguments)))
}

func (h *Handler) startTask(ctx context.Context, call Call, authorized bool) ToolResult {
	var input struct {
		DeviceID, CWD, Goal, Prompt, ClientTaskID string
		RiskTags                                  []string
	}
	if json.Unmarshal(call.Arguments, &input) != nil || input.DeviceID == "" || input.CWD == "" || input.ClientTaskID == "" || input.Prompt == "" {
		return toolError("INVALID_ARGUMENT", "invalid start_task arguments")
	}
	if existing, err := h.backend.Task(ctx, call.AssistantID, input.ClientTaskID); err == nil {
		return ToolResult{Success: true, Data: taskData(existing)}
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
	created := h.orchestrator.Forward(ctx, origin(call.AssistantID), input.DeviceID, "thread.create", mustJSON(map[string]any{"cwd": input.CWD}))
	if created.Outcome != "accepted" {
		return fromResult(created)
	}
	threadID, _ := created.Data["threadId"].(string)
	if threadID == "" {
		threadID = "pending-" + input.ClientTaskID
	}
	turn := h.orchestrator.Forward(ctx, origin(call.AssistantID), input.DeviceID, "turn.start", mustJSON(map[string]any{"threadId": threadID, "clientMessageId": input.ClientTaskID, "text": input.Prompt}))
	if turn.Outcome != "accepted" {
		return fromResult(turn)
	}
	turnID, _ := turn.Data["turnId"].(string)
	task := Task{ID: input.ClientTaskID, AssistantID: call.AssistantID, DeviceID: input.DeviceID, CWD: input.CWD, NativeThreadID: threadID, Goal: input.Goal, Status: "running", LastTurnID: turnID}
	if err := h.backend.SaveTask(ctx, task); err != nil {
		return toolError("PUCK_UNAVAILABLE", err.Error())
	}
	return ToolResult{Success: true, Data: taskData(task)}
}

func (h *Handler) sendTurn(ctx context.Context, call Call) ToolResult {
	var input struct{ TaskID, Text, ClientMessageID string }
	if json.Unmarshal(call.Arguments, &input) != nil || input.TaskID == "" || input.Text == "" || input.ClientMessageID == "" {
		return toolError("INVALID_ARGUMENT", "invalid send_turn arguments")
	}
	task, err := h.backend.Task(ctx, call.AssistantID, input.TaskID)
	if err != nil {
		return toolError("PUCK_ASSISTANT_NOT_FOUND", "task not found")
	}
	params := mustJSON(map[string]any{"threadId": task.NativeThreadID, "clientMessageId": input.ClientMessageID, "text": input.Text})
	return fromResult(h.orchestrator.Forward(ctx, origin(call.AssistantID), task.DeviceID, "turn.start", params))
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
