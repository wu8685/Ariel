package toolcontract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"unicode/utf8"
)

const Version = 2

const (
	maxIDLength   = 128
	maxCWDLength  = 4096
	maxGoalLength = 512
	maxTextLength = 65536
)

var (
	uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
	riskTags    = []string{"push", "deploy", "release", "delete", "destructive", "credential", "payment", "external_message"}
	riskTagSet  = func() map[string]struct{} {
		values := make(map[string]struct{}, len(riskTags))
		for _, value := range riskTags {
			values[value] = struct{}{}
		}
		return values
	}()
)

type FieldError struct {
	Field    string `json:"field"`
	Reason   string `json:"reason"`
	Expected string `json:"expected"`
}

type StartTaskRequest struct {
	DeviceID     string   `json:"deviceId"`
	CWD          string   `json:"cwd"`
	Goal         string   `json:"goal"`
	Prompt       string   `json:"prompt"`
	RiskTags     []string `json:"riskTags"`
	ClientTaskID string   `json:"clientTaskId"`
}

type SendTurnRequest struct {
	TaskID          string   `json:"taskId,omitempty"`
	DeviceID        string   `json:"deviceId,omitempty"`
	ThreadID        string   `json:"threadId,omitempty"`
	Text            string   `json:"text"`
	RiskTags        []string `json:"riskTags"`
	ClientMessageID string   `json:"clientMessageId"`
}

type Definition struct {
	Name        string
	Description string
	InputSchema map[string]any
}

func DefinitionFor(version int, name string) (Definition, bool) {
	if version < Version {
		return Definition{}, false
	}
	switch name {
	case "start_task":
		return Definition{Name: name, Description: "在指定设备和绝对 workspace 中创建执行任务，并原子发送首条输入。", InputSchema: startTaskSchema()}, true
	case "send_turn":
		return Definition{Name: name, Description: "通过 Puck task 或显式设备与 thread 定位，发送一个幂等 follow-up turn。", InputSchema: sendTurnSchema()}, true
	default:
		return Definition{}, false
	}
}

func DynamicTools(version int) []map[string]any {
	names := []string{"list_devices", "list_threads", "read_thread", "start_task", "send_turn", "interrupt", "respond_interaction", "get_task_status", "request_authorization", "ask_user", "share_result", "react"}
	tools := make([]map[string]any, 0, len(names))
	for _, name := range names {
		description := "Ariel Puck tool: " + name
		schema := map[string]any{"type": "object", "additionalProperties": true}
		if definition, ok := DefinitionFor(version, name); ok {
			description, schema = definition.Description, definition.InputSchema
		}
		tools = append(tools, map[string]any{"name": name, "description": description, "inputSchema": schema})
	}
	return tools
}

func Decode(tool string, raw json.RawMessage) (any, []FieldError) {
	switch tool {
	case "start_task":
		value, details := DecodeStartTask(raw)
		return value, details
	case "send_turn":
		value, details := DecodeSendTurn(raw)
		return value, details
	default:
		return nil, []FieldError{{Field: "tool", Reason: "unknown tool contract", Expected: "start_task or send_turn"}}
	}
}

func DecodeStartTask(raw json.RawMessage) (StartTaskRequest, []FieldError) {
	var value StartTaskRequest
	fields, detail := decodeObject(raw)
	if detail != nil {
		return value, []FieldError{*detail}
	}
	if details := rejectUnknown(fields, "deviceId", "cwd", "goal", "prompt", "riskTags", "clientTaskId"); len(details) != 0 {
		return value, details
	}
	checks := []struct {
		name     string
		target   *string
		expected string
	}{
		{"deviceId", &value.DeviceID, "non-empty string with at most 128 characters"},
		{"cwd", &value.CWD, "absolute path with at most 4096 characters"},
		{"goal", &value.Goal, "non-empty string with at most 512 characters"},
		{"prompt", &value.Prompt, "non-empty string with at most 65536 characters"},
		{"clientTaskId", &value.ClientTaskID, "UUID"},
	}
	for _, check := range checks {
		if detail := requiredString(fields, check.name, check.target, check.expected); detail != nil {
			return value, []FieldError{*detail}
		}
	}
	if utf8.RuneCountInString(value.DeviceID) > maxIDLength {
		return value, fieldError("deviceId", "value is too long", checks[0].expected)
	}
	if !filepath.IsAbs(value.CWD) || utf8.RuneCountInString(value.CWD) > maxCWDLength {
		return value, fieldError("cwd", "value must be an absolute bounded path", checks[1].expected)
	}
	if utf8.RuneCountInString(value.Goal) > maxGoalLength {
		return value, fieldError("goal", "value is too long", checks[2].expected)
	}
	if utf8.RuneCountInString(value.Prompt) > maxTextLength {
		return value, fieldError("prompt", "value is too long", checks[3].expected)
	}
	if !uuidPattern.MatchString(value.ClientTaskID) {
		return value, fieldError("clientTaskId", "value has invalid format", checks[4].expected)
	}
	tags, details := decodeRiskTags(fields)
	value.RiskTags = tags
	return value, details
}

func DecodeSendTurn(raw json.RawMessage) (SendTurnRequest, []FieldError) {
	var value SendTurnRequest
	fields, detail := decodeObject(raw)
	if detail != nil {
		return value, []FieldError{*detail}
	}
	if details := rejectUnknown(fields, "taskId", "deviceId", "threadId", "text", "riskTags", "clientMessageId"); len(details) != 0 {
		return value, details
	}
	if detail := requiredString(fields, "text", &value.Text, "non-empty string with at most 65536 characters"); detail != nil {
		return value, []FieldError{*detail}
	}
	if utf8.RuneCountInString(value.Text) > maxTextLength {
		return value, fieldError("text", "value is too long", "non-empty string with at most 65536 characters")
	}
	if detail := requiredString(fields, "clientMessageId", &value.ClientMessageID, "UUID"); detail != nil {
		return value, []FieldError{*detail}
	}
	if !uuidPattern.MatchString(value.ClientMessageID) {
		return value, fieldError("clientMessageId", "value has invalid format", "UUID")
	}
	tags, details := decodeRiskTags(fields)
	if len(details) != 0 {
		return value, details
	}
	value.RiskTags = tags
	for name, target := range map[string]*string{"taskId": &value.TaskID, "deviceId": &value.DeviceID, "threadId": &value.ThreadID} {
		if rawValue, exists := fields[name]; exists {
			if err := json.Unmarshal(rawValue, target); err != nil || *target == "" || utf8.RuneCountInString(*target) > maxIDLength {
				return value, fieldError(name, "value has invalid type or length", "non-empty string with at most 128 characters")
			}
		}
	}
	if value.TaskID != "" && (value.DeviceID != "" || value.ThreadID != "") {
		return value, fieldError("taskId", "locator forms are mutually exclusive", "taskId alone, or deviceId and threadId together")
	}
	if value.TaskID == "" && value.DeviceID == "" && value.ThreadID == "" {
		return value, fieldError("taskId", "a target locator is required", "taskId, or deviceId and threadId")
	}
	if value.TaskID == "" && value.DeviceID == "" {
		return value, fieldError("deviceId", "field is required with threadId", "non-empty string with at most 128 characters")
	}
	if value.TaskID == "" && value.ThreadID == "" {
		return value, fieldError("threadId", "field is required with deviceId", "non-empty string with at most 128 characters")
	}
	return value, nil
}

func decodeObject(raw json.RawMessage) (map[string]json.RawMessage, *FieldError) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var fields map[string]json.RawMessage
	if len(bytes.TrimSpace(raw)) == 0 || decoder.Decode(&fields) != nil || fields == nil {
		detail := FieldError{Field: "$", Reason: "arguments must be one JSON object", Expected: "object"}
		return nil, &detail
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		detail := FieldError{Field: "$", Reason: "arguments must contain exactly one JSON object", Expected: "object"}
		return nil, &detail
	}
	return fields, nil
}

func rejectUnknown(fields map[string]json.RawMessage, allowed ...string) []FieldError {
	set := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		set[name] = struct{}{}
	}
	unknown := make([]string, 0)
	for name := range fields {
		if _, ok := set[name]; !ok {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fieldError(unknown[0], "additional property is not allowed", "only documented properties")
}

func requiredString(fields map[string]json.RawMessage, name string, target *string, expected string) *FieldError {
	raw, exists := fields[name]
	if !exists {
		return &FieldError{Field: name, Reason: "required field is missing", Expected: expected}
	}
	if json.Unmarshal(raw, target) != nil || *target == "" {
		return &FieldError{Field: name, Reason: "value has invalid type or is empty", Expected: expected}
	}
	return nil
}

func decodeRiskTags(fields map[string]json.RawMessage) ([]string, []FieldError) {
	raw, exists := fields["riskTags"]
	if !exists {
		return nil, fieldError("riskTags", "required field is missing", "array of known risk tag strings")
	}
	var tags []string
	if json.Unmarshal(raw, &tags) != nil || tags == nil {
		return nil, fieldError("riskTags", "value has invalid type", "array of known risk tag strings")
	}
	if len(tags) > len(riskTags) {
		return nil, fieldError("riskTags", "too many values", "at most 8 unique known risk tags")
	}
	seen := map[string]struct{}{}
	for index, tag := range tags {
		if _, ok := riskTagSet[tag]; !ok {
			return nil, fieldError(fmt.Sprintf("riskTags[%d]", index), "unknown enum value", "push, deploy, release, delete, destructive, credential, payment, or external_message")
		}
		if _, duplicate := seen[tag]; duplicate {
			return nil, fieldError(fmt.Sprintf("riskTags[%d]", index), "duplicate value", "unique risk tags")
		}
		seen[tag] = struct{}{}
	}
	return tags, nil
}

func fieldError(field, reason, expected string) []FieldError {
	return []FieldError{{Field: field, Reason: reason, Expected: expected}}
}

func stringProperty(min, max int) map[string]any {
	return map[string]any{"type": "string", "minLength": min, "maxLength": max}
}

func riskTagsProperty() map[string]any {
	values := make([]any, len(riskTags))
	for index, value := range riskTags {
		values[index] = value
	}
	return map[string]any{"type": "array", "maxItems": 8, "uniqueItems": true, "items": map[string]any{"type": "string", "enum": values}}
}

func startTaskSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"deviceId", "cwd", "goal", "prompt", "riskTags", "clientTaskId"},
		"properties": map[string]any{
			"deviceId":     stringProperty(1, maxIDLength),
			"cwd":          map[string]any{"type": "string", "minLength": 1, "maxLength": maxCWDLength, "pattern": "^/"},
			"goal":         stringProperty(1, maxGoalLength),
			"prompt":       stringProperty(1, maxTextLength),
			"riskTags":     riskTagsProperty(),
			"clientTaskId": map[string]any{"type": "string", "format": "uuid", "pattern": uuidPattern.String()},
		},
		"examples": []any{map[string]any{"deviceId": "desktop-local", "cwd": "/workspace/fixture", "goal": "验证测试", "prompt": "运行无敏感内容的测试并总结结果", "riskTags": []any{}, "clientTaskId": "00000000-0000-4000-8000-000000000001"}},
	}
}

func sendTurnSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"text", "riskTags", "clientMessageId"},
		"properties": map[string]any{
			"taskId":          stringProperty(1, maxIDLength),
			"deviceId":        stringProperty(1, maxIDLength),
			"threadId":        stringProperty(1, maxIDLength),
			"text":            stringProperty(1, maxTextLength),
			"riskTags":        riskTagsProperty(),
			"clientMessageId": map[string]any{"type": "string", "format": "uuid", "pattern": uuidPattern.String()},
		},
		"oneOf": []any{
			map[string]any{"required": []string{"taskId"}, "not": map[string]any{"anyOf": []any{map[string]any{"required": []string{"deviceId"}}, map[string]any{"required": []string{"threadId"}}}}},
			map[string]any{"required": []string{"deviceId", "threadId"}, "not": map[string]any{"required": []string{"taskId"}}},
		},
		"examples": []any{
			map[string]any{"taskId": "task-a", "text": "继续检查失败测试", "riskTags": []any{}, "clientMessageId": "00000000-0000-4000-8000-000000000002"},
			map[string]any{"deviceId": "desktop-local", "threadId": "thread-a", "text": "继续检查失败测试", "riskTags": []any{}, "clientMessageId": "00000000-0000-4000-8000-000000000003"},
		},
	}
}
