package desktopipc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

var ErrTurnBusy = errors.New("Desktop thread is busy or interaction pending")
var ErrStaleTurn = errors.New("Desktop turn no longer matches expected turn")
var ErrNativeStateUncertain = errors.New("NATIVE_STATE_UNCERTAIN")

func nativeCanonicalAddressable(state json.RawMessage) bool {
	var history struct {
		TurnHistory struct {
			Kind    string `json:"kind"`
			History struct {
				Islands []struct {
					Entries []struct {
						Value string `json:"value"`
					} `json:"entries"`
				} `json:"islands"`
				Entities map[string]struct {
					TurnID string `json:"turnId"`
				} `json:"entitiesByKey"`
			} `json:"history"`
		} `json:"turnHistory"`
	}
	if json.Unmarshal(state, &history) != nil {
		return false
	}
	if history.TurnHistory.Kind == "" {
		return true
	}
	if history.TurnHistory.Kind != "canonical" {
		return false
	}
	for _, island := range history.TurnHistory.History.Islands {
		for _, entry := range island.Entries {
			turn, ok := history.TurnHistory.History.Entities[entry.Value]
			if !ok || turn.TurnID == "" {
				return false
			}
		}
	}
	return true
}

// TransientCanonicalPlaceholder recognizes only the empty, in-progress turn
// emitted while the Desktop owner is assigning a native turn identity. It is
// never safe to publish or mutate this state; callers may only wait briefly
// for a subsequent addressable owner update.
func TransientCanonicalPlaceholder(state json.RawMessage) bool {
	var s struct {
		Runtime struct {
			Type string `json:"type"`
		} `json:"threadRuntimeStatus"`
		Requests    []json.RawMessage `json:"requests"`
		TurnHistory struct {
			Kind    string `json:"kind"`
			History struct {
				Islands []struct {
					Entries []struct {
						Value string `json:"value"`
					} `json:"entries"`
				} `json:"islands"`
				Entities map[string]struct {
					TurnID string            `json:"turnId"`
					Status string            `json:"status"`
					Items  []json.RawMessage `json:"items"`
				} `json:"entitiesByKey"`
			} `json:"history"`
		} `json:"turnHistory"`
	}
	if json.Unmarshal(state, &s) != nil || (s.Runtime.Type != "active" && s.Runtime.Type != "inProgress") || s.Requests == nil || len(s.Requests) != 0 || s.TurnHistory.Kind != "canonical" {
		return false
	}
	ghosts := 0
	for _, island := range s.TurnHistory.History.Islands {
		for _, entry := range island.Entries {
			turn, ok := s.TurnHistory.History.Entities[entry.Value]
			if !ok {
				return false
			}
			if turn.TurnID == "" {
				if turn.Status != "inProgress" || turn.Items == nil || len(turn.Items) != 0 {
					return false
				}
				ghosts++
			}
		}
	}
	return ghosts == 1
}

func StartProductionTurn(ctx context.Context, c caller, owner, threadID, cwd string, state json.RawMessage, clientMessageID, text string) (string, error) {
	if owner == "" || threadID == "" || clientMessageID == "" || strings.TrimSpace(text) == "" {
		return "", ErrProtocol
	}
	if !nativeCanonicalAddressable(state) {
		return "", ErrNativeStateUncertain
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
