package desktopipc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

var ErrTurnBusy = errors.New("Desktop thread is busy or interaction pending")
var ErrStaleTurn = errors.New("Desktop turn no longer matches expected turn")

func StartProductionTurn(ctx context.Context, c caller, owner, threadID, cwd string, state json.RawMessage, clientMessageID, text string) (string, error) {
	if owner == "" || threadID == "" || clientMessageID == "" || strings.TrimSpace(text) == "" {
		return "", ErrProtocol
	}
	if !idleFixture(state, cwd) {
		return "", ErrTurnBusy
	}
	r, err := c.Call(ctx, Request{Method: "thread-follower-start-turn", Version: 2, TargetClientID: owner, Mutating: true, Params: map[string]any{
		"conversationId": threadID, "turnStart": map[string]any{"request": map[string]any{"threadId": threadID, "clientUserMessageId": clientMessageID, "input": []any{map[string]any{"type": "text", "text": text, "text_elements": []any{}}}}, "context": map[string]bool{"inheritThreadSettings": true}},
	}})
	if err != nil {
		return "", err
	}
	var accepted struct {
		Result struct {
			Turn struct {
				ID string `json:"id"`
			} `json:"turn"`
		} `json:"result"`
	}
	if json.Unmarshal(r.Result, &accepted) != nil || accepted.Result.Turn.ID == "" {
		return "", &CallError{Cause: ErrProtocol, Outcome: "unknown"}
	}
	if _, alreadyPresent := nativeTurnStatus(state, accepted.Result.Turn.ID); alreadyPresent {
		return "", &CallError{Cause: errors.New("native returned existing turn"), Outcome: "unknown"}
	}
	return accepted.Result.Turn.ID, nil
}

func InterruptProductionTurn(ctx context.Context, c caller, owner, threadID, cwd string, state json.RawMessage, expectedTurnID string) error {
	if owner == "" || threadID == "" || expectedTurnID == "" {
		return ErrProtocol
	}
	var s struct {
		CWD string `json:"cwd"`
	}
	if json.Unmarshal(state, &s) != nil || s.CWD != cwd {
		return ErrProtocol
	}
	status, known := nativeTurnStatus(state, expectedTurnID)
	if !known || status != "inProgress" {
		return ErrStaleTurn
	}
	return interruptFixtureTurn(ctx, c, owner, threadID, expectedTurnID)
}
