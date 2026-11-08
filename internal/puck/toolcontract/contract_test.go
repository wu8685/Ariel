package toolcontract

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestMutationSchemasAreStrictAndMachineReadable(t *testing.T) {
	for _, name := range []string{"start_task", "send_turn", "ask_user", "share_result", "request_authorization"} {
		definition, ok := DefinitionFor(Version, name)
		if !ok {
			t.Fatalf("missing definition for %s", name)
		}
		body, err := json.Marshal(definition.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
		if err != nil {
			t.Fatalf("%s schema: %v", name, err)
		}
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource("schema.json", doc); err != nil {
			t.Fatal(err)
		}
		if _, err := compiler.Compile("schema.json"); err != nil {
			t.Fatalf("compile %s: %v", name, err)
		}
		if definition.InputSchema["additionalProperties"] != false {
			t.Fatalf("%s accepts arbitrary properties: %+v", name, definition.InputSchema)
		}
		required, _ := definition.InputSchema["required"].([]string)
		_, hasOneOf := definition.InputSchema["oneOf"]
		if (len(required) == 0 && !hasOneOf) || len(definition.InputSchema["examples"].([]any)) == 0 {
			t.Fatalf("%s lacks required/examples", name)
		}
	}
}

func TestAskUserNormalizesSingleAndMultipleQuestions(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int
	}{
		{"single", `{"question":"部署到哪里？","options":[{"label":"测试","description":"先验证","value":"staging"}],"allowCustom":true}`, 1},
		{"multiple", `{"questions":[{"id":"entry","question":"从哪里进入？","options":[{"label":"Desktop"}]},{"id":"mode","question":"选择模式","options":[{"label":"安全"}],"multiSelect":true}]}`, 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, details := DecodeAskUser(json.RawMessage(test.raw))
			if len(details) != 0 || len(value.Questions) != test.want {
				t.Fatalf("value=%+v details=%+v", value, details)
			}
			if value.Questions[0].ID == "" || value.Questions[0].Options[0].Value == "" {
				t.Fatalf("normalization did not fill ids and values: %+v", value)
			}
		})
	}
}

func TestStructuredCardDecodersRejectInvalidInput(t *testing.T) {
	tests := []struct{ tool, raw, field string }{
		{"ask_user", `{"question":"Q","options":[],"questions":[]}`, "question"},
		{"ask_user", `{"questions":[{"id":"same","question":"Q1","options":[{"label":"A"}]},{"id":"same","question":"Q2","options":[{"label":"B"}]}]}`, "questions[1].id"},
		{"share_result", `{"summary":"完成","items":[{"kind":"url","title":"结果"}]}`, "items[0].url"},
		{"request_authorization", `{"action":"deploy","reason":"需要部署","cwd":"relative"}`, "cwd"},
	}
	for _, test := range tests {
		_, details := Decode(test.tool, json.RawMessage(test.raw))
		if len(details) == 0 || details[0].Field != test.field {
			t.Fatalf("%s details=%+v", test.tool, details)
		}
	}
}

func TestDynamicToolsMatchAppServerFunctionWireContract(t *testing.T) {
	tools := DynamicTools(Version)
	if len(tools) == 0 {
		t.Fatal("dynamic tool registry is empty")
	}
	for _, tool := range tools {
		if tool["type"] != "function" {
			t.Fatalf("tool %q is not an app-server function: %+v", tool["name"], tool)
		}
		if tool["name"] == "" || tool["description"] == "" || tool["inputSchema"] == nil {
			t.Fatalf("incomplete app-server function tool: %+v", tool)
		}
	}
}

func TestCanonicalDecodersRejectDriftWithFieldDetails(t *testing.T) {
	tests := []struct {
		name, tool, raw, field string
	}{
		{"start empty", "start_task", `{}`, "deviceId"},
		{"start unknown", "start_task", `{"deviceId":"d","cwd":"/w","goal":"g","prompt":"p","riskTags":[],"clientTaskId":"00000000-0000-4000-8000-000000000001","extra":true}`, "extra"},
		{"start risk enum", "start_task", `{"deviceId":"d","cwd":"/w","goal":"g","prompt":"p","riskTags":["mystery"],"clientTaskId":"00000000-0000-4000-8000-000000000001"}`, "riskTags[0]"},
		{"send locator", "send_turn", `{"text":"continue","riskTags":[],"clientMessageId":"00000000-0000-4000-8000-000000000002"}`, "taskId"},
		{"send mixed locator", "send_turn", `{"taskId":"t","deviceId":"d","threadId":"th","text":"continue","riskTags":[],"clientMessageId":"00000000-0000-4000-8000-000000000002"}`, "taskId"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, details := Decode(test.tool, json.RawMessage(test.raw))
			if len(details) == 0 || details[0].Field != test.field || details[0].Reason == "" || details[0].Expected == "" {
				t.Fatalf("details=%+v", details)
			}
		})
	}
}

func TestCanonicalDecodersReturnTypedRequests(t *testing.T) {
	start, details := DecodeStartTask(json.RawMessage(`{"deviceId":"exec-a","cwd":"/fixture","goal":"test","prompt":"run","riskTags":[],"clientTaskId":"00000000-0000-4000-8000-000000000001"}`))
	if len(details) != 0 || start.DeviceID != "exec-a" || start.CWD != "/fixture" {
		t.Fatalf("start=%+v details=%+v", start, details)
	}
	send, details := DecodeSendTurn(json.RawMessage(`{"taskId":"task-a","text":"continue","riskTags":[],"clientMessageId":"00000000-0000-4000-8000-000000000002"}`))
	if len(details) != 0 || send.TaskID != "task-a" || send.Text != "continue" {
		t.Fatalf("send=%+v details=%+v", send, details)
	}
}

func TestStartTaskValidationMatrix(t *testing.T) {
	valid := map[string]any{
		"deviceId": "exec-a", "cwd": "/fixture", "goal": "test", "prompt": "run",
		"riskTags": []any{}, "clientTaskId": "00000000-0000-4000-8000-000000000001",
	}
	for _, field := range []string{"deviceId", "cwd", "goal", "prompt", "riskTags", "clientTaskId"} {
		t.Run("missing "+field, func(t *testing.T) {
			input := cloneMap(valid)
			delete(input, field)
			_, details := DecodeStartTask(marshal(input))
			if len(details) == 0 || details[0].Field != field {
				t.Fatalf("details=%+v", details)
			}
		})
	}
	tests := []struct {
		name, field string
		mutate      func(map[string]any)
	}{
		{"relative cwd", "cwd", func(v map[string]any) { v["cwd"] = "relative" }},
		{"wrong prompt type", "prompt", func(v map[string]any) { v["prompt"] = 7 }},
		{"long prompt", "prompt", func(v map[string]any) { v["prompt"] = string(make([]byte, 65537)) }},
		{"wrong risk type", "riskTags", func(v map[string]any) { v["riskTags"] = "deploy" }},
		{"duplicate risk", "riskTags[1]", func(v map[string]any) { v["riskTags"] = []any{"deploy", "deploy"} }},
		{"bad uuid", "clientTaskId", func(v map[string]any) { v["clientTaskId"] = "task-a" }},
		{"attachment unsupported", "attachments", func(v map[string]any) { v["attachments"] = []any{} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := cloneMap(valid)
			test.mutate(input)
			_, details := DecodeStartTask(marshal(input))
			if len(details) == 0 || details[0].Field != test.field {
				t.Fatalf("details=%+v", details)
			}
		})
	}
}

func TestSendTurnValidationMatrix(t *testing.T) {
	valid := map[string]any{
		"taskId": "task-a", "text": "continue", "riskTags": []any{},
		"clientMessageId": "00000000-0000-4000-8000-000000000002",
	}
	for _, field := range []string{"text", "riskTags", "clientMessageId"} {
		t.Run("missing "+field, func(t *testing.T) {
			input := cloneMap(valid)
			delete(input, field)
			_, details := DecodeSendTurn(marshal(input))
			if len(details) == 0 || details[0].Field != field {
				t.Fatalf("details=%+v", details)
			}
		})
	}
	tests := []struct {
		name, field string
		mutate      func(map[string]any)
	}{
		{"empty text", "text", func(v map[string]any) { v["text"] = "" }},
		{"long text", "text", func(v map[string]any) { v["text"] = string(make([]byte, 65537)) }},
		{"device without thread", "threadId", func(v map[string]any) { delete(v, "taskId"); v["deviceId"] = "exec-a" }},
		{"thread without device", "deviceId", func(v map[string]any) { delete(v, "taskId"); v["threadId"] = "thread-a" }},
		{"unknown risk", "riskTags[0]", func(v map[string]any) { v["riskTags"] = []any{"unknown"} }},
		{"unknown field", "queue", func(v map[string]any) { v["queue"] = true }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := cloneMap(valid)
			test.mutate(input)
			_, details := DecodeSendTurn(marshal(input))
			if len(details) == 0 || details[0].Field != test.field {
				t.Fatalf("details=%+v", details)
			}
		})
	}
}

func TestStartTaskDescriptionDelegatesAuthorizationToRelay(t *testing.T) {
	definition, ok := DefinitionFor(Version, "start_task")
	if !ok || !strings.Contains(definition.Description, "授权由 Relay") || !strings.Contains(definition.Description, "无论") {
		t.Fatalf("description=%q", definition.Description)
	}
}

func cloneMap(input map[string]any) map[string]any {
	copy := make(map[string]any, len(input))
	for key, value := range input {
		copy[key] = value
	}
	return copy
}

func marshal(value any) json.RawMessage {
	body, _ := json.Marshal(value)
	return body
}
