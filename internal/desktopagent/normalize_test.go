package desktopagent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wu8685/Ariel/internal/codex/appserver"
)

func TestNormalizeStoredThreadPreservesIdentityAndText(t *testing.T) {
	stored := appserver.Thread{ID: "thread-1", Name: "Same title", CWD: "/project/a", UpdatedAt: 1730000000, Status: json.RawMessage(`{"type":"notLoaded"}`), Turns: []appserver.Turn{{ID: "turn-1", Status: "completed", Items: []json.RawMessage{json.RawMessage(`{"type":"userMessage","id":"u1","content":[{"type":"text","text":"hello"}]}`), json.RawMessage(`{"type":"agentMessage","id":"a1","text":"world"}`)}}}}
	thread, err := NormalizeStored(stored)
	if err != nil {
		t.Fatal(err)
	}
	if thread["threadId"] != "thread-1" || thread["cwd"] != "/project/a" || thread["runtime"] != "notLoaded" {
		t.Fatalf("identity: %v", thread)
	}
	permissions, ok := thread["permissions"].(map[string]any)
	if !ok || permissions["sandbox"] != "unknown" || permissions["approval"] != "unknown" {
		t.Fatalf("stored history incorrectly inferred permissions: %v", thread["permissions"])
	}
	turns := thread["turns"].([]any)
	items := turns[0].(map[string]any)["items"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["text"] != "hello" || items[1].(map[string]any)["text"] != "world" {
		t.Fatalf("items: %v", items)
	}
}

func TestNormalizeLiveExposesCurrentOwnerPermissions(t *testing.T) {
	state := json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[],"turns":[],"latestThreadSettings":{"approvalPolicy":"on-request","sandboxPolicy":{"type":"dangerFullAccess"}}}`)
	thread, err := NormalizeLive("thread", "Title", "/fixture", state)
	if err != nil {
		t.Fatal(err)
	}
	permissions, ok := thread["permissions"].(map[string]any)
	if !ok || permissions["sandbox"] != "full_access" || permissions["approval"] != "on_request" {
		t.Fatalf("current owner permission mode lost: %v", thread["permissions"])
	}
	unknown := json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[],"turns":[]}`)
	thread, err = NormalizeLive("thread", "Title", "/fixture", unknown)
	if err != nil {
		t.Fatal(err)
	}
	permissions, ok = thread["permissions"].(map[string]any)
	if !ok || permissions["sandbox"] != "unknown" || permissions["approval"] != "unknown" {
		t.Fatalf("missing settings must remain unknown: %v", thread["permissions"])
	}
}

func TestNormalizeKnownToolItemsWithoutNoisyReasoningPlaceholders(t *testing.T) {
	items := []json.RawMessage{
		json.RawMessage(`{"id":"r","type":"reasoning"}`),
		json.RawMessage(`{"id":"c","type":"commandExecution","command":"/usr/bin/true","status":"completed"}`),
		json.RawMessage(`{"id":"f","type":"fileChange","status":"completed","changes":[{"path":"/fixture/note.txt","kind":{"type":"add"},"diff":"approved\\n"}]}`),
		json.RawMessage(`{"id":"q","type":"userInputResponse","completed":true,"answers":{"secret":["do-not-show"]}}`),
	}
	turn, err := normalizeTurn("turn", "completed", items)
	if err != nil {
		t.Fatal(err)
	}
	normalized := turn["items"].([]any)
	if len(normalized) != 3 {
		t.Fatalf("reasoning placeholder leaked or activity lost: count=%d", len(normalized))
	}
	command := normalized[0].(map[string]any)["text"].(string)
	file := normalized[1].(map[string]any)["text"].(string)
	answer := normalized[2].(map[string]any)["text"].(string)
	if !strings.Contains(command, "/usr/bin/true") || !strings.Contains(command, "completed") || !strings.Contains(file, "/fixture/note.txt") || !strings.Contains(file, "completed") || strings.Contains(answer, "do-not-show") {
		t.Fatalf("incorrect tool summary: command=%q file=%q answer=%q", command, file, answer)
	}
}

func TestNormalizePendingUserInputResponseDoesNotClaimAnswered(t *testing.T) {
	for _, tc := range []struct {
		name, item, want string
	}{
		{"pending", `{"id":"q","type":"userInputResponse","completed":false}`, "等待补充回答"},
		{"missing completion", `{"id":"q","type":"userInputResponse"}`, "补充回答状态未知"},
		{"completed", `{"id":"q","type":"userInputResponse","completed":true}`, "已回答补充问题"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			turn, err := normalizeTurn("turn", "inProgress", []json.RawMessage{json.RawMessage(tc.item)})
			if err != nil {
				t.Fatal(err)
			}
			items := turn["items"].([]any)
			if len(items) != 1 || items[0].(map[string]any)["text"] != tc.want {
				t.Fatalf("status summary: %v", items)
			}
		})
	}
}

func TestNormalizeCanonicalLiveStatusAndPendingInput(t *testing.T) {
	state := json.RawMessage(`{"cwd":"/project/a","threadRuntimeStatus":{"type":"inProgress"},"requests":[{"id":7,"method":"item/tool/requestUserInput","params":{"threadId":"thread-1","turnId":"turn-1","questions":[{"id":"q","header":"Choice","question":"Choose?","options":[{"label":"A"},{"label":"B"}]}]}}],"turnHistory":{"kind":"canonical","history":{"islands":[{"entries":[{"value":"key-1"}]}],"entitiesByKey":{"key-1":{"turnId":"turn-1","status":"inProgress","items":[{"type":"agentMessage","id":"a1","text":"working"}]}}}}}`)
	thread, err := NormalizeLive("thread-1", "Title", "/project/a", state)
	if err != nil {
		t.Fatal(err)
	}
	if thread["runtime"] != "inProgress" {
		t.Fatalf("runtime: %v", thread)
	}
	turns := thread["turns"].([]any)
	if len(turns) != 1 || turns[0].(map[string]any)["turnId"] != "turn-1" {
		t.Fatalf("turns: %v", turns)
	}
	interactions := thread["pendingInteractions"].([]any)
	if len(interactions) != 1 || interactions[0].(map[string]any)["kind"] != "user_input" || interactions[0].(map[string]any)["interactionId"] == "" {
		t.Fatalf("interactions: %v", interactions)
	}
}

func TestNormalizeRejectsMissingCanonicalEntityAndWorkspaceMismatch(t *testing.T) {
	state := json.RawMessage(`{"cwd":"/wrong","threadRuntimeStatus":{"type":"idle"},"requests":[],"turnHistory":{"kind":"canonical","history":{"islands":[{"entries":[{"value":"missing"}]}],"entitiesByKey":{}}}}`)
	if _, err := NormalizeLive("thread-1", "Title", "/project/a", state); err == nil {
		t.Fatal("wrong cwd accepted")
	}
	state = json.RawMessage(`{"cwd":"/project/a","threadRuntimeStatus":{"type":"idle"},"requests":[],"turnHistory":{"kind":"canonical","history":{"islands":[{"entries":[{"value":"missing"}]}],"entitiesByKey":{}}}}`)
	if _, err := NormalizeLive("thread-1", "Title", "/project/a", state); err == nil {
		t.Fatal("missing entity accepted")
	}
}

func TestNormalizeApprovalDoesNotOfferStructuredNativeGrant(t *testing.T) {
	state := json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"inProgress"},"requests":[{"id":9,"method":"item/commandExecution/requestApproval","params":{"threadId":"thread","turnId":"turn","cwd":"/fixture","command":"/usr/bin/true","availableDecisions":["accept","cancel"]}}],"turns":[{"turnId":"turn","status":"inProgress","items":[]}]}`)
	thread, err := NormalizeLive("thread", "Title", "/fixture", state)
	if err != nil {
		t.Fatal(err)
	}
	card := thread["pendingInteractions"].([]any)[0].(map[string]any)
	decisions := card["availableDecisions"].([]string)
	if len(decisions) != 2 || decisions[0] != "accept_once" || decisions[1] != "deny_and_stop" {
		t.Fatalf("unverified approval offered: %v", decisions)
	}
}

func TestNormalizeFileApprovalIncludesExactPatchContext(t *testing.T) {
	state := json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"inProgress"},"requests":[{"id":9,"method":"item/fileChange/requestApproval","params":{"threadId":"thread","turnId":"turn","itemId":"file","grantRoot":null}}],"turns":[{"turnId":"turn","status":"inProgress","items":[{"id":"file","type":"fileChange","status":"inProgress","changes":[{"path":"/fixture/note.txt","kind":{"type":"add"},"diff":"approved\\n"}]}]}]}`)
	thread, err := NormalizeLive("thread", "Title", "/fixture", state)
	if err != nil {
		t.Fatal(err)
	}
	card := thread["pendingInteractions"].([]any)[0].(map[string]any)
	if card["kind"] != "file_approval" || !strings.Contains(card["prompt"].(string), "/fixture/note.txt") || !strings.Contains(card["prompt"].(string), "approved") {
		t.Fatalf("file context missing: %v", card)
	}
	changed := json.RawMessage(strings.Replace(string(state), "approved", "changed", 1))
	updated, err := NormalizeLive("thread", "Title", "/fixture", changed)
	if err != nil || updated["pendingInteractions"].([]any)[0].(map[string]any)["interactionId"] == card["interactionId"] {
		t.Fatal("changed diff did not invalidate old card")
	}
}

func TestNormalizePermissionRequestShowsExactScopeAndFailsClosed(t *testing.T) {
	state := json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"inProgress"},"requests":[{"id":91,"method":"item/permissions/requestApproval","params":{"threadId":"thread","turnId":"turn","itemId":"permission","cwd":"/fixture","reason":"write one fixture file","permissions":{"network":null,"fileSystem":{"read":null,"write":["/fixture"],"entries":[]}}}}],"turns":[{"turnId":"turn","status":"inProgress","items":[]}]}`)
	thread, err := NormalizeLive("thread", "Title", "/fixture", state)
	if err != nil {
		t.Fatal(err)
	}
	card := thread["pendingInteractions"].([]any)[0].(map[string]any)
	if card["kind"] != "permission_request" || !strings.Contains(card["prompt"].(string), "write one fixture file") || !strings.Contains(card["prompt"].(string), `"/fixture"`) {
		t.Fatalf("permission context missing: %v", card)
	}
	decisions := card["availableDecisions"].([]string)
	if len(decisions) != 2 || decisions[0] != "accept_once" || decisions[1] != "deny" {
		t.Fatalf("unsafe decisions: %v", decisions)
	}
	changed := json.RawMessage(strings.Replace(string(state), `"write":["/fixture"]`, `"write":["/other"]`, 1))
	updated, err := NormalizeLive("thread", "Title", "/fixture", changed)
	if err != nil || updated["pendingInteractions"].([]any)[0].(map[string]any)["interactionId"] == card["interactionId"] {
		t.Fatal("changed scope did not invalidate card")
	}
	unknown := json.RawMessage(strings.Replace(string(state), `"entries":[]`, `"futureGrant":true`, 1))
	updated, err = NormalizeLive("thread", "Title", "/fixture", unknown)
	if err != nil {
		t.Fatal(err)
	}
	decisions = updated["pendingInteractions"].([]any)[0].(map[string]any)["availableDecisions"].([]string)
	if len(decisions) != 1 || decisions[0] != "deny" {
		t.Fatalf("unknown profile allowed: %v", decisions)
	}
}

func TestNormalizeSecretUserInputDoesNotOfferRemoteAnswer(t *testing.T) {
	state := json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"inProgress"},"requests":[{"id":19,"method":"item/tool/requestUserInput","params":{"threadId":"thread","turnId":"turn","questions":[{"id":"password","question":"Secret?","isSecret":true}]}}],"turns":[{"turnId":"turn","status":"inProgress","items":[]}]}`)
	thread, err := NormalizeLive("thread", "Title", "/fixture", state)
	if err != nil {
		t.Fatal(err)
	}
	card := thread["pendingInteractions"].([]any)[0].(map[string]any)
	if card["kind"] != "unsupported" || len(card["availableDecisions"].([]string)) != 0 {
		t.Fatalf("secret input exposed for remote answer: %v", card)
	}
}
