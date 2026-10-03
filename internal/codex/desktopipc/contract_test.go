package desktopipc

import (
	"encoding/json"
	"os"
	"testing"
)

func TestUserInputRedactedContract(t *testing.T) {
	b, err := os.ReadFile("../../../tests/fixtures/desktop-0.160.0/user-input-completed.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Pending   json.RawMessage `json:"pending"`
		Completed json.RawMessage `json:"completed"`
	}
	if err := json.Unmarshal(b, &fixture); err != nil {
		t.Fatal(err)
	}
	p, err := pendingUserInput(fixture.Pending, "thread_fixture", "turn_fixture", "/fixture")
	if err != nil {
		t.Fatal(err)
	}
	if !userInputEchoMatches(fixture.Completed, p, map[string][]string{"choice": {"Blue"}, "note": {"ariel free text"}}) {
		t.Fatal("redacted echo mismatch")
	}
}
