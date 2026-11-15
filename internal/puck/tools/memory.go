package tools

import (
	"context"
	"database/sql"
	"errors"

	"github.com/wu8685/Ariel/internal/puck/toolcontract"
)

type MemoryWork interface {
	UpdateMemory(context.Context, string, toolcontract.UpdateMemoryRequest) (map[string]any, error)
	RecallMemory(context.Context, string, toolcontract.RecallMemoryRequest) (map[string]any, error)
}

func (h *Handler) updateMemory(ctx context.Context, call Call) ToolResult {
	value, details := toolcontract.DecodeUpdateMemory(call.Arguments)
	if len(details) != 0 {
		return invalidArguments("invalid update_memory arguments", details)
	}
	work, result := h.memoryWork()
	if result.Code != "" {
		return result
	}
	data, err := work.UpdateMemory(ctx, call.AssistantID, value)
	return memoryResult(data, err)
}

func (h *Handler) recallMemory(ctx context.Context, call Call) ToolResult {
	value, details := toolcontract.DecodeRecallMemory(call.Arguments)
	if len(details) != 0 {
		return invalidArguments("invalid recall_memory arguments", details)
	}
	work, result := h.memoryWork()
	if result.Code != "" {
		return result
	}
	data, err := work.RecallMemory(ctx, call.AssistantID, value)
	return memoryResult(data, err)
}

func (h *Handler) memoryWork() (MemoryWork, ToolResult) {
	work, ok := h.backend.(MemoryWork)
	if !ok {
		return nil, toolError("PUCK_UNAVAILABLE", "memory store is unavailable")
	}
	return work, ToolResult{}
}

func memoryResult(data map[string]any, err error) ToolResult {
	if err == nil {
		return ToolResult{Success: true, Data: data}
	}
	if coded, ok := err.(memoryCoder); ok {
		code, message, text := coded.MemoryCode()
		result := toolError(code, message)
		if text != "" {
			result.Data = map[string]any{"text": text}
		}
		return result
	}
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrNotFound) {
		return toolError("PUCK_ASSISTANT_NOT_FOUND", "assistant not found")
	}
	return toolError("INVALID_ARGUMENT", err.Error())
}

type memoryCoder interface {
	MemoryCode() (string, string, string)
}
