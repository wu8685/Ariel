package toolcontract

import "strings"

// Keywords the Codex model request rejects or drops. They are enforced by
// Ariel's own decoders. Sending them on dynamicTools makes turn/start succeed
// and every later turn/completed fail.
var disallowedSchemaKeys = map[string]bool{
	"minLength": true, "maxLength": true,
	"minimum": true, "maximum": true,
	"exclusiveMinimum": true, "exclusiveMaximum": true,
	"minItems": true, "maxItems": true, "uniqueItems": true,
	"pattern": true, "format": true, "multipleOf": true,
	"examples": true, "default": true, "$schema": true,
}

// DisallowedSchemaKey reports the first keyword Codex must not be given.
func DisallowedSchemaKey(schema map[string]any) string {
	return walkSchema(schema)
}

func walkSchema(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		for key := range typed {
			if disallowedSchemaKeys[key] {
				return key
			}
		}
		for _, child := range typed {
			if key := walkSchema(child); key != "" {
				return key
			}
		}
	case []any:
		for _, child := range typed {
			if key := walkSchema(child); key != "" {
				return key
			}
		}
	}
	return ""
}

// CodexWireSchema is the schema actually declared on a brain thread.
func CodexWireSchema(schema map[string]any) map[string]any {
	cleaned, _ := cleanSchema(schema).(map[string]any)
	if cleaned == nil {
		return map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{}}
	}
	return cleaned
}

func cleanSchema(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, child := range typed {
			if disallowedSchemaKeys[key] {
				continue
			}
			out[key] = cleanSchema(child)
		}
		if out["type"] == "object" {
			if _, ok := out["additionalProperties"]; !ok {
				out["additionalProperties"] = false
			}
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, child := range typed {
			out[i] = cleanSchema(child)
		}
		return out
	case []string:
		out := make([]string, len(typed))
		copy(out, typed)
		return out
	default:
		return value
	}
}

// CodexWireTools returns dynamic tool declarations safe to send to Codex.
func CodexWireTools(version int) []map[string]any {
	tools := DynamicTools(version)
	for i, tool := range tools {
		schema, _ := tool["inputSchema"].(map[string]any)
		if schema == nil {
			continue
		}
		copied := map[string]any{}
		for key, value := range tool {
			copied[key] = value
		}
		copied["inputSchema"] = CodexWireSchema(schema)
		tools[i] = copied
	}
	return tools
}

func SchemaProblem(name string, schema map[string]any) string {
	if strings.TrimSpace(name) == "" {
		return "tool"
	}
	if key := DisallowedSchemaKey(schema); key != "" {
		return name + " " + key
	}
	return ""
}
