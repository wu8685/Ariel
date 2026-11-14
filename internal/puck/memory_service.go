package puck

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/wu8685/Ariel/internal/puck/toolcontract"
)

func (m *Module) memoryBlock(ctx context.Context, assistant Assistant) string {
	if assistant.ToolsetVersion < memoryToolset || m == nil || m.store == nil {
		return ""
	}
	botProfile, sharedProfile, botJournal, sharedJournal, err := m.store.InjectionMemories(ctx, assistant.ID)
	if err != nil {
		return ""
	}
	return formatMemoryBlock(botProfile, sharedProfile, botJournal, sharedJournal)
}

func (m *Module) syncEmbedderModel(ctx context.Context) {
	embedder := m.embedder
	if embedder == nil || !embedder.Available() {
		return
	}
	m.embedMu.Lock()
	defer m.embedMu.Unlock()
	if m.embedderReady {
		return
	}
	if err := embedder.Probe(ctx); err != nil {
		return
	}
	current, _, err := m.store.EmbedderRecord(ctx)
	if err != nil {
		return
	}
	if current != "" && current != embedder.ModelID() {
		if err := m.store.ClearMemoryVectors(ctx); err != nil {
			return
		}
	}
	if err := m.store.SetEmbedderRecord(ctx, embedder.ModelID(), embedder.Dim()); err != nil {
		return
	}
	m.embedderReady = true
}

func (m *Module) embedMemory(ctx context.Context, entry Memory) {
	embedder := m.embedder
	if embedder == nil || !embedder.Available() {
		return
	}
	m.syncEmbedderModel(ctx)
	vectors, err := embedder.EmbedDocuments(ctx, []string{entry.Text})
	if err != nil || len(vectors) != 1 {
		return
	}
	_ = m.store.SaveMemoryVector(ctx, entry.ID, embedder.ModelID(), vectors[0], time.Now())
}

func (m *Module) runMemoryBackfill(ctx context.Context) {
	defer m.wg.Done()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.backfillMemories(ctx)
		}
	}
}

func (m *Module) backfillMemories(ctx context.Context) {
	embedder := m.embedder
	if embedder == nil || !embedder.Available() {
		return
	}
	m.syncEmbedderModel(ctx)
	if !m.embedderReady {
		return
	}
	entries, err := m.store.MemoriesMissingVector(ctx, embedder.ModelID(), 16)
	if err != nil || len(entries) == 0 {
		return
	}
	texts := make([]string, len(entries))
	for i, entry := range entries {
		texts[i] = entry.Text
	}
	vectors, err := embedder.EmbedDocuments(ctx, texts)
	if err != nil || len(vectors) != len(entries) {
		return
	}
	for i, entry := range entries {
		_ = m.store.SaveMemoryVector(ctx, entry.ID, embedder.ModelID(), vectors[i], time.Now())
	}
}

func (m *Module) updateMemory(ctx context.Context, assistantID string, request toolcontract.UpdateMemoryRequest) (map[string]any, error) {
	assistant, err := m.store.Assistant(ctx, assistantID)
	if err != nil {
		return nil, err
	}
	if request.Action == "forget" {
		entry, err := m.store.ForgetMemory(ctx, assistantID, request.Scope, request.Kind, request.Text)
		if err != nil {
			return nil, err
		}
		return memoryToolJSON("forget", entry, false), nil
	}
	entry, deduped, err := m.store.WriteMemory(ctx, assistantID, assistant.Name, request.Scope, request.Kind, request.Text)
	if err != nil {
		return nil, err
	}
	if !deduped {
		m.embedMemory(ctx, entry)
	}
	return memoryToolJSON("write", entry, deduped), nil
}

func memoryToolJSON(action string, entry Memory, deduped bool) map[string]any {
	name := entry.DisplayName
	if name == "" {
		name = entry.WriterName
	}
	payload := map[string]any{
		"action": action, "scope": entry.Scope, "kind": entry.Kind, "text": entry.Text,
		"writerName": name, "createdAt": entry.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if action == "write" {
		payload["deduped"] = deduped
	}
	return payload
}

func (m *Module) recallMemory(ctx context.Context, assistantID string, request toolcontract.RecallMemoryRequest) (map[string]any, error) {
	if _, err := m.store.Assistant(ctx, assistantID); err != nil {
		return nil, err
	}
	sources := map[string]bool{}
	for _, source := range request.Sources {
		sources[source] = true
	}
	payload := map[string]any{"embedder": "unavailable"}
	if sources["memory"] {
		memories, status := m.recallMemoryEntries(ctx, assistantID, request)
		payload["embedder"] = status
		items := make([]map[string]any, 0, len(memories))
		for _, entry := range memories {
			name := entry.DisplayName
			if name == "" {
				name = entry.WriterName
			}
			items = append(items, map[string]any{"scope": entry.Scope, "kind": entry.Kind, "text": entry.Text, "writerName": name, "createdAt": entry.CreatedAt.UTC().Format(time.RFC3339Nano)})
		}
		payload["memories"] = items
	} else {
		payload["memories"] = []any{}
	}
	if sources["tasks"] {
		page, err := m.store.ListBotTasks(ctx, assistantID, request.Query, "", 0, request.Limit)
		if err != nil {
			return nil, err
		}
		snippets := m.taskSnippets(ctx, assistantID, page.Tasks)
		tasks := make([]map[string]any, 0, len(page.Tasks))
		for _, task := range page.Tasks {
			item := map[string]any{"id": task.ID, "title": task.Title, "goal": task.Goal, "status": task.Status, "updatedAt": task.UpdatedAt.UTC().Format(time.RFC3339Nano)}
			if snippet := strings.TrimSpace(snippets[task.ID]); snippet != "" {
				item["lastUserMessage"] = snippet
			}
			tasks = append(tasks, item)
		}
		payload["tasks"] = tasks
	} else {
		payload["tasks"] = []any{}
	}
	if sources["messages"] {
		messages, err := m.store.SearchMessagesRecent(ctx, assistantID, request.Query, request.Limit)
		if err != nil {
			return nil, err
		}
		items := make([]map[string]any, 0, len(messages))
		for _, message := range messages {
			items = append(items, map[string]any{"id": message.ID, "role": message.Role, "text": message.Text, "createdAt": message.CreatedAt.UTC().Format(time.RFC3339Nano)})
		}
		payload["messages"] = items
	} else {
		payload["messages"] = []any{}
	}
	return fitRecallPayload(payload), nil
}

func (m *Module) recallMemoryEntries(ctx context.Context, assistantID string, request toolcontract.RecallMemoryRequest) ([]Memory, string) {
	status := "unavailable"
	distance := map[string]float64{}
	var semantic []Memory
	embedder := m.embedder
	if embedder != nil && embedder.Available() {
		m.syncEmbedderModel(ctx)
		if vector, err := embedder.EmbedQuery(ctx, request.Query); err == nil {
			encoded, encErr := json.Marshal(vector)
			if encErr == nil {
				hits, err := m.store.MemoryKNN(ctx, assistantID, request.Scope, request.Kind, embedder.ModelID(), string(encoded), 40)
				if err == nil {
					status = "ready"
					for _, hit := range hits {
						entry, err := m.store.MemoryByRowID(ctx, hit.RowID)
						if err != nil {
							continue
						}
						distance[entry.ID] = hit.Distance
						semantic = append(semantic, entry)
					}
				}
			}
		}
	}
	keywordIDs, err := m.store.SearchMemoryIDs(ctx, assistantID, request.Scope, request.Kind, request.Query, 20)
	if err != nil {
		keywordIDs = nil
	}
	keyword := map[string]bool{}
	exact := map[string]bool{}
	var pool []Memory
	pool = append(pool, semantic...)
	for _, id := range keywordIDs {
		keyword[id] = true
		if entry, err := m.store.memoryByID(ctx, id); err == nil {
			pool = append(pool, entry)
		}
	}
	for _, token := range exactTokens(request.Query) {
		ids, err := m.store.ExactMemoryIDs(ctx, assistantID, request.Scope, request.Kind, token, 20)
		if err != nil {
			continue
		}
		for _, id := range ids {
			exact[id] = true
			if entry, err := m.store.memoryByID(ctx, id); err == nil {
				pool = append(pool, entry)
			}
		}
	}
	ranked := rankMemories(pool, distance, keyword, exact)
	if len(ranked) > request.Limit {
		ranked = ranked[:request.Limit]
	}
	return ranked, status
}

func fitRecallPayload(payload map[string]any) map[string]any {
	encoded, _ := json.Marshal(payload)
	if len(encoded) <= 60<<10 {
		return payload
	}
	messages, _ := payload["messages"].([]map[string]any)
	tasks, _ := payload["tasks"].([]map[string]any)
	for len(messages) > 0 && len(encoded) > 60<<10 {
		messages = messages[:len(messages)-1]
		payload["messages"] = messages
		encoded, _ = json.Marshal(payload)
	}
	for len(tasks) > 0 && len(encoded) > 60<<10 {
		tasks = tasks[:len(tasks)-1]
		payload["tasks"] = tasks
		encoded, _ = json.Marshal(payload)
	}
	return payload
}

func (b toolBackend) UpdateMemory(ctx context.Context, assistantID string, request toolcontract.UpdateMemoryRequest) (map[string]any, error) {
	return b.module.updateMemory(ctx, assistantID, request)
}

func (b toolBackend) RecallMemory(ctx context.Context, assistantID string, request toolcontract.RecallMemoryRequest) (map[string]any, error) {
	return b.module.recallMemory(ctx, assistantID, request)
}
