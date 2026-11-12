package puck

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const queueRetryBackoff = 20 * time.Second

func (m *Module) noteToolCall(assistantID, tool string, success bool, code string) {
	if m == nil || assistantID == "" || tool == "" {
		return
	}
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	calls := append(m.toolCalls[assistantID], observedToolCall{Tool: tool, Success: success, Code: code})
	if len(calls) > 40 {
		calls = calls[len(calls)-40:]
	}
	m.toolCalls[assistantID] = calls
}

func (m *Module) recentToolCalls(assistantID string) []map[string]any {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	calls := m.toolCalls[assistantID]
	out := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		item := map[string]any{"tool": call.Tool, "success": call.Success}
		if call.Code != "" {
			item["code"] = call.Code
		}
		out = append(out, item)
	}
	return out
}

func (m *Module) noteWakeSkip(assistantID, taskKey, reason string) {
	if m == nil || assistantID == "" || reason == "" {
		return
	}
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	skips := append(m.wakeSkips[assistantID], observedWakeSkip{TaskKey: taskKey, Reason: reason, At: m.now()})
	if len(skips) > 20 {
		skips = skips[len(skips)-20:]
	}
	m.wakeSkips[assistantID] = skips
}

func (m *Module) recentWakeSkips(assistantID string) []map[string]any {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	skips := m.wakeSkips[assistantID]
	out := make([]map[string]any, 0, len(skips))
	for _, skip := range skips {
		out = append(out, map[string]any{"taskKey": skip.TaskKey, "reason": skip.Reason, "at": skip.At.UTC().Format(time.RFC3339Nano)})
	}
	return out
}

func unassignedInboxSummary(messageID, text string) string {
	return "unassigned inbox messageId=" + messageID + "\n这条用户消息尚未归属。\n" + text
}

func (m *Module) scheduleBotQueue(ctx context.Context, assistantID, skipMessageID, skipTaskID string) {
	if assistantID == "" {
		return
	}
	if m.pendingTurn(assistantID).turnID != "" {
		m.noteWakeSkip(assistantID, "", "turn_busy")
		return
	}
	wakes, err := m.store.BrainWakes(ctx, assistantID)
	if err != nil {
		return
	}
	for _, wake := range wakes {
		if strings.HasPrefix(wake.TaskKey, "queue:") {
			m.noteWakeSkip(assistantID, wake.TaskKey, "queue_wake_pending")
			return
		}
	}
	messageID, err := m.store.OldestInbox(ctx, assistantID, skipMessageID)
	if err != nil {
		return
	}
	if messageID != "" {
		message, err := m.store.Message(ctx, assistantID, messageID)
		if err != nil {
			return
		}
		m.enqueueWake(ctx, BrainWake{
			AssistantID: assistantID,
			TaskKey:     "queue:inbox:" + messageID,
			DedupeKey:   "inbox:" + messageID,
			Status:      "bot_queue",
			Summary:     unassignedInboxSummary(messageID, message.Text),
		})
		return
	}
	taskID, err := m.store.OldestOpenBotTaskID(ctx, assistantID)
	if err != nil || taskID == "" || taskID == skipTaskID {
		return
	}
	task, err := m.store.BotTask(ctx, assistantID, taskID)
	if err != nil {
		return
	}
	m.enqueueWake(ctx, BrainWake{
		AssistantID: assistantID,
		TaskKey:     "queue:task:" + taskID,
		DedupeKey:   "task:" + taskID,
		Status:      "bot_queue",
		Summary:     "botTask id=" + task.ID + "\ntitle=" + task.Title + "\ngoal=" + task.Goal + "\nstatus=" + task.Status,
	})
}

// rearmUnassignedInbox schedules one follow-up when a queue wake ended and the
// inbox message is still unassigned. The first wake is immediate; this retry
// waits queueRetryBackoff instead of the five-minute repeat interval.
func (m *Module) rearmUnassignedInbox(ctx context.Context, assistantID, queueKey string) {
	messageID, ok := strings.CutPrefix(queueKey, "queue:inbox:")
	if !ok || messageID == "" || assistantID == "" {
		return
	}
	inInbox, attached, err := m.store.InboxAssignment(ctx, assistantID, messageID)
	if err != nil || !inInbox || attached {
		return
	}
	message, err := m.store.Message(ctx, assistantID, messageID)
	if err != nil {
		return
	}
	m.wakeMu.Lock()
	defer m.wakeMu.Unlock()
	if m.pendingTurn(assistantID).turnID != "" {
		return
	}
	when := m.now().Add(queueRetryBackoff)
	taskKey := "queue:inbox:" + messageID
	existing, err := m.store.BrainWakeByTask(ctx, assistantID, taskKey)
	if err == nil {
		if existing.NotBefore.After(when) {
			existing.NotBefore = when
			existing.Summary = unassignedInboxSummary(messageID, message.Text)
			existing.Status = "bot_queue"
			_ = m.store.UpsertBrainWake(ctx, existing)
		}
		return
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return
	}
	wake := BrainWake{
		ID: newUUID(), AssistantID: assistantID, TaskKey: taskKey, DedupeKey: "inbox:" + messageID,
		Status: "bot_queue", Summary: unassignedInboxSummary(messageID, message.Text),
		CreatedAt: m.now(), NotBefore: when,
	}
	_ = m.store.UpsertBrainWake(ctx, wake)
}
