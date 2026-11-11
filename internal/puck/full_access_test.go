package puck

import (
	"context"
	"strings"
	"testing"
	"time"

	pucktools "github.com/wu8685/Ariel/internal/puck/tools"
	"github.com/wu8685/Ariel/internal/relay"
)

func TestSendMessagePostsOnceNotifiesAndDoesNotEcho(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	ctx := context.Background()
	created := module.Handle(ctx, "puck.create", mustJSON(map[string]any{"name": "Helper", "brainDeviceId": "brain-a"}))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	threadID := created.Data["assistant"].(map[string]any)["brainThreadId"].(string)
	module.clearPending(id)
	notice := "发布已经完成。"
	payload := mustJSON(map[string]any{"callId": "call-notice", "tool": "send_message", "arguments": map[string]any{"text": notice, "clientMessageId": "00000000-0000-4000-8000-000000000901"}})
	module.handleBrainEvent(ctx, relay.BrainEvent{AssistantID: id, DeviceID: "brain-a", ThreadID: threadID, Name: "puck.brain.toolCall", Payload: payload})
	module.handleBrainEvent(ctx, relay.BrainEvent{AssistantID: id, DeviceID: "brain-a", ThreadID: threadID, Name: "puck.brain.toolCall", Payload: payload})
	module.handleBrainEvent(ctx, brainEvent(id, "item/agentMessage/completed", `{"turnId":"brain-turn-a","item":{"id":"item-a","type":"agentMessage","text":"发布已经完成。"}}`))
	module.handleBrainEvent(ctx, brainEvent(id, "turn/completed", `{"turnId":"brain-turn-a","turn":{"id":"brain-turn-a","status":"completed"}}`))
	messages, err := module.store.Messages(ctx, id, 20)
	if err != nil {
		t.Fatal(err)
	}
	var posted []Message
	for _, message := range messages {
		if message.Text == notice {
			posted = append(posted, message)
		}
	}
	if len(posted) != 1 || posted[0].ID != "00000000-0000-4000-8000-000000000901" || !strings.Contains(posted[0].CardJSON, `"source":"notify_user"`) || !strings.Contains(posted[0].CardJSON, `"proactive":true`) {
		t.Fatalf("posted=%+v", posted)
	}
}

func TestUserEnvelopeReplyStillRenders(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	ctx := context.Background()
	created := module.Handle(ctx, "puck.create", mustJSON(map[string]any{"name": "Helper", "brainDeviceId": "brain-a"}))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	module.clearPending(id)
	sent := module.Handle(ctx, "puck.message.send", mustJSON(map[string]any{"assistantId": id, "clientMessageId": "00000000-0000-4000-8000-000000000902", "text": "在吗"}))
	if sent.Error != nil {
		t.Fatal(sent.Error)
	}
	module.handleBrainEvent(ctx, brainEvent(id, "item/agentMessage/completed", `{"turnId":"brain-turn-a","item":{"id":"item-a","type":"agentMessage","text":"在，有什么要做的？"}}`))
	module.handleBrainEvent(ctx, brainEvent(id, "turn/completed", `{"turnId":"brain-turn-a","turn":{"id":"brain-turn-a","status":"completed"}}`))
	messages, err := module.store.Messages(ctx, id, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, message := range messages {
		if message.Text == "在，有什么要做的？" && message.Kind == "text" {
			found = true
		}
	}
	if !found {
		t.Fatalf("user-turn reply missing: %+v", messages)
	}
}

func TestSendMessageIsOwnerScoped(t *testing.T) {
	bridge := &fakeModuleBridge{ensureThreads: []string{"brain-owner", "brain-other"}}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	ctx := context.Background()
	owner := module.Handle(ctx, "puck.create", mustJSON(map[string]any{"name": "Owner", "brainDeviceId": "brain-a"}))
	other := module.Handle(ctx, "puck.create", mustJSON(map[string]any{"name": "Other", "brainDeviceId": "brain-a"}))
	ownerID := owner.Data["assistant"].(map[string]any)["assistantId"].(string)
	otherID := other.Data["assistant"].(map[string]any)["assistantId"].(string)
	ownerThread := owner.Data["assistant"].(map[string]any)["brainThreadId"].(string)
	otherThread := other.Data["assistant"].(map[string]any)["brainThreadId"].(string)
	module.clearPending(ownerID)
	module.clearPending(otherID)
	spoofed := module.tools.Execute(ctx, pucktools.Call{AssistantID: ownerID, Tool: "send_message", Arguments: []byte(`{"text":"别写到别人那里","assistantId":"` + otherID + `"}`)})
	if spoofed.Success || spoofed.Code != "INVALID_ARGUMENT" {
		t.Fatalf("spoofed=%+v", spoofed)
	}
	module.handleBrainEvent(ctx, relay.BrainEvent{AssistantID: ownerID, DeviceID: "brain-a", ThreadID: ownerThread, Name: "puck.brain.toolCall", Payload: mustJSON(map[string]any{"callId": "call-owner", "tool": "send_message", "arguments": map[string]any{"text": "只给主人"}})})
	otherMessages, err := module.store.Messages(ctx, otherID, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range otherMessages {
		if strings.Contains(message.Text, "只给主人") || strings.Contains(message.Text, "别写到别人那里") {
			t.Fatalf("other chat=%+v", message)
		}
	}
	stolen := module.tools.Execute(ctx, pucktools.Call{AssistantID: otherID, Tool: "send_message", Arguments: []byte(`{"text":"占用","clientMessageId":"00000000-0000-4000-8000-000000000903"}`)})
	if !stolen.Success {
		t.Fatal(stolen)
	}
	cross := module.tools.Execute(ctx, pucktools.Call{AssistantID: ownerID, Tool: "send_message", Arguments: []byte(`{"text":"改写别人的消息","clientMessageId":"00000000-0000-4000-8000-000000000903"}`)})
	if cross.Success || cross.Code != "UNAUTHORIZED" {
		t.Fatalf("cross=%+v", cross)
	}
	if otherThread == ownerThread {
		t.Fatal("fixtures shared a brain thread")
	}
}

func TestExistingBotMigratesToFullAccessBrain(t *testing.T) {
	bridge := &fakeModuleBridge{ensureThreads: []string{"brain-old", "brain-new"}}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	ctx := context.Background()
	created := module.Handle(ctx, "puck.create", mustJSON(map[string]any{"name": "Helper", "brainDeviceId": "brain-a", "customContext": "先说结论"}))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	if _, err := module.store.db.ExecContext(ctx, `UPDATE assistants SET toolset_version=8 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if err := module.store.AppendMessage(ctx, Message{ID: "kept-chat", AssistantID: id, Role: "user", Kind: "text", Text: "保留的聊天"}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.UpsertTask(ctx, Task{ID: "task-kept", AssistantID: id, DeviceID: "exec-a", CWD: "/work/ariel", NativeThreadID: "exec-thread-kept", Goal: "继续", Status: "running", CreatedBy: "puck"}); err != nil {
		t.Fatal(err)
	}
	sent := module.Handle(ctx, "puck.message.send", mustJSON(map[string]any{"assistantId": id, "clientMessageId": "00000000-0000-4000-8000-000000000904", "text": "继续"}))
	if sent.Error != nil {
		t.Fatalf("send=%+v", sent)
	}
	stored, err := module.store.Assistant(ctx, id)
	if err != nil || stored.ToolsetVersion != currentToolsetVersion || stored.BrainThreadID != "brain-new" || !strings.Contains(stored.BrainThreadHistory, "brain-old") {
		t.Fatalf("assistant=%+v err=%v", stored, err)
	}
	task, err := module.store.Task(ctx, id, "task-kept")
	if err != nil || task.NativeThreadID != "exec-thread-kept" || task.Status != "running" {
		t.Fatalf("task=%+v err=%v", task, err)
	}
	kept, err := module.store.Message(ctx, id, "kept-chat")
	if err != nil || kept.Text != "保留的聊天" {
		t.Fatalf("chat=%+v err=%v", kept, err)
	}
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	var instructions string
	for i, method := range bridge.calls {
		if method == "puck.brain.ensure" {
			instructions = string(bridge.params[i])
		}
	}
	if !strings.Contains(instructions, "send_message") || !strings.Contains(instructions, "完整的 Codex") || strings.Contains(instructions, "不得尝试本地 shell") || !strings.Contains(instructions, "先说结论") {
		t.Fatalf("rotated instructions=%s", instructions)
	}
}

func TestDeleteAssistantPurgesItsPuckDataAndKeepsChat(t *testing.T) {
	bridge := &fakeModuleBridge{ensureThreads: []string{"brain-delete", "brain-keep"}}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	ctx := context.Background()
	first := module.Handle(ctx, "puck.create", mustJSON(map[string]any{"name": "Gone", "brainDeviceId": "brain-a"}))
	second := module.Handle(ctx, "puck.create", mustJSON(map[string]any{"name": "Stay", "brainDeviceId": "brain-a"}))
	goneID := first.Data["assistant"].(map[string]any)["assistantId"].(string)
	stayID := second.Data["assistant"].(map[string]any)["assistantId"].(string)
	if _, err := module.store.db.ExecContext(ctx, `UPDATE assistants SET custom_context='只属于被删的' WHERE id=?`, goneID); err != nil {
		t.Fatal(err)
	}
	if err := module.store.AppendMessage(ctx, Message{ID: "gone-chat", AssistantID: goneID, Role: "user", Kind: "text", Text: "留下的对话"}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.AppendMessage(ctx, Message{ID: "stay-chat", AssistantID: stayID, Role: "user", Kind: "text", Text: "别人的对话"}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.UpsertTask(ctx, Task{ID: "task-gone", AssistantID: goneID, DeviceID: "exec-a", CWD: "/work/gone", NativeThreadID: "thread-gone", Goal: "删", Status: "running", CreatedBy: "puck"}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.UpsertTask(ctx, Task{ID: "task-stay", AssistantID: stayID, DeviceID: "exec-a", CWD: "/work/stay", NativeThreadID: "thread-stay", Goal: "留", Status: "running", CreatedBy: "puck"}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.SaveThreadWatch(ctx, ThreadWatch{ID: "watch-gone", AssistantID: goneID, DeviceID: "exec-a", ThreadID: "thread-gone"}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.SaveThreadWatch(ctx, ThreadWatch{ID: "watch-stay", AssistantID: stayID, DeviceID: "exec-a", ThreadID: "thread-stay"}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.UpsertBrainWake(ctx, BrainWake{ID: "wake-gone", AssistantID: goneID, TaskKey: "task-gone", DedupeKey: "review", Status: "review", Summary: "待投递", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.UpsertBrainWake(ctx, BrainWake{ID: "wake-stay", AssistantID: stayID, TaskKey: "task-stay", DedupeKey: "review", Status: "review", Summary: "别人的", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.RecordWake(ctx, goneID, "task-gone", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := module.store.RecordWake(ctx, stayID, "task-stay", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := module.store.CreateGroup(ctx, Group{ID: "group-a", Name: "一组", Members: []GroupMember{{AssistantID: goneID}, {AssistantID: stayID}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := module.store.db.ExecContext(ctx, `INSERT INTO sidebar_placements(kind,entry_id,section_id,position) VALUES('assistant',?,'',1),('assistant',?,'',2)`, goneID, stayID); err != nil {
		t.Fatal(err)
	}
	deleted := module.Handle(ctx, "puck.delete", mustJSON(map[string]any{"assistantId": goneID}))
	if deleted.Error != nil {
		t.Fatalf("delete=%+v", deleted)
	}
	again := module.Handle(ctx, "puck.delete", mustJSON(map[string]any{"assistantId": goneID}))
	if again.Error == nil || again.Error.Code != "PUCK_ASSISTANT_NOT_FOUND" {
		t.Fatalf("second delete=%+v", again)
	}
	assertCount(t, module, `SELECT COUNT(*) FROM tasks WHERE assistant_id=?`, goneID, 0)
	assertCount(t, module, `SELECT COUNT(*) FROM thread_watches WHERE assistant_id=?`, goneID, 0)
	assertCount(t, module, `SELECT COUNT(*) FROM brain_wakes WHERE assistant_id=?`, goneID, 0)
	assertCount(t, module, `SELECT COUNT(*) FROM wake_log WHERE assistant_id=?`, goneID, 0)
	assertCount(t, module, `SELECT COUNT(*) FROM group_members WHERE assistant_id=?`, goneID, 0)
	assertCount(t, module, `SELECT COUNT(*) FROM sidebar_placements WHERE kind='assistant' AND entry_id=?`, goneID, 0)
	assertCount(t, module, `SELECT COUNT(*) FROM conversation_messages WHERE id='gone-chat'`, "", 1)
	var custom, brainThread string
	var toolset int
	if err := module.store.db.QueryRowContext(ctx, `SELECT custom_context, brain_thread_id, toolset_version FROM assistants WHERE id=?`, goneID).Scan(&custom, &brainThread, &toolset); err != nil {
		t.Fatal(err)
	}
	if custom != "" || brainThread != "" || toolset != 0 {
		t.Fatalf("brain record custom=%q thread=%q toolset=%d", custom, brainThread, toolset)
	}
	if task, err := module.store.Task(ctx, stayID, "task-stay"); err != nil || task.Status != "running" {
		t.Fatalf("other task=%+v err=%v", task, err)
	}
	assertCount(t, module, `SELECT COUNT(*) FROM thread_watches WHERE assistant_id=?`, stayID, 1)
	assertCount(t, module, `SELECT COUNT(*) FROM brain_wakes WHERE assistant_id=?`, stayID, 1)
	assertCount(t, module, `SELECT COUNT(*) FROM wake_log WHERE assistant_id=?`, stayID, 1)
	assertCount(t, module, `SELECT COUNT(*) FROM group_members WHERE assistant_id=?`, stayID, 1)
	assertCount(t, module, `SELECT COUNT(*) FROM sidebar_placements WHERE kind='assistant' AND entry_id=?`, stayID, 1)
	if message, err := module.store.Message(ctx, stayID, "stay-chat"); err != nil || message.Text != "别人的对话" {
		t.Fatalf("other chat=%+v err=%v", message, err)
	}
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	for _, method := range bridge.calls {
		if method == "turn.interrupt" || method == "thread.delete" {
			t.Fatalf("delete touched a Codex thread: %v", bridge.calls)
		}
	}
}

func TestSubsessionDeletionWakesEachOwnerOnce(t *testing.T) {
	bridge := &fakeModuleBridge{ensureThreads: []string{"brain-1", "brain-2", "brain-3"}}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	ctx := context.Background()
	var ids []string
	for _, name := range []string{"A", "B", "C"} {
		created := module.Handle(ctx, "puck.create", mustJSON(map[string]any{"name": name, "brainDeviceId": "brain-a"}))
		ids = append(ids, created.Data["assistant"].(map[string]any)["assistantId"].(string))
		module.clearPending(ids[len(ids)-1])
	}
	if err := module.store.UpsertTask(ctx, Task{ID: "task-a", AssistantID: ids[0], DeviceID: "exec-a", CWD: "/work/a", NativeThreadID: "thread-shared", Goal: "甲", Status: "running", CreatedBy: "puck"}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.UpsertTask(ctx, Task{ID: "task-b", AssistantID: ids[1], DeviceID: "exec-a", CWD: "/work/b", NativeThreadID: "thread-shared", Goal: "乙", Status: "running", CreatedBy: "puck"}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.UpsertTask(ctx, Task{ID: "task-c", AssistantID: ids[2], DeviceID: "exec-a", CWD: "/work/c", NativeThreadID: "thread-other", Goal: "丙", Status: "running", CreatedBy: "puck"}); err != nil {
		t.Fatal(err)
	}
	if err := module.store.SaveThreadWatch(ctx, ThreadWatch{ID: "watch-a", AssistantID: ids[0], DeviceID: "exec-a", ThreadID: "thread-shared", Until: "完成"}); err != nil {
		t.Fatal(err)
	}
	module.watchExecutionTask(module.rootCtx, Task{ID: "task-a", AssistantID: ids[0], DeviceID: "exec-a", NativeThreadID: "thread-shared", Status: "running"})
	if !bridge.emitThread("exec-a", "thread-shared", map[string]any{"deleted": true}) {
		t.Fatal("thread event was not delivered")
	}
	waitFor(t, func() bool { return len(deletedWakes(bridge)) == 2 })
	module.noteThreadDisappeared(ctx, "exec-a", "thread-shared")
	time.Sleep(40 * time.Millisecond)
	if len(deletedWakes(bridge)) != 2 {
		t.Fatalf("deletion woke again: %d", len(deletedWakes(bridge)))
	}
	for _, id := range []string{"task-a", "task-b"} {
		task, err := module.store.Task(ctx, "", id)
		if err == nil {
			t.Fatalf("lookup without owner succeeded: %+v", task)
		}
	}
	taskA, err := module.store.Task(ctx, ids[0], "task-a")
	taskB, errB := module.store.Task(ctx, ids[1], "task-b")
	taskC, errC := module.store.Task(ctx, ids[2], "task-c")
	if err != nil || errB != nil || errC != nil || taskA.Status != "orphaned" || taskB.Status != "orphaned" || taskC.Status != "running" {
		t.Fatalf("a=%+v b=%+v c=%+v errs=%v %v %v", taskA, taskB, taskC, err, errB, errC)
	}
	watch, err := module.store.ThreadWatch(ctx, ids[0], "exec-a", "thread-shared")
	if err != nil || watch.Status != "closed" {
		t.Fatalf("watch=%+v err=%v", watch, err)
	}
	bridge.snapshots = map[string]map[string]any{"exec-a\x00thread-other": {"missing": true}}
	module.primeSnapshot(ctx, ids[2], "exec-a", "thread-other", "task-c", false)
	waitFor(t, func() bool { return len(deletedWakes(bridge)) == 3 })
	if taskC, err = module.store.Task(ctx, ids[2], "task-c"); err != nil || taskC.Status != "orphaned" {
		t.Fatalf("reconcile task=%+v err=%v", taskC, err)
	}
	module.primeSnapshot(ctx, ids[2], "exec-a", "thread-other", "task-c", false)
	time.Sleep(30 * time.Millisecond)
	if len(deletedWakes(bridge)) != 3 {
		t.Fatalf("reconcile woke again: %d", len(deletedWakes(bridge)))
	}
}

func TestSendTurnNotFoundOrphansTheSubsession(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	ctx := context.Background()
	created := module.Handle(ctx, "puck.create", mustJSON(map[string]any{"name": "Helper", "brainDeviceId": "brain-a"}))
	id := created.Data["assistant"].(map[string]any)["assistantId"].(string)
	module.clearPending(id)
	if err := module.store.UpsertTask(ctx, Task{ID: "task-missing", AssistantID: id, DeviceID: "exec-a", CWD: "/work", NativeThreadID: "thread-missing", Goal: "查", Status: "running", CreatedBy: "puck"}); err != nil {
		t.Fatal(err)
	}
	bridge.turnStartCode = "NOT_FOUND"
	result := module.tools.Execute(ctx, pucktools.Call{AssistantID: id, Tool: "send_turn", Arguments: []byte(`{"taskId":"task-missing","text":"继续","riskTags":[],"clientMessageId":"00000000-0000-4000-8000-000000000905"}`)})
	if result.Success || result.Code != "NOT_FOUND" {
		t.Fatalf("send=%+v", result)
	}
	waitFor(t, func() bool {
		return len(deletedWakes(bridge)) == 1 && strings.Contains(deletedWakes(bridge)[0], "status=subsession_deleted")
	})
	task, err := module.store.Task(ctx, id, "task-missing")
	if err != nil || task.Status != "orphaned" {
		t.Fatalf("task=%+v err=%v", task, err)
	}
	bridge.snapshots = map[string]map[string]any{"exec-a\x00thread-missing": {"missing": true}}
	status := module.tools.Execute(ctx, pucktools.Call{AssistantID: id, Tool: "get_task_status", Arguments: []byte(`{"taskId":"task-missing"}`)})
	if !status.Success || status.Data["status"] != "orphaned" {
		t.Fatalf("status=%+v", status)
	}
	time.Sleep(30 * time.Millisecond)
	if len(deletedWakes(bridge)) != 1 {
		t.Fatalf("status read woke again: %d", len(deletedWakes(bridge)))
	}
}

func TestSeparateSubsessionsWakeIndependently(t *testing.T) {
	bridge := &fakeModuleBridge{}
	module, err := NewModule(t.TempDir(), bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	assistantID := seedWatchAssistant(t, module)
	module.clearPending(assistantID)
	first := runningTask(assistantID, "task-one", "exec-thread-one")
	second := runningTask(assistantID, "task-two", "exec-thread-two")
	second.CWD = "/work/other"
	for _, task := range []Task{first, second} {
		if err := module.store.UpsertTask(context.Background(), task); err != nil {
			t.Fatal(err)
		}
		module.watchExecutionTask(module.rootCtx, task)
	}
	if !bridge.emitThread(first.DeviceID, first.NativeThreadID, freshThread("turn-one", "idle", "completed", "第一件做完了", false)) {
		t.Fatal("first thread was not watched")
	}
	waitFor(t, func() bool { return len(taskEventTurns(bridge)) == 1 })
	finishBrain(module, assistantID, "PUCK_SILENT")
	if !bridge.emitThread(second.DeviceID, second.NativeThreadID, freshThread("turn-two", "idle", "failed", "第二件失败了", false)) {
		t.Fatal("second thread was not watched")
	}
	waitFor(t, func() bool { return len(taskEventTurns(bridge)) == 2 })
	joined := strings.Join(taskEventTurns(bridge), "\n")
	if !strings.Contains(joined, "task-one") || !strings.Contains(joined, "task-two") {
		t.Fatalf("wakes=%s", joined)
	}
}

func deletedWakes(bridge *fakeModuleBridge) []string {
	var matched []string
	for _, text := range taskEventTurns(bridge) {
		if strings.Contains(text, "status=subsession_deleted") {
			matched = append(matched, text)
		}
	}
	return matched
}

func assertCount(t *testing.T, module *Module, query, id string, want int) {
	t.Helper()
	var got int
	var err error
	if id == "" {
		err = module.store.db.QueryRow(query).Scan(&got)
	} else {
		err = module.store.db.QueryRow(query, id).Scan(&got)
	}
	if err != nil || got != want {
		t.Fatalf("query %s id=%s got=%d err=%v", query, id, got, err)
	}
}
