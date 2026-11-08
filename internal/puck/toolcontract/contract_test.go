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

