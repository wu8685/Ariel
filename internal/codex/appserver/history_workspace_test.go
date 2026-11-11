package appserver

import (
	"context"
	"encoding/json"
	"errors"
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
