package puck

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	pucktools "github.com/wu8685/Ariel/internal/puck/tools"
)

func sourceFromPending(ctx context.Context, m *Module, assistantID string, pending pendingBrainTurn) string {
	if id, ok := strings.CutPrefix(pending.queueKey, "queue:inbox:"); ok && id != "" {
		return id
	}
	if pending.messageID == "" {
		return ""
	}
	message, err := m.store.Message(ctx, assistantID, pending.messageID)
	if err != nil || message.Role != "user" {
		return ""
	}
	return pending.messageID
}

func (m *Module) currentSourceMessage(ctx context.Context, assistantID string) string {
	pending := m.pendingTurn(assistantID)
	if id, ok := strings.CutPrefix(pending.queueKey, "queue:inbox:"); ok && id != "" {
		return id
	}
	if pending.messageID == "" {
		return ""
	}
	message, err := m.store.Message(ctx, assistantID, pending.messageID)
	if err != nil || message.Role != "user" {
		return ""
	}
	return pending.messageID
}

func (m *Module) recordBookkeeping(ctx context.Context, assistantID, tool string, args json.RawMessage, result pucktools.ToolResult) {
	if !result.Success {
		return
	}
	sourceID := m.currentSourceMessage(ctx, assistantID)
	if sourceID == "" {
		return
	}
	switch tool {
	case "save_bot_task":
		task, _ := result.Data["task"].(map[string]any)
		taskID, _ := task["id"].(string)
		m.attachSource(ctx, assistantID, taskID, sourceID, nil)
	case "start_task", "send_turn", "finish_task":
		m.attachExecution(ctx, assistantID, sourceID, args, result)
	}
}

func (m *Module) attachExecution(ctx context.Context, assistantID, sourceID string, args json.RawMessage, result pucktools.ToolResult) {
	var input struct {
		TaskID   string `json:"taskId"`
		DeviceID string `json:"deviceId"`
		ThreadID string `json:"threadId"`
		Goal     string `json:"goal"`
	}
	_ = json.Unmarshal(args, &input)
	if input.TaskID == "" {
		input.TaskID, _ = result.Data["taskId"].(string)
	}
	if input.DeviceID == "" {
		input.DeviceID, _ = result.Data["deviceId"].(string)
	}
	if input.ThreadID == "" {
		input.ThreadID, _ = result.Data["threadId"].(string)
	}
	taskID, err := m.botTaskIDForSource(ctx, assistantID, sourceID)
	if err != nil || taskID == "" {
		title := strings.TrimSpace(input.Goal)
		if title == "" {
			title = "未命名任务"
		}
		saved, saveErr := m.store.SaveBotTask(ctx, BotTask{AssistantID: assistantID, Title: title, Goal: title, Status: "accepted"})
		if saveErr != nil {
			return
		}
		taskID = saved.ID
	}
	var sessions []BotTaskSession
	if input.TaskID != "" {
		if _, err := m.store.Task(ctx, assistantID, input.TaskID); err == nil {
			sessions = []BotTaskSession{{Kind: "task", RefID: input.TaskID}}
		}
	}
	m.attachSource(ctx, assistantID, taskID, sourceID, sessions)
}

func (m *Module) botTaskIDForSource(ctx context.Context, assistantID, messageID string) (string, error) {
	var id string
	err := m.store.db.QueryRowContext(ctx, `SELECT bot_task_id FROM bot_task_messages WHERE assistant_id=? AND message_id=? ORDER BY created_at LIMIT 1`, assistantID, messageID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func (m *Module) attachSource(ctx context.Context, assistantID, taskID, messageID string, sessions []BotTaskSession) {
	if taskID == "" || messageID == "" {
		return
	}
	_, _ = m.store.AttachBotTask(ctx, assistantID, taskID, []string{messageID}, sessions)
}

func (m *Module) ensureSourceAssigned(ctx context.Context, assistantID, sourceID, reply string) string {
	if sourceID == "" {
		return ""
	}
	inInbox, attached, err := m.store.InboxAssignment(ctx, assistantID, sourceID)
	if err != nil || !inInbox || attached {
		return ""
	}
	title := taskTitleFromReply(reply)
	saved, err := m.store.SaveBotTask(ctx, BotTask{AssistantID: assistantID, Title: title, Goal: title, Status: "accepted"})
	if err != nil {
		return ""
	}
	m.attachSource(ctx, assistantID, saved.ID, sourceID, nil)
	return saved.ID
}

func taskTitleFromReply(reply string) string {
	reply = strings.TrimSpace(reply)
	switch reply {
	case "", "正在处理…", "收到，我正在处理。", "正在根据任务状态写一条说明…", "完成。", "PUCK_SILENT":
		return "未命名任务"
	}
	line := reply
	if index := strings.IndexAny(reply, "\r\n"); index >= 0 {
		line = reply[:index]
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return "未命名任务"
	}
	if utf8.RuneCountInString(line) > 80 {
		runes := []rune(line)
		line = string(runes[:80])
	}
	return line
}

func storedSnippet(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	runes := []rune(text)
	if len(runes) > 160 {
		return string(runes[:160])
	}
	return text
}
