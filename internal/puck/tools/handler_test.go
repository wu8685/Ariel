package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type fakeOrchestrator struct {
	calls   []string
	origins []Origin
	fn      func(string, json.RawMessage) Result
}

func (f *fakeOrchestrator) Devices() []map[string]any {
	return []map[string]any{{"deviceId": "exec-a", "agentOnline": true}}
}
func (f *fakeOrchestrator) Forward(_ context.Context, origin Origin, _ string, method string, params json.RawMessage) Result {
	f.calls = append(f.calls, method)
	f.origins = append(f.origins, origin)
	if f.fn != nil {
		return f.fn(method, params)
	}
	return Result{Outcome: "accepted", Data: map[string]any{}}
}

func TestAuthorizedTaskCarriesConsumedUserDecisionOrigin(t *testing.T) {
	orchestrator := &fakeOrchestrator{}
	handler := New(orchestrator, &fakeBackend{tasks: map[string]Task{}})
	result := handler.ExecuteAuthorized(context.Background(), Call{AssistantID: "assistant-a", Tool: "start_task", Arguments: json.RawMessage(`{"deviceId":"exec-a","cwd":"/work","goal":"test","prompt":"do it","riskTags":[],"clientTaskId":"task-a"}`)})
	if !result.Success || len(orchestrator.origins) != 2 {
		t.Fatalf("result=%+v origins=%+v", result, orchestrator.origins)
	}
	for _, origin := range orchestrator.origins {
		if origin.Trigger != "user_decision" {
			t.Fatalf("origin=%+v", origin)
		}
	}
}

func TestReadToolRetriesOneRejectedResyncButMutationDoesNot(t *testing.T) {
	readAttempts := 0
	readOrchestrator := &fakeOrchestrator{fn: func(method string, _ json.RawMessage) Result {
		readAttempts++
		if readAttempts == 1 {
			return Result{Outcome: "rejected", Code: "RESYNC_REQUIRED", Message: "RESYNC_REQUIRED"}
		}
		return Result{Outcome: "accepted", Data: map[string]any{"threadId": "thread-a"}}
	}}
	read := New(readOrchestrator, &fakeBackend{tasks: map[string]Task{}}).Execute(context.Background(), Call{AssistantID: "assistant-a", Tool: "read_thread", Arguments: json.RawMessage(`{"deviceId":"exec-a","threadId":"thread-a"}`)})
	if !read.Success || readAttempts != 2 {
		t.Fatalf("read=%+v attempts=%d", read, readAttempts)
	}

	mutationAttempts := 0
	mutationOrchestrator := &fakeOrchestrator{fn: func(method string, _ json.RawMessage) Result {
		mutationAttempts++
		return Result{Outcome: "rejected", Code: "RESYNC_REQUIRED", Message: "RESYNC_REQUIRED"}
	}}
	mutation := New(mutationOrchestrator, &fakeBackend{tasks: map[string]Task{}}).Execute(context.Background(), Call{AssistantID: "assistant-a", Tool: "respond_interaction", Arguments: json.RawMessage(`{"deviceId":"exec-a","threadId":"thread-a","interactionId":"i","answer":"yes"}`)})
	if mutation.Success || mutationAttempts != 1 {
		t.Fatalf("mutation=%+v attempts=%d", mutation, mutationAttempts)
	}
}

type fakeBackend struct {
	tasks  map[string]Task
	events []Event
}

func (f *fakeBackend) Authorize(_ context.Context, _ string, action Action) (string, error) {
	if strings.Contains(string(action.Arguments), `"riskTags":["deploy"]`) {
		return "authorization-a", ErrAuthorizationRequired
	}
	return "", nil
}
func (f *fakeBackend) SaveTask(_ context.Context, task Task) error {
	f.tasks[task.ID] = task
	return nil
}
func (f *fakeBackend) Task(_ context.Context, assistantID, taskID string) (Task, error) {
	task, ok := f.tasks[taskID]
	if !ok || task.AssistantID != assistantID {
		return Task{}, ErrNotFound
	}
	return task, nil
}
func (f *fakeBackend) Emit(_ context.Context, event Event) error {
	f.events = append(f.events, event)
	return nil
}

func TestStartTaskIsIdempotentAndUnknownIsNeverReplayed(t *testing.T) {
	orchestrator := &fakeOrchestrator{}
	backend := &fakeBackend{tasks: map[string]Task{}}
	handler := New(orchestrator, backend)
	call := Call{AssistantID: "assistant-a", Tool: "start_task", Arguments: json.RawMessage(`{"deviceId":"exec-a","cwd":"/work","goal":"test","prompt":"do it","riskTags":[],"clientTaskId":"task-a"}`)}
	first := handler.Execute(context.Background(), call)
	second := handler.Execute(context.Background(), call)
	if !first.Success || !second.Success || len(orchestrator.calls) != 2 || orchestrator.calls[0] != "thread.create" || orchestrator.calls[1] != "turn.start" {
		t.Fatalf("first=%+v second=%+v calls=%v", first, second, orchestrator.calls)
	}

	unknownOrchestrator := &fakeOrchestrator{fn: func(method string, _ json.RawMessage) Result {
		if method == "thread.create" {
			return Result{Outcome: "unknown", Code: "OUTCOME_UNKNOWN"}
		}
		return Result{Outcome: "accepted"}
	}}
	unknown := New(unknownOrchestrator, &fakeBackend{tasks: map[string]Task{}})
	if result := unknown.Execute(context.Background(), Call{AssistantID: "assistant-a", Tool: "start_task", Arguments: json.RawMessage(`{"deviceId":"exec-a","cwd":"/work","goal":"test","prompt":"do it","riskTags":[],"clientTaskId":"task-b"}`)}); result.Success || result.Code != "OUTCOME_UNKNOWN" {
		t.Fatalf("unknown result=%+v", result)
	}
	if len(unknownOrchestrator.calls) != 1 {
		t.Fatalf("unknown write replayed: %v", unknownOrchestrator.calls)
	}
}

func TestToolsRequireTrustedAssistantAndBoundResults(t *testing.T) {
	orchestrator := &fakeOrchestrator{fn: func(string, json.RawMessage) Result {
		return Result{Outcome: "accepted", Data: map[string]any{"text": strings.Repeat("x", 70<<10)}}
	}}
	handler := New(orchestrator, &fakeBackend{tasks: map[string]Task{}})
	if result := handler.Execute(context.Background(), Call{Tool: "list_devices"}); result.Code != "UNAUTHORIZED" {
		t.Fatalf("missing identity=%+v", result)
	}
	result := handler.Execute(context.Background(), Call{AssistantID: "assistant-a", Tool: "read_thread", Arguments: json.RawMessage(`{"deviceId":"exec-a","threadId":"thread-a","turns":4}`)})
	encoded, _ := json.Marshal(result)
	if len(encoded) > MaxResultBytes || !result.Truncated {
		t.Fatalf("result bytes=%d truncated=%v", len(encoded), result.Truncated)
	}
}

func TestSensitiveToolReturnsAuthorizationWithoutForwarding(t *testing.T) {
	orchestrator := &fakeOrchestrator{}
	handler := New(orchestrator, &fakeBackend{tasks: map[string]Task{}})
	result := handler.Execute(context.Background(), Call{AssistantID: "assistant-a", Tool: "start_task", Arguments: json.RawMessage(`{"deviceId":"exec-a","cwd":"/work","goal":"deploy","prompt":"deploy","riskTags":["deploy"],"clientTaskId":"task-a"}`)})
	if result.Code != "AUTHORIZATION_REQUIRED" || result.AuthorizationID != "authorization-a" || len(orchestrator.calls) != 0 {
		t.Fatalf("result=%+v calls=%v", result, orchestrator.calls)
	}
}

func TestMutationValidationReturnsSafeFieldDetails(t *testing.T) {
	handler := New(&fakeOrchestrator{}, &fakeBackend{tasks: map[string]Task{}})
	for _, test := range []struct {
		tool, raw, field string
	}{
		{"start_task", `{}`, "deviceId"},
		{"send_turn", `{"taskId":"task-a","text":"x","riskTags":[],"clientMessageId":"bad"}`, "clientMessageId"},
	} {
		result := handler.Execute(context.Background(), Call{AssistantID: "assistant-a", Tool: test.tool, Arguments: json.RawMessage(test.raw)})
		if result.Code != "INVALID_ARGUMENT" || len(result.Details) == 0 || result.Details[0].Field != test.field {
			t.Fatalf("tool=%s result=%+v", test.tool, result)
		}
		encoded, _ := json.Marshal(result)
		if strings.Contains(string(encoded), test.raw) {
			t.Fatalf("error echoed arguments: %s", encoded)
		}
	}
}

func TestStartTaskUsesAtomicBootstrapAdapter(t *testing.T) {
	orchestrator := &fakeOrchestrator{fn: func(method string, params json.RawMessage) Result {
		if method != "thread.bootstrap" {
			t.Fatalf("method=%s", method)
		}
		var input struct{ CWD, ClientMessageID, Text string }
		if json.Unmarshal(params, &input) != nil || input.CWD != "/work" || input.Text != "do it" || input.ClientMessageID != "00000000-0000-4000-8000-000000000001" {
			t.Fatalf("params=%s", params)
		}
		return Result{Outcome: "accepted", Data: map[string]any{"threadId": "thread-a", "turnId": "turn-a"}}
	}}
	handler := New(orchestrator, &fakeBackend{tasks: map[string]Task{}})
	result := handler.ExecuteAuthorized(context.Background(), Call{AssistantID: "assistant-a", Tool: "start_task", Arguments: json.RawMessage(`{"deviceId":"exec-a","cwd":"/work","goal":"test","prompt":"do it","riskTags":[],"clientTaskId":"00000000-0000-4000-8000-000000000001"}`)})
	if !result.Success || len(orchestrator.calls) != 1 || orchestrator.calls[0] != "thread.bootstrap" {
		t.Fatalf("result=%+v calls=%v", result, orchestrator.calls)
	}
}

func TestSendTurnSupportsDirectThreadLocator(t *testing.T) {
	orchestrator := &fakeOrchestrator{fn: func(method string, params json.RawMessage) Result {
		if method != "turn.start" || !strings.Contains(string(params), `"threadId":"thread-a"`) {
			t.Fatalf("method=%s params=%s", method, params)
		}
		return Result{Outcome: "accepted", Data: map[string]any{"turnId": "turn-b"}}
	}}
	handler := New(orchestrator, &fakeBackend{tasks: map[string]Task{}})
	result := handler.ExecuteAuthorized(context.Background(), Call{AssistantID: "assistant-a", Tool: "send_turn", Arguments: json.RawMessage(`{"deviceId":"exec-a","threadId":"thread-a","text":"continue","riskTags":[],"clientMessageId":"00000000-0000-4000-8000-000000000002"}`)})
	if !result.Success {
		t.Fatalf("result=%+v", result)
	}
}
