package puck

import (
	"context"
	"strings"
)

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
			Summary:     "inbox messageId=" + messageID + "\n" + message.Text,
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
