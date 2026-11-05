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
	mu              sync.Mutex
	calls           []string
	brains          map[relay.BrainThread]string
	ensureFailures  int
	turnFailures    int
	turnFailureCode string
	ensureThreads   []string
	events          chan relay.BrainEvent
	params          []json.RawMessage
}

func (f *fakeModuleBridge) Devices() []map[string]any {
	return []map[string]any{{"deviceId": "brain-a", "capabilities": map[string]bool{"puckBrainReady": true}}}
}
func (f *fakeModuleBridge) BrainThreads(v map[relay.BrainThread]string) { f.brains = v }
func (f *fakeModuleBridge) Forward(_ context.Context, _ relay.Origin, _ string, method string, params json.RawMessage) relay.PuckResponse {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method)
	f.params = append(f.params, append(json.RawMessage(nil), params...))
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
		return relay.PuckResponse{Outcome: "accepted", Data: map[string]any{"turnId": "brain-turn-a"}}
	}
	if method == "thread.create" {
		return relay.PuckResponse{Outcome: "accepted", Data: map[string]any{"threadId": "exec-thread-a"}}
	}
	if method == "turn.start" {
		return relay.PuckResponse{Outcome: "accepted", Data: map[string]any{"turnId": "exec-turn-a"}}
	}
	return relay.PuckResponse{Outcome: "accepted", Data: map[string]any{}}
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
	var greetingPrompt string
	for index, method := range methods {
		if method == "puck.brain.turn.start" {
			var input struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(params[index], &input)
			greetingPrompt = input.Text
			break
		}
	}
	if !strings.Contains(greetingPrompt, "介绍自己") || !strings.Contains(greetingPrompt, "想先做什么") {
		t.Fatalf("greeting=%q calls=%v", greetingPrompt, methods)
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
	created := module.Handle(context.Background(), "puck.create", json.RawMessage(`{"name":"Helper","brainDeviceId":"brain-a"}`))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	arguments := `{"deviceId":"exec-a","cwd":"/work","goal":"test","prompt":"run tests","riskTags":[],"clientTaskId":"task-a"}`
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

func TestBrainInstructionsDefineConversationAndSafetyContract(t *testing.T) {
	got := brainInstructions("assistant-a", "小帕", "开发助理")
	for _, want := range []string{"默认使用中文", "先用一句短回复确认", "只在里程碑", "ask_user", "未经验证", "share_result", "Codex Desktop"} {
		if !strings.Contains(got, want) {
			t.Errorf("instructions missing %q:\n%s", want, got)
		}
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
