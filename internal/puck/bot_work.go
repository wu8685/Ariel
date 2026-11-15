package puck

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/wu8685/Ariel/internal/puck/toolcontract"
)

func (b toolBackend) ListBotTasks(ctx context.Context, assistantID string, request toolcontract.ListBotTasksRequest) (map[string]any, error) {
	offset, err := pageOffset(request.Cursor)
	if err != nil {
		return nil, err
	}
	page, err := b.module.store.ListBotTasks(ctx, assistantID, request.Query, request.Status, offset, request.Limit)
	if err != nil {
		return nil, err
	}
	tasks := make([]map[string]any, 0, len(page.Tasks))
	for _, task := range page.Tasks {
		tasks = append(tasks, botTaskChoiceJSON(task))
	}
	data := map[string]any{"tasks": tasks}
	if page.NextCursor != "" {
		data["nextCursor"] = page.NextCursor
	}
	return data, nil
}

func (b toolBackend) SearchMessages(ctx context.Context, assistantID string, request toolcontract.SearchMessagesRequest) (map[string]any, error) {
	offset, err := pageOffset(request.Cursor)
	if err != nil {
		return nil, err
	}
	page, err := b.module.store.SearchMessages(ctx, assistantID, request.Query, offset, request.Limit)
	if err != nil {
		return nil, err
	}
	messages := make([]map[string]any, 0, len(page.Messages))
	for _, message := range page.Messages {
		messages = append(messages, map[string]any{"id": message.ID, "role": message.Role, "kind": message.Kind, "text": message.Text, "createdAt": message.CreatedAt})
	}
	data := map[string]any{"messages": messages}
	if page.NextCursor != "" {
		data["nextCursor"] = page.NextCursor
	}
	return data, nil
}

func (b toolBackend) GetBotTask(ctx context.Context, assistantID, taskID string) (map[string]any, error) {
	task, err := b.module.store.BotTask(ctx, assistantID, taskID)
	if err != nil {
		return nil, err
	}
	payload := botTaskJSON(task, "")
	messages := make([]map[string]any, 0, len(task.MessageIDs))
	for _, id := range task.MessageIDs {
		message, err := b.module.store.Message(ctx, assistantID, id)
		if err != nil {
			messages = append(messages, map[string]any{"id": id})
			continue
		}
		messages = append(messages, map[string]any{"id": message.ID, "role": message.Role, "text": message.Text, "createdAt": message.CreatedAt})
	}
	payload["messages"] = messages
	return map[string]any{"task": payload}, nil
}

func (b toolBackend) SaveBotTask(ctx context.Context, assistantID string, request toolcontract.SaveBotTaskRequest) (map[string]any, error) {
	task, err := b.module.store.SaveBotTask(ctx, BotTask{ID: request.ID, AssistantID: assistantID, Title: request.Title, Goal: request.Goal, Description: request.Description, DescriptionSet: request.DescriptionSet, Status: request.Status})
	if err != nil {
		return nil, err
	}
	return map[string]any{"task": botTaskJSON(task, "")}, nil
}

func (b toolBackend) AttachBotTask(ctx context.Context, assistantID string, request toolcontract.AttachBotTaskRequest) (map[string]any, error) {
	sessions := make([]BotTaskSession, 0, len(request.Sessions))
	for _, session := range request.Sessions {
		sessions = append(sessions, BotTaskSession{Kind: session.Kind, RefID: session.ID, DeviceID: session.DeviceID, ThreadID: session.ThreadID})
	}
	task, err := b.module.store.AttachBotTask(ctx, assistantID, request.TaskID, request.MessageIDs, sessions)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, err
	}
	return map[string]any{"task": botTaskJSON(task, "")}, nil
}

func (b toolBackend) MergeBotTasks(ctx context.Context, assistantID string, request toolcontract.MergeBotTasksRequest) (map[string]any, error) {
	task, err := b.module.store.MergeBotTasks(ctx, assistantID, request.FromID, request.IntoID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"task": botTaskJSON(task, "")}, nil
}

func botTaskChoiceJSON(task BotTask) map[string]any {
	return map[string]any{"id": task.ID, "title": task.Title, "description": task.Description, "status": task.Status, "updatedAt": task.UpdatedAt}
}

func storedSnippet(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	runes := []rune(text)
	if len(runes) > 160 {
		return string(runes[:160])
	}
	return text
}

func botTaskJSON(task BotTask, snippet string) map[string]any {
	messages := task.MessageIDs
	if messages == nil {
		messages = []string{}
	}
	sessions := make([]map[string]any, 0, len(task.Sessions))
	for _, session := range task.Sessions {
		sessions = append(sessions, map[string]any{"kind": session.Kind, "id": session.RefID, "deviceId": session.DeviceID, "threadId": session.ThreadID})
	}
	payload := map[string]any{
		"id": task.ID, "title": task.Title, "goal": task.Goal, "description": task.Description, "status": task.Status, "mergedInto": task.MergedInto,
		"messageIds": messages, "sessions": sessions, "createdAt": task.CreatedAt, "updatedAt": task.UpdatedAt,
	}
	if strings.TrimSpace(snippet) != "" {
		payload["lastUserMessage"] = storedSnippet(snippet)
	}
	return payload
}
