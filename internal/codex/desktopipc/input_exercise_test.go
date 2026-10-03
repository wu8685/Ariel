package desktopipc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestStartUserInputInheritsCurrentModelAndKeepsSandbox(t *testing.T) {
	c := &inputCaller{}
	_, _ = startInputFixtureTurn(context.Background(), c, "owner", "thread", "/fixture", json.RawMessage(`{"cwd":"/fixture","latestModel":"fixture-model","threadRuntimeStatus":{"type":"idle"},"requests":[]}`), "questions")
	if c.calls != 1 {
		t.Fatal("no start")
	}
	b, _ := json.Marshal(c.req.Params)
	var p struct {
		TurnStart struct {
			Request map[string]json.RawMessage `json:"request"`
			Context map[string]bool            `json:"context"`
		} `json:"turnStart"`
	}
	json.Unmarshal(b, &p)
	var mode struct {
		Mode     string `json:"mode"`
		Settings struct {
			Model string `json:"model"`
		} `json:"settings"`
	}
	json.Unmarshal(p.TurnStart.Request["collaborationMode"], &mode)
	if mode.Mode != "plan" || mode.Settings.Model != "fixture-model" || !p.TurnStart.Context["inheritThreadSettings"] || string(p.TurnStart.Request["approvalPolicy"]) != `"never"` || string(p.TurnStart.Request["sandboxPolicy"]) != `{"type":"readOnly"}` {
		t.Fatalf("unsafe mode: %s", b)
	}
	_, err := startInputFixtureTurn(context.Background(), c, "owner", "thread", "/fixture", json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[]}`), "questions")
	if err == nil || c.calls != 1 {
		t.Fatal("guessed missing model")
	}
}

type inputHarness struct {
	states []json.RawMessage
	calls  []Request
	marker string
}

func (h *inputHarness) Snapshot(context.Context) (json.RawMessage, error) {
	if len(h.states) > 0 {
		s := h.states[0]
		h.states = h.states[1:]
		return s, nil
	}
	return json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[],"turns":[{"items":[{"type":"userInputResponse","requestId":42,"turnId":"turn","completed":true,"answers":{"choice":["Blue"],"note":["ariel free text"]}},{"type":"assistantMessage","text":"` + h.marker + `"}]}]}`), nil
}
func (h *inputHarness) Call(_ context.Context, r Request) (Reply, error) {
	h.calls = append(h.calls, r)
	if r.Method == "thread-follower-start-turn" {
		b, _ := json.Marshal(r.Params)
		var p struct {
			TurnStart struct {
				Request struct {
					Input []struct {
						Text string `json:"text"`
					} `json:"input"`
				} `json:"request"`
			} `json:"turnStart"`
		}
		json.Unmarshal(b, &p)
		text := p.TurnStart.Request.Input[0].Text
		i := strings.Index(text, "ARIEL_INPUT_")
		if i >= 0 {
			h.marker = strings.Fields(text[i:])[0]
		}
		return Reply{Result: json.RawMessage(`{"result":{"turn":{"id":"turn"}}}`)}, nil
	}
	return Reply{Result: json.RawMessage(`{"ok":true}`)}, nil
}
func TestUserInputExperimentWaitsForEchoAndDoesNotReplay(t *testing.T) {
	withOptions := strings.Replace(inputPending, `"question":"Text"}`, `"question":"Text","options":[{"label":"Preset A","description":"a"},{"label":"Preset B","description":"b"}]}`, 1)
	h := &inputHarness{states: []json.RawMessage{json.RawMessage(`{"cwd":"/fixture","latestModel":"fixture-model","threadRuntimeStatus":{"type":"idle"},"requests":[]}`), json.RawMessage(withOptions), json.RawMessage(withOptions)}}
	r, err := exerciseUserInput(context.Background(), h, "owner", "thread", "/fixture")
	if err != nil || !r.EchoVerified || !r.Completed || !r.ExpiredLocallyRejected || r.QuestionCount != 2 || r.Stage != "completed" {
		t.Fatalf("result: %+v %v", r, err)
	}
	if len(h.calls) != 2 {
		t.Fatalf("unexpected replay: %d", len(h.calls))
	}
}
