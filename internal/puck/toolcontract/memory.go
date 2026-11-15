package toolcontract

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

const (
	maxMemoryRunes = 300
	maxRecallQuery = 256
)

type UpdateMemoryRequest struct {
	Action string `json:"action"`
	Scope  string `json:"scope,omitempty"`
	Kind   string `json:"kind"`
	Text   string `json:"text"`
}

type RecallMemoryRequest struct {
	Query   string   `json:"query"`
	Scope   string   `json:"scope,omitempty"`
	Kind    string   `json:"kind,omitempty"`
	Sources []string `json:"sources,omitempty"`
	Limit   int      `json:"limit,omitempty"`
}

func memoryDefinition(name string) (Definition, bool) {
	switch name {
	case "update_memory":
		return Definition{Name: name, Description: "记下或忘掉一条长期事实。action 是 write 或 forget。scope 省略时是 bot；大家的 Bot 都该知道的用 shared。kind 是 profile（稳定的习惯和做法）或 journal（带日期的经过）。text 是原文。forget 必须传入当初记下的原文。改一条要先 forget 再 write。不要记密码、token 或密钥。", InputSchema: updateMemorySchema()}, true
	case "recall_memory":
		return Definition{Name: name, Description: "用关键词查找没有出现在本回合里的记忆原文。任务上下文不要靠它挑选：先 list_bot_tasks 看每条的 id 和 description，自己选定后再 get_bot_task。query 必填。scope 省略时是 all。kind 省略时是 any。", InputSchema: recallMemorySchema()}, true
	default:
		return Definition{}, false
	}
}

func DecodeUpdateMemory(raw json.RawMessage) (UpdateMemoryRequest, []FieldError) {
	var value UpdateMemoryRequest
	fields, detail := decodeObject(raw)
	if detail != nil {
		return value, []FieldError{*detail}
	}
	if details := rejectUnknown(fields, "action", "scope", "kind", "text"); len(details) != 0 {
		return value, details
	}
	if detail := requiredString(fields, "action", &value.Action, "write or forget"); detail != nil {
		return value, []FieldError{*detail}
	}
	if value.Action != "write" && value.Action != "forget" {
		return value, fieldError("action", "unknown enum value", "write or forget")
	}
	if details := optionalBoundedString(fields, "scope", &value.Scope, 16); len(details) != 0 {
		return value, details
	}
	if value.Scope == "" {
		value.Scope = "bot"
	}
	if value.Scope != "bot" && value.Scope != "shared" {
		return value, fieldError("scope", "unknown enum value", "bot or shared")
	}
	if detail := requiredString(fields, "kind", &value.Kind, "profile or journal"); detail != nil {
		return value, []FieldError{*detail}
	}
	if value.Kind != "profile" && value.Kind != "journal" {
		return value, fieldError("kind", "unknown enum value", "profile or journal")
	}
	if detail := requiredString(fields, "text", &value.Text, "the exact memory text, 1 to 300 characters"); detail != nil {
		return value, []FieldError{*detail}
	}
	value.Text = strings.TrimSpace(value.Text)
	if value.Text == "" || strings.ContainsRune(value.Text, 0) || utf8.RuneCountInString(value.Text) > maxMemoryRunes {
		return value, fieldError("text", "value has invalid type or length", "1 to 300 characters without NUL")
	}
	return value, nil
}

func DecodeRecallMemory(raw json.RawMessage) (RecallMemoryRequest, []FieldError) {
	var value RecallMemoryRequest
	fields, detail := decodeObject(raw)
	if detail != nil {
		return value, []FieldError{*detail}
	}
	if details := rejectUnknown(fields, "query", "scope", "kind", "sources", "limit"); len(details) != 0 {
		return value, details
	}
	if detail := requiredString(fields, "query", &value.Query, "1 to 256 characters"); detail != nil {
		return value, []FieldError{*detail}
	}
	value.Query = strings.TrimSpace(value.Query)
	if value.Query == "" || utf8.RuneCountInString(value.Query) > maxRecallQuery {
		return value, fieldError("query", "value has invalid type or length", "1 to 256 characters")
	}
	if details := optionalBoundedString(fields, "scope", &value.Scope, 16); len(details) != 0 {
		return value, details
	}
	if value.Scope == "" {
		value.Scope = "all"
	}
	if value.Scope != "bot" && value.Scope != "shared" && value.Scope != "all" {
		return value, fieldError("scope", "unknown enum value", "bot, shared, or all")
	}
	if details := optionalBoundedString(fields, "kind", &value.Kind, 16); len(details) != 0 {
		return value, details
	}
	if value.Kind == "" {
		value.Kind = "any"
	}
	if value.Kind != "profile" && value.Kind != "journal" && value.Kind != "any" {
		return value, fieldError("kind", "unknown enum value", "profile, journal, or any")
	}
	if rawSources, ok := fields["sources"]; ok {
		var sources []string
		if json.Unmarshal(rawSources, &sources) != nil || len(sources) == 0 || len(sources) > 3 {
			return value, fieldError("sources", "value has invalid type or length", "a non-empty subset of memory, tasks, messages")
		}
		seen := map[string]bool{}
		for _, source := range sources {
			if source != "memory" && source != "tasks" && source != "messages" || seen[source] {
				return value, fieldError("sources", "unknown enum value", "memory, tasks, or messages")
			}
			seen[source] = true
		}
		value.Sources = sources
	} else {
		value.Sources = []string{"memory", "tasks", "messages"}
	}
	limit, details := optionalRecallLimit(fields)
	if len(details) != 0 {
		return value, details
	}
	if limit == 0 {
		limit = 8
	}
	value.Limit = limit
	return value, nil
}

func optionalRecallLimit(fields map[string]json.RawMessage) (int, []FieldError) {
	raw, ok := fields["limit"]
	if !ok {
		return 0, nil
	}
	var limit int
	if json.Unmarshal(raw, &limit) != nil || limit < 1 || limit > 20 {
		return 0, fieldError("limit", "value has invalid type or range", "integer from 1 to 20")
	}
	return limit, nil
}

func updateMemorySchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "required": []string{"action", "kind", "text"}, "properties": map[string]any{
		"action": map[string]any{"type": "string", "enum": []any{"write", "forget"}},
		"scope":  map[string]any{"type": "string", "enum": []any{"bot", "shared"}},
		"kind":   map[string]any{"type": "string", "enum": []any{"profile", "journal"}},
		"text":   stringProperty(1, maxMemoryRunes),
	}}
}

func recallMemorySchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "required": []string{"query"}, "properties": map[string]any{
		"query":   stringProperty(1, maxRecallQuery),
		"scope":   map[string]any{"type": "string", "enum": []any{"bot", "shared", "all"}},
		"kind":    map[string]any{"type": "string", "enum": []any{"profile", "journal", "any"}},
		"sources": map[string]any{"type": "array", "minItems": 1, "maxItems": 3, "uniqueItems": true, "items": map[string]any{"type": "string", "enum": []any{"memory", "tasks", "messages"}}},
		"limit":   map[string]any{"type": "integer", "minimum": 1, "maximum": 20},
	}}
}
