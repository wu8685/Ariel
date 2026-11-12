package puck

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wu8685/Ariel/internal/puck/toolcontract"
	pucktools "github.com/wu8685/Ariel/internal/puck/tools"
)

func TestBotTaskSchemaMigratesToV11(t *testing.T) {
	store, err := OpenStore(t.TempDir() + "/puck.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.db.Exec(`PRAGMA user_version = 10`); err != nil {
		t.Fatal(err)
	}
	if err := store.migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	var version int
	if err := store.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 11 {
		t.Fatalf("version=%d err=%v", version, err)
	}
	for _, table := range []string{"bot_tasks", "bot_task_messages", "bot_task_sessions", "bot_inbox"} {
		var name string
		if err := store.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name); err != nil || name != table {
			t.Fatalf("table %s err=%v", table, err)
		}
	}
}

func TestUserMessageStaysInInboxUntilAttached(t *testing.T) {
	module, id := newQueueModule(t)
	sent := module.Handle(context.Background(), "puck.message.send", mustJSON(map[string]any{"assistantId": id, "clientMessageId": "user-inbox", "text": "请记住这件事"}))
	if sent.Error != nil {
		t.Fatal(sent.Error)
	}
	inbox, err := module.store.Inbox(context.Background(), id)
	if err != nil || len(inbox) != 1 || inbox[0] != "user-inbox" {
		t.Fatalf("inbox=%v err=%v", inbox, err)
	}
	saved := module.tools.Execute(context.Background(), pucktools.Call{AssistantID: id, Tool: "save_bot_task", Arguments: mustJSON(map[string]any{"title": "记住", "goal": "不要忘", "status": "accepted"})})
	if !saved.Success {
		t.Fatalf("save=%+v", saved)
	}
	taskID := saved.Data["task"].(map[string]any)["id"].(string)
	attached := module.tools.Execute(context.Background(), pucktools.Call{AssistantID: id, Tool: "attach_bot_task", Arguments: mustJSON(map[string]any{"taskId": taskID, "messageIds": []string{"user-inbox"}})})
	if !attached.Success {
		t.Fatalf("attach=%+v", attached)
	}
	inbox, err = module.store.Inbox(context.Background(), id)
	if err != nil || len(inbox) != 0 {
		t.Fatalf("inbox after attach=%v err=%v", inbox, err)
	}
}

func TestBotTaskMergeKeepsHistoryAndSearchReturnsFullText(t *testing.T) {
	module, id := newQueueModule(t)
	ctx := context.Background()
	if err := module.store.AppendMessage(ctx, Message{ID: "msg-long", AssistantID: id, Role: "user", Kind: "text", Text: "关键词 UNIQUE-BODY " + strings.Repeat("原", 800)}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.UpsertTask(ctx, Task{ID: "exec-task", AssistantID: id, DeviceID: "exec-a", CWD: "/work", NativeThreadID: "thread-exec", Goal: "执行", Status: "running", CreatedBy: "puck"}); err != nil {
		t.Fatal(err)
	}
	first := module.tools.Execute(ctx, pucktools.Call{AssistantID: id, Tool: "save_bot_task", Arguments: mustJSON(map[string]any{"id": "task-from", "title": "旧需求", "goal": "先做 A", "status": "in_progress"})})
	second := module.tools.Execute(ctx, pucktools.Call{AssistantID: id, Tool: "save_bot_task", Arguments: mustJSON(map[string]any{"id": "task-into", "title": "总任务", "goal": "继续做", "status": "accepted"})})
	if !first.Success || !second.Success {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	attached := module.tools.Execute(ctx, pucktools.Call{AssistantID: id, Tool: "attach_bot_task", Arguments: mustJSON(map[string]any{"taskId": "task-from", "messageIds": []string{"msg-long"}, "sessions": []any{map[string]any{"kind": "task", "id": "exec-task"}}})})
	if !attached.Success {
		t.Fatalf("attach=%+v", attached)
	}
	merged := module.tools.Execute(ctx, pucktools.Call{AssistantID: id, Tool: "merge_bot_tasks", Arguments: mustJSON(map[string]any{"fromId": "task-from", "intoId": "task-into"})})
	if !merged.Success {
		t.Fatalf("merge=%+v", merged)
	}
	from, err := module.store.BotTask(ctx, id, "task-from")
	if err != nil || from.Status != "merged" || from.MergedInto != "task-into" || len(from.MessageIDs) != 1 || len(from.Sessions) != 1 {
		t.Fatalf("from=%+v err=%v", from, err)
	}
	into, err := module.store.BotTask(ctx, id, "task-into")
	if err != nil || into.Status != "accepted" || len(into.MessageIDs) != 1 || into.MessageIDs[0] != "msg-long" || len(into.Sessions) != 1 || into.Sessions[0].ThreadID != "thread-exec" {
		t.Fatalf("into=%+v err=%v", into, err)
	}
	again := module.tools.Execute(ctx, pucktools.Call{AssistantID: id, Tool: "merge_bot_tasks", Arguments: mustJSON(map[string]any{"fromId": "task-from", "intoId": "task-into"})})
	if again.Success {
		t.Fatal("merged task was merged again")
	}
	found := module.tools.Execute(ctx, pucktools.Call{AssistantID: id, Tool: "list_bot_tasks", Arguments: mustJSON(map[string]any{"query": "旧需求", "limit": 10})})
	if !found.Success || len(found.Data["tasks"].([]map[string]any)) != 1 {
		t.Fatalf("search tasks=%+v", found)
	}
	messages := module.tools.Execute(ctx, pucktools.Call{AssistantID: id, Tool: "search_messages", Arguments: mustJSON(map[string]any{"query": "UNIQUE-BODY", "limit": 5})})
	if !messages.Success {
		t.Fatalf("search messages=%+v", messages)
	}
	got := messages.Data["messages"].([]map[string]any)[0]["text"].(string)
	if !strings.Contains(got, "UNIQUE-BODY") || !strings.Contains(got, strings.Repeat("原", 800)) {
		t.Fatalf("message text was altered: %s", got)
	}
	done := module.tools.Execute(ctx, pucktools.Call{AssistantID: id, Tool: "save_bot_task", Arguments: mustJSON(map[string]any{"id": "task-into", "title": "总任务", "goal": "继续做", "status": "done"})})
	if !done.Success || done.Data["task"].(map[string]any)["status"] != "done" {
		t.Fatalf("done=%+v", done)
	}
}

func TestIdleTurnWakesTheMessageItJustFinished(t *testing.T) {
	module, id := newQueueModule(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 12, 1, 0, 0, 0, time.UTC)
	module.now = func() time.Time { return now }
	if err := module.store.AppendMessage(ctx, Message{ID: "user-1", AssistantID: id, Role: "user", Kind: "text", Text: "FIXTURE_Q1"}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.AddInbox(ctx, id, "user-1", now); err != nil {
		t.Fatal(err)
	}
	assistant, err := module.store.Assistant(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	module.setPending(assistant, "brain-turn-a", "user-1", "user-1-processing", now, false, "")
	module.handleBrainEvent(ctx, brainEvent(id, "turn/completed", `{"turnId":"brain-turn-a","turn":{"id":"brain-turn-a","status":"completed"}}`))
	waitFor(t, func() bool { return len(botQueueTurns(moduleBridge(module))) == 1 })
	text := botQueueTurns(moduleBridge(module))[0]
	if !strings.Contains(text, "user-1") || !strings.Contains(text, "尚未归属") {
		t.Fatalf("wake=%s", text)
	}
	_, attached, err := module.store.InboxAssignment(ctx, id, "user-1")
	if err != nil || attached {
		t.Fatalf("code attached the message: attached=%v err=%v", attached, err)
	}
}

func TestHistoryExposesContextAndToolCode(t *testing.T) {
	module, id := newQueueModule(t)
	ctx := context.Background()
	module.noteToolCall(id, "send_turn", false, "TURN_BUSY")
	module.noteWakeSkip(id, "queue:inbox:user-1", "backoff")
	history := module.Handle(ctx, "puck.history", mustJSON(map[string]any{"assistantId": id}))
	if history.Error != nil {
		t.Fatal(history.Error)
	}
	contextField, _ := history.Data["systemContext"].(map[string]any)
	if contextField["observed"] != true || contextField["hasHandoff"] != true || contextField["hasSave"] != true || contextField["hasMerge"] != true {
		t.Fatalf("context=%v", contextField)
	}
	if lines, _ := contextField["lines"].(int); lines < 2 {
		t.Fatalf("lines=%v", contextField["lines"])
	}
	calls, _ := history.Data["toolCalls"].([]map[string]any)
	if len(calls) != 1 || calls[0]["tool"] != "send_turn" || calls[0]["success"] != false || calls[0]["code"] != "TURN_BUSY" {
		t.Fatalf("calls=%v", history.Data["toolCalls"])
	}
	skips, _ := history.Data["wakeSkips"].([]map[string]any)
	if len(skips) != 1 || skips[0]["reason"] != "backoff" {
		t.Fatalf("skips=%v", history.Data["wakeSkips"])
	}
	posted := module.tools.Execute(ctx, pucktools.Call{AssistantID: id, Tool: "send_message", Arguments: mustJSON(map[string]any{"text": "FIXTURE_NOTICE"})})
	if !posted.Success {
		t.Fatalf("send_message=%+v", posted)
	}
	history = module.Handle(ctx, "puck.history", mustJSON(map[string]any{"assistantId": id}))
	messages, _ := history.Data["messages"].([]Message)
	found := false
	for _, message := range messages {
		if strings.Contains(message.Text, "FIXTURE_NOTICE") && strings.Contains(message.CardJSON, "notify_user") {
			found = true
		}
	}
	if !found {
		t.Fatalf("history missed notify_user: %+v", messages)
	}
}

func TestUnassignedInboxWakesOnceThenRetries(t *testing.T) {
	module, id := newQueueModule(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	module.now = func() time.Time { return now }
	if err := module.store.AppendMessage(ctx, Message{ID: "inbox-1", AssistantID: id, Role: "user", Kind: "text", Text: "还没归属"}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.AddInbox(ctx, id, "inbox-1", now); err != nil {
		t.Fatal(err)
	}
	if _, err := module.store.SaveBotTask(ctx, BotTask{ID: "open-x", AssistantID: id, Title: "FIXTURE_X", Goal: "整理", Status: "accepted"}); err != nil {
		t.Fatal(err)
	}
	module.scheduleBotQueue(ctx, id, "", "")
	waitFor(t, func() bool { return len(botQueueTurns(moduleBridge(module))) == 1 })
	text := botQueueTurns(moduleBridge(module))[0]
	if !strings.Contains(text, "尚未归属") || !strings.Contains(text, "inbox-1") || !strings.Contains(text, "id=open-x") || !strings.Contains(text, "title=FIXTURE_X") {
		t.Fatalf("wake=%s", text)
	}
	module.handleBrainEvent(ctx, brainEvent(id, "turn/completed", `{"turnId":"brain-turn-a","turn":{"id":"brain-turn-a","status":"completed"}}`))
	_, attached, err := module.store.InboxAssignment(ctx, id, "inbox-1")
	if err != nil || attached {
		t.Fatalf("code attached the message: attached=%v err=%v", attached, err)
	}
	if len(botQueueTurns(moduleBridge(module))) != 1 {
		t.Fatal("unassigned inbox was woken again immediately")
	}
	wake, err := module.store.BrainWakeByTask(ctx, id, "queue:inbox:inbox-1")
	if err != nil || wake.NotBefore.After(now.Add(queueRetryBackoff)) {
		t.Fatalf("retry=%+v err=%v", wake, err)
	}
}

func TestEnvelopeListsOpenBotTasks(t *testing.T) {
	module, id := newQueueModule(t)
	ctx := context.Background()
	assistant, err := module.store.Assistant(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := module.store.SaveBotTask(ctx, BotTask{ID: "task-x", AssistantID: id, Title: "FIXTURE_X", Status: "accepted"}); err != nil {
		t.Fatal(err)
	}
	text := module.trustedEnvelope(ctx, assistant, "请处理", nil)
	if !strings.Contains(text, "id=task-x") || !strings.Contains(text, "title=FIXTURE_X") || strings.Contains(text, "goal=") {
		t.Fatalf("envelope=%s", text)
	}
	if strings.Count(text, "接住的事用 save_bot_task 记成任务。") != 0 {
		t.Fatal("envelope repeated the system-context task line")
	}
}

func TestIdleWakePicksNextTaskWithoutLooping(t *testing.T) {
	module, id := newQueueModule(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	module.now = func() time.Time { return now }
	if _, err := module.store.SaveBotTask(ctx, BotTask{ID: "open-task", AssistantID: id, Title: "未做完", Goal: "接着做", Status: "accepted"}); err != nil {
		t.Fatal(err)
	}
	module.scheduleBotQueue(ctx, id, "", "")
	waitFor(t, func() bool { return len(botQueueTurns(moduleBridge(module))) == 1 })
	text := botQueueTurns(moduleBridge(module))[0]
	if !strings.Contains(text, "kind=bot_queue") || !strings.Contains(text, "open-task") || !strings.Contains(text, "接着做") {
		t.Fatalf("wake=%s", text)
	}
	module.scheduleBotQueue(ctx, id, "", "")
	if len(botQueueTurns(moduleBridge(module))) != 1 {
		t.Fatal("idle wake ran while the brain was busy")
	}
	module.handleBrainEvent(ctx, brainEvent(id, "turn/completed", `{"turnId":"brain-turn-a","turn":{"id":"brain-turn-a","status":"completed"}}`))
	if len(botQueueTurns(moduleBridge(module))) != 1 {
		t.Fatal("same task was woken again immediately")
	}
	now = now.Add(6 * time.Minute)
	module.pumpAssistant(ctx, id)
	waitFor(t, func() bool { return len(botQueueTurns(moduleBridge(module))) == 2 })
}

func TestDeleteBotAndSubsessionCleanBotTasks(t *testing.T) {
	module, id := newQueueModule(t)
	ctx := context.Background()
	if err := module.store.AppendMessage(ctx, Message{ID: "keep-msg", AssistantID: id, Role: "user", Kind: "text", Text: "留下"}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.AddInbox(ctx, id, "keep-msg", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := module.store.SaveBotTask(ctx, BotTask{ID: "gone-task", AssistantID: id, Title: "会删", Goal: "删", Status: "accepted"}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.UpsertTask(ctx, Task{ID: "sub-task", AssistantID: id, DeviceID: "exec-a", CWD: "/work", NativeThreadID: "thread-sub", Goal: "子会话", Status: "running", CreatedBy: "puck"}); err != nil {
		t.Fatal(err)
	}
	if _, err := module.store.AttachBotTask(ctx, id, "gone-task", []string{"keep-msg"}, []BotTaskSession{{Kind: "task", RefID: "sub-task"}}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.SaveThreadWatch(ctx, ThreadWatch{ID: "watch-a", AssistantID: id, DeviceID: "exec-a", ThreadID: "thread-sub"}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.UpsertBrainWake(ctx, BrainWake{ID: "wake-a", AssistantID: id, TaskKey: "wake-task", DedupeKey: "once", Status: "review", NotBefore: time.Now(), CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	module.noteThreadDisappeared(ctx, "exec-a", "thread-sub")
	task, err := module.store.BotTask(ctx, id, "gone-task")
	if err != nil || len(task.Sessions) != 0 {
		t.Fatalf("sessions after unlink=%+v err=%v", task.Sessions, err)
	}
	deleted := module.Handle(ctx, "puck.delete", mustJSON(map[string]any{"assistantId": id, "confirm": true}))
	if deleted.Error != nil {
		t.Fatal(deleted.Error)
	}
	for _, query := range []string{
		`SELECT COUNT(*) FROM bot_tasks WHERE assistant_id=?`,
		`SELECT COUNT(*) FROM bot_task_messages WHERE assistant_id=?`,
		`SELECT COUNT(*) FROM bot_inbox WHERE assistant_id=?`,
		`SELECT COUNT(*) FROM bot_task_sessions WHERE assistant_id=?`,
		`SELECT COUNT(*) FROM thread_watches WHERE assistant_id=?`,
		`SELECT COUNT(*) FROM brain_wakes WHERE assistant_id=?`,
	} {
		var count int
		if err := module.store.db.QueryRow(query, id).Scan(&count); err != nil || count != 0 {
			t.Fatalf("query %s count=%d err=%v", query, count, err)
		}
	}
}

func TestToolset11RotatesOnNextUserMessage(t *testing.T) {
	bridge := &fakeModuleBridge{ensureThreads: []string{"brain-old", "brain-new"}}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	created := module.Handle(context.Background(), "puck.create", mustJSON(map[string]any{"name": "Helper", "brainDeviceId": "brain-a"}))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	if _, err := module.store.db.Exec(`UPDATE assistants SET toolset_version=? WHERE id=?`, botTaskToolset-1, id); err != nil {
		t.Fatal(err)
	}
	result := module.Handle(context.Background(), "puck.message.send", mustJSON(map[string]any{"assistantId": id, "clientMessageId": "user-upgrade", "text": "升级工具"}))
	if result.Error != nil {
		t.Fatal(result.Error)
	}
	assistant, err := module.store.Assistant(context.Background(), id)
	if err != nil || assistant.ToolsetVersion != toolcontract.Version || assistant.BrainThreadID != "brain-new" {
		t.Fatalf("assistant=%+v err=%v", assistant, err)
	}
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	var instructions string
	for index, call := range bridge.calls {
		if call != "puck.brain.ensure" {
			continue
		}
		var params map[string]any
		if err := jsonUnmarshal(bridge.params[index], &params); err != nil {
			t.Fatal(err)
		}
		if version, _ := params["toolsetVersion"].(float64); int(version) == toolcontract.Version {
			instructions, _ = params["developerInstructions"].(string)
		}
	}
	if strings.Count(instructions, "要记住的事用 save_bot_task 新建。") != 1 || !strings.Contains(instructions, "merge_bot_tasks") || !strings.Contains(instructions, "search_messages") || !strings.Contains(instructions, "attach_bot_task") {
		t.Fatalf("instructions=%s", instructions)
	}
}

func TestChosenTaskAttachesTheCurrentUserMessage(t *testing.T) {
	module, id := newQueueModule(t)
	ctx := context.Background()
	if err := module.store.AppendMessage(ctx, Message{ID: "user-now", AssistantID: id, Role: "user", Kind: "text", Text: "现在这件事"}); err != nil {
		t.Fatal(err)
	}
	created := module.tools.Execute(ctx, pucktools.Call{AssistantID: id, SourceMessageID: "user-now", Tool: "save_bot_task", Arguments: mustJSON(map[string]any{"title": "新的", "goal": "先记"})})
	if !created.Success {
		t.Fatal(created)
	}
	createdTask, _ := created.Data["task"].(map[string]any)
	createdID, _ := createdTask["id"].(string)
	if ids, _ := createdTask["messageIds"].([]string); createdID == "" || len(ids) != 0 {
		t.Fatalf("new task attached without an id: %+v", createdTask)
	}
	updated := module.tools.Execute(ctx, pucktools.Call{AssistantID: id, SourceMessageID: "user-now", Tool: "save_bot_task", Arguments: mustJSON(map[string]any{"taskId": createdID})})
	if !updated.Success || !strings.Contains(updated.Message, "挂上了当前用户消息") {
		t.Fatalf("update=%+v", updated)
	}
	updatedTask, _ := updated.Data["task"].(map[string]any)
	ids, _ := updatedTask["messageIds"].([]string)
	if len(ids) != 1 || ids[0] != "user-now" {
		t.Fatalf("messages=%v", updatedTask["messageIds"])
	}
	other := module.tools.Execute(ctx, pucktools.Call{AssistantID: id, Tool: "save_bot_task", Arguments: mustJSON(map[string]any{"title": "另一件", "goal": "不挂"})})
	otherTask, _ := other.Data["task"].(map[string]any)
	otherID, _ := otherTask["id"].(string)
	attached := module.tools.Execute(ctx, pucktools.Call{AssistantID: id, SourceMessageID: "user-now", Tool: "attach_bot_task", Arguments: mustJSON(map[string]any{"taskId": otherID})})
	if !attached.Success {
		t.Fatal(attached)
	}
	got, _ := attached.Data["task"].(map[string]any)
	gotIDs, _ := got["messageIds"].([]string)
	if len(gotIDs) != 1 || gotIDs[0] != "user-now" {
		t.Fatalf("attach=%v", got["messageIds"])
	}
}

func TestAttachAndMergeNameTheField(t *testing.T) {
	module, id := newQueueModule(t)
	ctx := context.Background()
	if _, err := module.store.SaveBotTask(ctx, BotTask{ID: "kept", AssistantID: id, Title: "甲", Goal: "甲", Status: "accepted"}); err != nil {
		t.Fatal(err)
	}
	if _, err := module.store.SaveBotTask(ctx, BotTask{ID: "other", AssistantID: id, Title: "乙", Goal: "乙", Status: "accepted"}); err != nil {
		t.Fatal(err)
	}
	_, err := module.store.AttachBotTask(ctx, id, "kept", []string{"missing-message"}, nil)
	if err == nil || !strings.Contains(err.Error(), "messageIds:") || !strings.Contains(err.Error(), "missing-message") {
		t.Fatalf("attach=%v", err)
	}
	if err := module.store.AppendMessage(ctx, Message{ID: "assistant-msg", AssistantID: id, Role: "assistant", Kind: "text", Text: "不是用户"}); err != nil {
		t.Fatal(err)
	}
	_, err = module.store.AttachBotTask(ctx, id, "kept", []string{"assistant-msg"}, nil)
	if err == nil || !strings.Contains(err.Error(), "messageIds:") || !strings.Contains(err.Error(), "role assistant") {
		t.Fatalf("role=%v", err)
	}
	merged := module.tools.Execute(ctx, pucktools.Call{AssistantID: id, Tool: "merge_bot_tasks", Arguments: mustJSON(map[string]any{"fromId": "other", "intoId": "kept"})})
	if !merged.Success || !strings.Contains(merged.Message, "只传 taskId") || !strings.Contains(merged.Message, "没有挂上") {
		t.Fatalf("merge=%+v", merged)
	}
	_, err = module.store.MergeBotTasks(ctx, id, "missing-from", "kept")
	if err == nil || !strings.Contains(err.Error(), "fromId:") {
		t.Fatalf("missing from=%v", err)
	}
}

func newQueueModule(t *testing.T) (*Module, string) {
	t.Helper()
	module, err := NewModule(t.TempDir(), &fakeModuleBridge{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { module.Close() })
	id := seedWatchAssistant(t, module)
	module.clearPending(id)
	return module, id
}

func moduleBridge(module *Module) *fakeModuleBridge {
	return module.bridge.(*fakeModuleBridge)
}

func botQueueTurns(bridge *fakeModuleBridge) []string {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	var matched []string
	for index, method := range bridge.calls {
		if method != "puck.brain.turn.start" {
			continue
		}
		var params map[string]any
		_ = jsonUnmarshal(bridge.params[index], &params)
		text, _ := params["text"].(string)
		if strings.Contains(text, "kind=bot_queue") {
			matched = append(matched, text)
		}
	}
	return matched
}

func TestListBotTasksToolIsAbsentBeforeToolset11(t *testing.T) {
	if _, ok := toolcontract.DefinitionFor(10, "save_bot_task"); ok {
		t.Fatal("toolset 10 advertised save_bot_task")
	}
	if _, ok := toolcontract.DefinitionFor(toolcontract.Version, "merge_bot_tasks"); !ok {
		t.Fatal("current toolset missing merge_bot_tasks")
	}
}
