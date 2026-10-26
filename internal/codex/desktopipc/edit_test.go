package desktopipc

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"
)

func editState(status, text string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"cwd": "/fixture", "threadRuntimeStatus": map[string]any{"type": "idle"}, "requests": []any{}, "turns": []any{map[string]any{"turnId": "old", "status": status, "params": map[string]any{"clientUserMessageId": "client-old", "input": []any{map[string]any{"type": "text", "text": text}}}, "items": []any{map[string]any{"id": "user", "type": "userMessage", "clientId": "client-old", "content": []any{map[string]any{"type": "text", "text": text}}}}}}})
	return b
}
func TestEditLatestFencesOriginalAndDoesNotOverridePermissions(t *testing.T) {
	raw := editState("interrupted", "old text")
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte("old text")))
	owner := &recordingOwner{reply: Reply{Result: json.RawMessage(`{"ok":true}`)}}
	if err := EditProductionLatestUserTurn(context.Background(), owner, "owner", "thread", "/fixture", raw, "old", "user", hash, "new text"); err != nil {
		t.Fatal(err)
	}
	if len(owner.calls) != 1 || owner.calls[0].Method != "thread-follower-edit-last-user-turn" || owner.calls[0].Version != 2 || !owner.calls[0].Mutating {
		t.Fatal("wrong native write")
	}
	b, _ := json.Marshal(owner.calls[0].Params)
	var p map[string]any
	_ = json.Unmarshal(b, &p)
	if len(p) != 4 || p["message"] != "new text" || p["turnId"] != "old" || p["shouldSendPermissionOverrides"] != false {
		t.Fatal("unexpected edit fields")
	}
	for _, status := range []string{"completed", "inProgress", "failed"} {
		if EditProductionLatestUserTurn(context.Background(), owner, "owner", "thread", "/fixture", editState(status, "old text"), "old", "user", hash, "new") == nil {
			t.Fatal("unsafe turn accepted")
		}
	}
	if EditProductionLatestUserTurn(context.Background(), owner, "owner", "thread", "/fixture", raw, "old", "user", "stale", "new") == nil || len(owner.calls) != 1 {
		t.Fatal("stale input reached native")
	}
}

func TestEditVerificationUsesNativeEditOriginNotReusedClientID(t *testing.T) {
	before := editState("interrupted", "old text")
	target, err := LatestEditTarget(before, "/fixture")
	if err != nil {
		t.Fatal(err)
	}
	after := json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[],"turns":[{"turnId":"replacement","status":"completed","params":{"turnTrigger":"edit_user_message","clientUserMessageId":"new-client","input":[{"type":"text","text":"new text"}]},"items":[{"id":"new-user","type":"userMessage","content":[{"type":"text","text":"new text"}]}]}]}`)
	if id, ok := verifyEdited(before, after, "/fixture", target, "new text"); !ok || id != "replacement" {
		t.Fatal("native edit creates a fresh client ID")
	}
	var x map[string]any
	_ = json.Unmarshal(after, &x)
	x["turns"].([]any)[0].(map[string]any)["params"].(map[string]any)["turnTrigger"] = "user"
	unsafe, _ := json.Marshal(x)
	if _, ok := verifyEdited(before, unsafe, "/fixture", target, "new text"); ok {
		t.Fatal("identical normal send misidentified as edit")
	}
}

func TestEditRejectsFileContextAndForeignTargetWithoutCallingOwner(t *testing.T) {
	for _, text := range []string{"<ariel_file_context>secret</ariel_file_context>", "[File context: x]"} {
		if _, err := LatestEditTarget(editState("interrupted", text), "/fixture"); err == nil {
			t.Fatal("file context treated as plain text")
		}
	}
	owner := &recordingOwner{}
	for _, ids := range [][3]string{{"other", "user", "hash"}, {"old", "other", "hash"}} {
		if EditProductionLatestUserTurn(context.Background(), owner, "owner", "thread", "/fixture", editState("interrupted", "old"), ids[0], ids[1], ids[2], "new") == nil {
			t.Fatal("stale target accepted")
		}
	}
	if len(owner.calls) != 0 {
		t.Fatal("stale target reached owner")
	}
}

func TestEditRejectsRequestsMultipleInputsAndReceiptOnlyProof(t *testing.T) {
	base := editState("interrupted", "old")
	var state map[string]any
	_ = json.Unmarshal(base, &state)
	state["requests"] = []any{map[string]any{"id": "question"}}
	raw, _ := json.Marshal(state)
	if _, e := LatestEditTarget(raw, "/fixture"); e == nil {
		t.Fatal("pending interaction accepted")
	}
	_ = json.Unmarshal(base, &state)
	state["threadRuntimeStatus"] = map[string]any{"type": "inProgress"}
	raw, _ = json.Marshal(state)
	if _, e := LatestEditTarget(raw, "/fixture"); e == nil {
		t.Fatal("active owner accepted")
	}
	_ = json.Unmarshal(base, &state)
	turn := state["turns"].([]any)[0].(map[string]any)
	turn["params"].(map[string]any)["input"] = []any{map[string]any{"type": "text", "text": "old"}, map[string]any{"type": "image", "url": "fixture"}}
	raw, _ = json.Marshal(state)
	if _, e := LatestEditTarget(raw, "/fixture"); e == nil {
		t.Fatal("attachment lost during editing")
	}
	target, _ := LatestEditTarget(base, "/fixture")
	if _, ok := verifyEdited(base, base, "/fixture", target, "old"); ok {
		t.Fatal("unchanged history treated as proof")
	}
	owner := &recordingOwner{reply: Reply{Result: json.RawMessage(`{"ok":false}`)}}
	if e := EditProductionLatestUserTurn(context.Background(), owner, "owner", "thread", "/fixture", base, target.TurnID, target.ItemID, target.Fingerprint, "new"); e == nil {
		t.Fatal("failed native receipt accepted")
	}
}
