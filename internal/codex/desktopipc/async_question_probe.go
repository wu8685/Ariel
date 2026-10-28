package desktopipc

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ProbeAsyncQuestionAvailability is fixture-only. The caller must verify a
// dedicated async-question manifest, workspace marker and native thread cwd.
// A plain-text request or synchronous user-input tool is not a passing result.
func (c *Client) ProbeAsyncQuestionAvailability(ctx context.Context, threadID, cwd string) (map[string]any, error) {
	out := map[string]any{"asyncQuestionObserved": false, "answerWriteVerified": false, "stage": "discover-owner"}
	r, err := c.Call(ctx, Request{Method: "thread-owner-discovery", Version: 1, Params: map[string]string{"hostId": "local", "conversationId": threadID}})
	if err != nil {
		return out, err
	}
	out["stage"] = "follow"
	f, err := Follow(ctx, c, threadID, r.HandledByClientID, cwd)
	if err != nil {
		return out, err
	}
	defer f.Close()
	state, err := f.Current()
	if err != nil || !idleFixture(state, cwd) {
		return out, errors.New("fixture not confirmed idle")
	}
	out["stage"] = "start-turn"
	turnID, err := f.Start(ctx, requestID(), "Authorized isolated async-question compatibility test. Use request_user_input_async exactly once to ask two questions: Choose a fixture color (options Blue and Green); Enter a fixture note (free text). Do not substitute request_user_input or prose. Continue independent private reasoning after asking. Do not inspect files, use other tools, access network, change settings or create tasks. If request_user_input_async is unavailable, reply ASYNC_TOOL_UNAVAILABLE and stop. Never fabricate a tool call.")
	if err != nil {
		return out, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		state, e := f.Current()
		if e == nil {
			if status, ok := nativeTurnStatus(state, turnID); ok && status == "inProgress" {
				_ = f.Interrupt(cleanup, turnID)
			}
		}
	}()
	out["stage"] = "await-question"
	for {
		state, err = f.Refresh(ctx)
		if err != nil {
			return out, err
		}
		items, ok := nativeTurnItems(state, turnID)
		if !ok {
			return out, ErrProtocol
		}
		for _, raw := range items {
			var item struct {
				Type      string
				Questions []json.RawMessage
			}
			if json.Unmarshal(raw, &item) == nil && item.Type == "agentMessage" && len(item.Questions) > 0 {
				out["asyncQuestionObserved"] = true
				return out, nil
			}
		}
		if status, ok := nativeTurnStatus(state, turnID); ok && status != "inProgress" {
			out["fixtureEndedWithoutAsyncQuestion"] = true
			return out, nil
		}
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
