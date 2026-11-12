package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type workspaceRPC struct{ body string }

func (r workspaceRPC) Call(_ context.Context, method string, _ any, result any) error {
	if method != "thread/read" {
		return errors.New(method)
	}
	return json.Unmarshal([]byte(r.body), result)
}

func TestReadKeepsSessionMetaWorkspaceApartFromProcessCWD(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	body := `{"thread":{"id":"` + id + `","cwd":"/Users/owner/.ariel/puck/assistant","session_meta":{"payload":{"cwd":"/work/watched"}},"turns":[{"id":"turn-1","status":"completed","cwd":"/work/from-turn","items":[]}]}}`
	thread, err := (HistoryReader{RPC: workspaceRPC{body: body}}).Read(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if thread.CWD != "/Users/owner/.ariel/puck/assistant" || thread.SessionCWD != "/work/watched" || len(thread.Turns) != 1 || thread.Turns[0].CWD != "/work/from-turn" {
		t.Fatalf("thread=%+v", thread)
	}
	if SessionMetaCWD(json.RawMessage(`{"sessionMeta":{"cwd":"/work/camel"}}`)) != "/work/camel" {
		t.Fatal("camelCase sessionMeta cwd was dropped")
	}
}

func TestReadPrefersSessionMetaWhenThreadCwdIsTheAgentStateDir(t *testing.T) {
	const id = "22222222-2222-4222-8222-222222222222"
	const agentDir = "/Users/owner/Library/Application Support/ArielLocal"
	const workspace = "/Users/owner/tmp/real-workspace"
	body, err := json.Marshal(map[string]any{
		"thread": map[string]any{
			"id": id, "cwd": agentDir, "name": "fix tests", "updatedAt": 1760000000,
			"status": map[string]any{"type": "idle"}, "turns": []any{},
			"cliVersion": "0.160.0", "source": "appServer",
			"session_meta": map[string]any{
				"timestamp": "2026-11-11T00:00:00.000Z",
				"type":      "session_meta",
				"payload": map[string]any{
					"id": id, "cwd": workspace, "originator": "codex_cli_rs",
					"cli_version": "0.160.0", "source": "vscode",
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := (HistoryReader{RPC: workspaceRPC{body: string(body)}}).Read(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if thread.CWD != agentDir || thread.SessionCWD != workspace {
		t.Fatalf("thread cwd=%q session=%q", thread.CWD, thread.SessionCWD)
	}
}

func TestReadLoadsSessionMetaFromRolloutWhenThreadReadOmitsIt(t *testing.T) {
	const id = "33333333-3333-4333-8333-333333333333"
	const agentDir = "/Users/owner/Library/Application Support/ArielLocal"
	const workspace = "/Users/owner/tmp/real-workspace"
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	line := `{"timestamp":"2026-11-11T00:00:00.000Z","type":"session_meta","payload":{"id":"` + id + `","cwd":"` + workspace + `","originator":"codex_cli_rs","cli_version":"0.160.0","source":"vscode"}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{
		"thread": map[string]any{
			"id": id, "cwd": agentDir, "path": path, "name": "fix tests",
			"updatedAt": 1760000000, "status": map[string]any{"type": "idle"}, "turns": []any{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := (HistoryReader{RPC: workspaceRPC{body: string(body)}}).Read(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if thread.CWD != agentDir || thread.SessionCWD != workspace || thread.Path != path {
		t.Fatalf("thread=%+v", thread)
	}
}

func TestThreadAcceptsStringUpdatedAtWrappedIDAndObjectTurnStatus(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	raw := []byte(`{"thread":{"threadId":"` + id + `","cwd":"/work","updatedAt":"2026-11-12T00:00:00Z","rollout_path":"/tmp/rollout.jsonl","status":{"type":"idle"},"turns":[{"turnId":"turn-1","status":{"type":"completed"},"items":[]}]}}`)
	var thread Thread
	if err := json.Unmarshal(raw, &thread); err != nil {
		t.Fatal(err)
	}
	if thread.ID != id || thread.Path != "/tmp/rollout.jsonl" || thread.UpdatedAt == 0 || len(thread.Turns) != 1 || thread.Turns[0].ID != "turn-1" || thread.Turns[0].Status != "completed" {
		t.Fatalf("%+v", thread)
	}
}
