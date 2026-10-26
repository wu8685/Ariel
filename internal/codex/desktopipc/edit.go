package desktopipc

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

type EditTarget struct {
	TurnID      string `json:"turnId"`
	ItemID      string `json:"itemId"`
	Text        string `json:"text"`
	Fingerprint string `json:"fingerprint"`
	ClientID    string `json:"-"`
}
type editInput struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
type editTurn struct {
	TurnID string `json:"turnId"`
	Status string `json:"status"`
	Params struct {
		Input    []editInput `json:"input"`
		ClientID string      `json:"clientUserMessageId"`
		Trigger  string      `json:"turnTrigger"`
	} `json:"params"`
	Items []struct {
		ID       string      `json:"id"`
		Type     string      `json:"type"`
		ClientID string      `json:"clientId"`
		Content  []editInput `json:"content"`
		Input    []editInput `json:"input"`
	} `json:"items"`
}

func editTurns(raw json.RawMessage, cwd string) ([]editTurn, string, error) {
	var s struct {
		CWD     string `json:"cwd"`
		Runtime struct {
			Type string `json:"type"`
		} `json:"threadRuntimeStatus"`
		Requests []json.RawMessage `json:"requests"`
		Turns    []editTurn        `json:"turns"`
		History  struct {
			Kind    string `json:"kind"`
			History struct {
				Islands []struct {
					Entries []struct {
						Value string `json:"value"`
					} `json:"entries"`
				} `json:"islands"`
				Entities map[string]editTurn `json:"entitiesByKey"`
			} `json:"history"`
		} `json:"turnHistory"`
	}
	if json.Unmarshal(raw, &s) != nil || s.CWD != cwd || s.Requests == nil || len(s.Requests) != 0 {
		return nil, "", ErrNativeStateUncertain
	}
	turns := s.Turns
	if s.History.Kind == "canonical" {
		turns = []editTurn{}
		for _, island := range s.History.History.Islands {
			for _, entry := range island.Entries {
				t, ok := s.History.History.Entities[entry.Value]
				if !ok || t.TurnID == "" {
					return nil, "", ErrNativeStateUncertain
				}
				turns = append(turns, t)
			}
		}
	} else if s.History.Kind != "" || turns == nil {
		return nil, "", ErrNativeStateUncertain
	}
	return turns, s.Runtime.Type, nil
}
func editUser(t editTurn) (EditTarget, error) {
	out := EditTarget{TurnID: t.TurnID, ClientID: t.Params.ClientID}
	for _, item := range t.Items {
		if item.Type == "steeringUserMessage" {
			return out, ErrNativeStateUncertain
		}
		if item.Type != "userMessage" {
			continue
		}
		if out.ItemID != "" {
			return out, ErrNativeStateUncertain
		}
		content := item.Content
		if content == nil {
			content = item.Input
		}
		if len(content) != 1 || content[0].Type != "text" {
			return out, ErrNativeStateUncertain
		}
		out.ItemID = item.ID
		out.Text = content[0].Text
		if out.ClientID == "" {
			out.ClientID = item.ClientID
		}
	}
	if out.ItemID == "" || strings.TrimSpace(out.Text) == "" || len(out.Text) > 65536 || strings.Contains(out.Text, "<ariel_file_context") || strings.Contains(out.Text, "<file_context") || strings.Contains(out.Text, "[File context") {
		return out, ErrNativeStateUncertain
	}
	if len(t.Params.Input) != 1 || t.Params.Input[0].Type != "text" || t.Params.Input[0].Text != out.Text {
		return out, ErrNativeStateUncertain
	}
	out.Fingerprint = fmt.Sprintf("%x", sha256.Sum256([]byte(out.Text)))
	return out, nil
}
func LatestEditTarget(raw json.RawMessage, cwd string) (EditTarget, error) {
	turns, runtime, err := editTurns(raw, cwd)
	if err != nil {
		return EditTarget{}, err
	}
	if runtime != "idle" || len(turns) == 0 || turns[len(turns)-1].Status != "interrupted" {
		return EditTarget{}, ErrStaleTurn
	}
	return editUser(turns[len(turns)-1])
}

// This confirms only receipt shape; Follower.EditLatest requires an exact owner
// state postcondition before reporting an accepted replacement.
func EditProductionLatestUserTurn(ctx context.Context, c caller, owner, threadID, cwd string, raw json.RawMessage, turnID, itemID, fingerprint, text string) error {
	if owner == "" || threadID == "" || strings.TrimSpace(text) == "" || len(text) > 65536 {
		return ErrProtocol
	}
	target, err := LatestEditTarget(raw, cwd)
	if err != nil {
		return err
	}
	if target.TurnID != turnID || target.ItemID != itemID || target.Fingerprint != fingerprint {
		return ErrStaleTurn
	}
	r, err := c.Call(ctx, Request{Method: "thread-follower-edit-last-user-turn", Version: 2, TargetClientID: owner, Mutating: true, Params: map[string]any{"conversationId": threadID, "turnId": turnID, "message": text, "shouldSendPermissionOverrides": false}})
	if err != nil {
		return &CallError{Cause: err, Outcome: "unknown"}
	}
	var receipt struct {
		OK bool `json:"ok"`
	}
	if json.Unmarshal(r.Result, &receipt) != nil || !receipt.OK {
		return &CallError{Cause: ErrProtocol, Outcome: "unknown"}
	}
	return nil
}
func verifyEdited(before, after json.RawMessage, cwd string, target EditTarget, text string) (string, bool) {
	old, _, e := editTurns(before, cwd)
	if e != nil {
		return "", false
	}
	next, _, e := editTurns(after, cwd)
	if e != nil || len(next) != len(old) || len(next) == 0 {
		return "", false
	}
	for i := 0; i < len(old)-1; i++ {
		if old[i].TurnID != next[i].TurnID {
			return "", false
		}
	}
	t := next[len(next)-1]
	if t.TurnID == target.TurnID || t.TurnID == "" {
		return "", false
	}
	candidate, e := editUser(t)
	return t.TurnID, e == nil && candidate.Text == text && candidate.ClientID != "" && t.Params.Trigger == "edit_user_message"
}
func (f *Follower) EditLatest(ctx context.Context, turnID, itemID, fingerprint, text string) (string, error) {
	f.startMu.Lock()
	defer f.startMu.Unlock()
	before, err := f.Current()
	if err != nil {
		return "", err
	}
	target, err := LatestEditTarget(before, f.cwd)
	if err != nil {
		return "", err
	}
	if err = EditProductionLatestUserTurn(ctx, f.c, f.o.owner, f.o.threadID, f.cwd, before, turnID, itemID, fingerprint, text); err != nil {
		return "", err
	}
	for {
		raw, e := f.Current()
		if e != nil {
			return "", &CallError{Cause: e, Outcome: "unknown"}
		}
		if id, ok := verifyEdited(before, raw, f.cwd, target, text); ok {
			return id, nil
		}
		select {
		case <-f.wake:
		case <-ctx.Done():
			return "", &CallError{Cause: ctx.Err(), Outcome: "unknown"}
		case <-f.Done():
			return "", &CallError{Cause: ErrResyncRequired, Outcome: "unknown"}
		}
	}
}
