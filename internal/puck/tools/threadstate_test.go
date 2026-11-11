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
	stopped := map[string]any{"freshness": "fresh", "thread": map[string]any{"runtime": "idle", "turns": []any{map[string]any{"turnId": "turn-e", "status": "interrupted", "items": []any{}}}}}
	view, ok = ClassifySnapshot(stopped)
	if !ok || view.Status != "stopped" || !view.Notable {
		t.Fatalf("stopped=%+v", view)
	}
}

func TestDecideOutcomeFiltersNoiseAndKeepsClearResults(t *testing.T) {
	progress := ThreadView{Freshness: "fresh", Runtime: "idle", Status: "review", TurnID: "t1", Summary: "还在检查，没有变化"}
	if got := DecideOutcome(progress, "发布完成", "", ""); got.Notify || got.Reason != "in_progress" {
		t.Fatalf("progress=%+v", got)
	}
	same := ThreadView{Freshness: "fresh", Runtime: "idle", Status: "review", TurnID: "t2", Summary: "发布已经完成。"}
	if got := DecideOutcome(same, "发布完成", "review", "发布已经完成。"); got.Notify || got.Reason != "repeat" {
		t.Fatalf("repeat=%+v", got)
	}
	done := DecideOutcome(same, "发布完成", "", "")
	if !done.Notify || done.Status != "review" || done.Reason != "condition" {
		t.Fatalf("done=%+v", done)
	}
	blocked := ThreadView{Freshness: "fresh", Runtime: "inProgress", Status: "waiting_user", Summary: "卡在发布前"}
	if got := DecideOutcome(blocked, "卡住了", "", ""); !got.Notify || got.Reason != "blocked" {
		t.Fatalf("blocked=%+v", got)
	}
	if got := DecideOutcome(blocked, "卡住了", "waiting_user", "卡在发布前"); got.Notify {
		t.Fatalf("repeat blocked=%+v", got)
	}
	failed := ThreadView{Freshness: "fresh", Runtime: "idle", Status: "attention", Summary: "构建失败"}
	if got := DecideOutcome(failed, "", "", ""); !got.Notify || got.Reason != "failed" {
		t.Fatalf("failed=%+v", got)
	}
	if got := DecideOutcome(failed, "", "attention", "构建失败"); got.Notify {
		t.Fatalf("repeat failure=%+v", got)
	}
}
