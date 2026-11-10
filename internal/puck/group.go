package puck

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/wu8685/Ariel/internal/relay"
)

const maxGroups = 20

func (m *Module) groupList(ctx context.Context) relay.PuckResponse {
	groups, err := m.store.Groups(ctx)
	if err != nil {
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	return accepted(map[string]any{"groups": groups})
}

func (m *Module) groupCreate(ctx context.Context, raw json.RawMessage) relay.PuckResponse {
	var input struct {
		Name      string   `json:"name"`
		MemberIDs []string `json:"memberIds"`
	}
	if json.Unmarshal(raw, &input) != nil {
		return moduleError("INVALID_ARGUMENT", "invalid group")
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || utf8.RuneCountInString(name) > 40 || len(input.MemberIDs) < 2 || len(input.MemberIDs) > 6 {
		return moduleError("INVALID_ARGUMENT", "invalid group")
	}
	seen := map[string]bool{}
	members := make([]GroupMember, 0, len(input.MemberIDs))
	for _, id := range input.MemberIDs {
		if id == "" || seen[id] {
			return moduleError("INVALID_ARGUMENT", "invalid group members")
		}
		seen[id] = true
		assistant, err := m.store.Assistant(ctx, id)
		if err != nil || assistant.Status != "ready" {
			return moduleError("INVALID_ARGUMENT", "group members must be ready assistants")
		}
		members = append(members, GroupMember{AssistantID: assistant.ID, Name: assistant.Name})
	}
	count, err := m.store.CountGroups(ctx)
	if err != nil {
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	if count >= maxGroups {
		return moduleError("OVERLOADED", "group limit reached")
	}
	group := Group{ID: randomID("group"), Name: name, Members: members}
	if err := m.store.CreateGroup(ctx, group); err != nil {
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	return accepted(map[string]any{"group": group})
}

func (m *Module) groupHistory(ctx context.Context, raw json.RawMessage) relay.PuckResponse {
	var input struct {
		GroupID string `json:"groupId"`
	}
	if json.Unmarshal(raw, &input) != nil || input.GroupID == "" {
		return moduleError("INVALID_ARGUMENT", "invalid group")
	}
	if _, err := m.store.Group(ctx, input.GroupID); err != nil {
		return notFound()
	}
	messages, err := m.namedGroupMessages(ctx, input.GroupID)
	if err != nil {
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	return accepted(map[string]any{"messages": messages})
}

func (m *Module) groupMessage(ctx context.Context, raw json.RawMessage) relay.PuckResponse {
	var input struct {
		GroupID         string `json:"groupId"`
		ClientMessageID string `json:"clientMessageId"`
		Text            string `json:"text"`
	}
	if json.Unmarshal(raw, &input) != nil || input.GroupID == "" || input.ClientMessageID == "" || strings.TrimSpace(input.Text) == "" || len(input.Text) > 65536 {
		return moduleError("INVALID_ARGUMENT", "invalid group message")
	}
	group, err := m.store.Group(ctx, input.GroupID)
	if err != nil {
		return notFound()
	}
	if len(group.Members) < 2 {
		return moduleError("INVALID_ARGUMENT", "group has no members")
	}
	if err := m.store.AppendGroupMessage(ctx, GroupMessage{ID: input.ClientMessageID, GroupID: group.ID, Role: "user", Kind: "text", Text: input.Text}); err != nil {
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	names := make([]string, 0, len(group.Members))
	for _, member := range group.Members {
		names = append(names, member.Name)
	}
	for _, member := range group.Members {
		responseID := input.ClientMessageID + "-" + member.AssistantID
		assistant, err := m.store.Assistant(ctx, member.AssistantID)
		if err != nil || assistant.Status != "ready" || assistant.BrainThreadID == "" {
			_ = m.store.AppendGroupMessage(ctx, GroupMessage{ID: responseID, GroupID: group.ID, Role: "assistant", AuthorAssistantID: member.AssistantID, Kind: "error", Text: member.Name + " 暂时无法加入这轮对话。"})
			continue
		}
		previous := m.clearPending(assistant.ID)
		if previous.turnID != "" {
			_ = m.bridge.Forward(ctx, relay.Origin{Kind: "puck", AssistantID: assistant.ID, Trigger: "user_message"}, assistant.BrainDeviceID, "puck.brain.turn.interrupt", mustJSON(map[string]any{"threadId": previous.threadID, "expectedTurnId": previous.turnID}))
		}
		if err := m.deletePendingResponse(ctx, assistant.ID, previous); err != nil {
			return moduleError("PUCK_UNAVAILABLE", err.Error())
		}
		prompt := fmt.Sprintf("你正在小组「%s」里发言，成员有：%s。请只用你自己的口吻用一两句话回复，不要调用工具，不要代替其他成员发言。\n\n%s", group.Name, strings.Join(names, "、"), input.Text)
		result := m.startBrainTurn(ctx, assistant, mustJSON(map[string]any{
			"assistantId": assistant.ID, "threadId": assistant.BrainThreadID, "clientMessageId": input.ClientMessageID,
			"text": brainEnvelope(assistant, prompt), "model": assistant.BrainModel, "effort": assistant.BrainReasoning,
		}))
		if result.Error != nil {
			_ = m.store.AppendGroupMessage(ctx, GroupMessage{ID: responseID, GroupID: group.ID, Role: "assistant", AuthorAssistantID: assistant.ID, Kind: "error", Text: member.Name + " 这轮没有开始。"})
			continue
		}
		turnID, _ := result.Data["turnId"].(string)
		startedAt := m.now()
		if err := m.store.AppendGroupMessage(ctx, GroupMessage{ID: responseID, GroupID: group.ID, Role: "assistant", AuthorAssistantID: assistant.ID, Kind: "processing", Text: "正在处理…", CreatedAt: startedAt}); err != nil {
			return moduleError("PUCK_UNAVAILABLE", err.Error())
		}
		m.setPending(assistant, turnID, input.ClientMessageID, responseID, startedAt, true, group.ID)
	}
	messages, err := m.namedGroupMessages(ctx, group.ID)
	if err != nil {
		return moduleError("PUCK_UNAVAILABLE", err.Error())
	}
	return accepted(map[string]any{"messageId": input.ClientMessageID, "messages": messages})
}

func (m *Module) namedGroupMessages(ctx context.Context, groupID string) ([]GroupMessage, error) {
	messages, err := m.store.GroupMessages(ctx, groupID)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	if group, err := m.store.Group(ctx, groupID); err == nil {
		for _, member := range group.Members {
			names[member.AssistantID] = member.Name
		}
	}
	for index := range messages {
		messages[index].AuthorName = names[messages[index].AuthorAssistantID]
	}
	return messages, nil
}
