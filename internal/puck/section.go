package puck

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/wu8685/Ariel/internal/relay"
)

func (m *Module) sectionList(ctx context.Context) relay.PuckResponse {
	view, err := m.store.Sidebar(ctx)
	if err != nil {
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	return accepted(sidebarData(view))
}

func (m *Module) sectionCreate(ctx context.Context, raw json.RawMessage) relay.PuckResponse {
	var input struct {
		Name string `json:"name"`
	}
	name := ""
	if json.Unmarshal(raw, &input) == nil {
		name = strings.TrimSpace(input.Name)
	}
	if name == "" || utf8.RuneCountInString(name) > 24 {
		return moduleError("INVALID_ARGUMENT", "invalid section")
	}
	if _, err := m.store.CreateSidebarSection(ctx, name); err != nil {
		if errors.Is(err, ErrAssistantLimitReached) {
			return moduleError("OVERLOADED", "section limit reached")
		}
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	return m.sectionList(ctx)
}

func (m *Module) sectionRename(ctx context.Context, raw json.RawMessage) relay.PuckResponse {
	var input struct {
		SectionID string `json:"sectionId"`
		Name      string `json:"name"`
	}
	if json.Unmarshal(raw, &input) != nil {
		return moduleError("INVALID_ARGUMENT", "invalid section")
	}
	name := strings.TrimSpace(input.Name)
	if input.SectionID == "" || name == "" || utf8.RuneCountInString(name) > 24 {
		return moduleError("INVALID_ARGUMENT", "invalid section")
	}
	if err := m.store.RenameSidebarSection(ctx, input.SectionID, name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return notFound()
		}
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	return m.sectionList(ctx)
}

func (m *Module) sectionDelete(ctx context.Context, raw json.RawMessage) relay.PuckResponse {
	var input struct {
		SectionID string `json:"sectionId"`
	}
	if json.Unmarshal(raw, &input) != nil || input.SectionID == "" {
		return moduleError("INVALID_ARGUMENT", "invalid section")
	}
	if err := m.store.DeleteSidebarSection(ctx, input.SectionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return notFound()
		}
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	return m.sectionList(ctx)
}

func (m *Module) sectionCollapse(ctx context.Context, raw json.RawMessage) relay.PuckResponse {
	var input struct {
		SectionID string `json:"sectionId"`
		Collapsed *bool  `json:"collapsed"`
	}
	if json.Unmarshal(raw, &input) != nil || input.Collapsed == nil {
		return moduleError("INVALID_ARGUMENT", "invalid section")
	}
	if err := m.store.CollapseSidebar(ctx, input.SectionID, *input.Collapsed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return notFound()
		}
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	return m.sectionList(ctx)
}

func (m *Module) sectionMove(ctx context.Context, raw json.RawMessage) relay.PuckResponse {
	var input struct {
		Kind      string `json:"kind"`
		EntryID   string `json:"entryId"`
		SectionID string `json:"sectionId"`
		Position  int    `json:"position"`
	}
	if json.Unmarshal(raw, &input) != nil || (input.Kind != "assistant" && input.Kind != "group") || input.EntryID == "" || input.Position < 0 || input.Position > 10000 {
		return moduleError("INVALID_ARGUMENT", "invalid section move")
	}
	if err := m.store.MoveSidebarEntry(ctx, input.Kind, input.EntryID, input.SectionID, input.Position); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return notFound()
		}
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	return m.sectionList(ctx)
}

func sidebarData(view SidebarView) map[string]any {
	return map[string]any{"sections": view.Sections, "placements": view.Placements, "ungroupedCollapsed": view.UngroupedCollapsed}
}
