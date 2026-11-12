package toolcontract

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const Version = 12
const canonicalMutationToolVersion = 2

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
	DeviceID         string   `json:"deviceId"`
	CWD              string   `json:"cwd"`
	Goal             string   `json:"goal"`
	Prompt           string   `json:"prompt"`
	RiskTags         []string `json:"riskTags"`
	ClientTaskID     string   `json:"clientTaskId"`
	IncludeMessages  []string `json:"includeMessages,omitempty"`
	ReferenceThreads []string `json:"referenceThreads,omitempty"`
}

type SendTurnRequest struct {
	TaskID           string   `json:"taskId,omitempty"`
	DeviceID         string   `json:"deviceId,omitempty"`
	ThreadID         string   `json:"threadId,omitempty"`
	Text             string   `json:"text"`
	RiskTags         []string `json:"riskTags"`
	ClientMessageID  string   `json:"clientMessageId"`
	IncludeMessages  []string `json:"includeMessages,omitempty"`
	ReferenceThreads []string `json:"referenceThreads,omitempty"`
}

type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Value       string `json:"value"`
}

type Question struct {
	ID          string           `json:"id"`
	Question    string           `json:"question"`
	Options     []QuestionOption `json:"options"`
	AllowCustom bool             `json:"allowCustom"`
	MultiSelect bool             `json:"multiSelect"`
}

type AskUserRequest struct {
	Questions []Question `json:"questions"`
}

type ResultItem struct {
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	DeviceID    string `json:"deviceId,omitempty"`
	ThreadID    string `json:"threadId,omitempty"`
	Path        string `json:"path,omitempty"`
	URL         string `json:"url,omitempty"`
}
type ShareResultRequest struct {
	Summary string       `json:"summary"`
	Items   []ResultItem `json:"items"`
}

type AuthorizationRequest struct {
	Action   string `json:"action"`
	Reason   string `json:"reason"`
	DeviceID string `json:"deviceId,omitempty"`
	CWD      string `json:"cwd,omitempty"`
	Summary  string `json:"summary,omitempty"`
}

type Definition struct {
	Name        string
	Description string
	InputSchema map[string]any
}

func DefinitionFor(version int, name string) (Definition, bool) {
	if version < canonicalMutationToolVersion {
		return Definition{}, false
	}
	switch name {
	case "start_task":
		return Definition{Name: name, Description: "在指定设备和已存在的绝对 workspace 中创建执行任务，并原子发送首条输入。如果可信上下文里已有同一 deviceId 与 cwd 的可复用执行线程，应改调用 send_turn；即使用户没说新会话，Relay 也会把 start_task 续投到该线程。只有设备或目录不同、旧线程不可用，或用户明确要求新会话／独立任务时才新建。cwd 必须直接使用 list_threads/read_thread 返回的值或用户确认的路径，不得猜测 /workspace 等路径。taskId 由 Relay 分配，不要生成 clientTaskId。无论 Bot 是 Full Access 还是每次询问模式，只要需要目标 workspace 实际执行就调用；授权由 Relay 处理，调用本工具不代表绕过授权。", InputSchema: startTaskSchema(version)}, true
	case "send_turn":
		return Definition{Name: name, Description: "通过 Puck task 或显式设备与 thread 定位，发送一个幂等 follow-up turn。clientMessageId 可省略；给出合法 UUID 时用它去重，否则由 Relay 生成。", InputSchema: sendTurnSchema(version)}, true
	case "ask_user":
		return Definition{Name: name, Description: "向用户展示结构化问题卡。通常只传 question 与 options；确需同时决定多个相关事项时才传 questions。必须使用本工具，不要调用 Codex request_user_input。", InputSchema: askUserSchema()}, true
	case "share_result":
		return Definition{Name: name, Description: "以结构化结果卡分享已经验证的文件、Diff、PR、thread 或 URL，不要在正文倾倒原始 JSON。", InputSchema: shareResultSchema()}, true
	case "request_authorization":
		return Definition{Name: name, Description: "请求用户授权敏感动作；说明 action、reason，并尽量提供 deviceId、绝对 cwd 与摘要供卡片展示。", InputSchema: authorizationSchema()}, true
	case "watch_thread":
		return Definition{Name: name, Description: "只读监听一条本助理没有启动的现有 Codex 会话。deviceId 可省略：这条会话只绑定过一台设备、只有一台设备在线，或能确定大脑自己的主机时会自动选。until 写用户要等的结果，例如“发布完成”或“卡住了”。中间进度不会打扰用户；出现明确结果、失败、卡住或停止时才通知，然后取消监听。不会发送 turn，也不授予执行权。只有返回 watching 为 true 之后，才能向用户承诺会主动通知。", InputSchema: watchThreadSchema()}, true
	case "finish_task":
		if version < 8 {
			return Definition{}, false
		}
		return Definition{Name: name, Description: "任务有明确结果后归档本助理自己的任务，并关闭这条执行线程上本助理的 watch 与尚未投递的唤醒。不删除对话历史，不发送 turn，不取消别人的任务。调用前如果曾在该执行会话里创建定时或周期任务，先用 send_turn 让那个会话关掉它。", InputSchema: finishTaskSchema()}, true
	case "send_message":
		if version < 9 {
			return Definition{}, false
		}
		return Definition{Name: name, Description: "把一句你决定要告诉用户的话写进这个 Bot 的 Ariel 对话，并触发页内通知。只用于被唤醒、定时、automation 或你自己发起的工作。回答带 PUCK_ENVELOPE 的用户消息时直接写回复，不要为同一句话再调用本工具。不能指定别的助理。", InputSchema: sendMessageSchema()}, true
	case "search_messages", "attach_bot_task":
		if version < 11 || version >= 12 {
			return Definition{}, false
		}
		return botTaskDefinition(name)
	case "list_bot_tasks", "save_bot_task", "merge_bot_tasks":
		if version < 11 {
			return Definition{}, false
		}
		return botTaskDefinition(name)
	default:
		return Definition{}, false
	}
}

func DynamicTools(version int) []map[string]any {
	names := []string{"list_devices", "list_threads", "read_thread", "start_task", "send_turn", "interrupt", "respond_interaction", "get_task_status", "watch_thread", "request_authorization", "ask_user", "share_result", "react"}
	if version >= 8 {
		names = append(names, "finish_task")
	}
	if version >= 9 {
		names = append(names, "send_message")
	}
	if version >= 12 {
		names = append(names, "list_bot_tasks", "save_bot_task", "merge_bot_tasks")
	} else if version >= 11 {
		names = append(names, "list_bot_tasks", "search_messages", "save_bot_task", "attach_bot_task", "merge_bot_tasks")
	}
	tools := make([]map[string]any, 0, len(names))
	for _, name := range names {
		description := "Ariel Puck tool: " + name
		schema := map[string]any{"type": "object", "additionalProperties": true}
		if definition, ok := DefinitionFor(version, name); ok {
			description, schema = definition.Description, definition.InputSchema
		}
		tools = append(tools, map[string]any{"type": "function", "name": name, "description": description, "inputSchema": schema})
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
	case "ask_user":
		value, details := DecodeAskUser(raw)
		return value, details
	case "share_result":
		value, details := DecodeShareResult(raw)
		return value, details
	case "request_authorization":
		value, details := DecodeAuthorization(raw)
		return value, details
	case "finish_task":
		value, details := DecodeFinishTask(raw)
		return value, details
	case "send_message":
		value, details := DecodeSendMessage(raw)
		return value, details
	case "list_bot_tasks":
		value, details := DecodeListBotTasks(raw)
		return value, details
	case "search_messages":
		value, details := DecodeSearchMessages(raw)
		return value, details
	case "save_bot_task":
		value, details := DecodeSaveBotTask(raw)
		return value, details
	case "attach_bot_task":
		value, details := DecodeAttachBotTask(raw)
		return value, details
	case "merge_bot_tasks":
		value, details := DecodeMergeBotTasks(raw)
		return value, details
	default:
		return nil, []FieldError{{Field: "tool", Reason: "unknown tool contract", Expected: "documented Puck tool"}}
	}
}

func DecodeAskUser(raw json.RawMessage) (AskUserRequest, []FieldError) {
	var result AskUserRequest
	fields, detail := decodeObject(raw)
	if detail != nil {
		return result, []FieldError{*detail}
	}
	if details := normalizeAliases(fields,
		aliasSet{"question", []string{"prompt", "text"}},
		aliasSet{"options", []string{"choices"}},
		aliasSet{"multiSelect", []string{"multiple"}},
		aliasSet{"allowCustom", []string{"allowFreeText", "allow_custom"}},
	); len(details) != 0 {
		return result, details
	}
	if details := rejectUnknown(fields, "question", "options", "allowCustom", "multiSelect", "questions"); len(details) != 0 {
		return result, details
	}
	_, hasSingle := fields["question"]
	_, hasMany := fields["questions"]
	if hasSingle == hasMany {
		return result, fieldError("question", "single question and questions are mutually exclusive", "question or questions, but not both")
	}
	if hasSingle {
		question, details := decodeQuestion(fields, "", "question")
		if len(details) != 0 {
			return result, details
		}
		question.ID = "question-1"
		result.Questions = []Question{question}
		return result, nil
	}
	var entries []json.RawMessage
	if json.Unmarshal(fields["questions"], &entries) != nil || len(entries) == 0 || len(entries) > 10 {
		return result, fieldError("questions", "value has invalid type or length", "array with 1 to 10 questions")
	}
	seen := map[string]struct{}{}
	for index, entry := range entries {
		questionFields, itemDetail := decodeObject(entry)
		if itemDetail != nil {
			return result, fieldError(fmt.Sprintf("questions[%d]", index), itemDetail.Reason, "question object")
		}
		if details := normalizeQuestionAliases(questionFields); len(details) != 0 {
			details[0].Field = fmt.Sprintf("questions[%d].%s", index, details[0].Field)
			return result, details
		}
		question, details := decodeQuestion(questionFields, fmt.Sprintf("questions[%d].", index), "question")
		if len(details) != 0 {
			return result, details
		}
		if rawID, ok := questionFields["id"]; ok {
			if json.Unmarshal(rawID, &question.ID) != nil || strings.TrimSpace(question.ID) == "" || utf8.RuneCountInString(question.ID) > maxIDLength {
				return result, fieldError(fmt.Sprintf("questions[%d].id", index), "value has invalid type or length", "non-empty string with at most 128 characters")
			}
		} else {
			question.ID = fmt.Sprintf("question-%d", index+1)
		}
		if _, duplicate := seen[question.ID]; duplicate {
			return result, fieldError(fmt.Sprintf("questions[%d].id", index), "duplicate value", "unique question id")
		}
		seen[question.ID] = struct{}{}
		result.Questions = append(result.Questions, question)
	}
	return result, nil
}

func decodeQuestion(fields map[string]json.RawMessage, prefix, questionField string) (Question, []FieldError) {
	var result Question
	if details := normalizeQuestionAliases(fields); len(details) != 0 {
		details[0].Field = prefix + details[0].Field
		return result, details
	}
	var selectionType *bool
	if rawType, ok := fields["type"]; ok {
		delete(fields, "type")
		var value string
		if json.Unmarshal(rawType, &value) != nil {
			return result, fieldError(prefix+"type", "value has invalid type", "single-select or multi-select")
		}
		multi := false
		switch value {
		case "single-select":
		case "multi-select":
			multi = true
		default:
			return result, fieldError(prefix+"type", "value is not supported", "single-select or multi-select")
		}
		selectionType = &multi
	}
	if details := rejectUnknown(fields, "id", "question", "options", "allowCustom", "multiSelect"); len(details) != 0 {
		details[0].Field = prefix + details[0].Field
		return result, details
	}
	if detail := requiredString(fields, questionField, &result.Question, "non-empty string with at most 2048 characters"); detail != nil {
		detail.Field = prefix + detail.Field
		return result, []FieldError{*detail}
	}
	if utf8.RuneCountInString(result.Question) > 2048 {
		return result, fieldError(prefix+"question", "value is too long", "at most 2048 characters")
	}
	for name, target := range map[string]*bool{"allowCustom": &result.AllowCustom, "multiSelect": &result.MultiSelect} {
		if value, ok := fields[name]; ok && json.Unmarshal(value, target) != nil {
			return result, fieldError(prefix+name, "value has invalid type", "boolean")
		}
	}
	if selectionType != nil {
		if _, explicit := fields["multiSelect"]; explicit && result.MultiSelect != *selectionType {
			return result, fieldError(prefix+"type", "value conflicts with multiSelect", "selection type matching multiSelect")
		}
		result.MultiSelect = *selectionType
	}
	var optionEntries []json.RawMessage
	if rawOptions, ok := fields["options"]; !ok || json.Unmarshal(rawOptions, &optionEntries) != nil || len(optionEntries) > 20 || (len(optionEntries) == 0 && !result.AllowCustom) {
		return result, fieldError(prefix+"options", "value has invalid type or length", "1 to 20 options, or empty when allowCustom is true")
	}
	seen := map[string]struct{}{}
	for index, rawOption := range optionEntries {
		var optionFields map[string]json.RawMessage
		var label string
		if json.Unmarshal(rawOption, &label) == nil {
			encoded, _ := json.Marshal(label)
			optionFields = map[string]json.RawMessage{"label": encoded, "value": encoded}
		} else if json.Unmarshal(rawOption, &optionFields) != nil || optionFields == nil {
			return result, fieldError(fmt.Sprintf("%soptions[%d]", prefix, index), "value has invalid type", "string or option object")
		}
		if details := normalizeAliases(optionFields,
			aliasSet{"label", []string{"text", "name"}},
			aliasSet{"value", []string{"id"}},
			aliasSet{"description", []string{"help"}},
		); len(details) != 0 {
			return result, fieldError(fmt.Sprintf("%soptions[%d].%s", prefix, index, details[0].Field), details[0].Reason, details[0].Expected)
		}
		if details := rejectUnknown(optionFields, "label", "description", "value"); len(details) != 0 {
			return result, fieldError(fmt.Sprintf("%soptions[%d].%s", prefix, index, details[0].Field), details[0].Reason, details[0].Expected)
		}
		var option QuestionOption
		if detail := requiredString(optionFields, "label", &option.Label, "non-empty string with at most 256 characters"); detail != nil {
			return result, fieldError(fmt.Sprintf("%soptions[%d].label", prefix, index), detail.Reason, detail.Expected)
		}
		if rawValue, ok := optionFields["value"]; ok {
			if json.Unmarshal(rawValue, &option.Value) != nil || option.Value == "" {
				return result, fieldError(fmt.Sprintf("%soptions[%d].value", prefix, index), "value has invalid type or is empty", "non-empty string")
			}
		} else {
			option.Value = option.Label
		}
		if rawDescription, ok := optionFields["description"]; ok && json.Unmarshal(rawDescription, &option.Description) != nil {
			return result, fieldError(fmt.Sprintf("%soptions[%d].description", prefix, index), "value has invalid type", "string")
		}
		if _, duplicate := seen[option.Value]; duplicate {
			return result, fieldError(fmt.Sprintf("%soptions[%d].value", prefix, index), "duplicate value", "unique option value")
		}
		seen[option.Value] = struct{}{}
		result.Options = append(result.Options, option)
	}
	return result, nil
}

func DecodeShareResult(raw json.RawMessage) (ShareResultRequest, []FieldError) {
	var result ShareResultRequest
	fields, detail := decodeObject(raw)
	if detail != nil {
		return result, []FieldError{*detail}
	}
	if details := normalizeAliases(fields,
		aliasSet{"summary", []string{"message", "text"}},
		aliasSet{"items", []string{"results", "attachments"}},
	); len(details) != 0 {
		return result, details
	}
	if rawItem, ok := fields["item"]; ok {
		if _, exists := fields["items"]; exists {
			return result, fieldError("items", "canonical field conflicts with alias item", "one equivalent representation")
		}
		fields["items"] = append(json.RawMessage{'['}, append(rawItem, ']')...)
		delete(fields, "item")
	}
	if details := rejectUnknown(fields, "summary", "items"); len(details) != 0 {
		return result, details
	}
	if detail := requiredString(fields, "summary", &result.Summary, "non-empty string"); detail != nil {
		return result, []FieldError{*detail}
	}
	var items []map[string]json.RawMessage
	if json.Unmarshal(fields["items"], &items) != nil || len(items) == 0 || len(items) > 20 {
		return result, fieldError("items", "value has invalid type or length", "array with 1 to 20 result items")
	}
	allowedKinds := map[string]bool{"diff": true, "file": true, "pr": true, "thread": true, "url": true}
	for index, item := range items {
		if details := normalizeAliases(item,
			aliasSet{"kind", []string{"type"}},
			aliasSet{"title", []string{"label", "name"}},
			aliasSet{"description", []string{"text"}},
			aliasSet{"deviceId", []string{"device_id"}},
			aliasSet{"threadId", []string{"thread_id"}},
			aliasSet{"path", []string{"filePath", "file_path"}},
			aliasSet{"url", []string{"href"}},
		); len(details) != 0 {
			return result, fieldError(fmt.Sprintf("items[%d].%s", index, details[0].Field), details[0].Reason, details[0].Expected)
		}
		if details := rejectUnknown(item, "kind", "title", "description", "deviceId", "threadId", "path", "url"); len(details) != 0 {
			return result, fieldError(fmt.Sprintf("items[%d].%s", index, details[0].Field), details[0].Reason, details[0].Expected)
		}
		value := ResultItem{}
		for name, target := range map[string]*string{"kind": &value.Kind, "title": &value.Title, "description": &value.Description, "deviceId": &value.DeviceID, "threadId": &value.ThreadID, "path": &value.Path, "url": &value.URL} {
			if rawValue, ok := item[name]; ok {
				if json.Unmarshal(rawValue, target) != nil {
					return result, fieldError(fmt.Sprintf("items[%d].%s", index, name), "value has invalid type", "string")
				}
			}
		}
		if !allowedKinds[value.Kind] {
			return result, fieldError(fmt.Sprintf("items[%d].kind", index), "unknown enum value", "diff, file, pr, thread, or url")
		}
		if value.Title == "" {
			return result, fieldError(fmt.Sprintf("items[%d].title", index), "required field is missing or empty", "non-empty string")
		}
		if value.Kind == "url" || value.Kind == "pr" {
			if value.URL == "" {
				return result, fieldError(fmt.Sprintf("items[%d].url", index), "required locator is missing", "URL")
			}
		}
		if value.Kind == "thread" && (value.DeviceID == "" || value.ThreadID == "") {
			return result, fieldError(fmt.Sprintf("items[%d].threadId", index), "required locator is missing", "deviceId and threadId")
		}
		if (value.Kind == "file" || value.Kind == "diff") && (value.DeviceID == "" || value.Path == "") {
			return result, fieldError(fmt.Sprintf("items[%d].path", index), "required locator is missing", "deviceId and path")
		}
		result.Items = append(result.Items, value)
	}
	return result, nil
}

func DecodeAuthorization(raw json.RawMessage) (AuthorizationRequest, []FieldError) {
	var result AuthorizationRequest
	fields, detail := decodeObject(raw)
	if detail != nil {
		return result, []FieldError{*detail}
	}
	if details := normalizeAliases(fields,
		aliasSet{"action", []string{"operation"}},
		aliasSet{"reason", []string{"justification", "message"}},
		aliasSet{"summary", []string{"text", "description"}},
		aliasSet{"deviceId", []string{"device_id"}},
		aliasSet{"cwd", []string{"workdir", "workingDirectory"}},
	); len(details) != 0 {
		return result, details
	}
	if details := rejectUnknown(fields, "action", "reason", "deviceId", "cwd", "summary"); len(details) != 0 {
		return result, details
	}
	if detail := requiredString(fields, "action", &result.Action, "non-empty string"); detail != nil {
		return result, []FieldError{*detail}
	}
	for name, target := range map[string]*string{"deviceId": &result.DeviceID, "cwd": &result.CWD, "summary": &result.Summary} {
		if value, ok := fields[name]; ok && (json.Unmarshal(value, target) != nil || *target == "") {
			return result, fieldError(name, "value has invalid type or is empty", "non-empty string")
		}
	}
	if rawReason, ok := fields["reason"]; ok {
		if json.Unmarshal(rawReason, &result.Reason) != nil || result.Reason == "" {
			return result, fieldError("reason", "value has invalid type or is empty", "non-empty string")
		}
	} else if result.Summary != "" {
		result.Reason = result.Summary // v2 compatibility; v3 schema still requires reason.
	} else {
		return result, fieldError("reason", "required field is missing", "non-empty string")
	}
	if result.CWD != "" && !filepath.IsAbs(result.CWD) {
		return result, fieldError("cwd", "value must be absolute", "absolute path")
	}
	return result, nil
}

func DecodeStartTask(raw json.RawMessage) (StartTaskRequest, []FieldError) {
	var value StartTaskRequest
	fields, detail := decodeObject(raw)
	if detail != nil {
		return value, []FieldError{*detail}
	}
	if details := rejectUnknown(fields, "deviceId", "cwd", "goal", "prompt", "riskTags", "clientTaskId", "includeMessages", "referenceThreads"); len(details) != 0 {
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
	value.ClientTaskID = optionalUUID(fields, "clientTaskId")
	tags, details := decodeRiskTags(fields)
	if len(details) != 0 {
		return value, details
	}
	value.RiskTags = tags
	if details = decodeOptionalIDs(fields, "includeMessages", &value.IncludeMessages); len(details) != 0 {
		return value, details
	}
	if details = decodeOptionalIDs(fields, "referenceThreads", &value.ReferenceThreads); len(details) != 0 {
		return value, details
	}
	return value, nil
}

func DecodeSendTurn(raw json.RawMessage) (SendTurnRequest, []FieldError) {
	var value SendTurnRequest
	fields, detail := decodeObject(raw)
	if detail != nil {
		return value, []FieldError{*detail}
	}
	if details := rejectUnknown(fields, "taskId", "deviceId", "threadId", "text", "riskTags", "clientMessageId", "includeMessages", "referenceThreads"); len(details) != 0 {
		return value, details
	}
	if detail := requiredString(fields, "text", &value.Text, "non-empty string with at most 65536 characters"); detail != nil {
		return value, []FieldError{*detail}
	}
	if utf8.RuneCountInString(value.Text) > maxTextLength {
		return value, fieldError("text", "value is too long", "non-empty string with at most 65536 characters")
	}
	value.ClientMessageID = optionalUUID(fields, "clientMessageId")
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
	if details = decodeOptionalIDs(fields, "includeMessages", &value.IncludeMessages); len(details) != 0 {
		return value, details
	}
	if details = decodeOptionalIDs(fields, "referenceThreads", &value.ReferenceThreads); len(details) != 0 {
		return value, details
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

type aliasSet struct {
	canonical string
	aliases   []string
}

func normalizeQuestionAliases(fields map[string]json.RawMessage) []FieldError {
	return normalizeAliases(fields,
		aliasSet{"question", []string{"prompt", "text"}},
		aliasSet{"options", []string{"choices"}},
		aliasSet{"multiSelect", []string{"multiple"}},
		aliasSet{"allowCustom", []string{"allowFreeText", "allow_custom"}},
	)
}

func normalizeAliases(fields map[string]json.RawMessage, sets ...aliasSet) []FieldError {
	for _, set := range sets {
		canonical, hasCanonical := fields[set.canonical]
		for _, alias := range set.aliases {
			value, exists := fields[alias]
			if !exists {
				continue
			}
			if hasCanonical && !jsonSemanticEqual(canonical, value) {
				return fieldError(set.canonical, "canonical field conflicts with alias "+alias, "one equivalent representation")
			}
			if !hasCanonical {
				fields[set.canonical] = value
				canonical, hasCanonical = value, true
			}
			delete(fields, alias)
		}
	}
	return nil
}

func jsonSemanticEqual(left, right json.RawMessage) bool {
	var a, b any
	return json.Unmarshal(left, &a) == nil && json.Unmarshal(right, &b) == nil && reflect.DeepEqual(a, b)
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

func decodeOptionalIDs(fields map[string]json.RawMessage, name string, target *[]string) []FieldError {
	raw, ok := fields[name]
	if !ok {
		return nil
	}
	var values []string
	if json.Unmarshal(raw, &values) != nil || values == nil {
		return fieldError(name, "value has invalid type", "array of at most 8 strings")
	}
	if len(values) > 8 {
		return fieldError(name, "too many values", "at most 8 strings")
	}
	for index, value := range values {
		if strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > maxIDLength {
			return fieldError(fmt.Sprintf("%s[%d]", name, index), "value has invalid type or length", "non-empty string with at most 128 characters")
		}
	}
	*target = values
	return nil
}

func idListSchema(description string) map[string]any {
	return map[string]any{
		"type": "array", "maxItems": 8, "description": description,
		"items": map[string]any{"type": "string", "minLength": 1, "maxLength": maxIDLength},
	}
}

func startTaskSchema(version int) map[string]any {
	properties := map[string]any{
		"deviceId":     stringProperty(1, maxIDLength),
		"cwd":          map[string]any{"type": "string", "minLength": 1, "maxLength": maxCWDLength, "pattern": "^/"},
		"goal":         stringProperty(1, maxGoalLength),
		"prompt":       stringProperty(1, maxTextLength),
		"riskTags":     riskTagsProperty(),
		"clientTaskId": map[string]any{"type": "string", "maxLength": maxIDLength, "description": "可选。合法 UUID 会保留给没有来源消息的调用去重；缺省或不是 UUID 时由 Relay 分配，可信 Brain 调用仍按来源消息分配。"},
	}
	if version >= 10 {
		properties["includeMessages"] = idListSchema("可选。这些 Ariel 消息里点名的、本助理拥有或监听过的会话会作为参考附上，不会把消息正文贴进子会话。")
		properties["referenceThreads"] = idListSchema("可选。本助理自己的 taskId 或监听过的 threadId，作为参考会话附上。")
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"deviceId", "cwd", "goal", "prompt", "riskTags"},
		"properties":           properties,
		"examples":             []any{map[string]any{"deviceId": "desktop-local", "cwd": "/workspace/fixture", "goal": "验证测试", "prompt": "运行无敏感内容的测试并总结结果", "riskTags": []any{}}},
	}
}

func sendTurnSchema(version int) map[string]any {
	properties := map[string]any{
		"taskId":          stringProperty(1, maxIDLength),
		"deviceId":        stringProperty(1, maxIDLength),
		"threadId":        stringProperty(1, maxIDLength),
		"text":            stringProperty(1, maxTextLength),
		"riskTags":        riskTagsProperty(),
		"clientMessageId": map[string]any{"type": "string", "maxLength": maxIDLength, "description": "可选。合法 UUID 会原样保留，用来去重；缺省或不是 UUID 时由 Relay 生成。"},
	}
	if version >= 10 {
		properties["includeMessages"] = idListSchema("可选。这些 Ariel 消息里点名的、本助理拥有或监听过的会话会作为参考附上，不会把消息正文贴进子会话。")
		properties["referenceThreads"] = idListSchema("可选。本助理自己的 taskId 或监听过的 threadId，作为参考会话附上。")
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"text", "riskTags"},
		"properties":           properties,
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

func questionProperties(includeID bool) map[string]any {
	properties := map[string]any{
		"question":    stringProperty(1, 2048),
		"options":     map[string]any{"type": "array", "maxItems": 20, "items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"label"}, "properties": map[string]any{"label": stringProperty(1, 256), "description": stringProperty(1, 1024), "value": stringProperty(1, 256)}}},
		"allowCustom": map[string]any{"type": "boolean"},
		"multiSelect": map[string]any{"type": "boolean"},
	}
	if includeID {
		properties["id"] = stringProperty(1, maxIDLength)
	}
	return properties
}

func askUserSchema() map[string]any {
	return map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"question":    stringProperty(1, 2048),
			"options":     questionProperties(false)["options"],
			"allowCustom": map[string]any{"type": "boolean"},
			"multiSelect": map[string]any{"type": "boolean"},
			"questions":   map[string]any{"type": "array", "minItems": 1, "maxItems": 10, "items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"question", "options"}, "properties": questionProperties(true)}},
		},
		"oneOf": []any{
			map[string]any{"required": []string{"question", "options"}, "not": map[string]any{"required": []string{"questions"}}},
			map[string]any{"required": []string{"questions"}, "not": map[string]any{"required": []string{"question"}}},
		},
		"required": []string{},
		"examples": []any{map[string]any{"question": "选择执行入口", "options": []any{map[string]any{"label": "Codex Desktop", "description": "桌面应用", "value": "desktop"}}, "allowCustom": true}},
	}
}

func shareResultSchema() map[string]any {
	item := map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"kind", "title"},
		"properties": map[string]any{
			"kind":  map[string]any{"type": "string", "enum": []any{"diff", "file", "pr", "thread", "url"}},
			"title": stringProperty(1, 512), "description": stringProperty(1, 2048),
			"deviceId": stringProperty(1, maxIDLength), "threadId": stringProperty(1, maxIDLength),
			"path": stringProperty(1, maxCWDLength), "url": stringProperty(1, 4096),
		},
	}
	return map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"summary", "items"},
		"properties": map[string]any{"summary": stringProperty(1, 2048), "items": map[string]any{"type": "array", "minItems": 1, "maxItems": 20, "items": item}},
		"examples":   []any{map[string]any{"summary": "已生成结果", "items": []any{map[string]any{"kind": "url", "title": "查看结果", "url": "https://example.invalid"}}}},
	}
}

func authorizationSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "required": []string{"action", "reason"}, "properties": map[string]any{
		"action": stringProperty(1, 512), "reason": stringProperty(1, 2048), "deviceId": stringProperty(1, maxIDLength), "cwd": map[string]any{"type": "string", "minLength": 1, "maxLength": maxCWDLength, "pattern": "^/"}, "summary": stringProperty(1, 2048),
	}, "examples": []any{map[string]any{"action": "deploy", "reason": "需要发布已验证版本", "deviceId": "desktop-local", "cwd": "/workspace", "summary": "部署测试环境"}}}
}

type FinishTaskRequest struct {
	TaskID  string `json:"taskId"`
	Outcome string `json:"outcome"`
	Note    string `json:"note,omitempty"`
}

func DecodeFinishTask(raw json.RawMessage) (FinishTaskRequest, []FieldError) {
	var result FinishTaskRequest
	fields, detail := decodeObject(raw)
	if detail != nil {
		return result, []FieldError{*detail}
	}
	if details := rejectUnknown(fields, "taskId", "outcome", "note"); len(details) != 0 {
		return result, details
	}
	if detail := requiredString(fields, "taskId", &result.TaskID, "non-empty string with at most 128 characters"); detail != nil {
		return result, []FieldError{*detail}
	}
	if len(result.TaskID) > maxIDLength {
		return result, fieldError("taskId", "value is too long", "at most 128 characters")
	}
	if detail := requiredString(fields, "outcome", &result.Outcome, "completed, failed, or cancelled"); detail != nil {
		return result, []FieldError{*detail}
	}
	switch result.Outcome {
	case "completed", "failed", "cancelled":
	default:
		return result, fieldError("outcome", "unknown enum value", "completed, failed, or cancelled")
	}
	if rawNote, ok := fields["note"]; ok {
		if json.Unmarshal(rawNote, &result.Note) != nil {
			return result, fieldError("note", "value has invalid type", "string")
		}
		result.Note = strings.TrimSpace(result.Note)
		if result.Note == "" || utf8.RuneCountInString(result.Note) > 240 {
			return result, fieldError("note", "value has invalid length", "1 to 240 characters")
		}
	}
	return result, nil
}

type SendMessageRequest struct {
	Text            string `json:"text"`
	ClientMessageID string `json:"clientMessageId,omitempty"`
}

func DecodeSendMessage(raw json.RawMessage) (SendMessageRequest, []FieldError) {
	var result SendMessageRequest
	fields, detail := decodeObject(raw)
	if detail != nil {
		return result, []FieldError{*detail}
	}
	if details := rejectUnknown(fields, "text", "clientMessageId"); len(details) != 0 {
		return result, details
	}
	if detail := requiredString(fields, "text", &result.Text, "1 to 4000 characters"); detail != nil {
		return result, []FieldError{*detail}
	}
	result.Text = strings.TrimSpace(result.Text)
	if result.Text == "" || utf8.RuneCountInString(result.Text) > 4000 {
		return result, fieldError("text", "value has invalid length", "1 to 4000 characters")
	}
	result.ClientMessageID = optionalUUID(fields, "clientMessageId")
	return result, nil
}

// optionalUUID keeps a valid UUID and drops a missing or invalid one so the
// caller can generate a replacement. Idempotency stays with the UUID the
// caller actually supplied.
func optionalUUID(fields map[string]json.RawMessage, name string) string {
	raw, ok := fields[name]
	if !ok || string(raw) == "null" {
		return ""
	}
	var id string
	if json.Unmarshal(raw, &id) != nil || !uuidPattern.MatchString(strings.TrimSpace(id)) {
		return ""
	}
	return id
}

// FillUUID returns id when it is a UUID and a new UUID otherwise.
func FillUUID(id string) string {
	if uuidPattern.MatchString(id) {
		return id
	}
	return NewUUID()
}

func NewUUID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}

func sendMessageSchema() map[string]any {
	return map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"text"},
		"properties": map[string]any{
			"text":            stringProperty(1, 4000),
			"clientMessageId": map[string]any{"type": "string", "maxLength": maxIDLength, "description": "可选。合法 UUID 会原样保留；缺省或不是 UUID 时由 Relay 生成。"},
		},
	}
}

func finishTaskSchema() map[string]any {
	return map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"taskId", "outcome"},
		"properties": map[string]any{
			"taskId":  stringProperty(1, maxIDLength),
			"outcome": map[string]any{"type": "string", "enum": []any{"completed", "failed", "cancelled"}},
			"note":    stringProperty(1, 240),
		},
		"examples": []any{map[string]any{"taskId": "task-a", "outcome": "completed", "note": "定时检查已关闭"}},
	}
}

func watchThreadSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "required": []string{"threadId"}, "properties": map[string]any{
		"deviceId": map[string]any{"type": "string", "maxLength": maxIDLength, "description": "可选。省略时，优先用这条会话已经绑定的唯一设备，否则用唯一在线设备或大脑自己的主机。"},
		"threadId": stringProperty(1, maxIDLength), "until": stringProperty(1, 240),
	}}
}
