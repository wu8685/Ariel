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
	if m.pendingTurn(wake.AssistantID).turnID != "" || wake.NotBefore.After(now) {
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
	if !lastTask.IsZero() {
		earliest := lastTask.Add(wakeTaskInterval)
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
				wakes[i].NotBefore = next
				_ = m.store.UpsertBrainWake(ctx, wakes[i])
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
	request := mustJSON(map[string]any{
		"assistantId": assistant.ID, "threadId": assistant.BrainThreadID, "clientMessageId": responseID,
		"text": taskEventEnvelope(assistant, wake), "model": assistant.BrainModel, "effort": assistant.BrainReasoning,
	})
	result := m.startBrainTurnTriggered(ctx, assistant, request, "event")
	if result.Error != nil || result.Outcome != "accepted" {
		_ = m.store.UpsertMessage(ctx, Message{ID: responseID, AssistantID: assistant.ID, Role: "assistant", Kind: "text", Text: text, CardJSON: string(card)})
		return
	}
	turnID, _ := result.Data["turnId"].(string)
	m.setPending(assistant, turnID, responseID, responseID, started, false, "")
	m.markPendingTaskEvent(assistant.ID, string(card))
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
	}
	summary = strings.TrimSpace(summary)
	if summary == "" || summary == lead {
		return lead + "。"
	}
	return lead + "。" + summary
}

func taskEventEnvelope(assistant Assistant, wake BrainWake) string {
	mode := "execution"
	if wake.WatchOnly {
		mode = "watch_only"
	}
	return "PUCK_ENVELOPE assistant=" + assistant.ID + " nonce=" + newUUID() + " kind=task_event\n" +
		"这是系统产生的任务事件，不是用户新消息。不要执行用户没有要求的新动作。\n" +
		"taskId=" + wake.TaskKey + "\n" +
		"status=" + wake.Status + "\n" +
		"watch=" + mode + "\n" +
		"summary=" + wake.Summary + "\n" +
		"这是一个新的结果。请用一句简短中文告诉用户，不要重复已经说过的话。watch_only 时不要调用 send_turn 或 start_task。若这没有新的结果，最终回复只写 PUCK_SILENT，不要写别的字；那条回复不会出现在对话里。review 只表示这一轮结束，不等于任务完成。"
}
