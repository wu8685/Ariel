package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type fakeOrchestrator struct {
	calls   []string
	origins []Origin
	params  []json.RawMessage
	fn      func(string, json.RawMessage) Result
}

func (f *fakeOrchestrator) Devices() []map[string]any {
	return []map[string]any{{"deviceId": "exec-a", "agentOnline": true, "capabilities": map[string]bool{"threadBootstrap": true, "send": true, "executionPolicy": true}}}
}
func (f *fakeOrchestrator) Forward(_ context.Context, origin Origin, _ string, method string, params json.RawMessage) Result {
	f.calls = append(f.calls, method)
	f.origins = append(f.origins, origin)
	f.params = append(f.params, append(json.RawMessage(nil), params...))
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
	if invalid.Success || invalid.Code != "INVALID_ARGUMENT" || invalid.Diagnostic == nil || len(backend.events) != 1 {
		t.Fatalf("invalid=%+v events=%+v", invalid, backend.events)
	}
}

func TestStructuredToolRejectionReturnsSafeRetryGuidance(t *testing.T) {
	handler := New(&fakeOrchestrator{}, &fakeBackend{tasks: map[string]Task{}})
	raw := json.RawMessage(`{"question":"do-not-leak","options":["private-option"],"secret":"private-secret"}`)
	result := handler.Execute(context.Background(), Call{AssistantID: "assistant-a", Tool: "ask_user", Arguments: raw})
	encoded, _ := json.Marshal(result)
	if result.Success || result.Code != "INVALID_ARGUMENT" || result.Diagnostic == nil || !strings.Contains(result.Message, "retry") {
		t.Fatalf("result=%+v", result)
	}
	for _, secret := range []string{"do-not-leak", "private-option", "private-secret"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("tool result leaked %q: %s", secret, encoded)
		}
	}
}

func TestStructuredToolAliasesEmitCanonicalPayloads(t *testing.T) {
	backend := &fakeBackend{tasks: map[string]Task{}}
	handler := New(&fakeOrchestrator{}, backend)
	shared := handler.Execute(context.Background(), Call{AssistantID: "assistant-a", Tool: "share_result", Arguments: json.RawMessage(`{"message":"done","item":{"type":"url","label":"report","href":"https://example.invalid/result"}}`)})
	if !shared.Success || len(backend.events) != 1 || !strings.Contains(string(backend.events[0].Payload), `"summary":"done"`) || strings.Contains(string(backend.events[0].Payload), `"message"`) {
		t.Fatalf("shared=%+v events=%+v", shared, backend.events)
	}
	authorized := handler.Execute(context.Background(), Call{AssistantID: "assistant-a", Tool: "request_authorization", Arguments: json.RawMessage(`{"operation":"deploy","justification":"needed"}`)})
	if !authorized.Success {
		t.Fatalf("authorization=%+v", authorized)
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
	tasks      map[string]Task
	events     []Event
	sourceText map[string]string
	authorized []Action
	access     string
	foreign    bool
	readOnly   bool
	watches    []string
	finished   []string
	handoff    Handoff
	handoffErr error
}

func (f *fakeBackend) Authorize(_ context.Context, _ string, action Action) (string, error) {
	f.authorized = append(f.authorized, action)
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
func (f *fakeBackend) TaskBySourceMessage(_ context.Context, assistantID, sourceMessageID string) (Task, error) {
	for _, task := range f.tasks {
		if task.AssistantID == assistantID && task.SourceMessageID == sourceMessageID {
			return task, nil
		}
	}
	return Task{}, ErrNotFound
}
func (f *fakeBackend) Emit(_ context.Context, event Event) error {
	f.events = append(f.events, event)
	return nil
}
func (f *fakeBackend) Tasks(_ context.Context, assistantID string) ([]Task, error) {
	var tasks []Task
	for _, task := range f.tasks {
		if task.AssistantID == assistantID {
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}
func (f *fakeBackend) SourceText(_ context.Context, _, messageID string) (string, error) {
	if f.sourceText == nil {
		return "", nil
	}
	return f.sourceText[messageID], nil
}
func (f *fakeBackend) ApplyRuntime(_ context.Context, assistantID string, update RuntimeUpdate) error {
	task, ok := f.tasks[update.TaskID]
	if !ok || task.AssistantID != assistantID {
		return ErrNotFound
	}
	task.Status = update.Status
	task.LastTurnID = update.TurnID
	f.tasks[update.TaskID] = task
	return nil
}
func (f *fakeBackend) ThreadAccess(context.Context, string, string, string) (string, error) {
	return f.access, nil
}
func (f *fakeBackend) ForeignThread(context.Context, string, string, string) (bool, error) {
	return f.foreign, nil
}
func (f *fakeBackend) SaveWatch(_ context.Context, _, deviceID, threadID, _ string) (string, error) {
	f.watches = append(f.watches, deviceID+"/"+threadID)
	return "watch-1", nil
}
func (f *fakeBackend) ReadOnlyThread(context.Context, string, string, string) (bool, error) {
	return f.readOnly, nil
}
func (f *fakeBackend) PostNotice(context.Context, string, string, string) (string, error) {
	return "notice-1", nil
}
func (f *fakeBackend) NoteThreadGone(context.Context, string, string, string) error { return nil }
func (f *fakeBackend) HandoffContext(context.Context, HandoffQuery) (Handoff, error) {
	return f.handoff, f.handoffErr
}
func (f *fakeBackend) FinishTask(_ context.Context, assistantID, taskID string, finish TaskFinish) (TaskFinishResult, error) {
	task, ok := f.tasks[taskID]
	if !ok || task.AssistantID != assistantID {
		return TaskFinishResult{}, ErrNotFound
	}
	if f.access == "PUCK_SELF_TARGET" {
		return TaskFinishResult{}, FinishError{Code: "PUCK_SELF_TARGET"}
	}
	if task.Status == "archived" {
		return TaskFinishResult{Task: task, AlreadyArchived: true}, nil
	}
	task.Status = "archived"
	f.tasks[taskID] = task
	f.finished = append(f.finished, taskID+":"+finish.Outcome)
	return TaskFinishResult{Task: task, ClosedWatches: len(f.watches), ClosedWakes: 1}, nil
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

func TestStartTaskIsIdempotentAcrossBrainIDsForOneUserMessage(t *testing.T) {
	orchestrator := &fakeOrchestrator{fn: func(method string, params json.RawMessage) Result {
		if method != "thread.bootstrap" {
			return Result{Outcome: "accepted", Data: map[string]any{}}
		}
		var body struct {
			CWD string `json:"cwd"`
		}
		_ = json.Unmarshal(params, &body)
		threadID := "thread-a"
		if body.CWD == "/work/changed" {
			threadID = "thread-b"
		}
		return Result{Outcome: "accepted", Data: map[string]any{"threadId": threadID, "turnId": "turn-a"}}
	}}
	backend := &fakeBackend{tasks: map[string]Task{}}
	handler := New(orchestrator, backend)
	first := handler.Execute(context.Background(), Call{AssistantID: "assistant-a", SourceMessageID: "user-message-a", Tool: "start_task", Arguments: json.RawMessage(`{"deviceId":"exec-a","cwd":"/work","goal":"test","prompt":"do it","riskTags":[],"clientTaskId":"00000000-0000-4000-8000-000000000101"}`)})
	second := handler.Execute(context.Background(), Call{AssistantID: "assistant-a", SourceMessageID: "user-message-a", Tool: "start_task", Arguments: json.RawMessage(`{"deviceId":"exec-a","cwd":"/work/changed","goal":"test again","prompt":"do it again","riskTags":[],"clientTaskId":"00000000-0000-4000-8000-000000000102"}`)})
	if !first.Success || !second.Success || len(backend.tasks) != 1 {
		t.Fatalf("first=%+v second=%+v tasks=%d calls=%v", first, second, len(backend.tasks), orchestrator.calls)
	}
	if first.Data["taskId"] != second.Data["taskId"] {
		t.Fatalf("task ids differ: first=%v second=%v", first.Data["taskId"], second.Data["taskId"])
	}
	var stored Task
	for _, task := range backend.tasks {
		stored = task
	}
	if second.Data["threadId"] != "thread-b" || stored.CWD != "/work/changed" || stored.NativeThreadID != "thread-b" {
		t.Fatalf("new cwd was not adopted: second=%+v task=%+v calls=%v", second, stored, orchestrator.calls)
	}
	for _, call := range orchestrator.calls {
		if call == "thread.archive.update" {
			t.Fatalf("successful re-bootstrap archived the new thread: %v", orchestrator.calls)
		}
	}
}

func TestStartTaskUsesTrustedSourceIdentityInsteadOfBrainGeneratedID(t *testing.T) {
	for _, raw := range []string{
		`{"deviceId":"exec-a","cwd":"/work","goal":"test","prompt":"do it","riskTags":[]}`,
		`{"deviceId":"exec-a","cwd":"/work","goal":"test","prompt":"do it","riskTags":[],"clientTaskId":"turn63-not-a-uuid"}`,
	} {
		orchestrator := &fakeOrchestrator{}
		backend := &fakeBackend{tasks: map[string]Task{}}
		result := New(orchestrator, backend).Execute(context.Background(), Call{
			AssistantID: "assistant-a", SourceMessageID: "user-message-turn63", Tool: "start_task", Arguments: json.RawMessage(raw),
		})
		if !result.Success || len(orchestrator.calls) != 1 {
			t.Fatalf("raw=%s result=%+v calls=%v", raw, result, orchestrator.calls)
		}
		taskID, _ := result.Data["taskId"].(string)
		if taskID == "" || taskID == "turn63-not-a-uuid" {
			t.Fatalf("task identity was not assigned by Relay: %q", taskID)
		}
	}
}

func TestStartTaskWithoutTrustedSourceStillRequiresUUID(t *testing.T) {
	result := New(&fakeOrchestrator{}, &fakeBackend{tasks: map[string]Task{}}).Execute(context.Background(), Call{
		AssistantID: "assistant-a", Tool: "start_task",
		Arguments: json.RawMessage(`{"deviceId":"exec-a","cwd":"/work","goal":"test","prompt":"do it","riskTags":[]}`),
	})
	if result.Success || result.Code != "INVALID_ARGUMENT" || len(result.Details) == 0 || result.Details[0].Field != "clientTaskId" {
		t.Fatalf("untrusted missing identity was accepted: %+v", result)
	}
}

func TestStartTaskTrustedIdentityDoesNotRelaxUnknownOrInvalidFields(t *testing.T) {
	for _, raw := range []string{
		`{"deviceId":"exec-a","cwd":"/work","goal":"test","prompt":"do it","riskTags":[],"clientTaskId":7}`,
		`{"deviceId":"exec-a","cwd":"/work","goal":"test","prompt":"do it","riskTags":[],"unknown":true}`,
	} {
		result := New(&fakeOrchestrator{}, &fakeBackend{tasks: map[string]Task{}}).Execute(context.Background(), Call{
			AssistantID: "assistant-a", SourceMessageID: "user-message-turn63", Tool: "start_task", Arguments: json.RawMessage(raw),
		})
		if result.Success || result.Code != "INVALID_ARGUMENT" {
			t.Fatalf("unsafe field was accepted: raw=%s result=%+v", raw, result)
		}
	}
}

func TestStartTaskPreservesActionableWorkspaceFailure(t *testing.T) {
	orchestrator := &fakeOrchestrator{fn: func(method string, _ json.RawMessage) Result {
		return Result{Outcome: "rejected", Code: "FILE_NOT_FOUND", Message: "目标 workspace 目录不存在；请先使用 list_threads 或 read_thread 返回的 cwd，或向用户确认目录后重试；不要猜测路径。"}
	}}
	result := New(orchestrator, &fakeBackend{tasks: map[string]Task{}}).Execute(context.Background(), Call{
		AssistantID: "assistant-a", SourceMessageID: "user-message-turn63", Tool: "start_task",
		Arguments: json.RawMessage(`{"deviceId":"exec-a","cwd":"/missing","goal":"test","prompt":"do it","riskTags":[]}`),
	})
	if result.Success || result.Code != "FILE_NOT_FOUND" || !strings.Contains(result.Message, "list_threads") || strings.Contains(result.Message, "/missing") {
		t.Fatalf("actionable safe error lost: %+v", result)
	}
}

func TestMissingCwdAfterExistingTaskIsNotEmptySuccess(t *testing.T) {
	const tip = "目标 workspace 目录不存在；请先使用 list_threads 或 read_thread 返回的 cwd，或向用户确认目录后重试；不要猜测路径。"
	orchestrator := &fakeOrchestrator{fn: func(method string, params json.RawMessage) Result {
		if method != "thread.bootstrap" {
			return Result{Outcome: "accepted", Data: map[string]any{}}
		}
		var body struct {
			CWD string `json:"cwd"`
		}
		if json.Unmarshal(params, &body) != nil || strings.Contains(body.CWD, "missing") {
			return Result{Outcome: "rejected", Code: "FILE_NOT_FOUND", Message: tip}
		}
		return Result{Outcome: "accepted", Data: map[string]any{"threadId": "thread-a", "turnId": "turn-a"}}
	}}
	backend := &fakeBackend{tasks: map[string]Task{}}
	handler := New(orchestrator, backend)
	first := handler.Execute(context.Background(), Call{
		AssistantID: "assistant-a", SourceMessageID: "user-message-turn70", Tool: "start_task",
		Arguments: json.RawMessage(`{"deviceId":"exec-a","cwd":"/work","goal":"test","prompt":"do it","riskTags":[]}`),
	})
	second := handler.Execute(context.Background(), Call{
		AssistantID: "assistant-a", SourceMessageID: "user-message-turn70", Tool: "start_task",
		Arguments: json.RawMessage(`{"deviceId":"exec-a","cwd":"/turn70-definitely-missing","goal":"test","prompt":"do it","riskTags":[]}`),
	})
	encoded, _ := json.Marshal(second)
	var parsed struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(encoded, &parsed) != nil || parsed.Success || parsed.Code != "FILE_NOT_FOUND" || !strings.Contains(parsed.Message, "list_threads") || strings.Contains(parsed.Message, "missing") {
		t.Fatalf("missing cwd masked as empty success: first=%+v wire=%s", first, encoded)
	}
	if len(backend.tasks) != 1 {
		t.Fatalf("missing cwd created or dropped tasks: %d", len(backend.tasks))
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
		if json.Unmarshal(params, &input) != nil || input.CWD != "/work" || !strings.Contains(input.Text, "【Bot 对任务的理解】\ndo it\n") || !strings.Contains(input.Text, HandoffSentinel) || input.ClientMessageID != "00000000-0000-4000-8000-000000000001" {
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

func reusableTask(id, assistant, device, cwd, thread, status string, at time.Time) Task {
	return Task{ID: id, AssistantID: assistant, SourceMessageID: "user-old-" + id, DeviceID: device, CWD: cwd, NativeThreadID: thread, Goal: "检查测试", Status: status, UpdatedAt: at}
}

func startFollowUp(source, device, cwd, prompt string, tags []string) Call {
	if tags == nil {
		tags = []string{}
	}
	raw, _ := json.Marshal(map[string]any{
		"deviceId": device, "cwd": cwd, "goal": "跟进", "prompt": prompt, "riskTags": tags,
		"clientTaskId": "00000000-0000-4000-8000-0000000000aa",
	})
	return Call{AssistantID: "assistant-a", SourceMessageID: source, Tool: "start_task", Arguments: raw}
}

func TestStartTaskReusesSameDeviceAndCwd(t *testing.T) {
	orchestrator := &fakeOrchestrator{fn: func(method string, params json.RawMessage) Result {
		if method == "thread.bootstrap" {
			t.Fatalf("follow-up created a thread: %s", params)
		}
		if method != "turn.start" || !strings.Contains(string(params), `"threadId":"thread-old"`) || !strings.Contains(string(params), "继续修失败测试") || !strings.Contains(string(params), HandoffSentinel) {
			t.Fatalf("method=%s params=%s", method, params)
		}
		return Result{Outcome: "accepted", Data: map[string]any{"turnId": "turn-follow"}}
	}}
	backend := &fakeBackend{tasks: map[string]Task{
		"task-old": reusableTask("task-old", "assistant-a", "exec-a", "/work/", "thread-old", "running", time.Now().Add(-time.Hour)),
	}}
	handler := New(orchestrator, backend)
	first := handler.Execute(context.Background(), startFollowUp("user-new", "exec-a", "/work", "继续修失败测试", nil))
	second := handler.Execute(context.Background(), startFollowUp("user-new", "exec-a", "/work", "继续修失败测试", nil))
	if !first.Success || first.Data["taskId"] != "task-old" || first.Data["threadId"] != "thread-old" || first.Data["reused"] != true {
		t.Fatalf("first=%+v", first)
	}
	if !second.Success || second.Data["taskId"] != "task-old" || len(orchestrator.calls) != 1 || orchestrator.calls[0] != "turn.start" {
		t.Fatalf("second=%+v calls=%v", second, orchestrator.calls)
	}
	stored := backend.tasks["task-old"]
	if stored.NativeThreadID != "thread-old" || stored.SourceMessageID != "user-new" || stored.CWD != "/work/" {
		t.Fatalf("stored=%+v", stored)
	}
	if len(backend.tasks) != 1 {
		t.Fatalf("tasks=%d", len(backend.tasks))
	}
}

func TestStartTaskCreatesThreadWhenTargetDiffers(t *testing.T) {
	t.Run("cwd", func(t *testing.T) {
		orchestrator := &fakeOrchestrator{}
		backend := &fakeBackend{tasks: map[string]Task{"task-old": reusableTask("task-old", "assistant-a", "exec-a", "/work", "thread-old", "running", time.Now())}}
		result := New(orchestrator, backend).Execute(context.Background(), startFollowUp("user-new", "exec-a", "/other", "在另一个目录做", nil))
		if !result.Success || result.Data["threadId"] != "thread-a" || len(orchestrator.calls) != 1 || orchestrator.calls[0] != "thread.bootstrap" {
			t.Fatalf("result=%+v calls=%v", result, orchestrator.calls)
		}
		if backend.tasks["task-old"].NativeThreadID != "thread-old" || backend.tasks["task-old"].CWD != "/work" || len(backend.tasks) != 2 {
			t.Fatalf("tasks=%+v", backend.tasks)
		}
	})
	t.Run("device", func(t *testing.T) {
		base := &fakeOrchestrator{}
		orchestrator := extraDeviceOrchestrator{fakeOrchestrator: base}
		backend := &fakeBackend{tasks: map[string]Task{"task-old": reusableTask("task-old", "assistant-a", "exec-a", "/work", "thread-old", "running", time.Now())}}
		result := New(orchestrator, backend).Execute(context.Background(), startFollowUp("user-new", "exec-b", "/work", "换一台机器", nil))
		if !result.Success || len(base.calls) != 1 || base.calls[0] != "thread.bootstrap" || len(backend.tasks) != 2 {
			t.Fatalf("result=%+v calls=%v tasks=%+v", result, base.calls, backend.tasks)
		}
		if backend.tasks["task-old"].DeviceID != "exec-a" {
			t.Fatalf("old task retargeted: %+v", backend.tasks["task-old"])
		}
	})
	t.Run("other assistant", func(t *testing.T) {
		orchestrator := &fakeOrchestrator{}
		backend := &fakeBackend{tasks: map[string]Task{"task-theirs": reusableTask("task-theirs", "assistant-b", "exec-a", "/work", "thread-theirs", "running", time.Now())}}
		result := New(orchestrator, backend).Execute(context.Background(), startFollowUp("user-new", "exec-a", "/work", "这是我的请求", nil))
		if !result.Success || len(orchestrator.calls) != 1 || orchestrator.calls[0] != "thread.bootstrap" {
			t.Fatalf("result=%+v calls=%v", result, orchestrator.calls)
		}
		if backend.tasks["task-theirs"].NativeThreadID != "thread-theirs" {
			t.Fatalf("other assistant task changed: %+v", backend.tasks["task-theirs"])
		}
	})
	for _, status := range []string{"cancelled", "failed_dispatch"} {
		t.Run(status, func(t *testing.T) {
			orchestrator := &fakeOrchestrator{}
			task := reusableTask("task-old", "assistant-a", "exec-a", "/work", "thread-old", status, time.Now())
			if status == "failed_dispatch" {
				task.NativeThreadID = ""
			}
			backend := &fakeBackend{tasks: map[string]Task{"task-old": task}}
			result := New(orchestrator, backend).Execute(context.Background(), startFollowUp("user-new", "exec-a", "/work", "重新做", nil))
			if !result.Success || len(orchestrator.calls) != 1 || orchestrator.calls[0] != "thread.bootstrap" || len(backend.tasks) != 2 {
				t.Fatalf("status=%s result=%+v calls=%v tasks=%d", status, result, orchestrator.calls, len(backend.tasks))
			}
		})
	}
}

func TestStartTaskExplicitNewSessionCreatesThread(t *testing.T) {
	for _, test := range []struct {
		name, prompt, userText string
		reuse                  bool
	}{
		{name: "prompt asks for new session", prompt: "请新开一个会话继续做", reuse: false},
		{name: "trusted user text asks for new session", prompt: "继续做测试", userText: "请单独开一个新会话", reuse: false},
		{name: "english new session", prompt: "please start a new session", reuse: false},
		{name: "negated chinese still reuses", prompt: "不要新开会话，继续修", reuse: true},
		{name: "negated english still reuses", prompt: "don't start a new session", reuse: true},
		{name: "negated longer phrase still reuses", prompt: "不要开一个新会话", reuse: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			orchestrator := &fakeOrchestrator{fn: func(method string, params json.RawMessage) Result {
				if method == "thread.bootstrap" {
					return Result{Outcome: "accepted", Data: map[string]any{"threadId": "thread-new", "turnId": "turn-new"}}
				}
				if method == "turn.start" {
					return Result{Outcome: "accepted", Data: map[string]any{"turnId": "turn-follow"}}
				}
				return Result{Outcome: "accepted"}
			}}
			backend := &fakeBackend{
				tasks:      map[string]Task{"task-old": reusableTask("task-old", "assistant-a", "exec-a", "/work", "thread-old", "done", time.Now())},
				sourceText: map[string]string{"user-new": test.userText},
			}
			result := New(orchestrator, backend).Execute(context.Background(), startFollowUp("user-new", "exec-a", "/work", test.prompt, nil))
			if !result.Success {
				t.Fatalf("result=%+v calls=%v", result, orchestrator.calls)
			}
			if test.reuse {
				if result.Data["taskId"] != "task-old" || len(orchestrator.calls) != 1 || orchestrator.calls[0] != "turn.start" {
					t.Fatalf("result=%+v calls=%v", result, orchestrator.calls)
				}
				return
			}
			if result.Data["threadId"] != "thread-new" || len(orchestrator.calls) != 1 || orchestrator.calls[0] != "thread.bootstrap" || backend.tasks["task-old"].NativeThreadID != "thread-old" {
				t.Fatalf("result=%+v calls=%v old=%+v", result, orchestrator.calls, backend.tasks["task-old"])
			}
		})
	}
}

func TestStartTaskReuseDoesNotClobberBusyOrBlockedTask(t *testing.T) {
	t.Run("host turn busy", func(t *testing.T) {
		orchestrator := &fakeOrchestrator{fn: func(method string, _ json.RawMessage) Result {
			if method == "thread.bootstrap" {
				t.Fatal("busy follow-up created a thread")
			}
			if method != "turn.start" {
				t.Fatalf("method=%s", method)
			}
			return Result{Outcome: "rejected", Code: "TURN_BUSY", Message: "当前会话仍在执行"}
		}}
		backend := &fakeBackend{tasks: map[string]Task{"task-old": reusableTask("task-old", "assistant-a", "exec-a", "/work", "thread-old", "running", time.Now())}}
		result := New(orchestrator, backend).Execute(context.Background(), startFollowUp("user-new", "exec-a", "/work", "再看一眼", nil))
		if result.Success || result.Code != "TURN_BUSY" || result.Data["taskId"] != "task-old" || len(backend.tasks) != 1 {
			t.Fatalf("result=%+v tasks=%d", result, len(backend.tasks))
		}
		if backend.tasks["task-old"].SourceMessageID == "user-new" {
			t.Fatalf("busy turn was recorded as delivered: %+v", backend.tasks["task-old"])
		}
	})
	for _, status := range []string{"awaiting_authorization", "waiting_user", "dispatching", "reconciling", "stopping"} {
		t.Run(status, func(t *testing.T) {
			orchestrator := &fakeOrchestrator{}
			backend := &fakeBackend{tasks: map[string]Task{"task-old": reusableTask("task-old", "assistant-a", "exec-a", "/work", "thread-old", status, time.Now())}}
			result := New(orchestrator, backend).Execute(context.Background(), startFollowUp("user-new", "exec-a", "/work", "继续", nil))
			if result.Success || result.Code != "TURN_BUSY" || result.Data["taskId"] != "task-old" || result.Data["status"] != status || len(orchestrator.calls) != 0 {
				t.Fatalf("status=%s result=%+v calls=%v", status, result, orchestrator.calls)
			}
		})
	}
	for _, status := range []string{"attention", "review", "done", "stopped"} {
		t.Run(status, func(t *testing.T) {
			orchestrator := &fakeOrchestrator{}
			backend := &fakeBackend{tasks: map[string]Task{"task-old": reusableTask("task-old", "assistant-a", "exec-a", "/work", "thread-old", status, time.Now())}}
			result := New(orchestrator, backend).Execute(context.Background(), startFollowUp("user-new", "exec-a", "/work", "按你的判断继续", nil))
			if !result.Success || result.Data["taskId"] != "task-old" || len(orchestrator.calls) != 1 || orchestrator.calls[0] != "turn.start" {
				t.Fatalf("status=%s result=%+v calls=%v", status, result, orchestrator.calls)
			}
		})
	}
}

func TestStartTaskGoneThreadCreatesReplacement(t *testing.T) {
	orchestrator := &fakeOrchestrator{fn: func(method string, _ json.RawMessage) Result {
		if method == "turn.start" {
			return Result{Outcome: "rejected", Code: "NOT_FOUND", Message: "thread gone"}
		}
		if method == "thread.bootstrap" {
			return Result{Outcome: "accepted", Data: map[string]any{"threadId": "thread-new", "turnId": "turn-new"}}
		}
		return Result{Outcome: "accepted"}
	}}
	backend := &fakeBackend{tasks: map[string]Task{"task-old": reusableTask("task-old", "assistant-a", "exec-a", "/work", "thread-old", "running", time.Now())}}
	result := New(orchestrator, backend).Execute(context.Background(), startFollowUp("user-new", "exec-a", "/work", "线程没了就重做", nil))
	if !result.Success || result.Data["threadId"] != "thread-new" || result.Data["taskId"] == "task-old" {
		t.Fatalf("result=%+v", result)
	}
	if orchestrator.calls[0] != "turn.start" || orchestrator.calls[1] != "thread.bootstrap" {
		t.Fatalf("calls=%v", orchestrator.calls)
	}
	old := backend.tasks["task-old"]
	if old.Status != "cancelled" || old.NativeThreadID != "thread-old" {
		t.Fatalf("old task=%+v", old)
	}
}

func TestStartTaskReuseKeepsRiskAuthorization(t *testing.T) {
	orchestrator := &fakeOrchestrator{}
	backend := &fakeBackend{tasks: map[string]Task{"task-old": reusableTask("task-old", "assistant-a", "exec-a", "/work", "thread-old", "running", time.Now())}}
	result := New(orchestrator, backend).Execute(context.Background(), startFollowUp("user-new", "exec-a", "/work", "deploy", []string{"deploy"}))
	if result.Code != "AUTHORIZATION_REQUIRED" || result.AuthorizationID != "authorization-a" || len(orchestrator.calls) != 0 || len(backend.tasks) != 1 {
		t.Fatalf("result=%+v calls=%v", result, orchestrator.calls)
	}
	if len(backend.authorized) != 1 || backend.authorized[0].Tool != "send_turn" {
		t.Fatalf("authorized=%+v", backend.authorized)
	}
}

func TestAuthorizedFrozenStartTaskDoesNotSteer(t *testing.T) {
	orchestrator := &fakeOrchestrator{}
	backend := &fakeBackend{tasks: map[string]Task{"task-old": reusableTask("task-old", "assistant-a", "exec-a", "/work", "thread-old", "running", time.Now())}}
	result := New(orchestrator, backend).ExecuteAuthorized(context.Background(), Call{
		AssistantID: "assistant-a", Tool: "start_task",
		Arguments: json.RawMessage(`{"deviceId":"exec-a","cwd":"/work","goal":"test","prompt":"do it","riskTags":[],"clientTaskId":"00000000-0000-4000-8000-0000000000bb"}`),
	})
	if !result.Success || len(orchestrator.calls) != 1 || orchestrator.calls[0] != "thread.bootstrap" || backend.tasks["task-old"].NativeThreadID != "thread-old" {
		t.Fatalf("result=%+v calls=%v old=%+v", result, orchestrator.calls, backend.tasks["task-old"])
	}
}

func TestNewerReusableTaskWinsAndDifferentCwdOnSameMessageKeepsIt(t *testing.T) {
	orchestrator := &fakeOrchestrator{fn: func(method string, params json.RawMessage) Result {
		if method == "turn.start" {
			if !strings.Contains(string(params), `"threadId":"thread-new"`) {
				t.Fatalf("params=%s", params)
			}
			return Result{Outcome: "accepted", Data: map[string]any{"turnId": "turn-follow"}}
		}
		if method == "thread.bootstrap" {
			return Result{Outcome: "accepted", Data: map[string]any{"threadId": "thread-created", "turnId": "turn-created"}}
		}
		return Result{Outcome: "accepted"}
	}}
	backend := &fakeBackend{tasks: map[string]Task{
		"task-old": reusableTask("task-old", "assistant-a", "exec-a", "/work", "thread-old", "done", time.Now().Add(-2*time.Hour)),
		"task-new": reusableTask("task-new", "assistant-a", "exec-a", "/work", "thread-new", "review", time.Now()),
	}}
	handler := New(orchestrator, backend)
	continued := handler.Execute(context.Background(), startFollowUp("user-new", "exec-a", "/work", "接着最新的做", nil))
	if !continued.Success || continued.Data["taskId"] != "task-new" {
		t.Fatalf("continued=%+v", continued)
	}
	retarget := handler.Execute(context.Background(), startFollowUp("user-new", "exec-a", "/elsewhere", "换目录", nil))
	if !retarget.Success || retarget.Data["threadId"] != "thread-created" {
		t.Fatalf("retarget=%+v calls=%v", retarget, orchestrator.calls)
	}
	if backend.tasks["task-new"].NativeThreadID != "thread-new" || backend.tasks["task-new"].CWD != "/work" {
		t.Fatalf("reused task was overwritten: %+v", backend.tasks["task-new"])
	}
}

type extraDeviceOrchestrator struct{ *fakeOrchestrator }

func (o extraDeviceOrchestrator) Devices() []map[string]any {
	return []map[string]any{
		{"deviceId": "exec-a", "agentOnline": true, "capabilities": map[string]bool{"threadBootstrap": true, "send": true, "executionPolicy": true}},
		{"deviceId": "exec-b", "agentOnline": true, "capabilities": map[string]bool{"threadBootstrap": true, "send": true, "executionPolicy": true}},
	}
}

func TestGetTaskStatusUsesLiveThreadInsteadOfStaleRow(t *testing.T) {
	orchestrator := &fakeOrchestrator{fn: func(method string, params json.RawMessage) Result {
		if method != "thread.read" {
			return Result{Outcome: "rejected", Code: "INVALID_ARGUMENT"}
		}
		return Result{Outcome: "accepted", Data: map[string]any{"thread": map[string]any{
			"runtime": "idle", "pendingInteractions": []any{},
			"turns": []any{map[string]any{"turnId": "turn-live", "status": "completed", "items": []any{map[string]any{"role": "assistant", "text": "现场已经结束"}}}},
		}}}
	}}
	backend := &fakeBackend{tasks: map[string]Task{
		"task-live": {ID: "task-live", AssistantID: "assistant-a", DeviceID: "exec-a", CWD: "/work", NativeThreadID: "thread-live", Status: "running"},
	}}
	handler := New(orchestrator, backend)
	status := handler.Execute(context.Background(), Call{AssistantID: "assistant-a", Tool: "get_task_status", Arguments: json.RawMessage(`{"taskId":"task-live"}`)})
	if !status.Success || status.Data["status"] != "review" || status.Data["runtime"] != "idle" || status.Data["summary"] != "现场已经结束" {
		t.Fatalf("status=%+v", status)
	}
	if backend.tasks["task-live"].Status != "review" {
		t.Fatalf("stale row was kept: %+v", backend.tasks["task-live"])
	}
	for _, method := range orchestrator.calls {
		if method == "turn.start" || method == "thread.bootstrap" {
			t.Fatalf("status read executed a turn: %v", orchestrator.calls)
		}
	}
}

func TestFinishTaskArchivesWithoutExecutionOrAuthorization(t *testing.T) {
	orchestrator := &fakeOrchestrator{}
	backend := &fakeBackend{tasks: map[string]Task{
		"task-a": {ID: "task-a", AssistantID: "assistant-a", DeviceID: "exec-a", NativeThreadID: "thread-a", Status: "review"},
	}, watches: []string{"exec-a/thread-a"}}
	handler := New(orchestrator, backend)
	result := handler.Execute(context.Background(), Call{AssistantID: "assistant-a", Tool: "finish_task", Arguments: json.RawMessage(`{"taskId":"task-a","outcome":"completed","note":"git push origin main"}`)})
	if !result.Success || result.Data["status"] != "archived" || result.Data["alreadyArchived"] != false || len(orchestrator.calls) != 0 || len(backend.authorized) != 0 {
		t.Fatalf("result=%+v calls=%v authorized=%v", result, orchestrator.calls, backend.authorized)
	}
	if backend.tasks["task-a"].Status != "archived" || !strings.Contains(backend.finished[0], "completed") {
		t.Fatalf("stored=%+v finished=%v", backend.tasks["task-a"], backend.finished)
	}
	again := handler.Execute(context.Background(), Call{AssistantID: "assistant-a", Tool: "finish_task", Arguments: json.RawMessage(`{"taskId":"task-a","outcome":"completed"}`)})
	if !again.Success || again.Data["alreadyArchived"] != true || len(orchestrator.calls) != 0 {
		t.Fatalf("again=%+v calls=%v", again, orchestrator.calls)
	}
	missing := handler.Execute(context.Background(), Call{AssistantID: "assistant-b", Tool: "finish_task", Arguments: json.RawMessage(`{"taskId":"task-a","outcome":"cancelled"}`)})
	if missing.Success || missing.Code != "PUCK_ASSISTANT_NOT_FOUND" {
		t.Fatalf("missing=%+v", missing)
	}
	self := New(&fakeOrchestrator{}, &fakeBackend{tasks: map[string]Task{
		"task-brain": {ID: "task-brain", AssistantID: "assistant-a", NativeThreadID: "brain-thread", Status: "running"},
	}, access: "PUCK_SELF_TARGET"})
	refused := self.Execute(context.Background(), Call{AssistantID: "assistant-a", Tool: "finish_task", Arguments: json.RawMessage(`{"taskId":"task-brain","outcome":"failed"}`)})
	if refused.Success || refused.Code != "PUCK_SELF_TARGET" || len(self.orchestrator.(*fakeOrchestrator).calls) != 0 {
		t.Fatalf("refused=%+v", refused)
	}
	invalid := handler.Execute(context.Background(), Call{AssistantID: "assistant-a", Tool: "finish_task", Arguments: json.RawMessage(`{"taskId":"task-a","outcome":"delete-history"}`)})
	if invalid.Success || invalid.Code != "INVALID_ARGUMENT" {
		t.Fatalf("invalid=%+v", invalid)
	}
	if taskReusable(Task{ID: "task-a", NativeThreadID: "thread-a", Status: "archived"}) {
		t.Fatal("archived task stayed reusable")
	}
}

func TestStartTaskHandoffIsVerbatimOnceAndMarksTruncation(t *testing.T) {
	threadID := "11111111-1111-4111-8111-111111111111"
	user := "原话一字不差。" + strings.Repeat("长", MaxHandoffUserRunes) + "USER_TAIL_OMITTED"
	conclusion := "最终结论：可以按方案 B 发布。\n" + strings.Repeat("结", MaxHandoffConclusionRunes) + "\nCONCLUSION-MARKER: use approach B"
	orchestrator := &fakeOrchestrator{}
	backend := &fakeBackend{tasks: map[string]Task{}, handoff: Handoff{
		UserText: user,
		Sessions: []HandoffSession{{
			Title: "方案讨论", DeviceName: "执行机", DeviceID: "exec-a", CWD: "/work/watched",
			ThreadID: threadID, Status: "completed", Watch: "closed", Outcome: "review", Conclusion: conclusion,
		}},
	}}
	handler := New(orchestrator, backend)
	call := Call{AssistantID: "assistant-a", SourceMessageID: "user-message-a", Tool: "start_task", Arguments: json.RawMessage(`{"deviceId":"exec-a","cwd":"/work/new","goal":"落地","prompt":"我理解为要在新目录实现发布步骤。","riskTags":[],"clientTaskId":"00000000-0000-4000-8000-000000000201"}`)}
	first := handler.Execute(context.Background(), call)
	second := handler.Execute(context.Background(), call)
	if !first.Success || !second.Success || len(orchestrator.calls) != 1 || orchestrator.calls[0] != "thread.bootstrap" {
		t.Fatalf("first=%+v second=%+v calls=%v", first, second, orchestrator.calls)
	}
	var payload struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(orchestrator.params[0], &payload) != nil {
		t.Fatal("bootstrap payload")
	}
	pack := payload.Text
	if strings.Count(pack, HandoffSentinel) != 1 || !strings.HasPrefix(pack, "【Bot 对任务的理解】\n我理解为要在新目录实现发布步骤。") {
		t.Fatalf("pack did not keep the brain instruction first: %s", pack)
	}
	for _, want := range []string{"原话一字不差。", "CONCLUSION-MARKER: use approach B", "status: completed", "watch: closed", "方案讨论", "/work/watched", "执行机", "codex://threads/" + threadID, "deviceId=exec-a threadId=" + threadID, "【Referenced sessions】"} {
		if !strings.Contains(pack, want) {
			t.Fatalf("missing %q in %s", want, pack)
		}
	}
	if strings.Contains(pack, "USER_TAIL_OMITTED") || strings.Contains(pack, "最终结论：可以按方案 B 发布。") || strings.Contains(pack, "status: closed") || strings.Count(pack, "已省略") < 2 {
		t.Fatalf("truncation or status was wrong: %s", pack)
	}
	suffix := pack[strings.Index(pack, HandoffSentinel):]
	if runeCount(suffix) > MaxHandoffRunes {
		t.Fatalf("pack rune count=%d", runeCount(suffix))
	}
	again := AppendHandoff(pack, backend.handoff)
	if again != pack || strings.Count(again, HandoffSentinel) != 1 {
		t.Fatal("retry duplicated the handoff pack")
	}
}

func runeCount(text string) int { return len([]rune(text)) }
