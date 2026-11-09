package puck

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	pucktools "github.com/wu8685/Ariel/internal/puck/tools"
	"github.com/wu8685/Ariel/internal/relay"
)

type fakeModuleBridge struct {
	mu                sync.Mutex
	calls             []string
	brains            map[relay.BrainThread]string
	ensureFailures    int
	turnFailures      int
	turnFailureCode   string
	toolResultFailure bool
	emptyTurn         bool
	ensureThreads     []string
	events            chan relay.BrainEvent
	failures          chan relay.BrainDeliveryFailure
	params            []json.RawMessage
	origins           []relay.Origin
}

func (f *fakeModuleBridge) Devices() []map[string]any {
	return []map[string]any{
		{"deviceId": "brain-a", "agentOnline": true, "capabilities": map[string]bool{"puckBrainReady": true}},
		{"deviceId": "exec-a", "agentOnline": true, "capabilities": map[string]bool{"threadBootstrap": true, "send": true, "executionPolicy": true}},
	}
}
func (f *fakeModuleBridge) BrainThreads(v map[relay.BrainThread]string) { f.brains = v }
func (f *fakeModuleBridge) Forward(_ context.Context, origin relay.Origin, _ string, method string, params json.RawMessage) relay.PuckResponse {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method)
	f.params = append(f.params, append(json.RawMessage(nil), params...))
	f.origins = append(f.origins, origin)
	if method == "puck.brain.ensure" {
		if f.ensureFailures > 0 {
			f.ensureFailures--
			return relay.PuckResponse{Outcome: "rejected", Error: &relay.PuckError{Code: "BRAIN_UNAVAILABLE", Message: "fixture unavailable"}}
		}
		threadID := "brain-thread-a"
		if len(f.ensureThreads) > 0 {
			threadID, f.ensureThreads = f.ensureThreads[0], f.ensureThreads[1:]
		}
		return relay.PuckResponse{Outcome: "accepted", Data: map[string]any{"threadId": threadID}}
	}
	if method == "puck.brain.turn.start" {
		if f.turnFailures > 0 {
			f.turnFailures--
			code := f.turnFailureCode
			if code == "" {
				code = "BRAIN_UNAVAILABLE"
			}
			return relay.PuckResponse{Outcome: "rejected", Error: &relay.PuckError{Code: code, Message: code}}
		}
		if f.emptyTurn {
			return relay.PuckResponse{Outcome: "accepted", Data: map[string]any{}}
		}
		return relay.PuckResponse{Outcome: "accepted", Data: map[string]any{"turnId": "brain-turn-a"}}
	}
	if method == "thread.bootstrap" {
		return relay.PuckResponse{Outcome: "accepted", Data: map[string]any{"threadId": "exec-thread-a", "turnId": "exec-turn-a"}}
	}
	if method == "turn.start" {
		return relay.PuckResponse{Outcome: "accepted", Data: map[string]any{"turnId": "exec-turn-a"}}
	}
	if method == "puck.brain.toolResult" && f.toolResultFailure {
		return relay.PuckResponse{Outcome: "unknown", Error: &relay.PuckError{Code: "OUTCOME_UNKNOWN", Message: "fixture receipt lost"}}
	}
	return relay.PuckResponse{Outcome: "accepted", Data: map[string]any{}}
}

func TestMessageRejectsAcceptedBrainTurnWithoutAuthoritativeID(t *testing.T) {
	bridge := &fakeModuleBridge{emptyTurn: true}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	result := module.Handle(context.Background(), "puck.message.send", mustJSON(map[string]any{"assistantId": id, "clientMessageId": "message-a", "text": "检查测试"}))
	if result.Error == nil || result.Error.Code != "BRAIN_UNAVAILABLE" {
		t.Fatalf("result=%+v", result)
	}
	history := module.Handle(context.Background(), "puck.history", mustJSON(map[string]any{"assistantId": id})).Data["messages"].([]Message)
	for _, message := range history {
		if message.Kind == "processing" || message.Kind == "acknowledged" {
			t.Fatalf("missing turn identity was presented as running: %+v", message)
		}
	}
}

func TestTextMessageOmitsNullImagesFromBrainTurn(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	result := module.Handle(context.Background(), "puck.message.send", mustJSON(map[string]any{"assistantId": id, "clientMessageId": "message-a", "text": "检查测试"}))
	if result.Error != nil {
		t.Fatalf("result=%+v", result)
	}
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	for index, method := range bridge.calls {
		if method == "puck.brain.turn.start" && strings.Contains(string(bridge.params[index]), `"images":null`) {
			t.Fatalf("text-only brain turn contains schema-invalid null images: %s", bridge.params[index])
		}
	}
}

func TestReplyMessageValidatesScopeAndBuildsBoundedBrainContext(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	ctx := context.Background()
	first := module.Handle(ctx, "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	second := module.Handle(ctx, "puck.create", json.RawMessage(`{"name":"Other","brainDeviceId":"brain-a"}`))
	firstID := first.Data["assistant"].(map[string]any)["assistantId"].(string)
	secondID := second.Data["assistant"].(map[string]any)["assistantId"].(string)
	module.clearPending(firstID)
	module.clearPending(secondID)
	long := strings.Repeat("界", 900)
	if err := module.store.AppendMessage(ctx, Message{ID: "assistant-answer", AssistantID: firstID, Role: "assistant", Kind: "text", Text: long}); err != nil {
		t.Fatal(err)
	}
	bridge.mu.Lock()
	bridge.calls, bridge.params = nil, nil
	bridge.mu.Unlock()
	result := module.Handle(ctx, "puck.message.send", mustJSON(map[string]any{"assistantId": firstID, "clientMessageId": "reply-message", "text": "请继续", "replyTo": "assistant-answer"}))
	if result.Error != nil {
		t.Fatalf("result=%+v", result)
	}
	history := module.Handle(ctx, "puck.history", mustJSON(map[string]any{"assistantId": firstID})).Data["messages"].([]Message)
	if history[len(history)-2].ReplyTo != "assistant-answer" {
		t.Fatalf("history=%+v", history)
	}
	bridge.mu.Lock()
	var turnText string
	for index, method := range bridge.calls {
		if method == "puck.brain.turn.start" {
			var params struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(bridge.params[index], &params)
			turnText = params.Text
		}
	}
	bridge.mu.Unlock()
	if !strings.Contains(turnText, "回复消息 assistant-answer") || !strings.Contains(turnText, "作者：助理") || strings.Count(turnText, "界") != 800 {
		t.Fatalf("unexpected brain context: %q", turnText)
	}

	before, _ := module.store.Messages(ctx, secondID, 200)
	rejected := module.Handle(ctx, "puck.message.send", mustJSON(map[string]any{"assistantId": secondID, "clientMessageId": "cross-reply", "text": "越界", "replyTo": "assistant-answer"}))
	if rejected.Error == nil || rejected.Error.Code != "INVALID_REPLY_TARGET" {
		t.Fatalf("cross reply=%+v", rejected)
	}
	after, _ := module.store.Messages(ctx, secondID, 200)
	if len(after) != len(before) {
		t.Fatalf("rejected reply was persisted: before=%d after=%d", len(before), len(after))
	}
}

func TestReplyToTaskCardGuidesBrainToSendTurnWithoutCreatingTask(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	ctx := context.Background()
	created := module.Handle(ctx, "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	module.clearPending(id)
	task := Task{ID: "task-existing", AssistantID: id, DeviceID: "exec-a", CWD: "/work", NativeThreadID: "exec-thread-existing", Goal: "检查测试", Status: "running"}
	if err := module.store.UpsertTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := module.store.AppendMessage(ctx, Message{ID: "task-card:task-existing", AssistantID: id, Role: "assistant", Kind: "task", Text: "检查测试", CardJSON: `{"taskId":"task-existing","status":"running"}`}); err != nil {
		t.Fatal(err)
	}
	bridge.mu.Lock()
	bridge.calls, bridge.params = nil, nil
	bridge.mu.Unlock()
	result := module.Handle(ctx, "puck.message.send", mustJSON(map[string]any{"assistantId": id, "clientMessageId": "reply-task", "text": "再检查一下失败项", "replyTo": "task-card:task-existing"}))
	if result.Error != nil {
		t.Fatalf("result=%+v", result)
	}
	bridge.mu.Lock()
	var turnText string
	for index, method := range bridge.calls {
		if method == "puck.brain.turn.start" {
			var params struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(bridge.params[index], &params)
			turnText = params.Text
		}
	}
	bridge.mu.Unlock()
	if !strings.Contains(turnText, "taskId：task-existing") || !strings.Contains(turnText, "send_turn") || !strings.Contains(turnText, "不得新建任务") {
		t.Fatalf("task reply context=%q", turnText)
	}
	module.handleBrainEvent(ctx, relay.BrainEvent{DeviceID: "brain-a", ThreadID: "brain-thread-a", AssistantID: id, Name: "puck.brain.toolCall", Payload: mustJSON(map[string]any{"callId": "call-follow-up", "tool": "send_turn", "arguments": map[string]any{"taskId": "task-existing", "text": "再检查一下失败项", "riskTags": []string{}, "clientMessageId": "00000000-0000-4000-8000-000000000251"}})})
	tasks, _ := module.store.Tasks(ctx, id)
	bridge.mu.Lock()
	calls := append([]string(nil), bridge.calls...)
	bridge.mu.Unlock()
	if len(tasks) != 1 || !slices.Contains(calls, "turn.start") || slices.Contains(calls, "thread.bootstrap") {
		t.Fatalf("tasks=%+v calls=%v", tasks, calls)
	}
}

func TestMessageGetDoesNotRevealCrossAssistantTargets(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	ctx := context.Background()
	first := module.Handle(ctx, "puck.create", json.RawMessage(`{"name":"One","brainDeviceId":"brain-a"}`)).Data["assistant"].(map[string]any)["assistantId"].(string)
	second := module.Handle(ctx, "puck.create", json.RawMessage(`{"name":"Two","brainDeviceId":"brain-a"}`)).Data["assistant"].(map[string]any)["assistantId"].(string)
	if err := module.store.AppendMessage(ctx, Message{ID: "private-message", AssistantID: first, Role: "assistant", Kind: "text", Text: "secret"}); err != nil {
		t.Fatal(err)
	}
	if got := module.Handle(ctx, "puck.message.get", mustJSON(map[string]any{"assistantId": first, "messageId": "private-message"})); got.Error != nil || got.Data["message"].(Message).Text != "secret" {
		t.Fatalf("same assistant get=%+v", got)
	}
	if got := module.Handle(ctx, "puck.message.get", mustJSON(map[string]any{"assistantId": second, "messageId": "private-message"})); got.Error == nil || got.Error.Code != "PUCK_MESSAGE_NOT_FOUND" {
		t.Fatalf("cross assistant get=%+v", got)
	}
}

func TestExplicitExecutionTargetDispatchesDeterministicallyBeforeBrainPlanning(t *testing.T) {
	for _, mode := range []string{"full", "ask"} {
		t.Run(mode, func(t *testing.T) {
			bridge := &fakeModuleBridge{}
			module, err := NewModule(t.TempDir(), bridge)
			if err != nil {
				t.Fatal(err)
			}
			defer module.Close()
			created := module.Handle(context.Background(), "puck.create", mustJSON(map[string]any{"name": "Helper", "brainDeviceId": "brain-a", "permissionMode": mode}))
			id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
			module.clearPending(id)
			bridge.mu.Lock()
			bridge.calls, bridge.params, bridge.origins = nil, nil, nil
			bridge.mu.Unlock()
			result := module.Handle(context.Background(), "puck.message.send", mustJSON(map[string]any{"assistantId": id, "clientMessageId": "message-a", "text": "请在 exec-a 的 /work/project 运行测试"}))
			if result.Error != nil {
				t.Fatalf("result=%+v", result)
			}
			tasks, _ := module.store.Tasks(context.Background(), id)
			var authorizations int
			if err := module.store.db.QueryRow(`SELECT COUNT(*) FROM authorizations WHERE assistant_id=?`, id).Scan(&authorizations); err != nil {
				t.Fatal(err)
			}
			if mode == "full" && (len(tasks) != 1 || authorizations != 0) {
				t.Fatalf("full tasks=%d authorizations=%d", len(tasks), authorizations)
			}
			if mode == "ask" && (len(tasks) != 0 || authorizations != 1) {
				t.Fatalf("ask tasks=%d authorizations=%d", len(tasks), authorizations)
			}
			bridge.mu.Lock()
			defer bridge.mu.Unlock()
			if slices.Contains(bridge.calls, "puck.brain.turn.start") {
				t.Fatalf("verified execution target was left to nondeterministic Brain planning: %v", bridge.calls)
			}
		})
	}
}

func TestExplicitExecutionTargetRequiresExactDeviceToken(t *testing.T) {
	devices := []map[string]any{
		{"deviceId": "exec", "capabilities": map[string]bool{"threadBootstrap": true}},
		{"deviceId": "exec-a", "capabilities": map[string]bool{"threadBootstrap": true}},
	}
	deviceID, cwd := explicitExecutionTarget("请在 exec-a 的 /work/project 运行测试", devices)
	if deviceID != "exec-a" || cwd != "/work/project" {
		t.Fatalf("device=%q cwd=%q", deviceID, cwd)
	}
}

func TestMessageResumesBrainAndRetriesOneDefinitelyRejectedTurn(t *testing.T) {
	bridge := &fakeModuleBridge{turnFailureCode: "RESYNC_REQUIRED"}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	module.clearPending(id)
	bridge.mu.Lock()
	bridge.calls, bridge.params, bridge.turnFailures = nil, nil, 1
	bridge.mu.Unlock()

	result := module.Handle(context.Background(), "puck.message.send", mustJSON(map[string]any{"assistantId": id, "clientMessageId": "recovery-message", "text": "继续"}))
	if result.Error != nil {
		t.Fatalf("result=%+v", result)
	}
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	if !slices.Equal(bridge.calls, []string{"puck.brain.turn.start", "puck.brain.ensure", "puck.brain.turn.start"}) {
		t.Fatalf("calls=%v", bridge.calls)
	}
	var resumed struct {
		ThreadID string `json:"threadId"`
	}
	if err := json.Unmarshal(bridge.params[1], &resumed); err != nil || resumed.ThreadID != "brain-thread-a" {
		t.Fatalf("resume params=%s thread=%q err=%v", bridge.params[1], resumed.ThreadID, err)
	}
}

func TestAnswerQuestionResumesBrainAndRetriesOneDefinitelyRejectedTurn(t *testing.T) {
	for _, failureCode := range []string{"BRAIN_UNAVAILABLE", "RESYNC_REQUIRED"} {
		t.Run(failureCode, func(t *testing.T) {
			bridge := &fakeModuleBridge{turnFailureCode: failureCode}
			module, err := NewModule(t.TempDir(), bridge)
			if err != nil {
				t.Fatal(err)
			}
			defer module.Close()
			created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
			id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
			module.clearPending(id)
			if err := module.store.AppendMessage(context.Background(), Message{ID: "question-card:rag-scope", AssistantID: id, Role: "assistant", Kind: "question", Text: "RAG 进度范围？", CardJSON: `{"cardState":"pending"}`}); err != nil {
				t.Fatal(err)
			}
			bridge.mu.Lock()
			bridge.calls, bridge.params, bridge.turnFailures = nil, nil, 1
			bridge.mu.Unlock()

			result := module.Handle(context.Background(), "puck.question.answer", mustJSON(map[string]any{"assistantId": id, "questionId": "rag-scope", "answer": "本周"}))
			if result.Error != nil {
				t.Fatalf("result=%+v", result)
			}
			bridge.mu.Lock()
			calls := append([]string(nil), bridge.calls...)
			params := append([]json.RawMessage(nil), bridge.params...)
			bridge.mu.Unlock()
			if !slices.Equal(calls, []string{"puck.brain.turn.start", "puck.brain.ensure", "puck.brain.turn.start"}) {
				t.Fatalf("calls=%v", calls)
			}
			resumeParams := params[1]
			var resumed struct {
				ThreadID string `json:"threadId"`
			}
			if err := json.Unmarshal(resumeParams, &resumed); err != nil || resumed.ThreadID != "brain-thread-a" {
				t.Fatalf("resume params=%s thread=%q err=%v", resumeParams, resumed.ThreadID, err)
			}
			history := module.Handle(context.Background(), "puck.history", mustJSON(map[string]any{"assistantId": id})).Data["messages"].([]Message)
			found := false
			for _, message := range history {
				if message.ID == "question-card:rag-scope" {
					found = true
					if !strings.Contains(message.CardJSON, `"cardState":"answered"`) {
						t.Fatalf("question card not finalized: %s", message.CardJSON)
					}
				}
			}
			if !found {
				t.Fatal("question card missing")
			}
		})
	}
}

func TestAnswerQuestionRetriesBrainTurnAtMostOnce(t *testing.T) {
	bridge := &fakeModuleBridge{turnFailureCode: "BRAIN_UNAVAILABLE"}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	module.clearPending(id)
	bridge.mu.Lock()
	bridge.calls, bridge.params, bridge.turnFailures = nil, nil, 2
	bridge.mu.Unlock()

	result := module.Handle(context.Background(), "puck.question.answer", mustJSON(map[string]any{"assistantId": id, "questionId": "rag-scope", "answer": "本周"}))
	if result.Error == nil || result.Error.Code != "BRAIN_UNAVAILABLE" {
		t.Fatalf("result=%+v", result)
	}
	bridge.mu.Lock()
	calls := append([]string(nil), bridge.calls...)
	bridge.mu.Unlock()
	if !slices.Equal(calls, []string{"puck.brain.turn.start", "puck.brain.ensure", "puck.brain.turn.start"}) {
		t.Fatalf("calls=%v", calls)
	}
}

func TestBrainErrorCodeIsNeverPersistedAsRawAssistantText(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	module.recordBrainMessage(context.Background(), relay.BrainEvent{AssistantID: id, Payload: json.RawMessage(`{"kind":"item/completed","payload":{"params":{"turnId":"turn-error","item":{"type":"agentMessage","text":"RESYNC_REQUIRED"}}}}`)})
	history := module.Handle(context.Background(), "puck.history", mustJSON(map[string]any{"assistantId": id})).Data["messages"].([]Message)
	last := history[len(history)-1]
	if last.Text == "RESYNC_REQUIRED" || !strings.Contains(last.Text, "重试") || last.Kind != "error" {
		t.Fatalf("last=%+v", last)
	}
}

func TestModuleStartsRealGreetingAndPersistsFallbackWhenTurnUnavailable(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Robin","brainDeviceId":"brain-a","description":"帮助整理研究"}`))
	if created.Error != nil {
		t.Fatalf("create=%+v", created)
	}
	bridge.mu.Lock()
	methods := append([]string(nil), bridge.calls...)
	params := append([]json.RawMessage(nil), bridge.params...)
	bridge.mu.Unlock()
	var greetingPrompt, greetingEffort string
	for index, method := range methods {
		if method == "puck.brain.turn.start" {
			var input struct {
				Text   string `json:"text"`
				Effort string `json:"effort"`
			}
			_ = json.Unmarshal(params[index], &input)
			greetingPrompt, greetingEffort = input.Text, input.Effort
			break
		}
	}
	if !strings.Contains(greetingPrompt, "介绍自己") || !strings.Contains(greetingPrompt, "想先做什么") {
		t.Fatalf("greeting=%q calls=%v", greetingPrompt, methods)
	}
	if greetingEffort != "low" {
		t.Fatalf("greeting effort=%q, want low", greetingEffort)
	}
	if len(greetingPrompt) > 4096 {
		t.Fatalf("greeting prompt unexpectedly grew: bytes=%d", len(greetingPrompt))
	}
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	greetingMessages := module.Handle(context.Background(), "puck.history", mustJSON(map[string]any{"assistantId": id})).Data["messages"].([]Message)
	if len(greetingMessages) != 1 || greetingMessages[0].Kind != "greeting_processing" || !strings.Contains(greetingMessages[0].WorkSummary, `"state":"running"`) {
		t.Fatalf("greeting messages=%+v", greetingMessages)
	}

	fallbackBridge := &fakeModuleBridge{turnFailures: 1}
	fallback, err := NewModule(t.TempDir(), fallbackBridge)
	if err != nil {
		t.Fatal(err)
	}
	defer fallback.Close()
	fallbackCreated := fallback.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Robin","brainDeviceId":"brain-a"}`))
	if fallbackCreated.Error != nil {
		t.Fatalf("fallback create=%+v", fallbackCreated)
	}
	id = fallbackCreated.Data["assistant"].(map[string]any)["assistantId"].(string)
	messages := fallback.Handle(context.Background(), "puck.history", mustJSON(map[string]any{"assistantId": id})).Data["messages"].([]Message)
	if len(messages) != 1 || messages[0].Kind != "notice" || !strings.Contains(messages[0].Text, "大脑") {
		t.Fatalf("messages=%+v", messages)
	}
}

func TestModuleFailedBrainProvisionDoesNotLeaveCapacityConsumingAssistant(t *testing.T) {
	bridge := &fakeModuleBridge{ensureThreads: []string{""}}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Ghost","brainDeviceId":"brain-a"}`))
	if created.Error == nil || created.Error.Code != "BRAIN_UNAVAILABLE" {
		t.Fatalf("create=%+v", created)
	}
	listed := module.Handle(context.Background(), "puck.list", json.RawMessage(`{}`))
	assistants, _ := listed.Data["assistants"].([]map[string]any)
	if len(assistants) != 0 {
		t.Fatalf("failed provisioning left assistants=%+v", assistants)
	}
}

func TestModuleReportsAndEnforcesFiftyActiveAssistantLimit(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	ctx := context.Background()
	for i := 0; i < 49; i++ {
		err := module.store.CreateAssistant(ctx, Assistant{ID: fmt.Sprintf("existing-%02d", i), Name: "Existing", BrainDeviceID: "brain-a", BrainCWD: "/brain", Status: "ready", ToolsetVersion: 1})
		if err != nil {
			t.Fatal(err)
		}
	}
	listed := module.Handle(ctx, "puck.list", nil)
	if got := listed.Data["maxActiveAssistants"]; got != 50 {
		t.Fatalf("reported maxActiveAssistants=%v", got)
	}
	fiftieth := module.Handle(ctx, "puck.create", json.RawMessage(`{"name":"Fiftieth","brainDeviceId":"brain-a"}`))
	if fiftieth.Error != nil {
		t.Fatalf("50th create=%+v", fiftieth)
	}
	overflow := module.Handle(ctx, "puck.create", json.RawMessage(`{"name":"Overflow","brainDeviceId":"brain-a"}`))
	if overflow.Error == nil || overflow.Error.Code != "OVERLOADED" {
		t.Fatalf("51st create=%+v", overflow)
	}
	assistants, err := module.store.Assistants(ctx)
	if err != nil || len(assistants) != 50 {
		t.Fatalf("assistants=%d err=%v", len(assistants), err)
	}
}

func TestModulePersistsRealTurnDurationStepsUnreadAndReactions(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	now := time.Date(2026, 11, 4, 2, 0, 0, 0, time.UTC)
	module.now = func() time.Time { return now }
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a","description":"只处理测试"}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	module.clearPending(id)
	sent := module.Handle(context.Background(), "puck.message.send", mustJSON(map[string]any{"assistantId": id, "clientMessageId": "00000000-0000-4000-8000-000000000130", "text": "开始"}))
	if sent.Error != nil || sent.Data["startedAt"] == nil {
		t.Fatalf("sent=%+v", sent)
	}
	module.handleBrainEvent(context.Background(), relay.BrainEvent{AssistantID: id, DeviceID: "brain-a", ThreadID: "brain-thread-a", Name: "puck.brain.toolCall", Payload: json.RawMessage(`{"callId":"call-a","tool":"list_devices","arguments":{}}`)})
	now = now.Add(2500 * time.Millisecond)
	module.handleBrainEvent(context.Background(), relay.BrainEvent{AssistantID: id, DeviceID: "brain-a", ThreadID: "brain-thread-a", Name: "puck.brain.event", Payload: json.RawMessage(`{"kind":"item/completed","payload":{"method":"item/completed","params":{"turnId":"brain-turn-a","item":{"id":"item-a","type":"agentMessage","text":"完成。"}}}}`)})
	history := module.Handle(context.Background(), "puck.history", mustJSON(map[string]any{"assistantId": id})).Data["messages"].([]Message)
	var final Message
	for _, message := range history {
		if message.Text == "完成。" {
			final = message
		}
	}
	var summary struct {
		ElapsedMS int64 `json:"elapsedMs"`
		Steps     int   `json:"steps"`
	}
	if json.Unmarshal([]byte(final.WorkSummary), &summary) != nil || summary.ElapsedMS != 2500 || summary.Steps != 1 {
		t.Fatalf("final=%+v summary=%+v", final, summary)
	}
	listed := module.Handle(context.Background(), "puck.list", nil).Data["assistants"].([]map[string]any)[0]
	if listed["unreadCount"] != 1 || listed["lastMessagePreview"] != "完成。" {
		t.Fatalf("assistant=%+v", listed)
	}
	if read := module.Handle(context.Background(), "puck.read", mustJSON(map[string]any{"assistantId": id})); read.Error != nil {
		t.Fatalf("read=%+v", read)
	}
	if got := module.Handle(context.Background(), "puck.list", nil).Data["assistants"].([]map[string]any)[0]["unreadCount"]; got != 0 {
		t.Fatalf("unread=%v", got)
	}
	userReaction := module.Handle(context.Background(), "puck.message.react", mustJSON(map[string]any{"assistantId": id, "messageId": "00000000-0000-4000-8000-000000000130", "emoji": "👍"}))
	if userReaction.Error != nil {
		t.Fatalf("user reaction=%+v", userReaction)
	}
	brainReaction := module.tools.Execute(context.Background(), pucktools.Call{AssistantID: id, Tool: "react", Arguments: json.RawMessage(`{"messageId":"00000000-0000-4000-8000-000000000130","emoji":"👀"}`)})
	if !brainReaction.Success {
		t.Fatalf("brain reaction=%+v", brainReaction)
	}
	history = module.Handle(context.Background(), "puck.history", mustJSON(map[string]any{"assistantId": id})).Data["messages"].([]Message)
	var reacted Message
	reactedCount := 0
	for _, message := range history {
		if message.ID == "00000000-0000-4000-8000-000000000130" {
			reacted = message
			reactedCount++
		}
	}
	if reactedCount != 1 {
		t.Fatalf("target message count=%d, want 1: %+v", reactedCount, history)
	}
	var actors map[string]string
	if json.Unmarshal([]byte(reacted.Reaction), &actors) != nil || actors["user"] != "👍" || actors["assistant"] != "👀" {
		t.Fatalf("reaction=%q", reacted.Reaction)
	}
}

func TestStructuredToolRejectionIsVisibleThenFallsBackWithoutRawJSON(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	module.clearPending(id)
	if sent := module.Handle(context.Background(), "puck.message.send", mustJSON(map[string]any{"assistantId": id, "clientMessageId": "00000000-0000-4000-8000-000000000171", "text": "trigger"})); sent.Error != nil {
		t.Fatalf("sent=%+v", sent)
	}
	payload := json.RawMessage(`{"callId":"bad-call","tool":"ask_user","arguments":{"question":"private-question","options":[],"unknown":"private-value"}}`)
	module.handleBrainEvent(context.Background(), relay.BrainEvent{AssistantID: id, DeviceID: "brain-a", ThreadID: "brain-thread-a", Name: "puck.brain.toolCall", Payload: payload})
	history := module.Handle(context.Background(), "puck.history", mustJSON(map[string]any{"assistantId": id})).Data["messages"].([]Message)
	if !hasSafeToolNotice(history, "ask_user", "unknown") {
		t.Fatalf("first rejection missing safe notice: %+v", history)
	}
	payload = json.RawMessage(`{"callId":"bad-call-2","tool":"ask_user","arguments":{"question":"another-private-question","options":[],"unknown":"another-private-value"}}`)
	module.handleBrainEvent(context.Background(), relay.BrainEvent{AssistantID: id, DeviceID: "brain-a", ThreadID: "brain-thread-a", Name: "puck.brain.toolCall", Payload: payload})
	history = module.Handle(context.Background(), "puck.history", mustJSON(map[string]any{"assistantId": id})).Data["messages"].([]Message)
	var fallback bool
	encoded, _ := json.Marshal(history)
	for _, message := range history {
		fallback = fallback || message.Kind == "question" && strings.Contains(message.Text, "直接发送消息") && strings.Contains(message.CardJSON, `"cardState":"invalid"`)
	}
	if !fallback || strings.Contains(string(encoded), "private-question") || strings.Contains(string(encoded), "private-value") {
		t.Fatalf("unsafe or missing fallback: %s", encoded)
	}
}

func hasSafeToolNotice(messages []Message, tool, field string) bool {
	for _, message := range messages {
		if message.Kind == "notice" && strings.Contains(message.Text, tool) && strings.Contains(message.Text, field) {
			return true
		}
	}
	return false
}

func TestSafeDiagnosticFieldRejectsMarkdownAndBoundsLength(t *testing.T) {
	if got := safeDiagnosticField("questions[0].options"); got != "questions[0].options" {
		t.Fatalf("safe path changed: %q", got)
	}
	for _, unsafe := range []string{"[click](https://example.invalid)", strings.Repeat("x", 257)} {
		if got := safeDiagnosticField(unsafe); got != "$" {
			t.Fatalf("unsafe field accepted: %q", got)
		}
	}
}

func TestMessageAcceptsImagesPersistsThemOutsideTextAndForwardsToBrain(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	module.clearPending(id)
	image := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	sent := module.Handle(context.Background(), "puck.message.send", mustJSON(map[string]any{"assistantId": id, "clientMessageId": "00000000-0000-4000-8000-000000000132", "text": "", "images": []string{image}}))
	if sent.Error != nil {
		t.Fatalf("sent=%+v", sent)
	}
	history := module.Handle(context.Background(), "puck.history", mustJSON(map[string]any{"assistantId": id})).Data["messages"].([]Message)
	var user Message
	for _, message := range history {
		if message.ID == "00000000-0000-4000-8000-000000000132" {
			user = message
		}
	}
	if user.Text != "" || !strings.Contains(user.CardJSON, image) {
		t.Fatalf("user=%+v", user)
	}
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	var forwarded struct {
		Text   string   `json:"text"`
		Images []string `json:"images"`
	}
	for index := len(bridge.calls) - 1; index >= 0; index-- {
		if bridge.calls[index] == "puck.brain.turn.start" {
			_ = json.Unmarshal(bridge.params[index], &forwarded)
			break
		}
	}
	if len(forwarded.Images) != 1 || forwarded.Images[0] != image || !strings.Contains(forwarded.Text, "PUCK_ENVELOPE") {
		t.Fatalf("forwarded=%+v", forwarded)
	}
}

func TestMessageRejectsEmptyOrInvalidImages(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	for _, input := range []map[string]any{
		{"assistantId": id, "clientMessageId": "00000000-0000-4000-8000-000000000133", "text": "", "images": []string{}},
		{"assistantId": id, "clientMessageId": "00000000-0000-4000-8000-000000000134", "text": "x", "images": []string{"data:image/svg+xml;base64,PHN2Zy8+"}},
		{"assistantId": id, "clientMessageId": "00000000-0000-4000-8000-000000000135", "text": "x", "images": []string{"data:image/png;base64,AAAA"}},
	} {
		if result := module.Handle(context.Background(), "puck.message.send", mustJSON(input)); result.Error == nil || result.Error.Code != "INVALID_ARGUMENT" {
			t.Fatalf("accepted invalid input=%+v result=%+v", input, result)
		}
	}
}

func TestUpdatedDescriptionIsIncludedInNextTrustedTurn(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	updated := module.Handle(context.Background(), "puck.update", mustJSON(map[string]any{"assistantId": id, "description": "优先给出风险清单"}))
	if updated.Error != nil {
		t.Fatal(updated.Error)
	}
	module.Handle(context.Background(), "puck.message.send", mustJSON(map[string]any{"assistantId": id, "clientMessageId": "00000000-0000-4000-8000-000000000131", "text": "检查方案"}))
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	last := bridge.params[len(bridge.params)-1]
	if !strings.Contains(string(last), "优先给出风险清单") || !strings.Contains(string(last), "检查方案") {
		t.Fatalf("params=%s", last)
	}
}

func TestBrainInstructionsRequireToolCallInSameTurnAndForbidDuplicateStart(t *testing.T) {
	instructions := brainInstructions("assistant-a", "Helper", "检查任务")
	for _, required := range []string{"同一个 turn 中立即继续", "不得在确认文字后结束 turn", "同一条用户消息", "get_task_status", "send_turn", "不得再次 start_task"} {
		if !strings.Contains(instructions, required) {
			t.Fatalf("brain instructions missing %q", required)
		}
	}
}

func (f *fakeModuleBridge) hasToolResult(callID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, method := range f.calls {
		if method == "puck.brain.toolResult" && strings.Contains(string(f.params[i]), `"callId":"`+callID+`"`) {
			return true
		}
	}
	return false
}
func (f *fakeModuleBridge) BrainEvents() <-chan relay.BrainEvent {
	if f.events == nil {
		f.events = make(chan relay.BrainEvent, 4)
	}
	return f.events
}

func (f *fakeModuleBridge) BrainFailures() <-chan relay.BrainDeliveryFailure {
	if f.failures == nil {
		f.failures = make(chan relay.BrainDeliveryFailure, 4)
	}
	return f.failures
}

func TestModuleMakesBrainDeliveryFailureVisible(t *testing.T) {
	bridge := &fakeModuleBridge{events: make(chan relay.BrainEvent, 1), failures: make(chan relay.BrainDeliveryFailure, 1)}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	if created.Error != nil {
		t.Fatal(created.Error)
	}
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	module.pending[id] = pendingBrainTurn{turnID: "turn-a", responseID: "response-a", startedAt: time.Now()}
	module.handleBrainDeliveryFailure(context.Background(), relay.BrainDeliveryFailure{AssistantID: id, Name: "puck.brain.toolCall"})
	messages, err := module.store.Messages(context.Background(), id, 20)
	if err != nil {
		t.Fatal(err)
	}
	last := messages[len(messages)-1]
	if last.ID != "response-a" || last.Kind != "error" || !strings.Contains(last.Text, "繁忙") {
		t.Fatalf("message = %+v", last)
	}
}

func TestModuleRetriesProvisioningWithoutCreatingAnotherAssistant(t *testing.T) {
	bridge := &fakeModuleBridge{ensureFailures: 1}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	failed := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	if failed.Error == nil {
		t.Fatalf("create=%+v", failed)
	}
	listed := module.Handle(context.Background(), "puck.list", nil)
	items := listed.Data["assistants"].([]map[string]any)
	if len(items) != 1 || items[0]["status"] != "provisioning" {
		t.Fatalf("assistants=%+v", items)
	}
	id := items[0]["assistantId"].(string)
	retried := module.Handle(context.Background(), "puck.update", mustJSON(map[string]any{"assistantId": id, "description": "retry"}))
	if retried.Error != nil || retried.Data["assistant"].(map[string]any)["status"] != "ready" {
		t.Fatalf("retry=%+v", retried)
	}
	listed = module.Handle(context.Background(), "puck.list", nil)
	if got := len(listed.Data["assistants"].([]map[string]any)); got != 1 {
		t.Fatalf("assistant count=%d", got)
	}
}

func TestModuleExecutesTrustedBrainToolCallAndReturnsResult(t *testing.T) {
	bridge := &fakeModuleBridge{events: make(chan relay.BrainEvent, 1)}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	bridge.events <- relay.BrainEvent{DeviceID: "brain-a", ThreadID: "brain-thread-a", AssistantID: id, Name: "puck.brain.toolCall", Payload: json.RawMessage(`{"callId":"call-a","tool":"list_devices","arguments":{}}`)}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if bridge.hasToolResult("call-a") {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("tool result was not returned")
}

func TestModuleAuditsFailedBrainToolResultDeliveryWithoutReplay(t *testing.T) {
	bridge := &fakeModuleBridge{events: make(chan relay.BrainEvent, 1), toolResultFailure: true}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	bridge.events <- relay.BrainEvent{DeviceID: "brain-a", ThreadID: "brain-thread-a", AssistantID: id, Name: "puck.brain.toolCall", Payload: json.RawMessage(`{"callId":"call-a","tool":"list_devices","arguments":{}}`)}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var count int
		_ = module.store.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM audit_log WHERE assistant_id=? AND action='brain_tool_result' AND target='list_devices' AND result='OUTCOME_UNKNOWN'`, id).Scan(&count)
		if count == 1 {
			bridge.mu.Lock()
			calls := append([]string(nil), bridge.calls...)
			bridge.mu.Unlock()
			if !slices.Equal(calls[len(calls)-1:], []string{"puck.brain.toolResult"}) {
				t.Fatalf("unexpected replay calls=%v", calls)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("failed tool-result delivery was not audited")
}

func TestModuleRecordsCompletedBrainMessage(t *testing.T) {
	bridge := &fakeModuleBridge{events: make(chan relay.BrainEvent, 1)}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	bridge.events <- relay.BrainEvent{DeviceID: "brain-a", ThreadID: "brain-thread-a", AssistantID: id, Name: "puck.brain.event", Payload: json.RawMessage(`{"kind":"item/completed","payload":{"method":"item/completed","params":{"turnId":"brain-turn-a","item":{"id":"item-a","type":"agentMessage","text":"已经完成。"}}}}`)}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		history := module.Handle(context.Background(), "puck.history", mustJSON(map[string]any{"assistantId": id}))
		if messages := history.Data["messages"].([]Message); len(messages) == 1 && messages[0].Text == "已经完成。" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("completed brain message was not recorded")
}

func TestModuleConsumesAuthorizationAndStopsStartedTask(t *testing.T) {
	bridge := &fakeModuleBridge{events: make(chan relay.BrainEvent, 1)}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a","permissionMode":"ask"}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	arguments := `{"deviceId":"exec-a","cwd":"/work","goal":"test","prompt":"run tests","riskTags":[],"clientTaskId":"00000000-0000-4000-8000-000000000141"}`
	bridge.events <- relay.BrainEvent{DeviceID: "brain-a", ThreadID: "brain-thread-a", AssistantID: id, Name: "puck.brain.toolCall", Payload: json.RawMessage(`{"callId":"call-task","tool":"start_task","arguments":` + arguments + `}`)}
	var authorizationID string
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		history := module.Handle(context.Background(), "puck.history", mustJSON(map[string]any{"assistantId": id}))
		for _, message := range history.Data["messages"].([]Message) {
			if message.Kind == "authorization" {
				var card map[string]any
				_ = json.Unmarshal([]byte(message.CardJSON), &card)
				authorizationID, _ = card["authorizationId"].(string)
			}
		}
		if authorizationID != "" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if authorizationID == "" {
		t.Fatal("authorization card missing")
	}
	history := module.Handle(context.Background(), "puck.history", mustJSON(map[string]any{"assistantId": id}))
	for _, message := range history.Data["messages"].([]Message) {
		if message.Kind != "authorization" {
			continue
		}
		var card map[string]any
		_ = json.Unmarshal([]byte(message.CardJSON), &card)
		for _, key := range []string{"action", "deviceId", "cwd", "expiresAt", "cardState"} {
			if card[key] == nil || card[key] == "" {
				t.Errorf("authorization card missing %s: %+v", key, card)
			}
		}
	}
	decided := module.Handle(context.Background(), "puck.authorization.decide", mustJSON(map[string]any{"assistantId": id, "authorizationId": authorizationID, "decision": "approve_once"}))
	if decided.Error != nil {
		t.Fatalf("decide=%+v", decided)
	}
	history = module.Handle(context.Background(), "puck.history", mustJSON(map[string]any{"assistantId": id}))
	var authCount int
	for _, message := range history.Data["messages"].([]Message) {
		if message.Kind == "authorization" {
			authCount++
			if !strings.Contains(message.CardJSON, `"cardState":"consumed"`) {
				t.Fatalf("authorization card not finalized: %s", message.CardJSON)
			}
		}
	}
	if authCount != 1 {
		t.Fatalf("authorization cards=%d", authCount)
	}
	stopped := module.Handle(context.Background(), "puck.stop", mustJSON(map[string]any{"assistantId": id}))
	if stopped.Error != nil || stopped.Data["summary"] != "已停止当前回复，并请求停止 1 项任务" {
		t.Fatalf("stop=%+v", stopped)
	}
}

func TestModuleApproveAlwaysAutoExecutesDescendantAndRevokeRestoresCard(t *testing.T) {
	bridge := &fakeModuleBridge{events: make(chan relay.BrainEvent, 4)}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a","permissionMode":"ask"}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	sendMessage := func(messageID, text string) {
		result := module.Handle(context.Background(), "puck.message.send", mustJSON(map[string]any{"assistantId": id, "clientMessageId": messageID, "text": text}))
		if result.Error != nil {
			t.Fatalf("message=%+v", result)
		}
	}
	toolCall := func(callID, taskID, cwd string) {
		bridge.events <- relay.BrainEvent{DeviceID: "brain-a", ThreadID: "brain-thread-a", AssistantID: id, Name: "puck.brain.toolCall", Payload: mustJSON(map[string]any{"callId": callID, "tool": "start_task", "arguments": map[string]any{"deviceId": "exec-a", "cwd": cwd, "goal": "test", "prompt": "run tests", "riskTags": []string{}, "clientTaskId": taskID}})}
	}
	waitForCounts := func(tasks, authorizations int) []Message {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			history := module.Handle(context.Background(), "puck.history", mustJSON(map[string]any{"assistantId": id})).Data["messages"].([]Message)
			gotTasks, gotAuthorizations := 0, 0
			for _, message := range history {
				if message.Kind == "task" {
					gotTasks++
				}
				if message.Kind == "authorization" {
					gotAuthorizations++
				}
			}
			if gotTasks == tasks && gotAuthorizations == authorizations {
				return history
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("did not reach tasks=%d authorizations=%d", tasks, authorizations)
		return nil
	}
	sendMessage("message-one", "first")
	toolCall("call-one", "00000000-0000-4000-8000-000000000151", "/work/project")
	history := waitForCounts(0, 1)
	var authorizationID string
	for _, message := range history {
		if message.Kind == "authorization" {
			var card map[string]any
			_ = json.Unmarshal([]byte(message.CardJSON), &card)
			authorizationID, _ = card["authorizationId"].(string)
		}
	}
	decided := module.Handle(context.Background(), "puck.authorization.decide", mustJSON(map[string]any{"assistantId": id, "authorizationId": authorizationID, "decision": "approve_always"}))
	if decided.Error != nil {
		t.Fatalf("decide=%+v", decided)
	}
	waitForCounts(1, 1)
	listed := module.Handle(context.Background(), "puck.authorization.rule.list", mustJSON(map[string]any{"assistantId": id}))
	rules, _ := listed.Data["rules"].([]AuthorizationRule)
	if listed.Error != nil || len(rules) != 1 {
		t.Fatalf("list=%+v", listed)
	}
	sendMessage("message-two", "second")
	toolCall("call-two", "00000000-0000-4000-8000-000000000152", "/work/project/sub")
	history = waitForCounts(2, 1)
	foundAutoRecord := false
	for _, message := range history {
		foundAutoRecord = foundAutoRecord || strings.Contains(message.Text, "以后都允许")
	}
	if !foundAutoRecord {
		t.Fatal("automatic execution record missing")
	}
	revoked := module.Handle(context.Background(), "puck.authorization.rule.revoke", mustJSON(map[string]any{"assistantId": id, "ruleId": rules[0].ID}))
	if revoked.Error != nil {
		t.Fatalf("revoke=%+v", revoked)
	}
	sendMessage("message-three", "third")
	toolCall("call-three", "00000000-0000-4000-8000-000000000153", "/work/project/sub")
	waitForCounts(2, 2)
}

func TestModuleDefaultsFullAndPersistsAskMode(t *testing.T) {
	bridge := &fakeModuleBridge{events: make(chan relay.BrainEvent, 1)}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	assistant := created.Data["assistant"].(map[string]any)
	if assistant["permissionMode"] != "full" {
		t.Fatalf("assistant=%+v", assistant)
	}
	id := assistant["assistantId"].(string)
	updated := module.Handle(context.Background(), "puck.update", mustJSON(map[string]any{"assistantId": id, "permissionMode": "ask"}))
	if updated.Error != nil || updated.Data["assistant"].(map[string]any)["permissionMode"] != "ask" {
		t.Fatalf("updated=%+v", updated)
	}
	stored, err := module.store.Assistant(context.Background(), id)
	if err != nil || permissionMode(stored) != "ask" {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
}

func TestFullModeHighRiskTaskDoesNotCreateAuthorizationCard(t *testing.T) {
	bridge := &fakeModuleBridge{events: make(chan relay.BrainEvent, 1)}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	arguments := `{"deviceId":"exec-a","cwd":"/work","goal":"deploy","prompt":"git push origin main","riskTags":["deploy"],"clientTaskId":"00000000-0000-4000-8000-000000000242"}`
	bridge.events <- relay.BrainEvent{DeviceID: "brain-a", ThreadID: "brain-thread-a", AssistantID: id, Name: "puck.brain.toolCall", Payload: json.RawMessage(`{"callId":"call-full","tool":"start_task","arguments":` + arguments + `}`)}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		tasks, _ := module.store.Tasks(context.Background(), id)
		if len(tasks) == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	tasks, _ := module.store.Tasks(context.Background(), id)
	if len(tasks) != 1 {
		t.Fatal("full task was not dispatched")
	}
	bridge.events <- relay.BrainEvent{DeviceID: "brain-a", ThreadID: "brain-thread-a", AssistantID: id, Name: "puck.brain.toolCall", Payload: mustJSON(map[string]any{"callId": "call-full-send", "tool": "send_turn", "arguments": map[string]any{"taskId": tasks[0].ID, "text": "deploy now", "riskTags": []string{"deploy"}, "clientMessageId": "00000000-0000-4000-8000-000000000243"}})}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		bridge.mu.Lock()
		sent := slices.Contains(bridge.calls, "turn.start")
		bridge.mu.Unlock()
		if sent {
			break
		}
		time.Sleep(time.Millisecond)
	}
	history := module.Handle(context.Background(), "puck.history", mustJSON(map[string]any{"assistantId": id})).Data["messages"].([]Message)
	for _, message := range history {
		if message.Kind == "authorization" {
			t.Fatalf("unexpected card=%s", message.CardJSON)
		}
	}
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	if !slices.Contains(bridge.calls, "turn.start") {
		t.Fatalf("full send_turn was not dispatched: %v", bridge.calls)
	}
	for i, call := range bridge.calls {
		if (call == "thread.bootstrap" || call == "turn.start") && bridge.origins[i].PermissionMode != "full" {
			t.Fatalf("origin=%+v", bridge.origins[i])
		}
	}
}

func TestFirstMessageStartTaskHonorsFullAndAskModes(t *testing.T) {
	for _, mode := range []string{"full", "ask"} {
		t.Run(mode, func(t *testing.T) {
			bridge := &fakeModuleBridge{events: make(chan relay.BrainEvent, 1)}
			module, err := NewModule(t.TempDir(), bridge)
			if err != nil {
				t.Fatal(err)
			}
			defer module.Close()
			created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Fixture","brainDeviceId":"brain-a"}`))
			id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
			if mode == "ask" {
				updated := module.Handle(context.Background(), "puck.update", mustJSON(map[string]any{"assistantId": id, "permissionMode": "ask"}))
				if updated.Error != nil {
					t.Fatalf("update=%+v", updated)
				}
			}
			sent := module.Handle(context.Background(), "puck.message.send", mustJSON(map[string]any{"assistantId": id, "clientMessageId": "first-message-" + mode, "text": "在隔离目录执行测试"}))
			if sent.Error != nil {
				t.Fatalf("send=%+v", sent)
			}
			arguments := `{"deviceId":"exec-a","cwd":"/fixture","goal":"test","prompt":"run isolated tests","riskTags":[],"clientTaskId":"00000000-0000-4000-8000-000000000244"}`
			bridge.events <- relay.BrainEvent{DeviceID: "brain-a", ThreadID: "brain-thread-a", AssistantID: id, Name: "puck.brain.toolCall", Payload: json.RawMessage(`{"callId":"first-call-` + mode + `","tool":"start_task","arguments":` + arguments + `}`)}
			duplicateArguments := `{"deviceId":"exec-a","cwd":"/fixture","goal":"test again","prompt":"run isolated tests again","riskTags":[],"clientTaskId":"00000000-0000-4000-8000-000000000245"}`
			bridge.events <- relay.BrainEvent{DeviceID: "brain-a", ThreadID: "brain-thread-a", AssistantID: id, Name: "puck.brain.toolCall", Payload: json.RawMessage(`{"callId":"duplicate-call-` + mode + `","tool":"start_task","arguments":` + duplicateArguments + `}`)}

			authorizationCount := func() int {
				var count int
				if err := module.store.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM authorizations WHERE assistant_id=? AND status='pending'`, id).Scan(&count); err != nil {
					t.Fatal(err)
				}
				return count
			}
			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) {
				tasks, _ := module.store.Tasks(context.Background(), id)
				if (len(tasks) > 0 || authorizationCount() > 0) && bridge.hasToolResult("duplicate-call-"+mode) {
					break
				}
				time.Sleep(time.Millisecond)
			}
			tasks, _ := module.store.Tasks(context.Background(), id)
			authorizations := authorizationCount()
			bridge.mu.Lock()
			calls := append([]string(nil), bridge.calls...)
			origins := append([]relay.Origin(nil), bridge.origins...)
			bridge.mu.Unlock()
			if mode == "full" {
				if len(tasks) != 1 || authorizations != 0 || !slices.Contains(calls, "thread.bootstrap") {
					t.Fatalf("tasks=%d authorizations=%d calls=%v", len(tasks), authorizations, calls)
				}
				for index, call := range calls {
					if call == "thread.bootstrap" && origins[index].PermissionMode != "full" {
						t.Fatalf("bootstrap origin=%+v", origins[index])
					}
				}
			} else if len(tasks) != 0 || authorizations != 1 || slices.Contains(calls, "thread.bootstrap") {
				t.Fatalf("tasks=%d authorizations=%d calls=%v", len(tasks), authorizations, calls)
			}
		})
	}
}

func TestModuleRotatesBrainBeforeRetiringOldThread(t *testing.T) {
	bridge := &fakeModuleBridge{ensureThreads: []string{"brain-old", "brain-new"}}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	updated := module.Handle(context.Background(), "puck.update", mustJSON(map[string]any{"assistantId": id, "brainDeviceId": "brain-a", "name": "Renamed"}))
	if updated.Error != nil {
		t.Fatalf("update=%+v", updated)
	}
	a := updated.Data["assistant"].(map[string]any)
	if a["brainThreadId"] != "brain-new" || a["name"] != "Renamed" {
		t.Fatalf("assistant=%+v", a)
	}
	if len(bridge.calls) < 3 || bridge.calls[len(bridge.calls)-2] != "puck.brain.ensure" || bridge.calls[len(bridge.calls)-1] != "puck.brain.retire" {
		t.Fatalf("calls=%v", bridge.calls)
	}
}

func TestModuleRotatesBrainWhenStoredToolsetIsObsolete(t *testing.T) {
	bridge := &fakeModuleBridge{ensureThreads: []string{"brain-old", "brain-new"}}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	if _, err := module.store.db.Exec(`UPDATE assistants SET toolset_version=0 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	updated := module.Handle(context.Background(), "puck.update", mustJSON(map[string]any{"assistantId": id, "description": "refresh"}))
	if updated.Error != nil {
		t.Fatalf("update=%+v", updated)
	}
	assistant := updated.Data["assistant"].(map[string]any)
	if assistant["brainThreadId"] != "brain-new" || assistant["toolsetVersion"] != currentToolsetVersion {
		t.Fatalf("assistant=%+v", assistant)
	}
}

func TestModuleLazilyRotatesObsoleteToolsetBeforeNextUserTurn(t *testing.T) {
	bridge := &fakeModuleBridge{ensureThreads: []string{"brain-old", "brain-new"}}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	if _, err := module.store.db.Exec(`UPDATE assistants SET toolset_version=1 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	bridge.mu.Lock()
	baseline := len(bridge.calls)
	bridge.mu.Unlock()
	result := module.Handle(context.Background(), "puck.message.send", mustJSON(map[string]any{"assistantId": id, "clientMessageId": "00000000-0000-4000-8000-000000000151", "text": "继续"}))
	if result.Error != nil {
		t.Fatalf("message=%+v", result)
	}
	a, err := module.store.Assistant(context.Background(), id)
	if err != nil || a.ToolsetVersion != currentToolsetVersion || a.BrainThreadID != "brain-new" {
		t.Fatalf("assistant=%+v err=%v", a, err)
	}
	bridge.mu.Lock()
	calls := append([]string(nil), bridge.calls...)
	bridge.mu.Unlock()
	sequence := calls[baseline:]
	ensure, retire, turn := slices.Index(sequence, "puck.brain.ensure"), slices.Index(sequence, "puck.brain.retire"), slices.Index(sequence, "puck.brain.turn.start")
	if ensure < 0 || retire < 0 || turn < 0 || ensure > retire || retire > turn {
		t.Fatalf("calls=%v", calls)
	}
}

func TestModuleCreateMessageHistoryAndDelete(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a","allowedScope":{}}`))
	if created.Error != nil {
		t.Fatalf("create=%+v", created)
	}
	assistant := created.Data["assistant"].(map[string]any)
	id := assistant["assistantId"].(string)
	if assistant["status"] != "ready" || bridge.brains[relay.BrainThread{DeviceID: "brain-a", ThreadID: "brain-thread-a"}] != id {
		t.Fatalf("assistant=%+v brains=%v", assistant, bridge.brains)
	}
	message := module.Handle(context.Background(), "puck.message.send", mustJSON(map[string]any{"assistantId": id, "clientMessageId": "00000000-0000-4000-8000-000000000130", "text": "hello"}))
	if message.Error != nil || message.Data["state"] != "processing" {
		t.Fatalf("message=%+v", message)
	}
	bridge.mu.Lock()
	var forwarded struct {
		ClientMessageID string `json:"clientMessageId"`
	}
	for i, method := range bridge.calls {
		if method == "puck.brain.turn.start" {
			_ = json.Unmarshal(bridge.params[i], &forwarded)
		}
	}
	bridge.mu.Unlock()
	if forwarded.ClientMessageID != "00000000-0000-4000-8000-000000000130" {
		t.Fatalf("brain clientMessageId=%q", forwarded.ClientMessageID)
	}
	history := module.Handle(context.Background(), "puck.history", mustJSON(map[string]any{"assistantId": id}))
	if history.Error != nil || len(history.Data["messages"].([]Message)) != 2 {
		t.Fatalf("history=%+v", history)
	}
	deleted := module.Handle(context.Background(), "puck.delete", mustJSON(map[string]any{"assistantId": id}))
	if deleted.Error != nil {
		t.Fatalf("delete=%+v", deleted)
	}
	if got := module.Handle(context.Background(), "puck.history", mustJSON(map[string]any{"assistantId": id})); got.Error == nil || got.Error.Code != "PUCK_ASSISTANT_NOT_FOUND" {
		t.Fatalf("deleted history=%+v", got)
	}
}

func TestDeleteRejectsPendingAuthorizationClearsBrainWorkAndPreservesExecutionTask(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	ctx := context.Background()
	created := module.Handle(ctx, "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a","allowedScope":{}}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	if result := module.Handle(ctx, "puck.message.send", mustJSON(map[string]any{"assistantId": id, "clientMessageId": "00000000-0000-4000-8000-000000000131", "text": "continue"})); result.Error != nil {
		t.Fatalf("message=%+v", result)
	}
	if err := module.store.UpsertTask(ctx, Task{ID: "task-a", AssistantID: id, DeviceID: "exec-a", CWD: "/work", NativeThreadID: "exec-thread-a", Goal: "keep running", Status: "running", LastTurnID: "exec-turn-a"}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.InsertAuthorization(ctx, Authorization{ID: "auth-a", AssistantID: id, ActionJSON: `{"tool":"start_task","arguments":{}}`, ActionHash: "hash", RulesHit: `[]`, Status: "pending", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	watchEvents, stopWatch, watchErr := module.Watch(ctx, id)
	if watchErr != nil {
		t.Fatal(watchErr)
	}
	defer stopWatch()
	select {
	case <-watchEvents:
	case <-time.After(time.Second):
		t.Fatal("initial watch snapshot timed out")
	}

	deleted := module.Handle(ctx, "puck.delete", mustJSON(map[string]any{"assistantId": id}))
	if deleted.Error != nil {
		t.Fatalf("delete=%+v", deleted)
	}
	if pending := module.clearPending(id); pending.turnID != "" || pending.timer != nil {
		t.Fatalf("pending brain work survived delete: %+v", pending)
	}
	authorization, err := module.store.Authorization(ctx, id, "auth-a")
	if err != nil || authorization.Status != "rejected" || authorization.DecidedAt.IsZero() {
		t.Fatalf("authorization=%+v err=%v", authorization, err)
	}
	task, err := module.store.Task(ctx, id, "task-a")
	if err != nil || task.Status != "running" || task.NativeThreadID != "exec-thread-a" {
		t.Fatalf("execution task was changed: %+v err=%v", task, err)
	}
	bridge.mu.Lock()
	calls := append([]string(nil), bridge.calls...)
	bridge.mu.Unlock()
	if !slices.Contains(calls, "puck.brain.retire") || slices.Contains(calls, "turn.interrupt") {
		t.Fatalf("delete calls=%v", calls)
	}
	listed := module.Handle(ctx, "puck.list", nil)
	if got := listed.Data["assistants"].([]map[string]any); len(got) != 0 {
		t.Fatalf("deleted assistant remains listed: %+v", got)
	}
	if _, _, watchErr := module.Watch(ctx, id); watchErr == nil || watchErr.Code != "PUCK_ASSISTANT_NOT_FOUND" {
		t.Fatalf("deleted assistant watch error=%+v", watchErr)
	}
	select {
	case _, ok := <-watchEvents:
		if ok {
			t.Fatal("existing watch emitted after assistant deletion")
		}
	case <-time.After(time.Second):
		t.Fatal("existing watch remained open after assistant deletion")
	}
	if again := module.Handle(ctx, "puck.delete", mustJSON(map[string]any{"assistantId": id})); again.Error == nil || again.Error.Code != "PUCK_ASSISTANT_NOT_FOUND" {
		t.Fatalf("second delete=%+v", again)
	}
}

func TestDeletedAssistantIgnoresQueuedBrainToolCall(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	ctx := context.Background()
	created := module.Handle(ctx, "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a","allowedScope":{}}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	if deleted := module.Handle(ctx, "puck.delete", mustJSON(map[string]any{"assistantId": id})); deleted.Error != nil {
		t.Fatal(deleted.Error)
	}
	bridge.mu.Lock()
	before := len(bridge.calls)
	bridge.mu.Unlock()
	module.handleBrainEvent(ctx, relay.BrainEvent{AssistantID: id, DeviceID: "brain-a", ThreadID: "brain-thread-a", Name: "puck.brain.toolCall", Payload: json.RawMessage(`{"CallID":"late-call","Tool":"request_authorization","Arguments":{"summary":"late","action":"deploy"}}`)})
	bridge.mu.Lock()
	after := len(bridge.calls)
	bridge.mu.Unlock()
	if after != before {
		t.Fatalf("queued tool call forwarded after delete: before=%d after=%d", before, after)
	}
	var authorizations int
	if err := module.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM authorizations WHERE assistant_id=?`, id).Scan(&authorizations); err != nil || authorizations != 0 {
		t.Fatalf("late authorizations=%d err=%v", authorizations, err)
	}
}

func TestBrainInstructionsDefineConversationAndSafetyContract(t *testing.T) {
	got := brainInstructions("assistant-a", "小帕", "开发助理")
	for _, want := range []string{"默认使用中文", "先用一句短回复确认", "只在里程碑", "ask_user", "未经验证", "share_result", "Codex Desktop", "必须调用 start_task", "不得只回复确认文字就结束", "不得尝试本地 shell 或 exec", "code_mode_host", "直接调用 Puck tool"} {
		if !strings.Contains(got, want) {
			t.Errorf("instructions missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "禁止调用 exec 或通过 code mode 间接调用工具") {
		t.Fatalf("instructions forbid the code_mode_host transport used by dynamic Puck tools:\n%s", got)
	}
}

func TestModulePersistsAcknowledgementAndInterruptsBrainTurn(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	module.ackDelay = time.Millisecond
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	result := module.Handle(context.Background(), "puck.message.send", mustJSON(map[string]any{"assistantId": id, "clientMessageId": "message-a", "text": "检查测试"}))
	if result.Error != nil {
		t.Fatalf("send=%+v", result)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		history := module.Handle(context.Background(), "puck.history", mustJSON(map[string]any{"assistantId": id}))
		messages := history.Data["messages"].([]Message)
		if len(messages) == 2 && messages[1].Kind == "acknowledged" {
			stopped := module.Handle(context.Background(), "puck.stop", mustJSON(map[string]any{"assistantId": id}))
			if stopped.Error != nil {
				t.Fatalf("stop=%+v", stopped)
			}
			persisted := module.Handle(context.Background(), "puck.history", mustJSON(map[string]any{"assistantId": id})).Data["messages"].([]Message)
			if persisted[len(persisted)-1].Kind != "stop_summary" {
				t.Fatalf("messages=%+v", persisted)
			}
			for _, message := range persisted {
				if message.Kind == "processing" || message.Kind == "acknowledged" || message.Kind == "greeting_processing" {
					t.Fatalf("stopped turn left transient message: %+v", persisted)
				}
			}
			bridge.mu.Lock()
			calls := append([]string(nil), bridge.calls...)
			bridge.mu.Unlock()
			if !slices.Contains(calls, "puck.brain.turn.interrupt") {
				t.Fatalf("calls=%v", calls)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("durable acknowledgement missing")
}

func TestModuleUpdatesAvatar(t *testing.T) {
	module, err := NewModule(t.TempDir(), &fakeModuleBridge{})
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	shapes := []string{"circle", "cloud", "tilted_square", "four_star", "clover", "heart", "flower", "drop", "capsule", "triangle", "tilted_rect", "burst", "hexagon", "pumpkin"}
	for _, shape := range shapes {
		updated := module.Handle(context.Background(), "puck.update", mustJSON(map[string]any{"assistantId": id, "avatar": map[string]any{"shape": shape, "color": "#ff2f92"}}))
		if updated.Error != nil {
			t.Fatalf("shape=%s update=%+v", shape, updated)
		}
		avatar := updated.Data["assistant"].(map[string]any)["avatar"]
		if avatar != fmt.Sprintf(`{"color":"#ff2f92","shape":"%s"}`, shape) {
			t.Fatalf("shape=%s avatar=%v", shape, avatar)
		}
	}
}

func TestModuleConversationWatchEmitsDurableChanges(t *testing.T) {
	module, err := NewModule(t.TempDir(), &fakeModuleBridge{})
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	ctx, cancelContext := context.WithCancel(context.Background())
	events, cancelWatch, watchErr := module.Watch(ctx, id)
	if watchErr != nil {
		t.Fatal(watchErr)
	}
	defer cancelWatch()
	initial := <-events
	if messages := initial.Data["messages"].([]Message); len(messages) != 1 || messages[0].Kind != "greeting_processing" {
		t.Fatalf("initial messages=%+v", messages)
	}
	if err := module.store.AppendMessage(context.Background(), Message{ID: "watch-message", AssistantID: id, Role: "assistant", Kind: "text", Text: "已更新"}); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		messages := event.Data["messages"].([]Message)
		if len(messages) != 2 || messages[1].Text != "已更新" {
			t.Fatalf("messages=%+v", messages)
		}
	case <-time.After(time.Second):
		t.Fatal("watch did not emit durable change")
	}
	cancelContext()
}

func TestModuleConversationWatchSkipsIdleSnapshotQueries(t *testing.T) {
	module, err := NewModule(t.TempDir(), &fakeModuleBridge{})
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	events, stop, watchErr := module.Watch(context.Background(), id)
	if watchErr != nil {
		t.Fatal(watchErr)
	}
	defer stop()
	<-events
	reads := module.watchSnapshotReads.Load()
	time.Sleep(350 * time.Millisecond)
	if got := module.watchSnapshotReads.Load(); got != reads {
		t.Fatalf("idle watcher queried snapshots: before=%d after=%d", reads, got)
	}
	started := time.Now()
	if err := module.store.AppendMessage(context.Background(), Message{ID: "revision-message", AssistantID: id, Role: "assistant", Kind: "text", Text: "revision changed"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-events:
		if elapsed := time.Since(started); elapsed >= 200*time.Millisecond {
			t.Fatalf("mutation notify was not prompt: %s", elapsed)
		}
	case <-time.After(time.Second):
		t.Fatal("watch did not observe mutation")
	}
}
