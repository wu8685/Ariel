package desktopipc

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type caller interface {
	Call(context.Context, Request) (Reply, error)
}

func idleFixture(state json.RawMessage, cwd string) bool {
	var s struct {
		CWD     string `json:"cwd"`
		Runtime struct {
			Type string `json:"type"`
		} `json:"threadRuntimeStatus"`
		Requests []json.RawMessage `json:"requests"`
	}
	return json.Unmarshal(state, &s) == nil && s.CWD == cwd && s.Runtime.Type == "idle" && s.Requests != nil && len(s.Requests) == 0
}
func startFixtureTurn(ctx context.Context, c caller, owner, threadID, cwd string, state json.RawMessage, text string) (string, error) {
	return startFixtureRequest(ctx, c, owner, threadID, cwd, state, text, map[string]any{})
}
func startFixtureRequest(ctx context.Context, c caller, owner, threadID, cwd string, state json.RawMessage, text string, request map[string]any) (string, error) {
	if owner == "" || threadID == "" || !idleFixture(state, cwd) {
		return "", errors.New("fixture state is busy, unknown, or has a different workspace")
	}
	request["threadId"] = threadID
	request["clientUserMessageId"] = requestID()
	request["input"] = []any{map[string]any{"type": "text", "text": text, "text_elements": []any{}}}
	r, err := c.Call(ctx, Request{Method: "thread-follower-start-turn", Version: 2, TargetClientID: owner, Mutating: true, Params: map[string]any{
		"conversationId": threadID, "turnStart": map[string]any{"request": request, "context": map[string]bool{"inheritThreadSettings": true}},
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
		return "", errors.New("owner acceptance lacks verified result.result.turn.id; outcome unknown")
	}
	return accepted.Result.Turn.ID, nil
}
func interruptFixtureTurn(ctx context.Context, c caller, owner, threadID, expectedTurnID string) error {
	if owner == "" || threadID == "" || expectedTurnID == "" {
		return ErrProtocol
	}
	r, err := c.Call(ctx, Request{Method: "thread-follower-interrupt-turn", Version: 4, TargetClientID: owner, Mutating: true, Params: map[string]any{"conversationId": threadID, "mode": "user-stop", "expectedTurnId": expectedTurnID}})
	if err != nil {
		return err
	}
	var result struct {
		OK     bool   `json:"ok"`
		TurnID string `json:"interruptedTurnId"`
	}
	if json.Unmarshal(r.Result, &result) != nil || !result.OK || result.TurnID != expectedTurnID {
		return errors.New("interrupt receipt did not confirm the exact target")
	}
	return nil
}

type fixtureFollower struct {
	c *Client
	o *observationState
}

func (f *fixtureFollower) Call(ctx context.Context, req Request) (Reply, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		r   Reply
		err error
	}
	done := make(chan result, 1)
	go func() { r, err := f.c.Call(ctx, req); done <- result{r, err} }()
	for {
		select {
		case r := <-done:
			return r.r, r.err
		case <-f.c.EventsReady():
			event, ok := f.c.NextEvent()
			if !ok {
				if f.c.Err() != nil {
					return Reply{}, f.c.Err()
				}
				continue
			}
			if err := f.o.apply(event); err != nil {
				return Reply{}, err
			}
		case <-ctx.Done():
			return Reply{}, ctx.Err()
		case <-f.c.Done():
			return Reply{}, f.c.Err()
		}
	}
}
func (f *fixtureFollower) refresh(ctx context.Context) error {
	before := f.o.summary.Snapshots
	_, err := f.Call(ctx, Request{Method: "thread-follower-load-complete-history", Version: 1, TargetClientID: f.o.owner, Params: map[string]string{"conversationId": f.o.threadID}})
	if err != nil {
		return err
	}
	for f.o.summary.Snapshots <= before {
		select {
		case <-f.c.EventsReady():
			event, ok := f.c.NextEvent()
			if !ok {
				if f.c.Err() != nil {
					return f.c.Err()
				}
				continue
			}
			if err := f.o.apply(event); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		case <-f.c.Done():
			return f.c.Err()
		}
	}
	return nil
}

// ExerciseResult keeps real identifiers in memory for independent history checks.
// Callers must not serialize this object into a committed report.
type ExerciseResult struct {
	ReplyTurnID, InterruptedTurnID, ReplyMarker string
	LiveReplyObserved, InterruptAccepted        bool
	SnapshotCount, PatchBatches                 int
}

// ExerciseFixture is a bounded compatibility probe, not the production controller.
// The caller must first validate a manifest and its marker using probe.Authorize.
func (c *Client) ExerciseFixture(ctx context.Context, threadID, cwd string) (out ExerciseResult, err error) {
	r, err := c.Call(ctx, Request{Method: "thread-owner-discovery", Version: 1, Params: map[string]string{"hostId": "local", "conversationId": threadID}})
	if err != nil {
		return out, err
	}
	owner := r.HandledByClientID
	if owner == "" {
		return out, ErrProtocol
	}
	f := &fixtureFollower{c: c, o: newObservationState(threadID, owner)}
	params := map[string]any{"hostId": "local", "conversationId": threadID, "following": true}
	if err := c.Broadcast(ctx, "thread-stream-following-changed", 1, params, []string{owner}); err != nil {
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
	out.ReplyMarker = "ARIEL_IPC_" + requestID()
	out.ReplyTurnID, err = startFixtureTurn(ctx, f, owner, threadID, cwd, state, "Authorized isolated connectivity fixture. Do not use tools, read or write files, or create tasks. Reply with exactly "+out.ReplyMarker+".")
	if err != nil {
		return out, err
	}
	// A failure after acceptance must not abandon this known test turn running.
	defer func() {
		if err != nil {
			target := out.InterruptedTurnID
			if target == "" {
				target = out.ReplyTurnID
			}
			cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			interruptFixtureTurn(cleanup, c, owner, threadID, target)
		}
	}()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		if err = f.refresh(ctx); err != nil {
			return out, err
		}
		state, _, _ = f.o.reducer.Snapshot()
		if hasLiteral(state, out.ReplyMarker) && idleFixture(state, cwd) {
			out.LiveReplyObserved = true
			break
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			return out, ctx.Err()
		}
	}
	out.InterruptedTurnID, err = startFixtureTurn(ctx, f, owner, threadID, cwd, state, "Authorized isolated stop test. Do not use tools or access files. Output the integers 1 through 5000, one per line, without commentary.")
	if err != nil {
		return out, err
	}
	err = interruptFixtureTurn(ctx, f, owner, threadID, out.InterruptedTurnID)
	if err != nil {
		return out, err
	}
	out.InterruptAccepted = true
	if err = f.refresh(ctx); err != nil {
		return out, err
	}
	out.SnapshotCount = f.o.summary.Snapshots
	out.PatchBatches = f.o.summary.PatchBatches
	return out, nil
}

func hasLiteral(state json.RawMessage, want string) bool {
	var root any
	if json.Unmarshal(state, &root) != nil {
		return false
	}
	var find func(any) bool
	find = func(v any) bool {
		switch value := v.(type) {
		case string:
			return value == want
		case []any:
			for _, e := range value {
				if find(e) {
					return true
				}
			}
		case map[string]any:
			for _, e := range value {
				if find(e) {
					return true
				}
			}
		}
		return false
	}
	return find(root)
}
