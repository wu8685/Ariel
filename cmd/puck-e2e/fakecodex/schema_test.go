package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wu8685/Ariel/internal/puck/toolcontract"
)

func TestFakeServerRejectsToolSchemasCodexWouldForward(t *testing.T) {
	raw := toolcontract.DynamicTools(toolcontract.Version)
	body, err := json.Marshal(map[string]any{"dynamicTools": raw})
	if err != nil {
		t.Fatal(err)
	}
	problem := dynamicToolSchemaProblem(body)
	if !strings.Contains(problem, "recall_memory") && !strings.Contains(problem, "update_memory") && problem == "" {
		t.Fatal("raw toolset 13 should fail the turn")
	}
	if problem == "" {
		t.Fatal("raw dynamic tools were accepted")
	}
	wire := toolcontract.CodexWireTools(toolcontract.Version)
	body, err = json.Marshal(map[string]any{"dynamicTools": wire})
	if err != nil {
		t.Fatal(err)
	}
	if got := dynamicToolSchemaProblem(body); got != "" {
		t.Fatalf("sanitized tools still fail the turn: %s", got)
	}
}
