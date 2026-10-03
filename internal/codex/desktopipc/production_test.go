package desktopipc

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestProductionStartTreatsUnverifiableNativeReceiptAsUnknown(t *testing.T) {
	owner := &recordingOwner{reply: Reply{Result: json.RawMessage(`{"result":{}}`)}}
	idle := json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[],"turns":[]}`)
	_, err := StartProductionTurn(context.Background(), owner, "owner", "thread", "/fixture", idle, "client-id", "hello")
	var call *CallError
	if !errors.As(err, &call) || call.Outcome != "unknown" || len(owner.calls) != 1 {
		t.Fatalf("ambiguous mutating receipt classified as safe rejection: %v", err)
	}
}

func TestProductionStartRejectsUnaddressableCanonicalTurn(t *testing.T) {
	owner := &recordingOwner{}
	state := json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[],"turnHistory":{"kind":"canonical","history":{"islands":[{"entries":[{"value":"ghost"}]}],"entitiesByKey":{"ghost":{"turnId":"","status":"inProgress","items":[]}}}}}`)
	_, err := StartProductionTurn(context.Background(), owner, "owner", "thread", "/fixture", state, "client-id", "hello")
	if err == nil || err.Error() != "NATIVE_STATE_UNCERTAIN" || len(owner.calls) != 0 {
		t.Fatalf("unaddressable turn reached owner: err=%v calls=%d", err, len(owner.calls))
	}
}

func TestProductionStartRejectsUnknownNativeHistoryKind(t *testing.T) {
	owner := &recordingOwner{}
	state := json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[],"turnHistory":{"kind":"futureFormat"}}`)
	_, err := StartProductionTurn(context.Background(), owner, "owner", "thread", "/fixture", state, "client-id", "hello")
	if err == nil || err.Error() != "NATIVE_STATE_UNCERTAIN" || len(owner.calls) != 0 {
		t.Fatalf("unsupported history reached owner: err=%v calls=%d", err, len(owner.calls))
	}
}

func TestProductionStartRejectsIncompleteNativeHistoryBeforeOwnerCall(t *testing.T) {
	for _, state := range []string{
		`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[],"turnHistory":{"kind":"canonical"}}`,
		`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[],"turnHistory":{"kind":"canonical","history":{"islands":[],"entitiesByKey":null}}}`,
		`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[],"turns":[{"turnId":"","status":"completed","items":[]}]}`,
		`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[]}`,
	} {
		owner := &recordingOwner{}
		_, err := StartProductionTurn(context.Background(), owner, "owner", "thread", "/fixture", json.RawMessage(state), "client-id", "hello")
		if !errors.Is(err, ErrNativeStateUncertain) || len(owner.calls) != 0 {
			t.Fatalf("incomplete history reached owner: %s err=%v calls=%d", state, err, len(owner.calls))
		}
	}
}

type recordingOwner struct {
	calls []Request
	reply Reply
}

func (o *recordingOwner) Call(_ context.Context, req Request) (Reply, error) {
	o.calls = append(o.calls, req)
	return o.reply, nil
}

func TestProductionStartRejectsBusyBeforeNativeCallAndPreservesMessageID(t *testing.T) {
	owner := &recordingOwner{reply: Reply{Result: json.RawMessage(`{"result":{"turn":{"id":"new-turn"}}}`)}}
	busy := json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"inProgress"},"requests":[],"turns":[{"turnId":"old","status":"inProgress"}]}`)
	if _, err := StartProductionTurn(context.Background(), owner, "owner", "thread", "/fixture", busy, "00000000-0000-4000-8000-000000000001", "hello"); err == nil || len(owner.calls) != 0 {
		t.Fatalf("busy forwarded: %v %v", err, owner.calls)
	}
	idle := json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[],"turns":[]}`)
	turnID, err := StartProductionTurn(context.Background(), owner, "owner", "thread", "/fixture", idle, "00000000-0000-4000-8000-000000000001", "hello")
	if err != nil || turnID != "new-turn" || len(owner.calls) != 1 {
		t.Fatalf("start: %q %v %+v", turnID, err, owner.calls)
	}
	b, _ := json.Marshal(owner.calls[0].Params)
	if !json.Valid(b) || !containsJSON(b, "clientUserMessageId", "00000000-0000-4000-8000-000000000001") {
		t.Fatalf("message ID lost: %s", b)
	}
}

func TestProductionInterruptRejectsStaleTurnBeforeNativeCall(t *testing.T) {
	owner := &recordingOwner{}
	state := json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"inProgress"},"requests":[],"turns":[{"turnId":"current","status":"inProgress"}]}`)
	if err := InterruptProductionTurn(context.Background(), owner, "owner", "thread", "/fixture", state, "old"); err == nil || len(owner.calls) != 0 {
		t.Fatalf("stale stop forwarded: %v", err)
	}
}

func containsJSON(b []byte, key, want string) bool {
	var root any
	if json.Unmarshal(b, &root) != nil {
		return false
	}
	var find func(any) bool
	find = func(v any) bool {
		switch x := v.(type) {
		case map[string]any:
			if x[key] == want {
				return true
			}
			for _, child := range x {
				if find(child) {
					return true
				}
			}
		case []any:
			for _, child := range x {
				if find(child) {
					return true
				}
			}
		}
		return false
	}
	return find(root)
}
