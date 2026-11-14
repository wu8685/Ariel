package toolcontract

import "testing"

func TestCodexWireSchemaDropsKeywordsTheModelRequestRejects(t *testing.T) {
	definition, ok := DefinitionFor(Version, "recall_memory")
	if !ok {
		t.Fatal("missing recall_memory")
	}
	if DisallowedSchemaKey(definition.InputSchema) == "" {
		t.Fatal("decoder schema should keep length and uniqueness keywords")
	}
	for _, tool := range CodexWireTools(Version) {
		schema, _ := tool["inputSchema"].(map[string]any)
		if key := DisallowedSchemaKey(schema); key != "" {
			t.Fatalf("%s wire schema still has %s", tool["name"], key)
		}
		if schema["type"] != "object" {
			t.Fatalf("%s wire schema=%v", tool["name"], schema)
		}
	}
}
