package desktopipc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestInputRaceRequiresOneExactEchoNotReceipts(t *testing.T) {
	p, err := pendingUserInput(json.RawMessage(inputPending), "thread", "turn", "/fixture")
	if err != nil {
		t.Fatal(err)
	}
	first := map[string][]string{"choice": {"Blue"}, "note": {"first"}}
	second := map[string][]string{"choice": {"Green"}, "note": {"second"}}
	state := json.RawMessage(`{"requests":[],"turns":[{"items":[{"type":"userInputResponse","requestId":42,"turnId":"turn","completed":true,"answers":{"choice":["Green"],"note":["second"]}}]}]}`)
	if got := classifyInputRace(state, p, first, second); got != "second" {
		t.Fatalf("winner %s", got)
	}
	if got := classifyInputRace(json.RawMessage(`{"requests":[],"turns":[]}`), p, first, second); got != "unknown" {
		t.Fatalf("false winner %s", got)
	}
	if got := classifyInputRace(json.RawMessage(`{"requests":[{"id":42}],"turns":[]}`), p, first, second); got != "unknown" {
		t.Fatalf("pending winner %s", got)
	}
	both := json.RawMessage(`{"requests":[],"turns":[{"items":[{"type":"userInputResponse","requestId":42,"turnId":"turn","completed":true,"answers":{"choice":["Blue"],"note":["first"]}},{"type":"userInputResponse","requestId":42,"turnId":"turn","completed":true,"answers":{"choice":["Green"],"note":["second"]}}]}]}`)
	if got := classifyInputRace(both, p, first, second); got != "unknown" {
		t.Fatalf("ambiguous winner %s", got)
	}
	if got := classifyInputRace(state, p, second, second); got != "unknown" {
		t.Fatalf("same-answer call attribution %s", got)
	}
}

func TestCompetingInputSubmissionsAreOnceEachAndGuarded(t *testing.T) {
	p, err := pendingUserInput(json.RawMessage(inputPending), "thread", "turn", "/fixture")
	if err != nil {
		t.Fatal(err)
	}
	first, second := &inputCaller{}, &inputCaller{}
	a := map[string][]string{"choice": {"Blue"}, "note": {"first"}}
	b := map[string][]string{"choice": {"Green"}, "note": {"second"}}
	r1, r2 := submitCompetingInputs(context.Background(), first, second, "owner", "/fixture", json.RawMessage(inputPending), p, a, b)
	if r1 != nil || r2 != nil || first.calls != 1 || second.calls != 1 {
		t.Fatalf("calls %d %d; errors %v %v", first.calls, second.calls, r1, r2)
	}
	first, second = &inputCaller{}, &inputCaller{}
	r1, r2 = submitCompetingInputs(context.Background(), first, second, "owner", "/fixture", json.RawMessage(`{"cwd":"/fixture","requests":[]}`), p, a, b)
	if r1 == nil || r2 == nil || first.calls != 0 || second.calls != 0 {
		t.Fatal("expired native request was sent")
	}
}

type raceInputHarness struct {
	states []json.RawMessage
	calls  []Request
	marker string
}

func (h *raceInputHarness) Snapshot(context.Context) (json.RawMessage, error) {
	if len(h.states) > 0 {
		state := h.states[0]
		h.states = h.states[1:]
		return state, nil
	}
	return json.RawMessage(fmt.Sprintf(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[],"turns":[{"turnId":"turn","status":"completed","items":[{"type":"userInputResponse","requestId":42,"turnId":"turn","completed":true,"answers":{"choice":["Green"],"note":["ariel second free text"]}},{"type":"agentMessage","text":%q}]}]}`, h.marker)), nil
}

func (h *raceInputHarness) Call(_ context.Context, r Request) (Reply, error) {
	h.calls = append(h.calls, r)
	if r.Method == "thread-follower-start-turn" {
		body, _ := json.Marshal(r.Params)
		start := strings.Index(string(body), "ARIEL_INPUT_RACE_")
		if start >= 0 {
			h.marker = strings.Fields(string(body)[start:])[0]
		}
		return Reply{Result: json.RawMessage(`{"result":{"turn":{"id":"turn"}}}`)}, nil
	}
	if r.Method == "thread-follower-interrupt-turn" {
		return Reply{Result: json.RawMessage(`{"ok":true,"interruptedTurnId":"turn"}`)}, nil
	}
	return Reply{Result: json.RawMessage(`{"ok":true}`)}, nil
}

func TestInputRaceFixtureReportsOneEchoDespiteTwoReceipts(t *testing.T) {
	pending := strings.Replace(inputPending, `"question":"Text"}`, `"question":"Text","options":[{"label":"Preset A"},{"label":"Preset B"}]}`, 1)
	h := &raceInputHarness{states: []json.RawMessage{
		json.RawMessage(`{"cwd":"/fixture","latestModel":"fixture-model","threadRuntimeStatus":{"type":"idle"},"requests":[]}`),
		json.RawMessage(pending),
		json.RawMessage(pending),
	}}
	second := &inputCaller{}
	r, err := exerciseUserInputRace(context.Background(), h, second, "owner", "thread", "/fixture")
	if err != nil || r.Winner != "second" || !r.FirstReceiptOK || !r.SecondReceiptOK || !r.Completed || len(h.calls) != 2 || second.calls != 1 {
		t.Fatalf("result %+v, calls %d %d, err %v", r, len(h.calls), second.calls, err)
	}
}

func TestInputRaceFixtureDoesNotSubmitChangedRequest(t *testing.T) {
	pending := strings.Replace(inputPending, `"question":"Text"}`, `"question":"Text","options":[{"label":"Preset A"},{"label":"Preset B"}]}`, 1)
	h := &raceInputHarness{states: []json.RawMessage{
		json.RawMessage(`{"cwd":"/fixture","latestModel":"fixture-model","threadRuntimeStatus":{"type":"idle"},"requests":[]}`),
		json.RawMessage(pending),
		json.RawMessage(`{"cwd":"/fixture","requests":[],"turns":[{"turnId":"turn","status":"inProgress"}]}`),
	}}
	second := &inputCaller{}
	_, err := exerciseUserInputRace(context.Background(), h, second, "owner", "thread", "/fixture")
	if err == nil || len(h.calls) != 2 || h.calls[0].Method != "thread-follower-start-turn" || h.calls[1].Method != "thread-follower-interrupt-turn" || second.calls != 0 {
		t.Fatalf("changed request was sent: calls %+v, second %d, err %v", h.calls, second.calls, err)
	}
}
