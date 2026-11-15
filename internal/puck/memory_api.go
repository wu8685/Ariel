package puck

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/wu8685/Ariel/internal/relay"
)

func (m *Module) memoryList(ctx context.Context, raw json.RawMessage) relay.PuckResponse {
	var input struct {
		AssistantID string `json:"assistantId"`
		Scope       string `json:"scope"`
		Kind        string `json:"kind"`
		Cursor      string `json:"cursor"`
		Limit       int    `json:"limit"`
	}
	if json.Unmarshal(raw, &input) != nil || strings.TrimSpace(input.AssistantID) == "" {
		return moduleError("INVALID_ARGUMENT", "assistantId is required")
	}
	if _, err := m.store.Assistant(ctx, input.AssistantID); err != nil {
		return notFound()
	}
	if input.Scope == "" {
		input.Scope = "all"
	}
	if input.Kind == "" {
		input.Kind = "any"
	}
	if input.Scope != "bot" && input.Scope != "shared" && input.Scope != "all" {
		return moduleError("INVALID_ARGUMENT", "scope must be bot, shared, or all")
	}
	if input.Kind != "profile" && input.Kind != "journal" && input.Kind != "any" {
		return moduleError("INVALID_ARGUMENT", "kind must be profile, journal, or any")
	}
	offset, err := pageOffset(input.Cursor)
	if err != nil {
		return moduleError("INVALID_ARGUMENT", "cursor is invalid")
	}
	page, err := m.store.ListMemories(ctx, input.AssistantID, input.Scope, input.Kind, offset, input.Limit)
	if err != nil {
		if failure, ok := memoryAPIError(err); ok {
			return failure
		}
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	entries := make([]map[string]any, 0, len(page.Entries))
	for _, entry := range page.Entries {
		entries = append(entries, memoryEntryJSON(entry))
	}
	data := map[string]any{"entries": entries}
	if page.NextCursor != "" {
		data["nextCursor"] = page.NextCursor
	}
	return accepted(data)
}

func (m *Module) memoryDelete(ctx context.Context, raw json.RawMessage) relay.PuckResponse {
	var input struct {
		AssistantID string `json:"assistantId"`
		MemoryID    string `json:"memoryId"`
	}
	if json.Unmarshal(raw, &input) != nil || strings.TrimSpace(input.AssistantID) == "" || strings.TrimSpace(input.MemoryID) == "" {
		return moduleError("INVALID_ARGUMENT", "assistantId and memoryId are required")
	}
	if _, err := m.store.Assistant(ctx, input.AssistantID); err != nil {
		return notFound()
	}
	entry, err := m.store.DeleteVisibleMemory(ctx, input.AssistantID, input.MemoryID)
	if err != nil {
		if failure, ok := memoryAPIError(err); ok {
			return failure
		}
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	return accepted(map[string]any{"entry": memoryEntryJSON(entry)})
}

func memoryEntryJSON(entry Memory) map[string]any {
	name := entry.DisplayName
	if name == "" {
		name = entry.WriterName
	}
	return map[string]any{
		"id": entry.ID, "scope": entry.Scope, "kind": entry.Kind, "text": entry.Text,
		"writerName": name, "createdAt": entry.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func memoryAPIError(err error) (relay.PuckResponse, bool) {
	var failure *MemoryFailure
	if errors.As(err, &failure) {
		return moduleError(failure.Code, failure.Message), true
	}
	return relay.PuckResponse{}, false
}
