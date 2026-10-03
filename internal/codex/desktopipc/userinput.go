package desktopipc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
)

var errNoPendingInput = errors.New("no pending user-input request")
var errInvalidInput = errors.New("user-input request is unknown, changed, expired, or outside fixture")

type inputQuestion struct {
	ID       string `json:"id"`
	IsSecret bool   `json:"isSecret"`
	Options  []struct {
		Label string `json:"label"`
	} `json:"options"`
}
type inputRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params struct {
		ThreadID  string          `json:"threadId"`
		TurnID    string          `json:"turnId"`
		Questions []inputQuestion `json:"questions"`
	} `json:"params"`
	raw json.RawMessage
}

func validRequestID(raw json.RawMessage) bool {
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return true
	}
	// Native request IDs are integers or strings. Do not coerce either type.
	_, err := strconv.ParseInt(string(raw), 10, 64)
	return err == nil
}
func sameJSON(a, b json.RawMessage) bool {
	decode := func(raw json.RawMessage) (any, error) {
		var v any
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		err := d.Decode(&v)
		return v, err
	}
	x, ex := decode(a)
	y, ey := decode(b)
	return ex == nil && ey == nil && reflect.DeepEqual(x, y)
}
func pendingUserInput(state json.RawMessage, threadID, turnID, cwd string) (inputRequest, error) {
	var p inputRequest
	var s struct {
		CWD      string            `json:"cwd"`
		Requests []json.RawMessage `json:"requests"`
	}
	if threadID == "" || turnID == "" || cwd == "" || json.Unmarshal(state, &s) != nil || s.CWD != cwd || s.Requests == nil {
		return p, errInvalidInput
	}
	if len(s.Requests) == 0 {
		return p, errNoPendingInput
	}
	if len(s.Requests) != 1 || json.Unmarshal(s.Requests[0], &p) != nil || !validRequestID(p.ID) || p.Method != "item/tool/requestUserInput" || p.Params.ThreadID != threadID || p.Params.TurnID != turnID || len(p.Params.Questions) == 0 {
		return p, errInvalidInput
	}
	seen := map[string]bool{}
	for _, q := range p.Params.Questions {
		if q.ID == "" || q.IsSecret || seen[q.ID] {
			return p, errInvalidInput
		}
		seen[q.ID] = true
	}
	p.raw = append(json.RawMessage(nil), s.Requests[0]...)
	return p, nil
}

// submitUserInput only confirms transport receipt, never application acceptance.
// Callers must supply a fresh owner snapshot and separately verify the echo.
func submitUserInput(ctx context.Context, c caller, owner, cwd string, state json.RawMessage, p inputRequest, answers map[string][]string) error {
	live, err := pendingUserInput(state, p.Params.ThreadID, p.Params.TurnID, cwd)
	status, known := nativeTurnStatus(state, p.Params.TurnID)
	if err != nil || !known || status != "inProgress" || owner == "" || !sameJSON(live.raw, p.raw) || len(answers) != len(p.Params.Questions) {
		return errInvalidInput
	}
	wire := map[string]any{}
	for _, q := range p.Params.Questions {
		a, ok := answers[q.ID]
		if !ok || len(a) != 1 || a[0] == "" {
			return errInvalidInput
		}
		wire[q.ID] = map[string]any{"answers": a}
	}
	r, err := c.Call(ctx, Request{Method: "thread-follower-submit-user-input", Version: 1, TargetClientID: owner, Mutating: true, Params: map[string]any{"conversationId": p.Params.ThreadID, "requestId": p.ID, "response": map[string]any{"answers": wire}}})
	if err != nil {
		return err
	}
	var receipt struct {
		OK bool `json:"ok"`
	}
	if json.Unmarshal(r.Result, &receipt) != nil || !receipt.OK {
		return errors.New("user-input receipt unknown")
	}
	return nil
}

func userInputEchoMatches(state json.RawMessage, p inputRequest, answers map[string][]string) bool {
	var s struct {
		Requests []struct {
			ID json.RawMessage `json:"id"`
		} `json:"requests"`
		Turns []struct {
			Items []json.RawMessage `json:"items"`
		} `json:"turns"`
		TurnHistory struct {
			Kind    string `json:"kind"`
			History struct {
				Islands []struct {
					Entries []struct {
						Value string `json:"value"`
					} `json:"entries"`
				} `json:"islands"`
				Entities map[string]struct {
					Items []json.RawMessage `json:"items"`
				} `json:"entitiesByKey"`
			} `json:"history"`
		} `json:"turnHistory"`
	}
	if json.Unmarshal(state, &s) != nil || s.Requests == nil {
		return false
	}
	for _, r := range s.Requests {
		if sameJSON(r.ID, p.ID) {
			return false
		}
	}
	if s.TurnHistory.Kind == "canonical" {
		s.Turns = nil
		for _, island := range s.TurnHistory.History.Islands {
			for _, entry := range island.Entries {
				turn, ok := s.TurnHistory.History.Entities[entry.Value]
				if !ok {
					return false
				}
				s.Turns = append(s.Turns, turn)
			}
		}
	}
	for _, t := range s.Turns {
		for _, raw := range t.Items {
			var item struct {
				Type      string              `json:"type"`
				RequestID json.RawMessage     `json:"requestId"`
				TurnID    string              `json:"turnId"`
				Completed bool                `json:"completed"`
				Answers   map[string][]string `json:"answers"`
			}
			if json.Unmarshal(raw, &item) == nil && item.Type == "userInputResponse" && sameJSON(item.RequestID, p.ID) && item.TurnID == p.Params.TurnID && item.Completed && reflect.DeepEqual(item.Answers, answers) {
				return true
			}
		}
	}
	return false
}
