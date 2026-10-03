package desktopipc

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

func sendMissingUserInput(ctx context.Context, c caller, owner, threadID, cwd string, state json.RawMessage) (bool, error) {
	if owner == "" || threadID == "" || !idleFixture(state, cwd) {
		return false, errors.New("stale interaction test requires an idle fixture with no pending requests")
	}
	r, err := c.Call(ctx, Request{Method: "thread-follower-submit-user-input", Version: 1, TargetClientID: owner, Mutating: true, Params: map[string]any{"conversationId": threadID, "requestId": "ariel-nonexistent-" + requestID(), "response": map[string]any{"answers": map[string]any{}}}})
	if err != nil {
		return false, err
	}
	var receipt struct {
		OK bool `json:"ok"`
	}
	if json.Unmarshal(r.Result, &receipt) != nil {
		return false, ErrProtocol
	}
	return receipt.OK, nil
}

type StaleObservation struct {
	MissingRequestReturnedOK      bool `json:"missingRequestReturnedOK"`
	StillIdle                     bool `json:"stillIdle"`
	NativeReceiptProvesAcceptance bool `json:"nativeReceiptProvesAcceptance"`
}

func (c *Client) ProbeStaleInteraction(ctx context.Context, threadID, cwd string) (StaleObservation, error) {
	var out StaleObservation
	r, err := c.Call(ctx, Request{Method: "thread-owner-discovery", Version: 1, Params: map[string]string{"hostId": "local", "conversationId": threadID}})
	if err != nil {
		return out, err
	}
	owner := r.HandledByClientID
	if owner == "" {
		return out, ErrProtocol
	}
	f := &fixtureFollower{c: c, o: newObservationState(threadID, owner)}
	if err := c.Broadcast(ctx, "thread-stream-following-changed", 1, map[string]any{"hostId": "local", "conversationId": threadID, "following": true}, []string{owner}); err != nil {
		return out, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		c.Broadcast(cleanup, "thread-stream-following-changed", 1, map[string]any{"hostId": "local", "conversationId": threadID, "following": false}, []string{owner})
	}()
	if err := f.refresh(ctx); err != nil {
		return out, err
	}
	state, _, _ := f.o.reducer.Snapshot()
	out.MissingRequestReturnedOK, err = sendMissingUserInput(ctx, f, owner, threadID, cwd, state)
	if err != nil {
		return out, err
	}
	if err := f.refresh(ctx); err != nil {
		return out, err
	}
	state, _, _ = f.o.reducer.Snapshot()
	out.StillIdle = idleFixture(state, cwd)
	return out, nil
}
