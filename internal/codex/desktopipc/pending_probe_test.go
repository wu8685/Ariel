package desktopipc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestPendingProbeStartsOnlyExactFixtureCommandAndLeavesApprovalOpen(t *testing.T) {
	state := strings.Replace(commandPending, `"accept","decline"`, `"accept","cancel"`, 1)
	h := &inputHarness{states: []json.RawMessage{
		json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[]}`),
		json.RawMessage(state),
	}}
	result, err := exercisePendingInteraction(context.Background(), h, "owner", "thread", "/fixture", "command-accept")
	if err != nil || !result.Pending || result.Kind != "command_approval" || len(h.calls) != 1 || h.calls[0].Method != "thread-follower-start-turn" {
		t.Fatalf("pending: %+v %v calls=%v", result, err, h.calls)
	}
	encoded, _ := json.Marshal(h.calls[0].Params)
	if !strings.Contains(string(encoded), "/usr/bin/true") || !strings.Contains(string(encoded), `"type":"readOnly"`) {
		t.Fatalf("unsafe fixture request: %s", encoded)
	}
}

func TestPendingProbeRecognizesFileChangeRequestWithoutResponding(t *testing.T) {
	h := &inputHarness{states: []json.RawMessage{
		json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[]}`),
		json.RawMessage(`{"cwd":"/fixture","requests":[{"id":9,"method":"item/fileChange/requestApproval","params":{"threadId":"thread","turnId":"turn","itemId":"file"}}],"turns":[{"turnId":"turn","status":"inProgress"}]}`),
	}}
	result, err := exercisePendingInteraction(context.Background(), h, "owner", "thread", "/fixture", "file-change")
	if err != nil || !result.Pending || result.Kind != "file_change" || len(h.calls) != 1 {
		t.Fatalf("file pending: %+v %v calls=%d", result, err, len(h.calls))
	}
}
