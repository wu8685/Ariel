package puck

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	pucktools "github.com/wu8685/Ariel/internal/puck/tools"
	"github.com/wu8685/Ariel/internal/relay"
)

func seedWatchAssistant(t *testing.T, module *Module) string {
	t.Helper()
	assistant := Assistant{ID: "assistant-watch", Name: "Helper", Description: "看任务", BrainDeviceID: "brain-a", BrainCWD: "agent-managed", BrainThreadID: "brain-thread-a", ToolsetVersion: currentToolsetVersion, Status: "ready", Policy: `{"permissionMode":"full"}`}
	if err := module.store.CreateAssistant(context.Background(), assistant); err != nil {
		t.Fatal(err)
	}
	module.syncBrainThreads(context.Background())
	return assistant.ID
}

func runningTask(assistantID, taskID, threadID string) Task {
	return Task{ID: taskID, AssistantID: assistantID, DeviceID: "exec-a", CWD: "/work/ariel", NativeThreadID: threadID, Goal: "发布", Status: "running", CreatedBy: "puck", LastTurnID: "turn-start"}
}

func freshThread(turnID, runtime, status, text string, blocked bool) map[string]any {
	pending := []any{}
	if blocked {
		pending = []any{map[string]any{"interactionId": "need-approval", "kind": "command_approval", "prompt": "SUPER_SECRET_PROMPT"}}
	}
	thread := map[string]any{
		"runtime": runtime, "pendingInteractions": pending,
		"turns": []any{map[string]any{"turnId": turnID, "status": status, "items": []any{map[string]any{"itemId": "item-1", "role": "assistant", "text": text}}}},
	}
	return map[string]any{"freshness": "fresh", "thread": thread}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}

func taskEventTurns(bridge *fakeModuleBridge) []string {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	var matched []string
	for i, method := range bridge.calls {
		if method != "puck.brain.turn.start" {
			continue
		}
		var params map[string]any
		_ = json.Unmarshal(bridge.params[i], &params)
		text, _ := params["text"].(string)
		if strings.Contains(text, "kind=task_event") {
			matched = append(matched, text)
		}
	}
	return matched
}

func brainEvent(assistantID, method, params string) relay.BrainEvent {
	payload, _ := json.Marshal(map[string]any{"kind": method, "payload": map[string]any{"method": method, "params": json.RawMessage(params)}})
	return relay.BrainEvent{AssistantID: assistantID, DeviceID: "brain-a", ThreadID: "brain-thread-a", Name: "puck.brain.event", Payload: payload}
}

func finishBrain(module *Module, assistantID, text string) {
	ctx := context.Background()
	module.handleBrainEvent(ctx, brainEvent(assistantID, "item/agentMessage/completed", `{"turnId":"brain-turn-a","item":{"id":"item-a","type":"agentMessage","text":"`+text+`"}}`))
	module.handleBrainEvent(ctx, brainEvent(assistantID, "turn/completed", `{"turnId":"brain-turn-a","turn":{"id":"brain-turn-a","status":"completed"}}`))
}

func TestTaskCompletionWakesBrainAndPostsUserMessage(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	assistantID := seedWatchAssistant(t, module)
	task := runningTask(assistantID, "task-done", "exec-thread-a")
	if err := module.store.UpsertTask(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	card, _ := json.Marshal(map[string]any{"taskId": task.ID, "status": "running", "threadId": task.NativeThreadID, "deviceId": task.DeviceID})
	if err := module.store.AppendMessage(context.Background(), Message{ID: "task-card:" + task.ID, AssistantID: assistantID, Role: "assistant", Kind: "task", Text: task.Goal, CardJSON: string(card)}); err != nil {
		t.Fatal(err)
	}
	module.watchExecutionTask(module.rootCtx, task)
	if !bridge.emitThread(task.DeviceID, task.NativeThreadID, freshThread("turn-done", "idle", "completed", "发布已经完成。", false)) {
		t.Fatal("execution thread was not watched")
	}
	waitFor(t, func() bool { return len(taskEventTurns(bridge)) == 1 })
	text := taskEventTurns(bridge)[0]
	if !strings.Contains(text, "taskId=task-done") || !strings.Contains(text, "status=review") || !strings.Contains(text, "发布已经完成。") || strings.Contains(text, "exec-thread-a") || strings.Contains(text, "SUPER_SECRET_PROMPT") {
		t.Fatalf("envelope=%s", text)
	}
	finishBrain(module, assistantID, "发布已经完成。")
	if got := taskEventMessages(t, module, assistantID); len(got) != 0 {
		t.Fatalf("wake text was posted without send_message: %+v", got)
	}
	stored, err := module.store.Task(context.Background(), assistantID, task.ID)
	if err != nil || stored.Status != "review" {
		t.Fatalf("task=%+v err=%v", stored, err)
	}
	updated, err := module.store.Message(context.Background(), assistantID, "task-card:"+task.ID)
	if err != nil || !strings.Contains(updated.CardJSON, `"status":"review"`) {
		t.Fatalf("card=%+v err=%v", updated, err)
	}
}

func TestBlockedTaskWakesBrain(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	assistantID := seedWatchAssistant(t, module)
	task := runningTask(assistantID, "task-blocked", "exec-thread-b")
	if err := module.store.UpsertTask(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	module.watchExecutionTask(module.rootCtx, task)
	if !bridge.emitThread(task.DeviceID, task.NativeThreadID, freshThread("turn-blocked", "inProgress", "inProgress", "卡在发布前", true)) {
		t.Fatal("execution thread was not watched")
	}
	waitFor(t, func() bool { return len(taskEventTurns(bridge)) == 1 })
	text := taskEventTurns(bridge)[0]
	if !strings.Contains(text, "status=waiting_user") || !strings.Contains(text, "卡在发布前") || strings.Contains(text, "SUPER_SECRET_PROMPT") {
		t.Fatalf("envelope=%s", text)
	}
	stored, err := module.store.Task(context.Background(), assistantID, task.ID)
	if err != nil || stored.Status != "waiting_user" {
		t.Fatalf("task=%+v err=%v", stored, err)
	}
}

func TestBusyBrainQueuesTaskWake(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	assistantID := seedWatchAssistant(t, module)
	task := runningTask(assistantID, "task-queued", "exec-thread-c")
	if err := module.store.UpsertTask(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	module.watchExecutionTask(module.rootCtx, task)
	module.setPending(Assistant{ID: assistantID, BrainDeviceID: "brain-a", BrainThreadID: "brain-thread-a"}, "busy-turn", "", "", time.Now(), false, "")
	if !bridge.emitThread(task.DeviceID, task.NativeThreadID, freshThread("turn-queued", "idle", "completed", "排队期间完成了", false)) {
		t.Fatal("execution thread was not watched")
	}
	time.Sleep(50 * time.Millisecond)
	if len(taskEventTurns(bridge)) != 0 {
		t.Fatalf("busy brain was woken immediately: %v", taskEventTurns(bridge))
	}
	module.handleBrainEvent(context.Background(), brainEvent(assistantID, "turn/completed", `{"turnId":"busy-turn","turn":{"id":"busy-turn","status":"completed"}}`))
	waitFor(t, func() bool { return len(taskEventTurns(bridge)) == 1 })
	if !strings.Contains(taskEventTurns(bridge)[0], "status=review") || !strings.Contains(taskEventTurns(bridge)[0], "排队期间完成了") {
		t.Fatalf("queued envelope=%s", taskEventTurns(bridge)[0])
	}
}

func TestTaskWakeRateLimitDefersSameTaskAndSeventhGlobal(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	module.now = func() time.Time { return now }
	assistantID := seedWatchAssistant(t, module)
	ctx := context.Background()
	task := runningTask(assistantID, "task-rate", "exec-thread-d")
	if err := module.store.UpsertTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	module.watchExecutionTask(module.rootCtx, task)
	if !bridge.emitThread(task.DeviceID, task.NativeThreadID, freshThread("turn-1", "idle", "completed", "第一次结束", false)) {
		t.Fatal("execution thread was not watched")
	}
	waitFor(t, func() bool { return len(taskEventTurns(bridge)) == 1 })
	finishBrain(module, assistantID, "第一次结束")
	if !bridge.emitThread(task.DeviceID, task.NativeThreadID, freshThread("turn-2", "idle", "completed", "第二次结束", false)) {
		t.Fatal("second snapshot was not delivered")
	}
	time.Sleep(40 * time.Millisecond)
	if len(taskEventTurns(bridge)) != 1 {
		t.Fatalf("same task woke again inside 5 minutes: %d", len(taskEventTurns(bridge)))
	}
	queued, err := module.store.BrainWakeByTask(ctx, assistantID, task.ID)
	if err != nil || !queued.NotBefore.Equal(now.Add(5*time.Minute)) || queued.Status != "review" {
		t.Fatalf("queued=%+v err=%v", queued, err)
	}
	now = now.Add(5 * time.Minute)
	module.pumpAssistant(ctx, assistantID)
	waitFor(t, func() bool { return len(taskEventTurns(bridge)) == 2 })

	for i := 0; i < 6; i++ {
		if err := module.store.RecordWake(ctx, assistantID, "other-"+string(rune('a'+i)), now); err != nil {
			t.Fatal(err)
		}
	}
	other := runningTask(assistantID, "task-seventh", "exec-thread-e")
	if err := module.store.UpsertTask(ctx, other); err != nil {
		t.Fatal(err)
	}
	module.watchExecutionTask(module.rootCtx, other)
	finishBrain(module, assistantID, "第二次结束")
	if !bridge.emitThread(other.DeviceID, other.NativeThreadID, freshThread("turn-7", "idle", "completed", "第七个", false)) {
		t.Fatal("seventh thread was not watched")
	}
	time.Sleep(40 * time.Millisecond)
	if len(taskEventTurns(bridge)) != 2 {
		t.Fatalf("seventh wake was not deferred: %d", len(taskEventTurns(bridge)))
	}
	seventh, err := module.store.BrainWakeByTask(ctx, assistantID, other.ID)
	if err != nil || seventh.NotBefore.Before(now.Add(time.Minute)) {
		t.Fatalf("seventh=%+v err=%v", seventh, err)
	}
}

func TestRestartReconcilesPersistedWatch(t *testing.T) {
	dir := t.TempDir()
	firstBridge := &fakeModuleBridge{}
	first, err := NewModule(dir, firstBridge)
	if err != nil {
		t.Fatal(err)
	}
	assistantID := seedWatchAssistant(t, first)
	task := runningTask(assistantID, "task-restart", "exec-thread-f")
	if err := first.store.UpsertTask(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	bridge := &fakeModuleBridge{snapshots: map[string]map[string]any{
		task.DeviceID + "\x00" + task.NativeThreadID: freshThread("turn-restart", "idle", "completed", "重启后发现已经完成", false),
	}}
	module, err := NewModule(dir, bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	waitFor(t, func() bool { return len(taskEventTurns(bridge)) == 1 })
	text := taskEventTurns(bridge)[0]
	if !strings.Contains(text, "taskId=task-restart") || !strings.Contains(text, "status=review") || !strings.Contains(text, "重启后发现已经完成") {
		t.Fatalf("reconciled envelope=%s", text)
	}
	stored, err := module.store.Task(context.Background(), assistantID, task.ID)
	if err != nil || stored.Status != "review" {
		t.Fatalf("task=%+v err=%v", stored, err)
	}
}

func TestWatchOfUnownedThreadCannotSendTurns(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	assistantID := seedWatchAssistant(t, module)
	ctx := context.Background()
	if err := module.store.CreateAssistant(ctx, Assistant{ID: "assistant-other", Name: "Other", BrainDeviceID: "brain-a", BrainCWD: "agent-managed", BrainThreadID: "brain-thread-other", ToolsetVersion: currentToolsetVersion, Status: "ready"}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.UpsertTask(ctx, Task{ID: "task-foreign", AssistantID: "assistant-other", DeviceID: "exec-a", CWD: "/work/other", NativeThreadID: "foreign-thread", Goal: "别人的", Status: "running", CreatedBy: "puck"}); err != nil {
		t.Fatal(err)
	}
	brain := module.tools.Execute(ctx, pucktools.Call{AssistantID: assistantID, Tool: "watch_thread", Arguments: json.RawMessage(`{"deviceId":"brain-a","threadId":"brain-thread-a"}`)})
	if brain.Success || brain.Code != "PUCK_SELF_TARGET" {
		t.Fatalf("brain watch=%+v", brain)
	}
	foreign := module.tools.Execute(ctx, pucktools.Call{AssistantID: assistantID, Tool: "watch_thread", Arguments: json.RawMessage(`{"deviceId":"exec-a","threadId":"foreign-thread"}`)})
	if foreign.Success || foreign.Code != "UNAUTHORIZED" {
		t.Fatalf("foreign watch=%+v", foreign)
	}
	watched := module.tools.Execute(ctx, pucktools.Call{AssistantID: assistantID, Tool: "watch_thread", Arguments: json.RawMessage(`{"deviceId":"exec-a","threadId":"user-thread"}`)})
	if !watched.Success || watched.Data["watching"] != true || watched.Data["watchOnly"] != true {
		t.Fatalf("watch=%+v", watched)
	}
	sent := module.tools.Execute(ctx, pucktools.Call{AssistantID: assistantID, Tool: "send_turn", Arguments: json.RawMessage(`{"deviceId":"exec-a","threadId":"user-thread","text":"接管","riskTags":[],"clientMessageId":"00000000-0000-4000-8000-000000000091"}`)})
	if sent.Success || sent.Code != "WATCH_ONLY" {
		t.Fatalf("send=%+v", sent)
	}
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	for _, method := range bridge.calls {
		if method == "turn.start" || method == "thread.bootstrap" {
			t.Fatalf("read-only watch executed a turn: %v", bridge.calls)
		}
	}
}

func taskEventMessages(t *testing.T, module *Module, assistantID string) []Message {
	t.Helper()
	messages, err := module.store.Messages(context.Background(), assistantID, 50)
	if err != nil {
		t.Fatal(err)
	}
	var visible []Message
	for _, message := range messages {
		if message.Role == "assistant" && message.Kind == "text" && strings.Contains(message.CardJSON, `"source":"task_event"`) {
			visible = append(visible, message)
		}
		if strings.Contains(message.Text, "PUCK_SILENT") || strings.Contains(message.Text, "正在根据任务状态写一条说明") {
			t.Fatalf("noise was rendered: %+v", message)
		}
	}
	return visible
}

func TestRepeatedSameStatusDoesNotPostAgain(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	assistantID := seedWatchAssistant(t, module)
	task := runningTask(assistantID, "task-repeat", "exec-thread-repeat")
	if err := module.store.UpsertTask(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	module.watchExecutionTask(module.rootCtx, task)
	if !bridge.emitThread(task.DeviceID, task.NativeThreadID, freshThread("turn-block-1", "inProgress", "inProgress", "需要你批准发布", true)) {
		t.Fatal("not watched")
	}
	waitFor(t, func() bool { return len(taskEventTurns(bridge)) == 1 })
	finishBrain(module, assistantID, "发布卡住了，需要你批准。")
	if !bridge.emitThread(task.DeviceID, task.NativeThreadID, freshThread("turn-block-2", "inProgress", "inProgress", "还是需要你批准发布", true)) {
		t.Fatal("second event was not delivered")
	}
	time.Sleep(40 * time.Millisecond)
	if len(taskEventTurns(bridge)) != 1 {
		t.Fatalf("same blocked state woke again: %d", len(taskEventTurns(bridge)))
	}
	if got := taskEventMessages(t, module, assistantID); len(got) != 0 {
		t.Fatalf("messages=%+v", got)
	}
}

func TestIntermediateTurnEndStaysSilent(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	assistantID := seedWatchAssistant(t, module)
	task := runningTask(assistantID, "task-progress", "exec-thread-progress")
	if err := module.store.UpsertTask(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	module.watchExecutionTask(module.rootCtx, task)
	if !bridge.emitThread(task.DeviceID, task.NativeThreadID, freshThread("turn-progress", "idle", "completed", "还在检查，没有变化", false)) {
		t.Fatal("not watched")
	}
	time.Sleep(40 * time.Millisecond)
	if len(taskEventTurns(bridge)) != 0 {
		t.Fatalf("progress woke the brain: %v", taskEventTurns(bridge))
	}
	stored, err := module.store.Task(context.Background(), assistantID, task.ID)
	if err != nil || stored.Status != "running" {
		t.Fatalf("task=%+v err=%v", stored, err)
	}
	if got := taskEventMessages(t, module, assistantID); len(got) != 0 {
		t.Fatalf("messages=%+v", got)
	}
}

func TestFinalCompletionPostsOneSummary(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	assistantID := seedWatchAssistant(t, module)
	task := runningTask(assistantID, "task-final", "exec-thread-final")
	task.Goal = "发布完成"
	if err := module.store.UpsertTask(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	module.watchExecutionTask(module.rootCtx, task)
	if !bridge.emitThread(task.DeviceID, task.NativeThreadID, freshThread("turn-mid", "idle", "completed", "还在发布，稍后再看", false)) {
		t.Fatal("not watched")
	}
	time.Sleep(30 * time.Millisecond)
	if !bridge.emitThread(task.DeviceID, task.NativeThreadID, freshThread("turn-final", "idle", "completed", "发布已经完成。", false)) {
		t.Fatal("final snapshot was not delivered")
	}
	waitFor(t, func() bool { return len(taskEventTurns(bridge)) == 1 })
	finishBrain(module, assistantID, "发布已经完成。")
	if !bridge.emitThread(task.DeviceID, task.NativeThreadID, freshThread("turn-again", "idle", "completed", "发布已经完成。", false)) {
		t.Fatal("repeat snapshot was not delivered")
	}
	time.Sleep(40 * time.Millisecond)
	if len(taskEventTurns(bridge)) != 1 {
		t.Fatalf("completion was repeated: %d", len(taskEventTurns(bridge)))
	}
	if got := taskEventMessages(t, module, assistantID); len(got) != 0 {
		t.Fatalf("messages=%+v", got)
	}
}

func TestFailurePostsOneMessage(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	assistantID := seedWatchAssistant(t, module)
	task := runningTask(assistantID, "task-failed", "exec-thread-failed")
	if err := module.store.UpsertTask(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	module.watchExecutionTask(module.rootCtx, task)
	if !bridge.emitThread(task.DeviceID, task.NativeThreadID, freshThread("turn-fail", "idle", "failed", "构建失败了", false)) {
		t.Fatal("not watched")
	}
	waitFor(t, func() bool { return len(taskEventTurns(bridge)) == 1 })
	if !strings.Contains(taskEventTurns(bridge)[0], "status=attention") {
		t.Fatalf("envelope=%s", taskEventTurns(bridge)[0])
	}
	finishBrain(module, assistantID, "构建失败了。")
	if !bridge.emitThread(task.DeviceID, task.NativeThreadID, freshThread("turn-fail-2", "idle", "failed", "构建还是失败", false)) {
		t.Fatal("second failure was not delivered")
	}
	time.Sleep(40 * time.Millisecond)
	if len(taskEventTurns(bridge)) != 1 {
		t.Fatalf("failure woke again: %d", len(taskEventTurns(bridge)))
	}
	if got := taskEventMessages(t, module, assistantID); len(got) != 0 {
		t.Fatalf("messages=%+v", got)
	}
}

func TestBrainSilentOutcomeIsNotRendered(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	assistantID := seedWatchAssistant(t, module)
	task := runningTask(assistantID, "task-silent", "exec-thread-silent")
	if err := module.store.UpsertTask(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	module.watchExecutionTask(module.rootCtx, task)
	if !bridge.emitThread(task.DeviceID, task.NativeThreadID, freshThread("turn-silent", "idle", "completed", "发布已经完成。", false)) {
		t.Fatal("not watched")
	}
	waitFor(t, func() bool { return len(taskEventTurns(bridge)) == 1 })
	finishBrain(module, assistantID, "PUCK_SILENT")
	messages, err := module.store.Messages(context.Background(), assistantID, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if strings.Contains(message.Text, "PUCK_SILENT") || strings.Contains(message.CardJSON, `"source":"task_event"`) || message.Text == "完成。" {
			t.Fatalf("silent outcome was rendered: %+v", message)
		}
	}
}
