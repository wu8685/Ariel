package desktopagent

import (
	"encoding/json"
	"testing"

	"github.com/wu8685/Ariel/internal/codex/appserver"
)

func TestNormalizeBootstrapUserInputPreservesStructuredQuestion(t *testing.T) {
	card := normalizeBootstrapInteraction("input-1", appserver.IncomingFrame{
		ID:     json.RawMessage(`7`),
		Method: "item/tool/requestUserInput",
		Params: json.RawMessage(`{"questions":[{"id":"mode","header":"Mode","question":"Continue?","options":[{"label":"Yes","description":"Keep going"}]}]}`),
	})
	if card["kind"] != "user_input" {
		t.Fatalf("kind: %v", card)
	}
	question := card["questions"].([]any)[0].(map[string]any)
	if question["header"] != "Mode" {
		t.Fatalf("header lost: %v", question)
	}
	option := question["options"].([]any)[0].(map[string]any)
	if option["label"] != "Yes" || option["description"] != "Keep going" {
		t.Fatalf("option metadata lost: %v", option)
	}
}

func TestNormalizeBootstrapUserInputFailsClosedForSecretOrInvalidQuestions(t *testing.T) {
	for _, params := range []string{
		`{"questions":[]}`,
		`{"questions":[{"id":"secret","question":"Password?","isSecret":true}]}`,
	} {
		card := normalizeBootstrapInteraction("input-1", appserver.IncomingFrame{ID: json.RawMessage(`7`), Method: "item/tool/requestUserInput", Params: json.RawMessage(params)})
		if card["kind"] != "unsupported" || len(card["availableDecisions"].([]any)) != 0 {
			t.Fatalf("unsafe input exposed remotely: %v", card)
		}
	}
}
