package desktopagent

import (
	"strings"
	"testing"
)

func TestTaskSafeProgressPublishesOnlyDeterministicStatusAndCounts(t *testing.T) {
	turn := map[string]any{"items": []any{
		map[string]any{"role": "assistant", "text": "PRIVATE_SENTINEL /secret/file API_SECRET"},
		map[string]any{"role": "tool", "text": "PRIVATE_SENTINEL", "activity": map[string]any{"kind": "commandExecution", "status": "completed", "label": "PRIVATE_SENTINEL", "details": []any{"PRIVATE_SENTINEL"}}},
		map[string]any{"role": "tool", "activity": map[string]any{"kind": "fileChange", "status": "completed", "label": "/private/file"}},
		map[string]any{"role": "tool", "activity": map[string]any{"kind": "mcpToolCall", "status": "inProgress", "details": "PRIVATE_SENTINEL"}},
	}}
	progress := taskSafeProgress("running", turn)
	if !strings.Contains(progress, "2") || !strings.Contains(progress, "3") {
		t.Fatalf("missing observed counts: %q", progress)
	}
	for _, secret := range []string{"PRIVATE_SENTINEL", "/secret", "/private", "API_SECRET"} {
		if strings.Contains(progress, secret) {
			t.Fatal("private tool or assistant body published")
		}
	}
	if got := taskSafeProgress("completed", turn); !strings.Contains(got, "待验收") {
		t.Fatalf("completion bypasses human acceptance: %q", got)
	}
	if got := taskSafeProgress("waiting", nil); !strings.Contains(got, "等待") {
		t.Fatalf("missing waiting state: %q", got)
	}
	if got := taskSafeProgress("PRIVATE_SENTINEL", turn); strings.Contains(got, "PRIVATE_SENTINEL") {
		t.Fatal("unknown status reflected into progress")
	}
}

func TestTaskResponseRequiresActualAssistantText(t *testing.T) {
	for _, test := range []struct {
		name string
		item map[string]any
		want bool
	}{
		{"no text field", map[string]any{"role": "assistant"}, false},
		{"null text", map[string]any{"role": "assistant", "text": nil}, false},
		{"empty text", map[string]any{"role": "assistant", "text": ""}, false},
		{"whitespace text", map[string]any{"role": "assistant", "text": " \n"}, false},
		{"non-text value", map[string]any{"role": "assistant", "text": true}, false},
		{"tool output", map[string]any{"role": "tool", "text": "completed"}, false},
		{"assistant output", map[string]any{"role": "assistant", "text": "Completed the requested fixture work"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			turn := map[string]any{"items": []any{test.item}}
			if got := taskTurnHasAssistantOutput(turn); got != test.want {
				t.Fatalf("responded=%v, expected=%v", got, test.want)
			}
		})
	}
}
