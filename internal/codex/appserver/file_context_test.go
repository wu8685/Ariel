package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type fileProofRPC struct {
	calls     int
	wrongTurn bool
	repeat    bool
}

func (r *fileProofRPC) Call(_ context.Context, method string, params, result any) error {
	p := params.(map[string]any)
	r.calls++
	var value any
	switch method {
	case "thread/read":
		if p["includeTurns"] != false {
			return errors.New("full history forbidden")
		}
		value = map[string]any{"thread": map[string]any{"id": "thread", "cwd": "/work/fixture"}}
	case "thread/items/list":
		if p["threadId"] != "thread" || p["turnId"] != "turn" || p["limit"] != 100 {
			return errors.New("wrong scope")
		}
		turn := "turn"
		if r.wrongTurn {
			turn = "other"
		}
		value = map[string]any{"data": []any{map[string]any{"turnId": turn, "item": map[string]any{"id": "file", "type": "fileChange", "changes": []any{map[string]any{"path": "a.md", "kind": map[string]any{"type": "update"}, "diff": "+new"}}}}}}
		if r.repeat {
			value.(map[string]any)["nextCursor"] = "repeat"
		}
	default:
		return errors.New("unexpected method")
	}
	body, _ := json.Marshal(value)
	return json.Unmarshal(body, result)
}

func TestFileSourceReadsMetadataAndOnlyTargetTurnItems(t *testing.T) {
	rpc := &fileProofRPC{}
	source := FileContextSource{RPC: rpc}
	thread, err := source.Thread(context.Background(), "thread")
	if err != nil || thread.CWD != "/work/fixture" {
		t.Fatalf("thread: %+v %v", thread, err)
	}
	items, err := source.Items(context.Background(), "thread", "turn")
	if err != nil || len(items) != 1 || items[0].Changes[0].Diff != "+new" || rpc.calls != 2 {
		t.Fatalf("items: %+v %v calls=%d", items, err, rpc.calls)
	}
	rpc.wrongTurn = true
	if _, err = source.Items(context.Background(), "thread", "turn"); err == nil {
		t.Fatal("cross-turn wrapper must fail closed")
	}
	rpc.wrongTurn = false
	rpc.repeat = true
	rpc.calls = 0
	if _, err = source.Items(context.Background(), "thread", "turn"); err == nil || rpc.calls > 32 {
		t.Fatalf("unbounded pagination: %v %d", err, rpc.calls)
	}
}
