package desktopipc

import (
	"context"
	"errors"
	"time"
)

type UserInputInterruptResult struct {
	Stage                  string `json:"stage"`
	PendingObserved        bool   `json:"pendingObserved"`
	Interrupted            bool   `json:"interrupted"`
	ExpiredLocallyRejected bool   `json:"expiredLocallyRejected"`
	CleanupStopped         bool   `json:"cleanupStopped"`
	TurnID                 string `json:"-"`
}

func exerciseUserInputInterrupted(ctx context.Context, h inputExperiment, owner, threadID, cwd string) (out UserInputInterruptResult, err error) {
	out.Stage = "initial-snapshot"
	state, err := h.Snapshot(ctx)
	if err != nil {
		return out, err
	}
	out.Stage = "start-turn"
	out.TurnID, err = startInputFixtureTurn(ctx, h, owner, threadID, cwd, state, userInputFixturePrompt("ARIEL_INTERRUPTED_"+requestID()))
	if err != nil {
		return out, err
	}
	defer func() {
		if err != nil && !out.Interrupted {
			cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			out.CleanupStopped = interruptFixtureTurn(cleanup, h, owner, threadID, out.TurnID) == nil
		}
	}()
	out.Stage = "await-pending"
	var pending inputRequest
	for {
		state, err = h.Snapshot(ctx)
		if err != nil {
			return out, err
		}
		pending, err = pendingUserInput(state, threadID, out.TurnID, cwd)
		if err == nil {
			out.PendingObserved = true
			break
		}
		if !errors.Is(err, errNoPendingInput) {
			return out, err
		}
		if idleFixture(state, cwd) {
			return out, errors.New("fixture ended without a native user-input request")
		}
		select {
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
			return out, ctx.Err()
		}
	}
	out.Stage = "interrupt-turn"
	if err = interruptFixtureTurn(ctx, h, owner, threadID, out.TurnID); err != nil {
		return out, err
	}
	out.Stage = "await-interrupted"
	for {
		state, err = h.Snapshot(ctx)
		if err != nil {
			return out, err
		}
		status, known := nativeTurnStatus(state, out.TurnID)
		if known && status == "interrupted" && idleFixture(state, cwd) {
			out.Interrupted = true
			break
		}
		if known && status == "completed" {
			return out, errors.New("fixture completed despite exact interrupt receipt")
		}
		select {
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
			return out, ctx.Err()
		}
	}
	out.Stage = "reject-expired"
	answers := map[string][]string{"choice": {"Blue"}, "note": {"must not be sent"}}
	out.ExpiredLocallyRejected = submitUserInput(ctx, h, owner, cwd, state, pending, answers) != nil
	if !out.ExpiredLocallyRejected {
		return out, errors.New("expired user-input answer was submitted")
	}
	out.Stage = "completed"
	return out, nil
}

// ProbeUserInputInterrupted is restricted by the CLI to a dedicated fixture.
func (c *Client) ProbeUserInputInterrupted(ctx context.Context, threadID, cwd string) (UserInputInterruptResult, error) {
	r, err := c.Call(ctx, Request{Method: "thread-owner-discovery", Version: 1, Params: map[string]string{"hostId": "local", "conversationId": threadID}})
	if err != nil {
		return UserInputInterruptResult{Stage: "discover-owner"}, err
	}
	owner := r.HandledByClientID
	if owner == "" {
		return UserInputInterruptResult{}, ErrProtocol
	}
	if err := c.Broadcast(ctx, "thread-stream-following-changed", 1, map[string]any{"hostId": "local", "conversationId": threadID, "following": true}, []string{owner}); err != nil {
		return UserInputInterruptResult{}, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		c.Broadcast(cleanup, "thread-stream-following-changed", 1, map[string]any{"hostId": "local", "conversationId": threadID, "following": false}, []string{owner})
	}()
	return exerciseUserInputInterrupted(ctx, &fixtureFollower{c: c, o: newObservationState(threadID, owner)}, owner, threadID, cwd)
}
