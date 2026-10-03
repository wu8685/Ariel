package desktopipc

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type inputExperiment interface {
	caller
	Snapshot(context.Context) (json.RawMessage, error)
}

func (f *fixtureFollower) Snapshot(ctx context.Context) (json.RawMessage, error) {
	if err := f.refresh(ctx); err != nil {
		return nil, err
	}
	state, _, valid := f.o.reducer.Snapshot()
	if !valid {
		return nil, ErrProtocol
	}
	return state, nil
}

func startInputFixtureTurn(ctx context.Context, c caller, owner, threadID, cwd string, state json.RawMessage, text string) (string, error) {
	var s struct {
		Model string `json:"latestModel"`
	}
	if json.Unmarshal(state, &s) != nil || s.Model == "" {
		return "", errors.New("fixture current model unavailable; cannot select Plan mode safely")
	}
	return startFixtureRequest(ctx, c, owner, threadID, cwd, state, text, map[string]any{
		"collaborationMode": map[string]any{"mode": "plan", "settings": map[string]any{"model": s.Model}},
		"approvalPolicy":    "never", "sandboxPolicy": map[string]string{"type": "readOnly"},
	})
}

type UserInputResult struct {
	Stage                  string `json:"stage"`
	QuestionCount          int    `json:"questionCount"`
	ReceiptOK              bool   `json:"receiptOK"`
	EchoVerified           bool   `json:"exactAnswerEchoVerified"`
	Completed              bool   `json:"completed"`
	ExpiredLocallyRejected bool   `json:"expiredLocallyRejected"`
	TurnID                 string `json:"-"`
	Marker                 string `json:"-"`
}

func exerciseUserInput(ctx context.Context, h inputExperiment, owner, threadID, cwd string) (out UserInputResult, err error) {
	out.Stage = "initial-snapshot"
	state, err := h.Snapshot(ctx)
	if err != nil {
		return out, err
	}
	out.Marker = "ARIEL_INPUT_" + requestID()
	prompt := userInputFixturePrompt(out.Marker)
	out.Stage = "start-turn"
	out.TurnID, err = startInputFixtureTurn(ctx, h, owner, threadID, cwd, state, prompt)
	if err != nil {
		return out, err
	}
	defer func() {
		if err != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			interruptFixtureTurn(cleanup, h, owner, threadID, out.TurnID)
		}
	}()
	var pending inputRequest
	out.Stage = "await-pending"
	for {
		state, err = h.Snapshot(ctx)
		if err != nil {
			return out, err
		}
		pending, err = pendingUserInput(state, threadID, out.TurnID, cwd)
		if err == nil {
			break
		}
		if !errors.Is(err, errNoPendingInput) {
			return out, err
		}
		if idleFixture(state, cwd) {
			return out, errors.New("fixture turn ended without a native user-input request")
		}
		select {
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
			return out, ctx.Err()
		}
	}
	out.QuestionCount = len(pending.Params.Questions)
	if out.QuestionCount != 2 || pending.Params.Questions[0].ID != "choice" || pending.Params.Questions[1].ID != "note" || len(pending.Params.Questions[0].Options) != 2 || pending.Params.Questions[0].Options[0].Label != "Blue" || len(pending.Params.Questions[1].Options) != 2 {
		return out, errors.New("fixture did not emit the specified option and free-text questions")
	}
	answers := map[string][]string{"choice": {"Blue"}, "note": {"ariel free text"}}
	out.Stage = "submit-answer"
	state, err = h.Snapshot(ctx)
	if err != nil {
		return out, err
	}
	if err = submitUserInput(ctx, h, owner, cwd, state, pending, answers); err != nil {
		return out, err
	}
	out.ReceiptOK = true
	out.Stage = "await-echo"
	for {
		state, err = h.Snapshot(ctx)
		if err != nil {
			return out, err
		}
		out.EchoVerified = userInputEchoMatches(state, pending, answers)
		if out.EchoVerified && idleFixture(state, cwd) && hasLiteral(state, out.Marker) {
			out.Completed = true
			break
		}
		select {
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
			return out, ctx.Err()
		}
	}
	// This is a local guard test: no second IPC response is sent.
	out.ExpiredLocallyRejected = submitUserInput(ctx, h, owner, cwd, state, pending, answers) != nil
	out.Stage = "completed"
	return out, nil
}

func userInputFixturePrompt(marker string) string {
	return "Authorized isolated user-input compatibility test. Use request_user_input exactly once with two questions. First question: id choice, header Choice, question Choose a fixture color, options Blue (description blue) and Green (description green). Second question: id note, header Note, question Enter a fixture note or pick a preset, options Preset A (description a) and Preset B (description b). Keep option labels exact; neither option is recommended. The client may submit a free-text answer instead of a preset. Do not use any other tools or inspect anything. After receiving the two answers, reply with exactly " + marker + " and nothing else. If the tool is unavailable, report that and stop."
}

// ProbeUserInput requires a manifest authorized specifically for user-input.
func (c *Client) ProbeUserInput(ctx context.Context, threadID, cwd string) (UserInputResult, error) {
	r, err := c.Call(ctx, Request{Method: "thread-owner-discovery", Version: 1, Params: map[string]string{"hostId": "local", "conversationId": threadID}})
	if err != nil {
		return UserInputResult{Stage: "discover-owner"}, err
	}
	owner := r.HandledByClientID
	if owner == "" {
		return UserInputResult{}, ErrProtocol
	}
	if err := c.Broadcast(ctx, "thread-stream-following-changed", 1, map[string]any{"hostId": "local", "conversationId": threadID, "following": true}, []string{owner}); err != nil {
		return UserInputResult{}, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		c.Broadcast(cleanup, "thread-stream-following-changed", 1, map[string]any{"hostId": "local", "conversationId": threadID, "following": false}, []string{owner})
	}()
	return exerciseUserInput(ctx, &fixtureFollower{c: c, o: newObservationState(threadID, owner)}, owner, threadID, cwd)
}
