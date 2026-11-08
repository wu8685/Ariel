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
	return []map[string]any{{"deviceId": "exec-a", "agentOnline": true, "capabilities": map[string]bool{"threadBootstrap": true, "send": true, "executionPolicy": true}}}
}
func (f *fakeOrchestrator) Forward(_ context.Context, origin Origin, _ string, method string, params json.RawMessage) Result {
	f.calls = append(f.calls, method)
	f.origins = append(f.origins, origin)
	if f.fn != nil {
		return f.fn(method, params)
	}
	if method == "thread.bootstrap" {
		return Result{Outcome: "accepted", Data: map[string]any{"threadId": "thread-a", "turnId": "turn-a"}}
	}
	return Result{Outcome: "accepted", Data: map[string]any{}}
}

func TestAuthorizedTaskCarriesConsumedUserDecisionOrigin(t *testing.T) {
	orchestrator := &fakeOrchestrator{}
	handler := New(orchestrator, &fakeBackend{tasks: map[string]Task{}})
	result := handler.ExecuteAuthorized(context.Background(), Call{AssistantID: "assistant-a", Tool: "start_task", Arguments: json.RawMessage(`{"deviceId":"exec-a","cwd":"/work","goal":"test","prompt":"do it","riskTags":[],"clientTaskId":"00000000-0000-4000-8000-000000000001"}`)})
	if !result.Success || len(orchestrator.origins) != 1 {
		t.Fatalf("result=%+v origins=%+v", result, orchestrator.origins)
	}
	for _, origin := range orchestrator.origins {
		if origin.Trigger != "user_decision" {
			t.Fatalf("origin=%+v", origin)
		}
	}
}

func TestRequestAuthorizationCanBeAutoApprovedByPersistentMode(t *testing.T) {
	handler := New(&fakeOrchestrator{}, &fakeBackend{tasks: map[string]Task{}})
	result := handler.Execute(context.Background(), Call{AssistantID: "assistant-a", Tool: "request_authorization", Arguments: json.RawMessage(`{"summary":"ship","action":"deploy"}`)})
	if !result.Success || result.AuthorizationID != "" {
		t.Fatalf("result=%+v", result)
	}
}

func TestAskUserEmitsNormalizedQuestionsAndRejectsMalformedArguments(t *testing.T) {
	backend := &fakeBackend{tasks: map[string]Task{}}
	handler := New(&fakeOrchestrator{}, backend)
	result := handler.Execute(context.Background(), Call{AssistantID: "assistant-a", Tool: "ask_user", Arguments: json.RawMessage(`{"questions":[{"id":"entry","question":"从哪里进入？","options":[{"label":"Desktop","description":"桌面应用"}]}]}`)})
	if !result.Success || len(backend.events) != 1 || !strings.Contains(string(backend.events[0].Payload), `"value":"Desktop"`) {
		t.Fatalf("result=%+v events=%+v", result, backend.events)
	}
	invalid := handler.Execute(context.Background(), Call{AssistantID: "assistant-a", Tool: "ask_user", Arguments: json.RawMessage(`{"questions":[]}`)})
	if invalid.Success || invalid.Code != "INVALID_ARGUMENT" || len(backend.events) != 1 {
		t.Fatalf("invalid=%+v events=%+v", invalid, backend.events)
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
func (f *fakeBackend) PermissionMode(context.Context, string) string { return "full" }
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
	call := Call{AssistantID: "assistant-a", Tool: "start_task", Arguments: json.RawMessage(`{"deviceId":"exec-a","cwd":"/work","goal":"test","prompt":"do it","riskTags":[],"clientTaskId":"00000000-0000-4000-8000-000000000001"}`)}
	first := handler.Execute(context.Background(), call)
	second := handler.Execute(context.Background(), call)
	if !first.Success || !second.Success || len(orchestrator.calls) != 1 || orchestrator.calls[0] != "thread.bootstrap" {
		t.Fatalf("first=%+v second=%+v calls=%v", first, second, orchestrator.calls)
	}

	unknownOrchestrator := &fakeOrchestrator{fn: func(method string, _ json.RawMessage) Result {
		if method == "thread.bootstrap" {
			return Result{Outcome: "unknown", Code: "OUTCOME_UNKNOWN"}
		}
		return Result{Outcome: "accepted"}
	}}
	unknown := New(unknownOrchestrator, &fakeBackend{tasks: map[string]Task{}})
	if result := unknown.Execute(context.Background(), Call{AssistantID: "assistant-a", Tool: "start_task", Arguments: json.RawMessage(`{"deviceId":"exec-a","cwd":"/work","goal":"test","prompt":"do it","riskTags":[],"clientTaskId":"00000000-0000-4000-8000-000000000002"}`)}); result.Success || result.Code != "OUTCOME_UNKNOWN" {
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
	result := handler.Execute(context.Background(), Call{AssistantID: "assistant-a", Tool: "start_task", Arguments: json.RawMessage(`{"deviceId":"exec-a","cwd":"/work","goal":"deploy","prompt":"deploy","riskTags":["deploy"],"clientTaskId":"00000000-0000-4000-8000-000000000001"}`)})
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

func TestSendTurnResolvesOwnedTaskAndAppliesRiskPolicy(t *testing.T) {
	task := Task{ID: "task-a", AssistantID: "assistant-a", DeviceID: "exec-a", CWD: "/work", NativeThreadID: "thread-a"}
	backend := &fakeBackend{tasks: map[string]Task{"task-a": task}}
	orchestrator := &fakeOrchestrator{}
	handler := New(orchestrator, backend)
	risky := handler.Execute(context.Background(), Call{AssistantID: "assistant-a", Tool: "send_turn", Arguments: json.RawMessage(`{"taskId":"task-a","text":"deploy","riskTags":["deploy"],"clientMessageId":"00000000-0000-4000-8000-000000000003"}`)})
	if risky.Code != "AUTHORIZATION_REQUIRED" || len(orchestrator.calls) != 0 {
		t.Fatalf("risky=%+v calls=%v", risky, orchestrator.calls)
	}
	result := handler.Execute(context.Background(), Call{AssistantID: "assistant-a", Tool: "send_turn", Arguments: json.RawMessage(`{"taskId":"task-a","text":"continue","riskTags":[],"clientMessageId":"00000000-0000-4000-8000-000000000004"}`)})
	if !result.Success || len(orchestrator.calls) != 1 || orchestrator.calls[0] != "turn.start" {
		t.Fatalf("result=%+v calls=%v", result, orchestrator.calls)
	}
}

func TestMutationsFailClosedWhenTargetCapabilityIsMissing(t *testing.T) {
	orchestrator := &fakeOrchestrator{}
	orchestrator.fn = func(string, json.RawMessage) Result { return Result{Outcome: "accepted"} }
	handler := New(orchestrator, &fakeBackend{tasks: map[string]Task{}})
	orchestratorWithoutCapabilities := &capabilitylessOrchestrator{fakeOrchestrator: orchestrator}
	handler = New(orchestratorWithoutCapabilities, &fakeBackend{tasks: map[string]Task{}})
	result := handler.ExecuteAuthorized(context.Background(), Call{AssistantID: "assistant-a", Tool: "start_task", Arguments: json.RawMessage(`{"deviceId":"exec-a","cwd":"/work","goal":"test","prompt":"run","riskTags":[],"clientTaskId":"00000000-0000-4000-8000-000000000005"}`)})
	if result.Code != "PROTOCOL_UNSUPPORTED" || len(orchestrator.calls) != 0 {
		t.Fatalf("result=%+v calls=%v", result, orchestrator.calls)
	}
}

func TestMutationDoesNotQueueOrReplayWhenDeviceIsOffline(t *testing.T) {
	orchestrator := &offlineOrchestrator{fakeOrchestrator: &fakeOrchestrator{}}
	handler := New(orchestrator, &fakeBackend{tasks: map[string]Task{}})
	call := Call{AssistantID: "assistant-a", Tool: "start_task", Arguments: json.RawMessage(`{"deviceId":"exec-a","cwd":"/work","goal":"test","prompt":"run","riskTags":[],"clientTaskId":"00000000-0000-4000-8000-000000000008"}`)}
	for range 2 {
		result := handler.ExecuteAuthorized(context.Background(), call)
		if result.Code != "DEVICE_OFFLINE" {
			t.Fatalf("result=%+v", result)
		}
	}
	if len(orchestrator.calls) != 0 {
		t.Fatalf("offline mutation was forwarded: %v", orchestrator.calls)
	}
}

type capabilitylessOrchestrator struct{ *fakeOrchestrator }

func (f *capabilitylessOrchestrator) Devices() []map[string]any {
	return []map[string]any{{"deviceId": "exec-a", "agentOnline": true, "capabilities": map[string]bool{}}}
}

type offlineOrchestrator struct{ *fakeOrchestrator }

func (f *offlineOrchestrator) Devices() []map[string]any {
	return []map[string]any{{"deviceId": "exec-a", "agentOnline": false, "capabilities": map[string]bool{"threadBootstrap": true}}}
}

func TestMutationFlowCreatesTaskSendsFollowUpAndPreservesReadTools(t *testing.T) {
	orchestrator := &fakeOrchestrator{fn: func(method string, _ json.RawMessage) Result {
		switch method {
		case "thread.bootstrap":
			return Result{Outcome: "accepted", Data: map[string]any{"threadId": "thread-a", "turnId": "turn-a"}}
		case "turn.start":
			return Result{Outcome: "accepted", Data: map[string]any{"turnId": "turn-b"}}
		case "thread.list":
			return Result{Outcome: "accepted", Data: map[string]any{"threads": []any{}}}
		default:
			return Result{Outcome: "rejected", Code: "INVALID_ARGUMENT"}
		}
	}}
	backend := &fakeBackend{tasks: map[string]Task{}}
	handler := New(orchestrator, backend)
	start := Call{AssistantID: "assistant-a", Tool: "start_task", Arguments: json.RawMessage(`{"deviceId":"exec-a","cwd":"/work","goal":"test","prompt":"run","riskTags":[],"clientTaskId":"00000000-0000-4000-8000-000000000006"}`)}
	if result := handler.ExecuteAuthorized(context.Background(), start); !result.Success || result.Data["threadId"] != "thread-a" {
		t.Fatalf("start=%+v", result)
	}
	if result := handler.Execute(context.Background(), start); !result.Success || len(orchestrator.calls) != 1 {
		t.Fatalf("idempotent start=%+v calls=%v", result, orchestrator.calls)
	}
	send := handler.Execute(context.Background(), Call{AssistantID: "assistant-a", Tool: "send_turn", Arguments: json.RawMessage(`{"taskId":"00000000-0000-4000-8000-000000000006","text":"continue","riskTags":[],"clientMessageId":"00000000-0000-4000-8000-000000000007"}`)})
	if !send.Success || send.Data["turnId"] != "turn-b" {
		t.Fatalf("send=%+v", send)
	}
	status := handler.Execute(context.Background(), Call{AssistantID: "assistant-a", Tool: "get_task_status", Arguments: json.RawMessage(`{"taskId":"00000000-0000-4000-8000-000000000006"}`)})
	if !status.Success || status.Data["status"] != "running" {
		t.Fatalf("status=%+v", status)
	}
	read := handler.Execute(context.Background(), Call{AssistantID: "assistant-a", Tool: "list_threads", Arguments: json.RawMessage(`{"deviceId":"exec-a","limit":1}`)})
	if !read.Success {
		t.Fatalf("read=%+v", read)
	}
}
