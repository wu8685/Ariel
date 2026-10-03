package desktopipc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type interruptedInputHarness struct {
	states []json.RawMessage
	calls  []Request
}

func (h *interruptedInputHarness) Snapshot(context.Context) (json.RawMessage, error) {
	if len(h.states) == 0 {
		return json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[],"turns":[{"turnId":"turn","status":"interrupted"}]}`), nil
	}
	state := h.states[0]
	h.states = h.states[1:]
	return state, nil
}

func (h *interruptedInputHarness) Call(_ context.Context, r Request) (Reply, error) {
	h.calls = append(h.calls, r)
	switch r.Method {
	case "thread-follower-start-turn":
		return Reply{Result: json.RawMessage(`{"result":{"turn":{"id":"turn"}}}`)}, nil
	case "thread-follower-interrupt-turn":
		return Reply{Result: json.RawMessage(`{"ok":true,"interruptedTurnId":"turn"}`)}, nil
	default:
		return Reply{Result: json.RawMessage(`{"ok":true}`)}, nil
	}
}

func TestInterruptedInputFixtureNeverSubmitsExpiredAnswer(t *testing.T) {
	pending := strings.Replace(inputPending, `"question":"Text"}`, `"question":"Text","options":[{"label":"Preset A"},{"label":"Preset B"}]}`, 1)
	h := &interruptedInputHarness{states: []json.RawMessage{
		json.RawMessage(`{"cwd":"/fixture","latestModel":"fixture-model","threadRuntimeStatus":{"type":"idle"},"requests":[]}`),
		json.RawMessage(pending),
		json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[],"turns":[{"turnId":"turn","status":"interrupted"}]}`),
	}}
	r, err := exerciseUserInputInterrupted(context.Background(), h, "owner", "thread", "/fixture")
	if err != nil || !r.PendingObserved || !r.Interrupted || !r.ExpiredLocallyRejected || len(h.calls) != 2 || h.calls[0].Method != "thread-follower-start-turn" || h.calls[1].Method != "thread-follower-interrupt-turn" {
		t.Fatalf("result %+v, calls %+v, err %v", r, h.calls, err)
	}
}
