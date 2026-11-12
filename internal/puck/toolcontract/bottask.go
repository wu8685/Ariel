package toolcontract

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

type ListBotTasksRequest struct {
	Query  string `json:"query,omitempty"`
	Status string `json:"status,omitempty"`
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type SearchMessagesRequest struct {
	Query  string `json:"query,omitempty"`
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type SaveBotTaskRequest struct {
	ID     string `json:"id,omitempty"`
	Title  string `json:"title,omitempty"`
	Goal   string `json:"goal,omitempty"`
	Status string `json:"status,omitempty"`
}

type BotTaskSessionRef struct {
	Kind     string `json:"kind"`
	ID       string `json:"id,omitempty"`
	DeviceID string `json:"deviceId,omitempty"`
	ThreadID string `json:"threadId,omitempty"`
}

type AttachBotTaskRequest struct {
	TaskID     string              `json:"taskId"`
	MessageIDs []string            `json:"messageIds,omitempty"`
	Sessions   []BotTaskSessionRef `json:"sessions,omitempty"`
}

type MergeBotTasksRequest struct {
	FromID string `json:"fromId"`
	IntoID string `json:"intoId"`
}

func botTaskDefinition(name string) (Definition, bool) {
	switch name {
	case "list_bot_tasks":
		return Definition{Name: name, Description: "列出或按关键词搜索这个 Bot 自己的任务。每条含 title、最近更新时间，以及最后一条用户消息的原文片段，不另写摘要。可用 status 过滤，用 cursor 翻页。", InputSchema: listBotTasksSchema()}, true
	case "search_messages":
		return Definition{Name: name, Description: "按关键词搜索这个 Bot 的 Ariel 对话。返回消息原文，不摘要。用 cursor 翻页。", InputSchema: searchMessagesSchema()}, true
	case "save_bot_task":
		return Definition{Name: name, Description: "新建或更新这个 Bot 的任务。传入已有任务的 id 会更新那一条，不会自动挂上当前消息。要挂消息请再调用 attach_bot_task。不传 id 则新建。status 可以是 accepted、in_progress、waiting_user、done、cancelled。", InputSchema: saveBotTaskSchema()}, true
	case "attach_bot_task":
		return Definition{Name: name, Description: "把用户消息或本助理的子会话挂到已有任务上。messageIds 是 Ariel 消息 id。sessions.kind 为 task 时传 id，为 watch 时传 deviceId 和 threadId。", InputSchema: attachBotTaskSchema()}, true
	case "merge_bot_tasks":
		return Definition{Name: name, Description: "把 fromId 合并进 intoId。fromId 标成 merged 并保留历史，消息和子会话链接同时挂到 intoId。是否合并由你决定。", InputSchema: mergeBotTasksSchema()}, true
	default:
		return Definition{}, false
	}
}

func DecodeListBotTasks(raw json.RawMessage) (ListBotTasksRequest, []FieldError) {
	var value ListBotTasksRequest
	fields, detail := decodeObject(raw)
	if detail != nil {
		return value, []FieldError{*detail}
	}
	if details := rejectUnknown(fields, "query", "status", "cursor", "limit"); len(details) != 0 {
		return value, details
	}
	if details := optionalBoundedString(fields, "query", &value.Query, 256); len(details) != 0 {
		return value, details
	}
	if details := optionalBoundedString(fields, "status", &value.Status, 32); len(details) != 0 {
		return value, details
	}
	if value.Status != "" && !knownBotTaskStatus(value.Status) && value.Status != "merged" {
		return value, fieldError("status", "unknown enum value", "accepted, in_progress, waiting_user, done, cancelled, or merged")
	}
	if details := optionalBoundedString(fields, "cursor", &value.Cursor, 32); len(details) != 0 {
		return value, details
	}
	limit, details := optionalLimit(fields)
	if len(details) != 0 {
		return value, details
	}
	value.Limit = limit
	return value, nil
}

func DecodeSearchMessages(raw json.RawMessage) (SearchMessagesRequest, []FieldError) {
	var value SearchMessagesRequest
	fields, detail := decodeObject(raw)
	if detail != nil {
		return value, []FieldError{*detail}
	}
	if details := rejectUnknown(fields, "query", "cursor", "limit"); len(details) != 0 {
		return value, details
	}
	if details := optionalBoundedString(fields, "query", &value.Query, 256); len(details) != 0 {
		return value, details
	}
	if details := optionalBoundedString(fields, "cursor", &value.Cursor, 32); len(details) != 0 {
		return value, details
	}
	limit, details := optionalLimit(fields)
	if len(details) != 0 {
		return value, details
	}
	value.Limit = limit
	return value, nil
}

func DecodeSaveBotTask(raw json.RawMessage) (SaveBotTaskRequest, []FieldError) {
	var value SaveBotTaskRequest
	fields, detail := decodeObject(raw)
	if detail != nil {
		return value, []FieldError{*detail}
	}
	if details := rejectUnknown(fields, "id", "title", "goal", "status"); len(details) != 0 {
		return value, details
	}
	if details := optionalBoundedString(fields, "id", &value.ID, maxIDLength); len(details) != 0 {
		return value, details
	}
	if details := optionalBoundedString(fields, "title", &value.Title, maxGoalLength); len(details) != 0 {
		return value, details
	}
	if details := optionalBoundedString(fields, "goal", &value.Goal, maxTextLength); len(details) != 0 {
		return value, details
	}
	if details := optionalBoundedString(fields, "status", &value.Status, 32); len(details) != 0 {
		return value, details
	}
	if strings.TrimSpace(value.Title) == "" && strings.TrimSpace(value.Goal) == "" && value.ID == "" {
		return value, fieldError("title", "title or goal is required", "non-empty title or goal, or an existing task id")
	}
	keepStatus := value.Status == "" && strings.TrimSpace(value.Title) == "" && strings.TrimSpace(value.Goal) == "" && value.ID != ""
	if value.Status == "" && !keepStatus {
		value.Status = "accepted"
	}
	if !keepStatus && !knownBotTaskStatus(value.Status) {
		return value, fieldError("status", "unknown enum value", "accepted, in_progress, waiting_user, done, or cancelled")
	}
	return value, nil
}

func DecodeAttachBotTask(raw json.RawMessage) (AttachBotTaskRequest, []FieldError) {
	var value AttachBotTaskRequest
	fields, detail := decodeObject(raw)
	if detail != nil {
		return value, []FieldError{*detail}
	}
	if details := rejectUnknown(fields, "taskId", "messageIds", "sessions"); len(details) != 0 {
		return value, details
	}
	if detail := requiredString(fields, "taskId", &value.TaskID, "non-empty string"); detail != nil {
		return value, []FieldError{*detail}
	}
	if utf8.RuneCountInString(value.TaskID) > maxIDLength {
		return value, fieldError("taskId", "value is too long", "at most 128 characters")
	}
	if rawIDs, ok := fields["messageIds"]; ok {
		if details := decodeOptionalIDs(map[string]json.RawMessage{"messageIds": rawIDs}, "messageIds", &value.MessageIDs); len(details) != 0 {
			return value, details
		}
	}
	if rawSessions, ok := fields["sessions"]; ok {
		var entries []json.RawMessage
		if json.Unmarshal(rawSessions, &entries) != nil || len(entries) > 8 {
			return value, fieldError("sessions", "value has invalid type or length", "array of at most 8 sessions")
		}
		for index, entry := range entries {
			sessionFields, itemDetail := decodeObject(entry)
			if itemDetail != nil {
				return value, fieldError("sessions", itemDetail.Reason, "session object")
			}
			if details := rejectUnknown(sessionFields, "kind", "id", "deviceId", "threadId"); len(details) != 0 {
				return value, details
			}
			var session BotTaskSessionRef
			if detail := requiredString(sessionFields, "kind", &session.Kind, "task or watch"); detail != nil {
				return value, []FieldError{*detail}
			}
			if session.Kind != "task" && session.Kind != "watch" {
				return value, fieldError("kind", "unknown enum value", "task or watch")
			}
			_ = optionalString(sessionFields, "id", &session.ID)
			_ = optionalString(sessionFields, "deviceId", &session.DeviceID)
			_ = optionalString(sessionFields, "threadId", &session.ThreadID)
			if session.Kind == "task" && session.ID == "" {
				return value, fieldError("sessions["+itoa(index)+"].id", "required field is missing", "task id")
			}
			if session.Kind == "watch" && (session.DeviceID == "" || session.ThreadID == "") {
				return value, fieldError("sessions["+itoa(index)+"].deviceId", "required field is missing", "deviceId and threadId")
			}
			value.Sessions = append(value.Sessions, session)
		}
	}
	if len(value.MessageIDs) == 0 && len(value.Sessions) == 0 {
		return value, fieldError("messageIds", "nothing to attach", "messageIds or sessions")
	}
	return value, nil
}

func DecodeMergeBotTasks(raw json.RawMessage) (MergeBotTasksRequest, []FieldError) {
	var value MergeBotTasksRequest
	fields, detail := decodeObject(raw)
	if detail != nil {
		return value, []FieldError{*detail}
	}
	if details := rejectUnknown(fields, "fromId", "intoId"); len(details) != 0 {
		return value, details
	}
	if detail := requiredString(fields, "fromId", &value.FromID, "non-empty string"); detail != nil {
		return value, []FieldError{*detail}
	}
	if detail := requiredString(fields, "intoId", &value.IntoID, "non-empty string"); detail != nil {
		return value, []FieldError{*detail}
	}
	if value.FromID == value.IntoID {
		return value, fieldError("intoId", "merge target must differ", "a different task id")
	}
	return value, nil
}

func knownBotTaskStatus(status string) bool {
	switch status {
	case "accepted", "in_progress", "waiting_user", "done", "cancelled":
		return true
	default:
		return false
	}
}

func optionalBoundedString(fields map[string]json.RawMessage, name string, target *string, max int) []FieldError {
	raw, ok := fields[name]
	if !ok {
		return nil
	}
	if json.Unmarshal(raw, target) != nil || utf8.RuneCountInString(*target) > max {
		return fieldError(name, "value has invalid type or length", "string within the documented limit")
	}
	*target = strings.TrimSpace(*target)
	return nil
}

func optionalString(fields map[string]json.RawMessage, name string, target *string) bool {
	raw, ok := fields[name]
	if !ok {
		return false
	}
	_ = json.Unmarshal(raw, target)
	*target = strings.TrimSpace(*target)
	return true
}

func optionalLimit(fields map[string]json.RawMessage) (int, []FieldError) {
	raw, ok := fields["limit"]
	if !ok {
		return 0, nil
	}
	var limit int
	if json.Unmarshal(raw, &limit) != nil || limit < 1 || limit > 50 {
		return 0, fieldError("limit", "value has invalid type or range", "integer from 1 to 50")
	}
	return limit, nil
}

func itoa(value int) string {
	return strings.TrimPrefix(strings.ReplaceAll(jsonNumber(value), " ", ""), "+")
}

func jsonNumber(value int) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func listBotTasksSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
		"query": stringProperty(0, 256), "status": map[string]any{"type": "string", "enum": []any{"accepted", "in_progress", "waiting_user", "done", "cancelled", "merged"}},
		"cursor": stringProperty(0, 32), "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 50},
	}}
}

func searchMessagesSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
		"query": stringProperty(0, 256), "cursor": stringProperty(0, 32), "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 50},
	}}
}

func saveBotTaskSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
		"id": stringProperty(0, maxIDLength), "title": stringProperty(0, maxGoalLength), "goal": stringProperty(0, maxTextLength),
		"status": map[string]any{"type": "string", "enum": []any{"accepted", "in_progress", "waiting_user", "done", "cancelled"}},
	}}
}

func attachBotTaskSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "required": []string{"taskId"}, "properties": map[string]any{
		"taskId": stringProperty(1, maxIDLength), "messageIds": idListSchema("要挂上的用户消息 id"),
		"sessions": map[string]any{"type": "array", "maxItems": 8, "items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"kind"}, "properties": map[string]any{
			"kind": map[string]any{"type": "string", "enum": []any{"task", "watch"}}, "id": stringProperty(0, maxIDLength), "deviceId": stringProperty(0, maxIDLength), "threadId": stringProperty(0, maxIDLength),
		}}},
	}}
}

func mergeBotTasksSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "required": []string{"fromId", "intoId"}, "properties": map[string]any{
		"fromId": stringProperty(1, maxIDLength), "intoId": stringProperty(1, maxIDLength),
	}}
}
