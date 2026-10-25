package desktopagent

import "testing"

func TestTaskIntentRequiresExactBoundIdentity(t *testing.T) {
	turn := map[string]any{"items": []any{
		map[string]any{"role": "assistant", "text": `{"task_action":"claim","task_id":"task-1","task_revision":"rev-1"}`},
	}}
	intent, ok := taskIntentFromTurn(turn, "task-1", "rev-1", "claim")
	if !ok || intent.Action != "claim" || intent.Revision != "rev-1" {
		t.Fatalf("intent=%+v ok=%v", intent, ok)
	}
	for _, text := range []string{
		`{"task_action":"claim","task_id":"another","task_revision":"rev-1"}`,
		`{"task_action":"claim","task_id":"task-1","task_revision":"stale"}`,
		"```json\n{\"task_action\":\"claim\",\"task_id\":\"task-1\",\"task_revision\":\"rev-1\"}\n```",
		`{"task_action":"claim","task_id":"task-1","task_revision":"rev-1","private_text":"leak"}`,
	} {
		turn["items"] = []any{map[string]any{"role": "assistant", "text": text}}
		if _, ok := taskIntentFromTurn(turn, "task-1", "rev-1", "claim"); ok {
			t.Fatalf("accepted unbound intent: %s", text)
		}
	}
}
