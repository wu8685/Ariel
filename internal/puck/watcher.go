package puck

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	pucktools "github.com/wu8685/Ariel/internal/puck/tools"
	"github.com/wu8685/Ariel/internal/relay"
)

const (
	wakeTaskInterval = 5 * time.Minute
	wakeGlobalLimit  = 6
	wakeGlobalWindow = time.Minute
)

type deviceStatusSource interface {
	DeviceStatus() <-chan map[string]any
}

func watchMapKey(deviceID, threadID, assistantID string) string {
	return deviceID + "\x00" + threadID + "\x00" + assistantID
}

func (m *Module) runTaskWatches(ctx context.Context) {
	defer m.wg.Done()
	m.reconcileWatches(ctx)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var device <-chan map[string]any
	if source, ok := m.bridge.(deviceStatusSource); ok {
		device = source.DeviceStatus()
	}
	for {
		select {
		case <-ctx.Done():
			m.stopWatches()
			return
		case <-ticker.C:
			m.pumpAll(ctx)
		case status, ok := <-device:
			if !ok {
				m.stopWatches()
				return
			}
			online, _ := status["agentOnline"].(bool)
			deviceID, _ := status["deviceId"].(string)
			if online && deviceID != "" {
				m.reconcileDevice(ctx, deviceID)
			}
		}
	}
}

func (m *Module) reconcileWatches(ctx context.Context) {
	tasks, err := m.store.ActiveExecutionTasks(ctx)
	if err != nil {
		log.Printf("puck task watch reconcile failed: code=PUCK_STORE_ERROR")
		return
	}
	for _, task := range tasks {
		m.startWatch(ctx, task.AssistantID, task.DeviceID, task.NativeThreadID, task.ID, false)
		m.primeSnapshot(ctx, task.AssistantID, task.DeviceID, task.NativeThreadID, task.ID, false)
	}
	watches, err := m.store.ActiveThreadWatches(ctx)
	if err != nil {
		log.Printf("puck thread watch reconcile failed: code=PUCK_STORE_ERROR")
		return
	}
	for _, watch := range watches {
		m.startWatch(ctx, watch.AssistantID, watch.DeviceID, watch.ThreadID, watch.ID, true)
		m.primeSnapshot(ctx, watch.AssistantID, watch.DeviceID, watch.ThreadID, watch.ID, true)
	}
}

func (m *Module) reconcileDevice(ctx context.Context, deviceID string) {
	m.releaseDevice(deviceID)
	tasks, err := m.store.ActiveExecutionTasks(ctx)
	if err != nil {
		return
	}
	for _, task := range tasks {
		if task.DeviceID != deviceID {
			continue
		}
		m.startWatch(ctx, task.AssistantID, task.DeviceID, task.NativeThreadID, task.ID, false)
		m.primeSnapshot(ctx, task.AssistantID, task.DeviceID, task.NativeThreadID, task.ID, false)
	}
	watches, err := m.store.ActiveThreadWatches(ctx)
	if err != nil {
		return
	}
	for _, watch := range watches {
		if watch.DeviceID != deviceID {
			continue
		}
		m.startWatch(ctx, watch.AssistantID, watch.DeviceID, watch.ThreadID, watch.ID, true)
		m.primeSnapshot(ctx, watch.AssistantID, watch.DeviceID, watch.ThreadID, watch.ID, true)
	}
}

func (m *Module) watchExecutionTask(ctx context.Context, task Task) {
	if task.NativeThreadID == "" || !taskWatchable(task.Status) {
		if task.NativeThreadID != "" {
			m.releaseWatch(task.DeviceID, task.NativeThreadID, task.AssistantID)
		}
		return
	}
	m.startWatch(ctx, task.AssistantID, task.DeviceID, task.NativeThreadID, task.ID, false)
}

func (m *Module) startWatch(ctx context.Context, assistantID, deviceID, threadID, taskID string, watchOnly bool) {
	if deviceID == "" || threadID == "" || assistantID == "" {
		return
	}
	key := watchMapKey(deviceID, threadID, assistantID)
	m.watchMu.Lock()
	if m.watchClosed || ctx.Err() != nil {
		m.watchMu.Unlock()
		return
	}
	if _, exists := m.watchCancel[key]; exists {
		m.watchMu.Unlock()
		return
	}
	watchCtx, cancel := context.WithCancel(ctx)
	if m.watchCancel == nil {
		m.watchCancel = map[string]context.CancelFunc{}
	}
	m.watchCancel[key] = cancel
	m.watchWg.Add(1)
	m.watchMu.Unlock()

	events, cancelWatch, watchErr := m.bridge.Watch(watchCtx, relay.Origin{Kind: "puck", AssistantID: assistantID, Trigger: "event"}, deviceID, threadID)
	if watchErr != nil {
		log.Printf("puck task watch rejected: code=%s", watchErr.Code)
		cancel()
		if cancelWatch != nil {
			cancelWatch()
		}
		m.watchMu.Lock()
		delete(m.watchCancel, key)
		m.watchMu.Unlock()
		m.watchWg.Done()
		return
	}
	go func() {
		defer m.watchWg.Done()
		defer cancelWatch()
		for {
			select {
			case <-watchCtx.Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				m.applyThreadSnapshot(watchCtx, assistantID, deviceID, threadID, taskID, watchOnly, event)
			}
		}
	}()
}

func (m *Module) primeSnapshot(ctx context.Context, assistantID, deviceID, threadID, taskID string, watchOnly bool) {
	result := m.bridge.Forward(ctx, relay.Origin{Kind: "puck", AssistantID: assistantID, Trigger: "event"}, deviceID, "thread.read", mustJSON(map[string]any{"threadId": threadID}))
	if result.Error != nil && result.Error.Code == "NOT_FOUND" {
		m.noteThreadDisappeared(ctx, deviceID, threadID)
		return
	}
	if result.Outcome != "accepted" || result.Error != nil || result.Data == nil {
		return
	}
	m.applyThreadSnapshot(ctx, assistantID, deviceID, threadID, taskID, watchOnly, result.Data)
}

func watchMark(seen, told string) string {
	return "seen=" + seen + ";told=" + told
}

func parseWatchMark(raw string) (seen, told string) {
	if strings.HasPrefix(raw, "seen=") {
		body := strings.TrimPrefix(raw, "seen=")
		seen, told, _ = strings.Cut(body, ";told=")
		return seen, told
	}
	if index := strings.IndexByte(raw, '|'); index > 0 {
		return raw, raw[:index]
	}
	return raw, ""
}

func (m *Module) applyThreadSnapshot(ctx context.Context, assistantID, deviceID, threadID, taskID string, watchOnly bool, data map[string]any) {
	if threadDisappeared(data) {
		m.noteThreadDisappeared(ctx, deviceID, threadID)
		return
	}
	view, ok := pucktools.ClassifySnapshot(data)
	if !ok || view.Runtime == "unknown" || view.Freshness != "fresh" {
		return
	}
	seenKey := view.Status + "|" + view.TurnID
	if watchOnly {
		m.wakeMu.Lock()
		watch, err := m.store.ThreadWatch(ctx, assistantID, deviceID, threadID)
		if err != nil || watch.Status != "watching" {
			m.wakeMu.Unlock()
			return
		}
		if watch.LastTurnID == seenKey && view.Status != "running" {
			m.wakeMu.Unlock()
			return
		}
		decision := pucktools.DecideOutcome(view, watch.Until, watch.NotifiedKey, watch.LastSummary)
		if !decision.Notify {
			_ = m.store.RememberThreadSeen(ctx, assistantID, deviceID, threadID, seenKey)
			m.wakeMu.Unlock()
			return
		}
		if err := m.store.SetWatchNotified(ctx, assistantID, deviceID, threadID, decision.Status, view.Summary, seenKey); err != nil {
			m.wakeMu.Unlock()
			return
		}
		m.enqueueLocked(ctx, BrainWake{AssistantID: assistantID, TaskKey: watch.ID, DedupeKey: decision.Status + "|" + seenKey, Status: decision.Status, Summary: view.Summary, WatchOnly: true})
		_ = m.store.CloseThreadWatch(ctx, assistantID, deviceID, threadID)
		m.wakeMu.Unlock()
		m.releaseWatch(deviceID, threadID, assistantID)
		return
	}
	task, err := m.store.Task(ctx, assistantID, taskID)
	if err != nil || !taskWatchable(task.Status) {
		return
	}
	seen, told := parseWatchMark(task.LastSeenRevision)
	if view.Status == "running" {
		if told != "" || (task.Status != "running" && task.Status != "dispatching") {
			_ = m.store.UpdateTaskObservation(ctx, assistantID, taskID, "running", view.TurnID, task.LastSummary, watchMark(seenKey, ""))
			_ = m.updateTaskCardDetails(ctx, assistantID, taskID, "running", "")
		}
		return
	}
	if seen == seenKey {
		return
	}
	decision := pucktools.DecideOutcome(view, task.Goal, told, task.LastSummary)
	if !decision.Notify {
		_ = m.store.RememberTaskSeen(ctx, assistantID, taskID, watchMark(seenKey, told))
		return
	}
	m.wakeMu.Lock()
	defer m.wakeMu.Unlock()
	current, err := m.store.Task(ctx, assistantID, taskID)
	if err != nil || !taskWatchable(current.Status) {
		return
	}
	if seenNow, toldNow := parseWatchMark(current.LastSeenRevision); seenNow == seenKey || (toldNow == decision.Status && (decision.Status == "waiting_user" || decision.Status == "attention" || decision.Status == "stopped")) {
		_ = m.store.RememberTaskSeen(ctx, assistantID, taskID, watchMark(seenKey, toldNow))
		return
	}
	if err := m.store.UpdateTaskObservation(ctx, assistantID, taskID, decision.Status, view.TurnID, view.Summary, watchMark(seenKey, decision.Status)); err != nil {
		return
	}
	_ = m.updateTaskCardDetails(ctx, assistantID, taskID, decision.Status, view.Summary)
	m.enqueueLocked(ctx, BrainWake{AssistantID: assistantID, TaskKey: current.ID, DedupeKey: decision.Status + "|" + seenKey, Status: decision.Status, Summary: view.Summary, WatchOnly: false})
}

func threadDisappeared(data map[string]any) bool {
	if data == nil {
		return false
	}
	if truthy(data["deleted"]) || truthy(data["archived"]) || truthy(data["missing"]) {
		return true
	}
	if code, _ := data["code"].(string); code == "NOT_FOUND" {
		return true
	}
	if runtime, _ := data["runtime"].(string); runtime == "deleted" || runtime == "archived" || runtime == "missing" {
		return true
	}
	thread, _ := data["thread"].(map[string]any)
	if thread == nil {
		return false
	}
	if truthy(thread["deleted"]) || truthy(thread["archived"]) {
		return true
	}
	runtime, _ := thread["runtime"].(string)
	return runtime == "deleted" || runtime == "archived" || runtime == "missing"
}

func truthy(value any) bool {
	flag, ok := value.(bool)
	return ok && flag
}

func (m *Module) NoteThreadDisappeared(ctx context.Context, deviceID, threadID string) {
	m.noteThreadDisappeared(ctx, deviceID, threadID)
}

func (m *Module) noteThreadDisappeared(ctx context.Context, deviceID, threadID string) {
	if deviceID == "" || threadID == "" {
		return
	}
	// Releasing the watch cancels its context. Keep the cleanup on the module
	// context so one owner cannot abort the others mid-update.
	if m.rootCtx.Err() == nil {
		ctx = m.rootCtx
	}
	seen := map[string]struct{}{}
	if tasks, err := m.store.TasksForThread(ctx, deviceID, threadID); err == nil {
		for _, task := range tasks {
			seen[task.AssistantID] = struct{}{}
		}
	}
	if watches, err := m.store.ActiveThreadWatches(ctx); err == nil {
		for _, watch := range watches {
			if watch.DeviceID == deviceID && watch.ThreadID == threadID {
				seen[watch.AssistantID] = struct{}{}
			}
		}
	}
	if assistants, err := m.store.AssistantsForBotSession(ctx, deviceID, threadID); err == nil {
		for _, assistantID := range assistants {
			seen[assistantID] = struct{}{}
		}
	}
	for assistantID := range seen {
		m.noteSubsessionGone(ctx, assistantID, deviceID, threadID)
	}
}

func (m *Module) noteSubsessionGone(ctx context.Context, assistantID, deviceID, threadID string) {
	result, err := m.store.OrphanSubsession(ctx, assistantID, deviceID, threadID)
	if err != nil || !result.Changed {
		return
	}
	m.releaseWatch(deviceID, threadID, assistantID)
	for _, taskID := range result.TaskIDs {
		_ = m.updateTaskCardDetails(ctx, assistantID, taskID, "orphaned", "子会话已删除或归档")
	}
	summary := "子会话已删除或归档，不能再向它发送 turn。"
	if len(result.TaskIDs) > 0 {
		summary += "相关任务：" + strings.Join(result.TaskIDs, "、") + "。"
	}
	m.enqueueWake(ctx, BrainWake{
		AssistantID: assistantID,
		TaskKey:     "gone:" + deviceID + ":" + threadID,
		DedupeKey:   "subsession_deleted",
		Status:      "subsession_deleted",
		Summary:     summary,
		WatchOnly:   result.WatchOnly,
	})
	m.pumpAssistant(ctx, assistantID)
}

func (m *Module) releaseAssistant(assistantID string) {
	suffix := "\x00" + assistantID
	m.watchMu.Lock()
	var cancels []context.CancelFunc
	for key, cancel := range m.watchCancel {
		if strings.HasSuffix(key, suffix) {
			cancels = append(cancels, cancel)
			delete(m.watchCancel, key)
		}
	}
	m.watchMu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (m *Module) releaseWatch(deviceID, threadID, assistantID string) {
	key := watchMapKey(deviceID, threadID, assistantID)
	m.watchMu.Lock()
	cancel := m.watchCancel[key]
	delete(m.watchCancel, key)
	m.watchMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (m *Module) releaseDevice(deviceID string) {
	prefix := deviceID + "\x00"
	m.watchMu.Lock()
	var cancels []context.CancelFunc
	for key, cancel := range m.watchCancel {
		if strings.HasPrefix(key, prefix) {
			cancels = append(cancels, cancel)
			delete(m.watchCancel, key)
		}
	}
	m.watchMu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (m *Module) stopWatches() {
	m.watchMu.Lock()
	m.watchClosed = true
	cancels := m.watchCancel
	m.watchCancel = map[string]context.CancelFunc{}
	m.watchMu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (m *Module) enqueueWake(ctx context.Context, wake BrainWake) {
	m.wakeMu.Lock()
	defer m.wakeMu.Unlock()
	m.enqueueLocked(ctx, wake)
}

func (m *Module) enqueueLocked(ctx context.Context, wake BrainWake) {
	existing, err := m.store.BrainWakeByTask(ctx, wake.AssistantID, wake.TaskKey)
	if err == nil {
		if existing.DedupeKey == wake.DedupeKey {
			return
		}
		existing.Status = wake.Status
		existing.Summary = wake.Summary
		existing.DedupeKey = wake.DedupeKey
		existing.WatchOnly = wake.WatchOnly
		next := m.nextWakeTimeLocked(ctx, wake.AssistantID, wake.TaskKey, m.now())
		if next.After(existing.NotBefore) {
			existing.NotBefore = next
		}
		_ = m.store.UpsertBrainWake(ctx, existing)
		return
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return
	}
	now := m.now()
	wake.ID = newUUID()
	wake.CreatedAt = now
	wake.NotBefore = m.nextWakeTimeLocked(ctx, wake.AssistantID, wake.TaskKey, now)
	if m.pendingTurn(wake.AssistantID).turnID != "" {
		m.noteWakeSkip(wake.AssistantID, wake.TaskKey, "turn_busy")
		_ = m.store.UpsertBrainWake(ctx, wake)
		return
	}
	if wake.NotBefore.After(now) {
		m.noteWakeSkip(wake.AssistantID, wake.TaskKey, "backoff")
		_ = m.store.UpsertBrainWake(ctx, wake)
		return
	}
	m.deliverWakeLocked(ctx, wake)
}

func (m *Module) nextWakeTimeLocked(ctx context.Context, assistantID, taskKey string, now time.Time) time.Time {
	notBefore := now
	count, oldest, lastTask, err := m.store.WakeStats(ctx, assistantID, taskKey, now.Add(-wakeGlobalWindow))
	if err != nil {
		return notBefore
	}
	interval := wakeTaskInterval
	if messageID, ok := strings.CutPrefix(taskKey, "queue:inbox:"); ok {
		inInbox, attached, assignErr := m.store.InboxAssignment(ctx, assistantID, messageID)
		if assignErr == nil && inInbox && !attached {
			interval = queueRetryBackoff
		}
	}
	if !lastTask.IsZero() {
		earliest := lastTask.Add(interval)
		if earliest.After(notBefore) {
			notBefore = earliest
		}
	}
	if count >= wakeGlobalLimit && !oldest.IsZero() {
		earliest := oldest.Add(wakeGlobalWindow)
		if earliest.After(notBefore) {
			notBefore = earliest
		}
	}
	return notBefore
}

func (m *Module) pumpAll(ctx context.Context) {
	ids, err := m.store.AssistantsWithWakes(ctx)
	if err != nil {
		return
	}
	for _, id := range ids {
		m.pumpAssistant(ctx, id)
	}
}

func (m *Module) pumpAssistant(ctx context.Context, assistantID string) {
	m.wakeMu.Lock()
	defer m.wakeMu.Unlock()
	m.pumpLocked(ctx, assistantID)
}

func (m *Module) pumpLocked(ctx context.Context, assistantID string) {
	for {
		if m.pendingTurn(assistantID).turnID != "" {
			return
		}
		wakes, err := m.store.BrainWakes(ctx, assistantID)
		if err != nil || len(wakes) == 0 {
			return
		}
		now := m.now()
		index := -1
		for i := range wakes {
			if wakes[i].NotBefore.After(now) {
				continue
			}
			next := m.nextWakeTimeLocked(ctx, assistantID, wakes[i].TaskKey, now)
			if next.After(now) {
				if next.After(wakes[i].NotBefore) {
					wakes[i].NotBefore = next
					_ = m.store.UpsertBrainWake(ctx, wakes[i])
					m.noteWakeSkip(assistantID, wakes[i].TaskKey, "rate_limit")
				}
				continue
			}
			index = i
			break
		}
		if index < 0 {
			return
		}
		m.deliverWakeLocked(ctx, wakes[index])
		if m.pendingTurn(assistantID).turnID != "" {
			return
		}
	}
}

func (m *Module) deliverWakeLocked(ctx context.Context, wake BrainWake) {
	if wake.ID != "" {
		_ = m.store.DeleteBrainWake(ctx, wake.ID)
	}
	_ = m.store.RecordWake(ctx, wake.AssistantID, wake.TaskKey, m.now())
	assistant, err := m.store.Assistant(ctx, wake.AssistantID)
	if err != nil || assistant.Status == "deleted" {
		return
	}
	if upgraded, upgradeErr := m.ensureCurrentToolset(ctx, assistant); upgradeErr == nil {
		assistant = upgraded
	}
	card, _ := json.Marshal(map[string]any{"proactive": true, "source": "task_event", "taskId": wake.TaskKey, "status": wake.Status, "watchOnly": wake.WatchOnly})
	text := taskEventTemplate(wake.Status, wake.Summary)
	if !m.brainReady(assistant.BrainDeviceID) || assistant.BrainThreadID == "" {
		_ = m.store.AppendMessage(ctx, Message{ID: newUUID(), AssistantID: assistant.ID, Role: "assistant", Kind: "text", Text: text, CardJSON: string(card)})
		return
	}
	responseID := "task-event-" + wake.ID
	if wake.ID == "" {
		responseID = newUUID()
	}
	started := m.now()
	openTasks, _ := m.store.RecentOpenBotTasks(ctx, assistant.ID, 10)
	request := mustJSON(map[string]any{
		"assistantId": assistant.ID, "threadId": assistant.BrainThreadID, "clientMessageId": responseID,
		"text": taskEventEnvelope(assistant, wake, openTasks, m.taskSnippets(ctx, assistant.ID, openTasks)), "model": assistant.BrainModel, "effort": assistant.BrainReasoning,
	})
	result := m.startBrainTurnTriggered(ctx, assistant, request, "event")
	if result.Error != nil || result.Outcome != "accepted" {
		_ = m.store.UpsertMessage(ctx, Message{ID: responseID, AssistantID: assistant.ID, Role: "assistant", Kind: "text", Text: text, CardJSON: string(card)})
		return
	}
	turnID, _ := result.Data["turnId"].(string)
	m.setPending(assistant, turnID, responseID, responseID, started, false, "")
	m.noteQueueTarget(assistant.ID, wake.TaskKey)
	m.markPendingTaskEvent(assistant.ID, string(card))
}

func (m *Module) noteQueueTarget(assistantID, taskKey string) {
	if !strings.HasPrefix(taskKey, "queue:") {
		return
	}
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	pending, ok := m.pending[assistantID]
	if !ok {
		return
	}
	pending.queueKey = taskKey
	m.pending[assistantID] = pending
}

func taskEventTemplate(status, summary string) string {
	lead := "任务状态有更新"
	switch status {
	case "review":
		lead = "这一轮已经结束"
	case "waiting_user":
		lead = "任务被卡住了，需要你处理"
	case "attention":
		lead = "任务失败或中断了"
	case "subsession_deleted":
		lead = "子会话已经不在了"
	}
	summary = strings.TrimSpace(summary)
	if summary == "" || summary == lead {
		return lead + "。"
	}
	return lead + "。" + summary
}

func taskEventEnvelope(assistant Assistant, wake BrainWake, openTasks []BotTask, snippets map[string]string) string {
	if wake.Status == "bot_queue" {
		return "PUCK_ENVELOPE assistant=" + assistant.ID + " nonce=" + newUUID() + " kind=bot_queue\n" +
			"这是还没做完的工作或尚未归入任务的用户消息，不是新的执行结果。需要时自己读全文。\n" +
			wake.Summary + "\n" +
			openBotTaskContext(openTasks, snippets) + "\n" +
			"若这是尚未归属的用户消息，它会在这轮结束时记成任务。如果它延续上面的未完成任务，把那个 taskId 传给 save_bot_task。两条任务要合并才用 merge_bot_tasks。没有新的话要说就只回复 PUCK_SILENT。\n" +
			envelopeContext(assistant)
	}
	mode := "execution"
	if wake.WatchOnly {
		mode = "watch_only"
	}
	cleanup := "若需要关掉你启动的定时或周期检查，只能用已经声明的 send_turn。不要调用尚未声明的工具，也不要删除聊天记录。"
	if wake.Status == "subsession_deleted" {
		cleanup = "这个子会话已经删除或归档。不要再 send_turn。关掉你在本会话为它创建的定时或 automation。若工具集里有 finish_task，归档相关任务。"
	} else if wake.WatchOnly {
		cleanup = "watch_only 时不要调用 send_turn、start_task 或 finish_task。只读监听由 Puck 在通知后关闭，自定义上下文不能改写这套订阅。"
	} else if assistant.ToolsetVersion >= finishTaskToolset {
		cleanup = "若用户交代的事已经有明确结果，先用 send_turn 取消该执行会话里由你启动的定时或周期任务，再调用 finish_task。review 只表示这一轮结束；工作还没做完时不要 finish_task。不要删除聊天记录。"
	}
	speak := "这是一个新的结果。请用一句简短中文告诉用户，不要重复已经说过的话。"
	if assistant.ToolsetVersion >= sendMessageToolset {
		speak = "这是一个新的结果。若值得告诉用户，调用 send_message 说一句，不要重复已经说过的话。多个子会话会分别唤醒你，由你汇总。不要把本轮正文当作已经发给用户。"
	}
	return "PUCK_ENVELOPE assistant=" + assistant.ID + " nonce=" + newUUID() + " kind=task_event\n" +
		"这是系统产生的任务事件，不是用户新消息。不要执行用户没有要求的新动作。\n" +
		"taskId=" + wake.TaskKey + "\n" +
		"status=" + wake.Status + "\n" +
		"watch=" + mode + "\n" +
		"summary=" + wake.Summary + "\n" +
		speak + "watch_only 时不要调用 send_turn 或 start_task。若这没有新的结果，最终回复只写 PUCK_SILENT，不要写别的字，也不要调用 send_message。review 只表示这一轮结束，不等于任务完成。\n" +
		cleanup + "\n" + envelopeContext(assistant)
}
