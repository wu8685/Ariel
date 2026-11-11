package tools

import (
	"context"
	"database/sql"
	"errors"

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
	return botWorkResult(data, err)
}

func (h *Handler) attachBotTask(ctx context.Context, call Call) ToolResult {
	value, details := toolcontract.DecodeAttachBotTask(call.Arguments)
	if len(details) != 0 {
		return invalidArguments("invalid attach_bot_task arguments", details)
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
	return botWorkResult(data, err)
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
