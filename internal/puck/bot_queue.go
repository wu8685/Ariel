package puck

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const queueRetryBackoff = 20 * time.Second

func unassignedInboxSummary(messageID, text string) string {
	return "unassigned inbox messageId=" + messageID + "\n这条用户消息尚未归属。必须 attach_bot_task 到已有任务、merge_bot_tasks，或 save_bot_task 新建后再 attach。不要把它留在 inbox。\n" + text
}

func (m *Module) scheduleBotQueue(ctx context.Context, assistantID, skipMessageID string) {
	if assistantID == "" || m.pendingTurn(assistantID).turnID != "" {
		return
	}
	wakes, err := m.store.BrainWakes(ctx, assistantID)
	if err != nil {
		return
	}
	for _, wake := range wakes {
		if strings.HasPrefix(wake.TaskKey, "queue:") {
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
	if err != nil || taskID == "" {
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
