package tools

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/wu8685/Ariel/internal/puck/toolcontract"
)

type BotTaskSession struct {
	Kind, RefID, DeviceID, ThreadID string
}

type BotWork interface {
	ListBotTasks(context.Context, string, toolcontract.ListBotTasksRequest) (map[string]any, error)
	SearchMessages(context.Context, string, toolcontract.SearchMessagesRequest) (map[string]any, error)
	SaveBotTask(context.Context, string, toolcontract.SaveBotTaskRequest) (map[string]any, error)
	AttachBotTask(context.Context, string, toolcontract.AttachBotTaskRequest) (map[string]any, error)
	MergeBotTasks(context.Context, string, toolcontract.MergeBotTasksRequest) (map[string]any, error)
}

func (h *Handler) listBotTasks(ctx context.Context, call Call) ToolResult {
	value, details := toolcontract.DecodeListBotTasks(call.Arguments)
	if len(details) != 0 {
		return invalidArguments("invalid list_bot_tasks arguments", details)
	}
	work, result := h.botWork()
	if result.Code != "" {
		return result
	}
	data, err := work.ListBotTasks(ctx, call.AssistantID, value)
	return botWorkResult(data, err)
}

func (h *Handler) searchMessages(ctx context.Context, call Call) ToolResult {
	value, details := toolcontract.DecodeSearchMessages(call.Arguments)
	if len(details) != 0 {
		return invalidArguments("invalid search_messages arguments", details)
	}
	work, result := h.botWork()
	if result.Code != "" {
		return result
	}
	data, err := work.SearchMessages(ctx, call.AssistantID, value)
	return botWorkResult(data, err)
}

func (h *Handler) saveBotTask(ctx context.Context, call Call) ToolResult {
	value, details := toolcontract.DecodeSaveBotTask(call.Arguments)
	if len(details) != 0 {
		return invalidArguments("invalid save_bot_task arguments", details)
	}
	work, result := h.botWork()
	if result.Code != "" {
		return result
	}
	data, err := work.SaveBotTask(ctx, call.AssistantID, value)
	result = botWorkResult(data, err)
	if !result.Success || value.ID == "" || strings.TrimSpace(call.SourceMessageID) == "" {
		return result
	}
	attached, attachErr := work.AttachBotTask(ctx, call.AssistantID, toolcontract.AttachBotTaskRequest{TaskID: value.ID, MessageIDs: []string{call.SourceMessageID}})
	if attachErr != nil {
		return botWorkResult(nil, attachErr)
	}
	result = botWorkResult(attached, nil)
	result.Message = "已更新该任务，并挂上了当前用户消息。"
	return result
}

func (h *Handler) attachBotTask(ctx context.Context, call Call) ToolResult {
	value, details := toolcontract.DecodeAttachBotTask(call.Arguments)
	if len(details) != 0 {
		return invalidArguments("invalid attach_bot_task arguments", details)
	}
	if len(value.MessageIDs) == 0 && len(value.Sessions) == 0 {
		if strings.TrimSpace(call.SourceMessageID) == "" {
			return invalidArguments("", []toolcontract.FieldError{{Field: "messageIds", Reason: "no current user message is in this turn", Expected: "omit messageIds to attach the current user message, or pass messageIds"}})
		}
		value.MessageIDs = []string{call.SourceMessageID}
	}
	work, result := h.botWork()
	if result.Code != "" {
		return result
	}
	data, err := work.AttachBotTask(ctx, call.AssistantID, value)
	return botWorkResult(data, err)
}

func (h *Handler) mergeBotTasks(ctx context.Context, call Call) ToolResult {
	value, details := toolcontract.DecodeMergeBotTasks(call.Arguments)
	if len(details) != 0 {
		return invalidArguments("invalid merge_bot_tasks arguments", details)
	}
	work, result := h.botWork()
	if result.Code != "" {
		return result
	}
	data, err := work.MergeBotTasks(ctx, call.AssistantID, value)
	result = botWorkResult(data, err)
	if result.Success {
		result.Message = "已把来源任务并入目标，并复制了两边已经挂上的消息和子会话。当前这条用户消息没有挂上。要挂上，调用 attach_bot_task，只传 taskId。"
	}
	return result
}

func (h *Handler) botWork() (BotWork, ToolResult) {
	work, ok := h.backend.(BotWork)
	if !ok {
		return nil, toolError("PUCK_UNAVAILABLE", "bot task store is unavailable")
	}
	return work, ToolResult{}
}

func botWorkResult(data map[string]any, err error) ToolResult {
	if err == nil {
		return ToolResult{Success: true, Data: data}
	}
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrNotFound) {
		return toolError("PUCK_ASSISTANT_NOT_FOUND", "bot task not found")
	}
	return toolError("INVALID_ARGUMENT", err.Error())
}
