package puck

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	pucktools "github.com/wu8685/Ariel/internal/puck/tools"
	"github.com/wu8685/Ariel/internal/relay"
)

func TestWatchedConclusionTravelsWithTheNextStartTask(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	ctx := context.Background()
	assistantID := seedWatchAssistant(t, module)
	if err := module.store.CreateAssistant(ctx, Assistant{ID: "assistant-other", Name: "Other", BrainDeviceID: "brain-a", BrainCWD: "agent-managed", BrainThreadID: "brain-thread-other", ToolsetVersion: currentToolsetVersion, Status: "ready"}); err != nil {
		t.Fatal(err)
	}
	const (
		threadID    = "11111111-1111-4111-8111-111111111111"
		unrelatedID = "22222222-2222-4222-8222-222222222222"
		foreignID   = "33333333-3333-4333-8333-333333333333"
	)
	bridge.snapshots = map[string]map[string]any{"exec-a\x00" + threadID: watchedThreadSnapshot(threadID)}
	if err := module.store.SaveThreadWatch(ctx, ThreadWatch{ID: "watch-unrelated", AssistantID: assistantID, DeviceID: "exec-a", ThreadID: unrelatedID}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.SetWatchNotified(ctx, assistantID, "exec-a", unrelatedID, "review", "UNRELATED_SHOULD_STAY_OUT", "seen"); err != nil {
		t.Fatal(err)
	}
	if err := module.store.SaveThreadWatch(ctx, ThreadWatch{ID: "watch-foreign", AssistantID: "assistant-other", DeviceID: "exec-a", ThreadID: foreignID}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.SetWatchNotified(ctx, "assistant-other", "exec-a", foreignID, "review", "FOREIGN_SECRET_CONCLUSION", "seen"); err != nil {
		t.Fatal(err)
	}
	if err := module.store.SaveThreadWatch(ctx, ThreadWatch{ID: "watch-brain", AssistantID: assistantID, DeviceID: "brain-a", ThreadID: "brain-thread-a"}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.SetWatchNotified(ctx, assistantID, "brain-a", "brain-thread-a", "review", "BRAIN_THREAD_SECRET", "seen"); err != nil {
		t.Fatal(err)
	}
	watched := module.tools.Execute(ctx, pucktools.Call{AssistantID: assistantID, Tool: "watch_thread", Arguments: mustJSON(map[string]any{"deviceId": "exec-a", "threadId": threadID, "until": "方案 B"})})
	if !watched.Success || watched.Data["watching"] != true {
		t.Fatalf("watch=%+v", watched)
	}
	waitFor(t, func() bool { return len(taskEventTurns(bridge)) == 1 })
	user := "请按会话 " + threadID + " 的结论，在新目录落地。" + strings.Repeat("长", 500) + "USER_TAIL_OMITTED"
	module.clearPending(assistantID)
	sent := module.Handle(ctx, "puck.message.send", mustJSON(map[string]any{"assistantId": assistantID, "clientMessageId": "user-act", "text": user}))
	if sent.Error != nil {
		t.Fatalf("message=%+v", sent.Error)
	}
	start := func(callID string) {
		module.handleBrainEvent(ctx, relay.BrainEvent{
			AssistantID: assistantID, DeviceID: "brain-a", ThreadID: "brain-thread-a", Name: "puck.brain.toolCall",
			Payload: mustJSON(map[string]any{"callId": callID, "tool": "start_task", "arguments": map[string]any{
				"deviceId": "exec-a", "cwd": "/work/handoff-new", "goal": "落地结论",
				"prompt": "我理解为要在新目录实现发布步骤。", "riskTags": []string{},
				"clientTaskId":     "00000000-0000-4000-8000-000000000401",
				"referenceThreads": []string{foreignID, "brain-thread-a", threadID},
			}}),
		})
	}
	start("call-handoff")
	start("call-handoff-retry")
	text := bootstrapText(t, bridge)
	if strings.Count(text, pucktools.HandoffSentinel) != 1 {
		t.Fatalf("sentinel count in %s", text)
	}
	if !strings.HasPrefix(text, "【Bot 对任务的理解】\n我理解为要在新目录实现发布步骤。") {
		t.Fatalf("brain instruction was rewritten: %s", text)
	}
	for _, want := range []string{
		"请按会话 " + threadID, "最终结论：可以按方案 B 发布。", "方案讨论", "/work/watched", "执行机",
		"codex://threads/" + threadID, "deviceId=exec-a threadId=" + threadID, "【Referenced sessions】", "已省略",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
	for _, absent := range []string{"USER_TAIL_OMITTED", "CONCLUSION_TAIL_OMITTED", "EARLY_TURN_NOT_FOR_HANDOFF", "SUPER_SECRET_PROMPT", "UNRELATED_SHOULD_STAY_OUT", "FOREIGN_SECRET_CONCLUSION", "BRAIN_THREAD_SECRET", unrelatedID, foreignID} {
		if strings.Contains(text, absent) {
			t.Fatalf("pack included %s", absent)
		}
	}
	suffix := text[strings.Index(text, pucktools.HandoffSentinel):]
	if utf8.RuneCountInString(suffix) > pucktools.MaxHandoffRunes {
		t.Fatalf("pack runes=%d", utf8.RuneCountInString(suffix))
	}
	if strings.Count(bootstrapCalls(bridge), "x") != 1 {
		t.Fatalf("bootstrap calls=%d", strings.Count(bootstrapCalls(bridge), "x"))
	}
}

func TestIncludeMessagesAttachesSessionWithoutDumpingTheMessage(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	ctx := context.Background()
	assistantID := seedWatchAssistant(t, module)
	const threadID = "44444444-4444-4444-8444-444444444444"
	if err := module.store.UpsertTask(ctx, Task{ID: "task-cited", AssistantID: assistantID, DeviceID: "exec-a", CWD: "/work/cited", NativeThreadID: threadID, Goal: "被点名的会话", Status: "review", CreatedBy: "puck", LastSummary: "INCLUDE_CONCLUSION"}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.AppendMessage(ctx, Message{ID: "cite-message", AssistantID: assistantID, Role: "user", Kind: "text", Text: "UNIQUE_MESSAGE_BODY " + threadID}); err != nil {
		t.Fatal(err)
	}
	sent := module.Handle(ctx, "puck.message.send", mustJSON(map[string]any{"assistantId": assistantID, "clientMessageId": "user-cite", "text": "请在另一个目录处理。"}))
	if sent.Error != nil {
		t.Fatal(sent.Error)
	}
	module.handleBrainEvent(ctx, relay.BrainEvent{
		AssistantID: assistantID, DeviceID: "brain-a", ThreadID: "brain-thread-a", Name: "puck.brain.toolCall",
		Payload: mustJSON(map[string]any{"callId": "call-cite", "tool": "start_task", "arguments": map[string]any{
			"deviceId": "exec-a", "cwd": "/work/other", "goal": "处理",
			"prompt": "我来处理。", "riskTags": []string{},
			"clientTaskId": "00000000-0000-4000-8000-000000000402", "includeMessages": []string{"cite-message"},
		}}),
	})
	text := bootstrapText(t, bridge)
	if !strings.Contains(text, "INCLUDE_CONCLUSION") || !strings.Contains(text, "codex://threads/"+threadID) || !strings.Contains(text, "请在另一个目录处理。") {
		t.Fatalf("cited session missing: %s", text)
	}
	if strings.Contains(text, "UNIQUE_MESSAGE_BODY") {
		t.Fatalf("includeMessages dumped the message body: %s", text)
	}
}

func TestSendTurnHandoffUsesTheTriggerMessage(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	ctx := context.Background()
	assistantID := seedWatchAssistant(t, module)
	if err := module.store.UpsertTask(ctx, Task{ID: "task-live", AssistantID: assistantID, DeviceID: "exec-a", CWD: "/work/live", NativeThreadID: "55555555-5555-4555-8555-555555555555", Goal: "续投", Status: "running", CreatedBy: "puck"}); err != nil {
		t.Fatal(err)
	}
	sent := module.Handle(ctx, "puck.message.send", mustJSON(map[string]any{"assistantId": assistantID, "clientMessageId": "user-follow", "text": "请按原话继续。"}))
	if sent.Error != nil {
		t.Fatal(sent.Error)
	}
	module.handleBrainEvent(ctx, relay.BrainEvent{
		AssistantID: assistantID, DeviceID: "brain-a", ThreadID: "brain-thread-a", Name: "puck.brain.toolCall",
		Payload: mustJSON(map[string]any{"callId": "call-send", "tool": "send_turn", "arguments": map[string]any{
			"taskId": "task-live", "text": "我来续上。", "riskTags": []string{},
			"clientMessageId": "00000000-0000-4000-8000-000000000403",
		}}),
	})
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	var text string
	for index, method := range bridge.calls {
		if method != "turn.start" {
			continue
		}
		var params map[string]any
		if err := jsonUnmarshal(bridge.params[index], &params); err != nil {
			t.Fatal(err)
		}
		text, _ = params["text"].(string)
	}
	if !strings.Contains(text, "请按原话继续。") || !strings.Contains(text, "我来续上。") || strings.Count(text, pucktools.HandoffSentinel) != 1 {
		t.Fatalf("send_turn text=%s", text)
	}
}

func watchedThreadSnapshot(threadID string) map[string]any {
	return map[string]any{
		"freshness": "fresh",
		"thread": map[string]any{
			"threadId": threadID, "title": "方案讨论", "cwd": "/work/watched", "runtime": "idle",
			"pendingInteractions": []any{map[string]any{"prompt": "SUPER_SECRET_PROMPT"}},
			"turns": []any{
				map[string]any{"turnId": "turn-1", "status": "completed", "items": []any{map[string]any{"role": "assistant", "text": "EARLY_TURN_NOT_FOR_HANDOFF" + strings.Repeat("前", 1500)}}},
				map[string]any{"turnId": "turn-2", "status": "completed", "items": []any{map[string]any{"role": "user", "text": "SUPER_SECRET_PROMPT"}}},
				map[string]any{"turnId": "turn-3", "status": "completed", "items": []any{map[string]any{"role": "assistant", "text": "最终结论：可以按方案 B 发布。" + strings.Repeat("结", 800) + "CONCLUSION_TAIL_OMITTED"}}},
			},
		},
	}
}

func bootstrapText(t *testing.T, bridge *fakeModuleBridge) string {
	t.Helper()
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	var text string
	count := 0
	for index, method := range bridge.calls {
		if method != "thread.bootstrap" {
			continue
		}
		count++
		var params map[string]any
		if err := jsonUnmarshal(bridge.params[index], &params); err != nil {
			t.Fatal(err)
		}
		text, _ = params["text"].(string)
	}
	if count != 1 {
		t.Fatalf("bootstrap count=%d text=%s", count, text)
	}
	return text
}

func bootstrapCalls(bridge *fakeModuleBridge) string {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	count := 0
	for _, method := range bridge.calls {
		if method == "thread.bootstrap" {
			count++
		}
	}
	return strings.Repeat("x", count)
}

func jsonUnmarshal(raw []byte, target any) error {
	return json.Unmarshal(raw, target)
}
