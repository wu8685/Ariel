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

func TestIdleWakePicksNextTaskWithoutLooping(t *testing.T) {
	module, id := newQueueModule(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	module.now = func() time.Time { return now }
	if _, err := module.store.SaveBotTask(ctx, BotTask{ID: "open-task", AssistantID: id, Title: "未做完", Goal: "接着做", Status: "accepted"}); err != nil {
		t.Fatal(err)
	}
	module.scheduleBotQueue(ctx, id, "")
	waitFor(t, func() bool { return len(botQueueTurns(moduleBridge(module))) == 1 })
	text := botQueueTurns(moduleBridge(module))[0]
	if !strings.Contains(text, "kind=bot_queue") || !strings.Contains(text, "open-task") || !strings.Contains(text, "接着做") {
		t.Fatalf("wake=%s", text)
	}
	module.scheduleBotQueue(ctx, id, "")
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
	if _, err := module.store.AttachBotTask(ctx, id, "gone-task", nil, []BotTaskSession{{Kind: "task", RefID: "sub-task"}}); err != nil {
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
		`SELECT COUNT(*) FROM bot_inbox WHERE assistant_id=?`,
		`SELECT COUNT(*) FROM bot_task_sessions WHERE assistant_id=?`,
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
	if !strings.Contains(instructions, "save_bot_task") || !strings.Contains(instructions, "merge_bot_tasks") || strings.Count(instructions, "接住的事用 save_bot_task 记成任务。") != 1 {
		t.Fatalf("instructions=%s", instructions)
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
