package puck

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

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
	if strings.Count(text, pucktools.HandoffSentinel) != 1 || !strings.HasPrefix(text, "我理解为要在新目录实现发布步骤。\n") {
		t.Fatalf("pack=%s", text)
	}
	for _, want := range []string{
		"方案讨论", "/work/watched", "codex://threads/" + threadID, "deviceId: exec-a", "threadId: " + threadID, "【Referenced sessions】",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
	for _, absent := range []string{"USER_TAIL_OMITTED", "请按会话", "HEAD_ONLY_AT_START", "CONCLUSION-MARKER", "EARLY_TURN_NOT_FOR_HANDOFF", "SUPER_SECRET_PROMPT", "UNRELATED_SHOULD_STAY_OUT", "FOREIGN_SECRET_CONCLUSION", "BRAIN_THREAD_SECRET", "/.ariel/puck", "/work/from-turn", "status:", "watch:", unrelatedID, foreignID} {
		if strings.Contains(text, absent) {
			t.Fatalf("pack included %s in %s", absent, text)
		}
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
	if !strings.HasPrefix(text, "我来处理。\n") || !strings.Contains(text, "codex://threads/"+threadID) || !strings.Contains(text, "被点名的会话") || !strings.Contains(text, "/work/cited") {
		t.Fatalf("cited session missing: %s", text)
	}
	if strings.Contains(text, "UNIQUE_MESSAGE_BODY") || strings.Contains(text, "INCLUDE_CONCLUSION") || strings.Contains(text, "请在另一个目录处理。") {
		t.Fatalf("handoff copied message text: %s", text)
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
	const referenced = "55555555-5555-4555-8555-555555555555"
	const target = "66666666-6666-4666-8666-666666666666"
	if err := module.store.UpsertTask(ctx, Task{ID: "task-live", AssistantID: assistantID, DeviceID: "exec-a", CWD: "/work/live", NativeThreadID: target, Goal: "续投", Status: "running", CreatedBy: "puck"}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.UpsertTask(ctx, Task{ID: "task-ref", AssistantID: assistantID, DeviceID: "exec-a", CWD: "/work/ref", NativeThreadID: referenced, Goal: "被引用", Status: "review", CreatedBy: "puck"}); err != nil {
		t.Fatal(err)
	}
	sent := module.Handle(ctx, "puck.message.send", mustJSON(map[string]any{"assistantId": assistantID, "clientMessageId": "user-follow", "text": "请看会话 " + referenced + "，这句话不要抄进子会话。"}))
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
	module.handleBrainEvent(ctx, relay.BrainEvent{
		AssistantID: assistantID, DeviceID: "brain-a", ThreadID: "brain-thread-a", Name: "puck.brain.toolCall",
		Payload: mustJSON(map[string]any{"callId": "call-send-retry", "tool": "send_turn", "arguments": map[string]any{
			"taskId": "task-live", "text": "我来续上。", "riskTags": []string{},
			"clientMessageId": "00000000-0000-4000-8000-000000000404",
		}}),
	})
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	var text string
	sends := 0
	for index, method := range bridge.calls {
		if method != "turn.start" {
			continue
		}
		var params map[string]any
		if err := jsonUnmarshal(bridge.params[index], &params); err != nil {
			t.Fatal(err)
		}
		text, _ = params["text"].(string)
		sends++
		if strings.Count(text, pucktools.HandoffSentinel) != 1 {
			t.Fatalf("send %d duplicated the pack: %s", sends, text)
		}
	}
	if sends != 2 || !strings.HasPrefix(text, "我来续上。\n") || !strings.Contains(text, "codex://threads/"+referenced) {
		t.Fatalf("sends=%d text=%s", sends, text)
	}
	if strings.Contains(text, "这句话不要抄进子会话") || strings.Contains(text, target) {
		t.Fatalf("send_turn copied the user text or the target thread: %s", text)
	}
}

func watchedThreadSnapshot(threadID string) map[string]any {
	return map[string]any{
		"freshness": "fresh",
		"thread": map[string]any{
			"threadId": threadID, "title": "方案讨论", "cwd": "/Users/owner/.ariel/puck/assistant-watch", "runtime": "idle",
			"session_meta":        map[string]any{"payload": map[string]any{"cwd": "/work/watched"}},
			"pendingInteractions": []any{map[string]any{"prompt": "SUPER_SECRET_PROMPT"}},
			"turns": []any{
				map[string]any{"turnId": "turn-1", "status": "completed", "items": []any{map[string]any{"role": "assistant", "text": "EARLY_TURN_NOT_FOR_HANDOFF" + strings.Repeat("前", 1500)}}},
				map[string]any{"turnId": "turn-2", "status": "completed", "items": []any{map[string]any{"role": "user", "text": "SUPER_SECRET_PROMPT"}}},
				map[string]any{"turnId": "turn-3", "status": "completed", "cwd": "/work/from-turn", "items": []any{map[string]any{"role": "assistant", "text": "HEAD_ONLY_AT_START\n" + strings.Repeat("中", 800) + "\nCONCLUSION-MARKER: use approach B"}}},
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

func TestSendTurnRetryKeepsThePackWithoutRepeatingTheThread(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	ctx := context.Background()
	assistantID := seedWatchAssistant(t, module)
	const referenced = "77777777-7777-4777-8777-777777777777"
	const target = "88888888-8888-4888-8888-888888888888"
	if err := module.store.AppendMessage(ctx, Message{ID: "origin-msg", AssistantID: assistantID, Role: "user", Kind: "text", Text: "ORIGIN_SENTENCE threadId 是 " + referenced}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.SaveThreadWatch(ctx, ThreadWatch{ID: "watch-ref", AssistantID: assistantID, DeviceID: "exec-a", ThreadID: referenced}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.UpsertTask(ctx, Task{ID: "task-live", AssistantID: assistantID, SourceMessageID: "origin-msg", DeviceID: "exec-a", CWD: "/work/live", NativeThreadID: target, Goal: "续投", Status: "running", CreatedBy: "puck"}); err != nil {
		t.Fatal(err)
	}
	rejected := module.tools.Execute(ctx, pucktools.Call{AssistantID: assistantID, Tool: "send_turn", Arguments: mustJSON(map[string]any{
		"taskId": "task-live", "threadId": target, "text": "继续看引用", "riskTags": []string{},
	})})
	if rejected.Success || rejected.Code != "INVALID_ARGUMENT" || !strings.Contains(rejected.Message, "taskId:") || !strings.Contains(rejected.Message, "mutually exclusive") {
		t.Fatalf("rejection=%+v", rejected)
	}
	sent := module.Handle(ctx, "puck.message.send", mustJSON(map[string]any{"assistantId": assistantID, "clientMessageId": "user-retry", "text": "请对刚才的执行任务调用一次 send_turn，text 只写继续看引用。"}))
	if sent.Error != nil {
		t.Fatal(sent.Error)
	}
	module.handleBrainEvent(ctx, relay.BrainEvent{
		AssistantID: assistantID, DeviceID: "brain-a", ThreadID: "brain-thread-a", Name: "puck.brain.toolCall",
		Payload: mustJSON(map[string]any{"callId": "call-bad", "tool": "send_turn", "arguments": map[string]any{
			"taskId": "task-live", "threadId": target, "text": "继续看引用", "riskTags": []string{},
		}}),
	})
	module.handleBrainEvent(ctx, relay.BrainEvent{
		AssistantID: assistantID, DeviceID: "brain-a", ThreadID: "brain-thread-a", Name: "puck.brain.toolCall",
		Payload: mustJSON(map[string]any{"callId": "call-good", "tool": "send_turn", "arguments": map[string]any{
			"taskId": "task-live", "text": "继续看引用", "riskTags": []string{},
		}}),
	})
	history := module.Handle(ctx, "puck.history", mustJSON(map[string]any{"assistantId": assistantID}))
	calls, _ := history.Data["toolCalls"].([]map[string]any)
	var sawField bool
	for _, call := range calls {
		if call["tool"] == "send_turn" && call["code"] == "INVALID_ARGUMENT:taskId" {
			sawField = true
		}
	}
	if !sawField {
		t.Fatalf("tool calls=%v", calls)
	}
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
	if !strings.Contains(text, "codex://threads/"+referenced) || !strings.Contains(text, "deviceId: exec-a") || strings.Contains(text, "ORIGIN_SENTENCE") || strings.Contains(text, target) {
		t.Fatalf("retry pack=%s", text)
	}
}
