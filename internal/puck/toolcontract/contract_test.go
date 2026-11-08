package toolcontract

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestMutationSchemasAreStrictAndMachineReadable(t *testing.T) {
	for _, name := range []string{"start_task", "send_turn"} {
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
		if len(definition.InputSchema["required"].([]string)) == 0 || len(definition.InputSchema["examples"].([]any)) == 0 {
			t.Fatalf("%s lacks required/examples", name)
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
