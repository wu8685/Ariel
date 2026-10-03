package desktopipc

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestFixtureStartRefusesBusyUnknownAndWrongWorkspace(t *testing.T) {
	for _, tc := range []struct {
		state string
		ok    bool
	}{
		{`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[]}`, true},
		{`{"cwd":"/fixture","threadRuntimeStatus":{"type":"active"},"requests":[]}`, false},
		{`{"cwd":"/fixture","requests":[]}`, false},
		{`{"cwd":"/business","threadRuntimeStatus":{"type":"idle"},"requests":[]}`, false},
		{`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"}}`, false},
		{`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[{}]}`, false},
	} {
		f := &controlRPC{}
		turn, err := startFixtureTurn(context.Background(), f, "owner", "fixture", "/fixture", json.RawMessage(tc.state), "marker")
		if (err == nil) != tc.ok {
			t.Fatalf("%s: %v", tc.state, err)
		}
		if tc.ok && (turn != "returned-turn" || f.calls != 1) {
			t.Fatal("acceptance not bound to real turn")
		}
		if !tc.ok && f.calls != 0 {
			t.Fatal("invalid state reached owner")
		}
	}
}
func TestFixtureInterruptRequiresExactTurnAndMatchingReceipt(t *testing.T) {
	f := &controlRPC{}
	if err := interruptFixtureTurn(context.Background(), f, "owner", "fixture", ""); err == nil || f.calls != 0 {
		t.Fatal("unscoped interrupt allowed")
	}
	if err := interruptFixtureTurn(context.Background(), f, "owner", "fixture", "returned-turn"); err != nil {
		t.Fatal(err)
	}
	f.wrongReceipt = true
	if err := interruptFixtureTurn(context.Background(), f, "owner", "fixture", "returned-turn"); err == nil {
		t.Fatal("wrong interrupted ID accepted")
	}
}

type controlRPC struct {
	calls        int
	wrongReceipt bool
}

func (f *controlRPC) Call(ctx context.Context, req Request) (Reply, error) {
	f.calls++
	if !req.Mutating || req.TargetClientID != "owner" {
		return Reply{}, errors.New("not bound to owner")
	}
	var body string
	switch req.Method {
	case "thread-follower-start-turn":
		if req.Version != 2 {
			return Reply{}, errors.New("wrong method version")
		}
		b, _ := json.Marshal(req.Params)
		var params struct {
			ThreadID  string `json:"conversationId"`
			TurnStart struct {
				Request struct {
					ThreadID string `json:"threadId"`
					ClientID string `json:"clientUserMessageId"`
				} `json:"request"`
				Context map[string]any `json:"context"`
			} `json:"turnStart"`
		}
		json.Unmarshal(b, &params)
		if params.ThreadID != "fixture" || params.TurnStart.Request.ThreadID != "fixture" || params.TurnStart.Request.ClientID == "" || len(params.TurnStart.Context) != 1 || params.TurnStart.Context["inheritThreadSettings"] != true {
			return Reply{}, errors.New("wrong inheritance or identity")
		}
		body = `{"result":{"turn":{"id":"returned-turn"}}}`
	case "thread-follower-interrupt-turn":
		b, _ := json.Marshal(req.Params)
		var p map[string]any
		json.Unmarshal(b, &p)
		if req.Version != 4 || p["expectedTurnId"] != "returned-turn" || p["mode"] != "user-stop" {
			return Reply{}, errors.New("not exact interrupt")
		}
		// Inspected 0.160.0 follower handler unwraps interrupt directly, unlike start.
		body = `{"ok":true,"interruptedTurnId":"returned-turn"}`
		if f.wrongReceipt {
			body = `{"ok":true,"interruptedTurnId":"different-turn"}`
		}
	default:
		return Reply{}, errors.New("unexpected method")
	}
	return Reply{Result: json.RawMessage(body)}, nil
}
