package toolcontract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestMutationSchemasAreStrictAndMachineReadable(t *testing.T) {
	for _, name := range []string{"start_task", "send_turn", "ask_user", "share_result", "request_authorization", "finish_task"} {
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

func TestStartTaskSchemaAssignsIdentityServerSide(t *testing.T) {
	definition, ok := DefinitionFor(Version, "start_task")
	if !ok {
		t.Fatal("start_task definition missing")
	}
	required, _ := definition.InputSchema["required"].([]string)
	if slices.Contains(required, "clientTaskId") {
		t.Fatalf("Brain must not be required to generate clientTaskId: %v", required)
	}
	properties, _ := definition.InputSchema["properties"].(map[string]any)
	if _, ok := properties["clientTaskId"]; !ok {
		t.Fatal("legacy clientTaskId compatibility property missing")
	}
	if Version < 5 {
		t.Fatalf("toolset version=%d must rotate existing Brain threads", Version)
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

func TestAskUserNormalizesObservedSelectionTypeDrift(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		multiSelect bool
	}{
		{"single select", `{"questions":[{"id":"choice","question":"Q","type":"single-select","options":[{"label":"A"}]}]}`, false},
		{"multi select", `{"questions":[{"id":"choice","question":"Q","type":"multi-select","options":[{"label":"A"}]}]}`, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, details := DecodeAskUser(json.RawMessage(test.raw))
			if len(details) != 0 || len(value.Questions) != 1 || value.Questions[0].MultiSelect != test.multiSelect {
				t.Fatalf("value=%+v details=%+v", value, details)
			}
		})
	}
}

func TestAskUserNormalizesCodex0160ObservedTopLevelMultiple(t *testing.T) {
	value, details := DecodeAskUser(json.RawMessage(`{"question":"Q","options":[{"label":"A","value":"a"},{"label":"B","value":"b"}],"multiple":false}`))
	if len(details) != 0 || len(value.Questions) != 1 || value.Questions[0].MultiSelect {
		t.Fatalf("value=%+v details=%+v", value, details)
	}
}

func TestStructuredToolsNormalizeKnownEquivalentAliases(t *testing.T) {
	ask, details := DecodeAskUser(json.RawMessage(`{"prompt":"Q","choices":["A",{"text":"B","id":"b","help":"second"}],"allowFreeText":true,"multiple":true}`))
	if len(details) != 0 || len(ask.Questions) != 1 || !ask.Questions[0].AllowCustom || !ask.Questions[0].MultiSelect || ask.Questions[0].Options[0].Value != "A" || ask.Questions[0].Options[1].Description != "second" {
		t.Fatalf("ask=%+v details=%+v", ask, details)
	}

	shared, details := DecodeShareResult(json.RawMessage(`{"message":"done","item":{"type":"url","label":"report","href":"https://example.invalid/result"}}`))
	if len(details) != 0 || shared.Summary != "done" || len(shared.Items) != 1 || shared.Items[0].Kind != "url" || shared.Items[0].Title != "report" {
		t.Fatalf("shared=%+v details=%+v", shared, details)
	}

	authorization, details := DecodeAuthorization(json.RawMessage(`{"operation":"deploy","justification":"needed","device_id":"device-a","workdir":"/workspace","text":"ship"}`))
	if len(details) != 0 || authorization.Action != "deploy" || authorization.Reason != "needed" || authorization.DeviceID != "device-a" || authorization.CWD != "/workspace" || authorization.Summary != "ship" {
		t.Fatalf("authorization=%+v details=%+v", authorization, details)
	}
}

func TestStructuredToolsRejectConflictingAliases(t *testing.T) {
	for _, test := range []struct {
		name, tool, raw, field string
	}{
		{"ask", "ask_user", `{"question":"Q1","prompt":"Q2","options":["A"]}`, "question"},
		{"share", "share_result", `{"summary":"one","message":"two","items":[{"kind":"url","title":"x","url":"https://example.invalid"}]}`, "summary"},
		{"authorization", "request_authorization", `{"action":"deploy","operation":"delete","reason":"x"}`, "action"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, details := Decode(test.tool, json.RawMessage(test.raw))
			if len(details) == 0 || details[0].Field != test.field {
				t.Fatalf("details=%+v", details)
			}
		})
	}
}

func TestAskUserRejectsSelectionTypeConflictsAndUnknownDrift(t *testing.T) {
	for _, test := range []struct {
		name, raw string
	}{
		{"conflict", `{"questions":[{"question":"Q","type":"single-select","multiSelect":true,"options":[{"label":"A"}]}]}`},
		{"unknown type", `{"questions":[{"question":"Q","type":"dropdown","options":[{"label":"A"}]}]}`},
		{"unrelated unknown", `{"questions":[{"question":"Q","widget":"radio","options":[{"label":"A"}]}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, details := DecodeAskUser(json.RawMessage(test.raw))
			if len(details) == 0 {
				t.Fatal("drift unexpectedly accepted")
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

func TestRejectionDiagnosticContainsOnlyShapeAndFieldMetadata(t *testing.T) {
	raw := json.RawMessage(`{"questions":[{"prompt":"private-user-text","options":["Alpha",{"label":"Beta","selected":true}],"count":57}],"token":"secret-value"}`)
	details := []FieldError{{Field: "questions[0].prompt", Reason: "additional property is not allowed", Expected: "only documented properties"}}
	diagnostic := RejectionDiagnostic("ask_user", raw, details)
	encoded, err := json.Marshal(diagnostic)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, secret := range []string{"private-user-text", "Alpha", "Beta", "secret-value", "57", "true"} {
		if strings.Contains(text, secret) {
			t.Fatalf("diagnostic leaked argument value %q: %s", secret, text)
		}
	}
	for _, want := range []string{`"tool":"ask_user"`, `"field":"questions[0].prompt"`, `"questions"`, `"options"`, `"label"`, `"type":"string"`, `"length":2`} {
		if !strings.Contains(text, want) {
			t.Fatalf("diagnostic omitted %s: %s", want, text)
		}
	}
}

func TestRejectionDiagnosticBoundsMalformedAndDeepArguments(t *testing.T) {
	diagnostic := RejectionDiagnostic("share_result", json.RawMessage(`{"items":`), []FieldError{{Field: "$", Reason: "invalid", Expected: "object"}})
	encoded, _ := json.Marshal(diagnostic)
	if strings.Contains(string(encoded), `{"items":`) || len(encoded) > 4096 {
		t.Fatalf("malformed diagnostic is unsafe or unbounded: %s", encoded)
	}

	deep := json.RawMessage(strings.Repeat(`[`, 80) + strings.Repeat(`]`, 80))
	encoded, _ = json.Marshal(RejectionDiagnostic("ask_user", deep, nil))
	if len(encoded) > 4096 {
		t.Fatalf("deep diagnostic is unbounded: %d", len(encoded))
	}

	wide := map[string]any{}
	for index := 0; index < 32; index++ {
		wide[fmt.Sprintf("field_%02d_%s", index, strings.Repeat("x", 180))] = map[string]any{"nested": strings.Repeat("private", 20)}
	}
	encoded, _ = json.Marshal(RejectionDiagnostic("ask_user", marshal(wide), nil))
	if len(encoded) > 8192 || strings.Contains(string(encoded), "privateprivate") {
		t.Fatalf("wide diagnostic is unsafe or unbounded: %d", len(encoded))
	}
}

func TestFinishTaskArrivesWithToolsetEight(t *testing.T) {
	for _, tool := range DynamicTools(7) {
		if tool["name"] == "finish_task" {
			t.Fatal("toolset 7 advertised finish_task")
		}
	}
	found := false
	for _, tool := range DynamicTools(8) {
		if tool["name"] == "finish_task" {
			found = true
			schema := tool["inputSchema"].(map[string]any)
			if schema["additionalProperties"] != false {
				t.Fatalf("schema=%+v", schema)
			}
		}
	}
	if !found {
		t.Fatal("toolset 8 missing finish_task")
	}
	for _, tool := range DynamicTools(8) {
		if tool["name"] == "send_message" {
			t.Fatal("toolset 8 advertised send_message")
		}
	}
	found = false
	for _, tool := range DynamicTools(9) {
		if tool["name"] == "send_message" {
			found = true
		}
	}
	if !found {
		t.Fatal("toolset 9 missing send_message")
	}
	if _, details := DecodeSendMessage(json.RawMessage(`{"text":"好了","assistantId":"other"}`)); len(details) != 1 || details[0].Field != "assistantId" {
		t.Fatalf("details=%+v", details)
	}
	if _, details := DecodeFinishTask(json.RawMessage(`{"taskId":"task-a","outcome":"completed","extra":true}`)); len(details) != 1 || details[0].Field != "extra" {
		t.Fatalf("details=%+v", details)
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

func TestHandoffReferencesAreOptionalOnTheCurrentToolset(t *testing.T) {
	current, ok := DefinitionFor(Version, "start_task")
	if !ok {
		t.Fatal("missing start_task")
	}
	properties, _ := current.InputSchema["properties"].(map[string]any)
	if _, found := properties["includeMessages"]; !found {
		t.Fatal("current start_task schema missing includeMessages")
	}
	if _, found := properties["referenceThreads"]; !found {
		t.Fatal("current start_task schema missing referenceThreads")
	}
	previous, ok := DefinitionFor(Version-1, "send_turn")
	if !ok {
		t.Fatal("missing previous send_turn")
	}
	oldProperties, _ := previous.InputSchema["properties"].(map[string]any)
	if _, found := oldProperties["referenceThreads"]; found {
		t.Fatal("previous toolset advertised referenceThreads")
	}
	start, details := DecodeStartTask(json.RawMessage(`{"deviceId":"exec-a","cwd":"/fixture","goal":"test","prompt":"run","riskTags":[],"clientTaskId":"00000000-0000-4000-8000-000000000001","referenceThreads":["11111111-1111-4111-8111-111111111111"],"includeMessages":["message-a"]}`))
	if len(details) != 0 || len(start.ReferenceThreads) != 1 || start.IncludeMessages[0] != "message-a" {
		t.Fatalf("start=%+v details=%+v", start, details)
	}
	if _, details := DecodeSendTurn(json.RawMessage(`{"taskId":"task-a","text":"continue","riskTags":[],"clientMessageId":"00000000-0000-4000-8000-000000000002","referenceThreads":null}`)); len(details) == 0 {
		t.Fatal("null referenceThreads was accepted")
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
