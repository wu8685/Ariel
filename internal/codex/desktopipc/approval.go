package desktopipc

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

var errInvalidApproval = errors.New("command approval is unknown, changed, expired, outside fixture, or does not offer the requested rejection")

type commandRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params struct {
		ThreadID           string            `json:"threadId"`
		TurnID             string            `json:"turnId"`
		ItemID             string            `json:"itemId"`
		CWD                string            `json:"cwd"`
		Command            string            `json:"command"`
		AvailableDecisions []json.RawMessage `json:"availableDecisions"`
	} `json:"params"`
	raw json.RawMessage
}

func pendingCommand(state json.RawMessage, threadID, turnID, cwd string) (commandRequest, error) {
	return pendingCommandForDecision(state, threadID, turnID, cwd, "decline")
}

func pendingCommandForDecision(state json.RawMessage, threadID, turnID, cwd, decision string) (commandRequest, error) {
	var p commandRequest
	if decision != "decline" && decision != "cancel" && decision != "accept" {
		return p, errInvalidApproval
	}
	var s struct {
		CWD      string            `json:"cwd"`
		Requests []json.RawMessage `json:"requests"`
	}
	if threadID == "" || turnID == "" || cwd == "" || json.Unmarshal(state, &s) != nil || s.CWD != cwd || s.Requests == nil {
		return p, errInvalidApproval
	}
	if len(s.Requests) == 0 {
		return p, errNoPendingInput
	}
	if len(s.Requests) != 1 || json.Unmarshal(s.Requests[0], &p) != nil || !validRequestID(p.ID) || p.Method != "item/commandExecution/requestApproval" || p.Params.ThreadID != threadID || p.Params.TurnID != turnID || p.Params.ItemID == "" || p.Params.CWD != cwd || p.Params.Command == "" {
		return p, errInvalidApproval
	}
	found := false
	for _, d := range p.Params.AvailableDecisions {
		var value string
		if json.Unmarshal(d, &value) == nil && value == decision {
			found = true
		}
	}
	if !found {
		return p, errInvalidApproval
	}
	p.raw = append(json.RawMessage(nil), s.Requests[0]...)
	return p, nil
}

// Decline-only by construction: no caller-supplied decision or grant is accepted.
func declineCommand(ctx context.Context, c caller, owner, cwd string, state json.RawMessage, p commandRequest) error {
	return respondCommandDecision(ctx, c, owner, cwd, state, p, "decline")
}

// Only a native offer of decline or cancel can be sent. The caller cannot
// pass any approval or persistent grant through this route.
func respondCommandDecision(ctx context.Context, c caller, owner, cwd string, state json.RawMessage, p commandRequest, decision string) error {
	if decision != "decline" && decision != "cancel" {
		return errInvalidApproval
	}
	return submitCommandDecision(ctx, c, owner, cwd, state, p, decision)
}

// acceptCommandOnce is separate from the M0 decline-only probe route. It can
// emit only the literal native one-time "accept", never a structured grant.
func acceptCommandOnce(ctx context.Context, c caller, owner, cwd string, state json.RawMessage, p commandRequest) error {
	return submitCommandDecision(ctx, c, owner, cwd, state, p, "accept")
}

func submitCommandDecision(ctx context.Context, c caller, owner, cwd string, state json.RawMessage, p commandRequest, decision string) error {
	live, err := pendingCommandForDecision(state, p.Params.ThreadID, p.Params.TurnID, cwd, decision)
	if err != nil || owner == "" || !sameJSON(live.raw, p.raw) {
		return errInvalidApproval
	}
	r, err := c.Call(ctx, Request{Method: "thread-follower-command-approval-decision", Version: 1, TargetClientID: owner, Mutating: true, Params: map[string]any{"conversationId": p.Params.ThreadID, "requestId": p.ID, "decision": decision}})
	if err != nil {
		return err
	}
	var receipt struct {
		OK bool `json:"ok"`
	}
	if json.Unmarshal(r.Result, &receipt) != nil || !receipt.OK {
		return errors.New("command rejection receipt unknown")
	}
	return nil
}

type CommandDeclineResult struct {
	Stage              string   `json:"stage"`
	ReceiptOK          bool     `json:"receiptOK"`
	PendingCleared     bool     `json:"pendingCleared"`
	TurnID             string   `json:"-"`
	ItemID             string   `json:"-"`
	PendingMethod      string   `json:"pendingMethod,omitempty"`
	PendingFields      []string `json:"pendingFields,omitempty"`
	RequestIDValid     bool     `json:"requestIdValid"`
	ThreadMatches      bool     `json:"threadMatches"`
	TurnMatches        bool     `json:"turnMatches"`
	CWDMatches         bool     `json:"cwdMatches"`
	StateCWDMatches    bool     `json:"stateCwdMatches"`
	PendingCount       int      `json:"pendingCount"`
	PendingMethods     []string `json:"pendingMethods,omitempty"`
	DecisionKinds      []string `json:"decisionKinds,omitempty"`
	ItemIDPresent      bool     `json:"itemIdPresent"`
	LiveTurnStopped    bool     `json:"liveTurnStopped"`
	CleanupInterrupted bool     `json:"cleanupInterrupted"`
	ObservedTurnStatus string   `json:"observedTurnStatus,omitempty"`
}

type nativeTurn struct {
	TurnID string `json:"turnId"`
	Status string `json:"status"`
}

func nativeTurnStatus(state json.RawMessage, turnID string) (string, bool) {
	var s struct {
		Turns       []nativeTurn `json:"turns"`
		TurnHistory struct {
			Kind    string `json:"kind"`
			History struct {
				Islands []struct {
					Entries []struct {
						Value string `json:"value"`
					} `json:"entries"`
				} `json:"islands"`
				Entities map[string]nativeTurn `json:"entitiesByKey"`
			} `json:"history"`
		} `json:"turnHistory"`
	}
	if turnID == "" || json.Unmarshal(state, &s) != nil {
		return "", false
	}
	if s.TurnHistory.Kind == "canonical" {
		s.Turns = nil
		for _, island := range s.TurnHistory.History.Islands {
			for _, entry := range island.Entries {
				t, ok := s.TurnHistory.History.Entities[entry.Value]
				if !ok {
					return "", false
				}
				s.Turns = append(s.Turns, t)
			}
		}
	}
	for _, t := range s.Turns {
		if t.TurnID == turnID && t.Status != "" {
			return t.Status, true
		}
	}
	return "", false
}

func exerciseCommandDecline(ctx context.Context, h inputExperiment, owner, threadID, cwd string) (out CommandDeclineResult, err error) {
	return exerciseCommandDecision(ctx, h, owner, threadID, cwd, "decline")
}

func exerciseCommandCancel(ctx context.Context, h inputExperiment, owner, threadID, cwd string) (CommandDeclineResult, error) {
	return exerciseCommandDecision(ctx, h, owner, threadID, cwd, "cancel")
}

func exerciseCommandDecision(ctx context.Context, h inputExperiment, owner, threadID, cwd, decision string) (out CommandDeclineResult, err error) {
	if decision != "decline" && decision != "cancel" {
		return out, errInvalidApproval
	}
	out.Stage = "initial-snapshot"
	state, err := h.Snapshot(ctx)
	if err != nil {
		return out, err
	}
	out.Stage = "start-turn"
	prompt := "New independent authorized isolated command approval test. Previous turns are not this test; do not report their outcome. Use exec_command once now to request a one-time approval for exactly /usr/bin/true, using sandbox_permissions require_escalated and a justification explaining this approval will be rejected. Do not add a prefix rule. Do not use any other tools, read or write files, access the network, or create tasks. If this request is rejected, do not retry or seek alternative permissions; stop. If unavailable, state that and stop."
	out.TurnID, err = startFixtureRequest(ctx, h, owner, threadID, cwd, state, prompt, map[string]any{"approvalPolicy": "on-request", "sandboxPolicy": map[string]string{"type": "readOnly"}})
	if err != nil {
		return out, err
	}
	defer func() {
		if err != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if interruptFixtureTurn(cleanup, h, owner, threadID, out.TurnID) == nil {
				out.CleanupInterrupted = true
			}
		}
	}()
	out.Stage = "await-pending"
	var pending commandRequest
	for {
		state, err = h.Snapshot(ctx)
		if err != nil {
			return out, err
		}
		pending, err = pendingCommandForDecision(state, threadID, out.TurnID, cwd, decision)
		if !errors.Is(err, errNoPendingInput) {
			out.PendingMethod = pending.Method
			out.RequestIDValid = validRequestID(pending.ID)
			out.ThreadMatches = pending.Params.ThreadID == threadID
			out.TurnMatches = pending.Params.TurnID == out.TurnID
			out.CWDMatches = pending.Params.CWD == cwd
			out.ItemIDPresent = pending.Params.ItemID != ""
			for _, d := range pending.Params.AvailableDecisions {
				var label string
				if json.Unmarshal(d, &label) == nil {
					out.DecisionKinds = append(out.DecisionKinds, label)
				} else {
					out.DecisionKinds = append(out.DecisionKinds, "structured")
				}
			}
			var diag struct {
				CWD      string `json:"cwd"`
				Requests []struct {
					Method string                     `json:"method"`
					Params map[string]json.RawMessage `json:"params"`
				} `json:"requests"`
			}
			if json.Unmarshal(state, &diag) == nil {
				out.StateCWDMatches = diag.CWD == cwd
				out.PendingCount = len(diag.Requests)
				for _, r := range diag.Requests {
					out.PendingMethods = append(out.PendingMethods, r.Method)
				}
			}
			if len(diag.Requests) == 1 {
				for field := range diag.Requests[0].Params {
					out.PendingFields = append(out.PendingFields, field)
				}
				sort.Strings(out.PendingFields)
			}
		}
		if err == nil {
			break
		}
		if !errors.Is(err, errNoPendingInput) {
			return out, err
		}
		if idleFixture(state, cwd) {
			return out, errors.New("fixture ended without native command approval")
		}
		select {
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
			return out, ctx.Err()
		}
	}
	out.ItemID = pending.Params.ItemID
	out.Stage = "submit-rejection"
	state, err = h.Snapshot(ctx)
	if err != nil {
		return out, err
	}
	if err = respondCommandDecision(ctx, h, owner, cwd, state, pending, decision); err != nil {
		return out, err
	}
	out.ReceiptOK = true
	out.Stage = "await-idle"
	waitCtx, cancelWait := context.WithTimeout(ctx, 20*time.Second)
	defer cancelWait()
	for {
		state, err = h.Snapshot(waitCtx)
		if err != nil {
			return out, err
		}
		if idleFixture(state, cwd) {
			out.PendingCleared = true
			if decision == "decline" {
				break
			}
			if status, ok := nativeTurnStatus(state, out.TurnID); ok {
				out.ObservedTurnStatus = status
				if status == "interrupted" {
					out.LiveTurnStopped = true
					break
				}
				if status == "completed" || status == "failed" {
					return out, errors.New("cancel did not stop target turn")
				}
			}
		}
		select {
		case <-time.After(200 * time.Millisecond):
		case <-waitCtx.Done():
			return out, waitCtx.Err()
		}
	}
	out.Stage = "await-persisted-rejection"
	return out, nil
}

func (c *Client) ProbeCommandDecline(ctx context.Context, threadID, cwd string) (CommandDeclineResult, error) {
	return c.probeCommandDecision(ctx, threadID, cwd, "decline")
}
func (c *Client) ProbeCommandCancel(ctx context.Context, threadID, cwd string) (CommandDeclineResult, error) {
	return c.probeCommandDecision(ctx, threadID, cwd, "cancel")
}
func (c *Client) probeCommandDecision(ctx context.Context, threadID, cwd, decision string) (CommandDeclineResult, error) {
	r, err := c.Call(ctx, Request{Method: "thread-owner-discovery", Version: 1, Params: map[string]string{"hostId": "local", "conversationId": threadID}})
	if err != nil {
		return CommandDeclineResult{Stage: "discover-owner"}, err
	}
	owner := r.HandledByClientID
	if owner == "" {
		return CommandDeclineResult{}, ErrProtocol
	}
	if err := c.Broadcast(ctx, "thread-stream-following-changed", 1, map[string]any{"hostId": "local", "conversationId": threadID, "following": true}, []string{owner}); err != nil {
		return CommandDeclineResult{}, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		c.Broadcast(cleanup, "thread-stream-following-changed", 1, map[string]any{"hostId": "local", "conversationId": threadID, "following": false}, []string{owner})
	}()
	return exerciseCommandDecision(ctx, &fixtureFollower{c: c, o: newObservationState(threadID, owner)}, owner, threadID, cwd, decision)
}
