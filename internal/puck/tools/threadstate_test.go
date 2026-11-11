package tools

import (
	"encoding/json"
	"testing"
)

func TestClassifySnapshotReadsCompletionBlockAndFailureWithoutPrompt(t *testing.T) {
	completed := map[string]any{"freshness": "fresh", "thread": json.RawMessage(`{"runtime":"idle","pendingInteractions":[],"turns":[{"turnId":"turn-a","status":"completed","items":[{"role":"assistant","text":"发布已经完成。"}]}]}`)}
	view, ok := ClassifySnapshot(completed)
	if !ok || !view.Notable || view.Status != "review" || view.Runtime != "idle" || view.Summary != "发布已经完成。" || view.TurnID != "turn-a" {
		t.Fatalf("completed=%+v ok=%v", view, ok)
	}
	blocked := map[string]any{"freshness": "fresh", "thread": map[string]any{
		"runtime":             "inProgress",
		"pendingInteractions": []any{map[string]any{"kind": "command_approval", "prompt": "SUPER_SECRET_PROMPT"}},
		"turns":               []any{map[string]any{"turnId": "turn-b", "status": "inProgress", "items": []any{map[string]any{"role": "assistant", "text": "卡在发布前"}}}},
	}}
	view, ok = ClassifySnapshot(blocked)
	if !ok || view.Status != "waiting_user" || view.Summary != "卡在发布前" {
		t.Fatalf("blocked=%+v", view)
	}
	failed := map[string]any{"thread": map[string]any{"runtime": "idle", "turns": []any{map[string]any{"turnId": "turn-c", "status": "failed", "items": []any{}}}}}
	view, ok = ClassifySnapshot(failed)
	if !ok || view.Status != "attention" || !view.Notable || view.Summary == "" {
		t.Fatalf("failed=%+v", view)
	}
	stale := map[string]any{"freshness": "stale", "staleReason": "DEVICE_OFFLINE", "thread": map[string]any{"runtime": "idle", "turns": []any{map[string]any{"turnId": "turn-d", "status": "completed", "items": []any{map[string]any{"role": "assistant", "text": "完成"}}}}}}
	view, ok = ClassifySnapshot(stale)
	if !ok || view.Notable || view.Runtime != "unknown" {
		t.Fatalf("stale snapshot looked finished: %+v", view)
	}
}
