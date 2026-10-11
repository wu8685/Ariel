package desktopagent

import (
	"reflect"
	"testing"
)

func deltaThread(turnID, status string, items ...map[string]any) map[string]any {
	projected := make([]any, len(items))
	for index := range items {
		projected[index] = items[index]
	}
	return map[string]any{
		"threadId":            "thread",
		"title":               "Fixture",
		"cwd":                 "/fixture",
		"updatedAt":           "2026-10-11T00:00:00Z",
		"runtime":             map[bool]string{true: "inProgress", false: "idle"}[status == "inProgress"],
		"historyComplete":     false,
		"recentComplete":      true,
		"permissions":         map[string]any{"sandbox": "read_only", "approval": "on_request"},
		"turns":               []any{map[string]any{"turnId": turnID, "status": status, "items": projected}},
		"pendingInteractions": []any{},
		"queuedMessages":      []any{},
	}
}

func TestBuildThreadPatchOnlyUpsertsChangedItems(t *testing.T) {
	user := map[string]any{"itemId": "user", "role": "user", "text": "hello"}
	before := deltaThread("turn", "inProgress", user, map[string]any{"itemId": "assistant", "role": "assistant", "text": "A"})
	after := deltaThread("turn", "inProgress", user, map[string]any{"itemId": "assistant", "role": "assistant", "text": "AB"})

	patch, changed, err := buildThreadPatch(before, after)
	if err != nil || !changed {
		t.Fatalf("diff: changed=%v err=%v", changed, err)
	}
	if _, leaked := patch["turns"]; leaked {
		t.Fatal("patch leaked the full recent window")
	}
	if got := patch["turnOrder"]; !reflect.DeepEqual(got, []any{"turn"}) {
		t.Fatalf("turn order: %#v", got)
	}
	turnPatches := patch["turnPatches"].([]any)
	if len(turnPatches) != 1 {
		t.Fatalf("turn patches: %#v", turnPatches)
	}
	turnPatch := turnPatches[0].(map[string]any)
	if got := turnPatch["itemOrder"]; !reflect.DeepEqual(got, []any{"user", "assistant"}) {
		t.Fatalf("item order: %#v", got)
	}
	upserts := turnPatch["upsertItems"].([]any)
	if len(upserts) != 1 || upserts[0].(map[string]any)["itemId"] != "assistant" || upserts[0].(map[string]any)["text"] != "AB" {
		t.Fatalf("upserts: %#v", upserts)
	}
}

func TestBuildThreadPatchCapturesTurnAndItemRemoval(t *testing.T) {
	before := deltaThread("old", "completed", map[string]any{"itemId": "gone", "role": "assistant", "text": "old"})
	after := deltaThread("new", "inProgress", map[string]any{"itemId": "fresh", "role": "user", "text": "new"})

	patch, changed, err := buildThreadPatch(before, after)
	if err != nil || !changed {
		t.Fatalf("diff: changed=%v err=%v", changed, err)
	}
	if got := patch["removedTurnIds"]; !reflect.DeepEqual(got, []any{"old"}) {
		t.Fatalf("removed turns: %#v", got)
	}
	turnPatches := patch["turnPatches"].([]any)
	if len(turnPatches) != 1 || len(turnPatches[0].(map[string]any)["upsertItems"].([]any)) != 1 {
		t.Fatalf("new turn was not complete: %#v", turnPatches)
	}

	trimmed := deltaThread("new", "inProgress")
	patch, changed, err = buildThreadPatch(after, trimmed)
	if err != nil || !changed {
		t.Fatalf("item removal diff: changed=%v err=%v", changed, err)
	}
	turnPatch := patch["turnPatches"].([]any)[0].(map[string]any)
	if got := turnPatch["removedItemIds"]; !reflect.DeepEqual(got, []any{"fresh"}) {
		t.Fatalf("removed items: %#v", got)
	}
}

func TestBuildThreadPatchSkipsTimestampOnlyChangeAndRejectsDuplicateIDs(t *testing.T) {
	before := deltaThread("turn", "completed", map[string]any{"itemId": "one", "role": "assistant", "text": "done"})
	after := deltaThread("turn", "completed", map[string]any{"itemId": "one", "role": "assistant", "text": "done"})
	after["updatedAt"] = "2026-10-11T00:00:01Z"
	if _, changed, err := buildThreadPatch(before, after); err != nil || changed {
		t.Fatalf("timestamp-only update emitted: changed=%v err=%v", changed, err)
	}

	duplicate := deltaThread("turn", "completed",
		map[string]any{"itemId": "one", "role": "assistant", "text": "a"},
		map[string]any{"itemId": "one", "role": "assistant", "text": "b"},
	)
	if _, _, err := buildThreadPatch(before, duplicate); err == nil {
		t.Fatal("duplicate item IDs were accepted")
	}
}
