package desktopipc

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// submitCompetingInputs is fixture-only. Both calls use the same observed
// pending request, so the later native state decides the result of the race.
func submitCompetingInputs(ctx context.Context, first, second caller, owner, cwd string, state json.RawMessage, request inputRequest, firstAnswers, secondAnswers map[string][]string) (error, error) {
	firstResult := make(chan error, 1)
	secondResult := make(chan error, 1)
	start := make(chan struct{})
	go func() {
		<-start
		firstResult <- submitUserInput(ctx, first, owner, cwd, state, request, firstAnswers)
	}()
	go func() {
		<-start
		secondResult <- submitUserInput(ctx, second, owner, cwd, state, request, secondAnswers)
	}()
	close(start)
	return <-firstResult, <-secondResult
}

// classifyInputRace attributes a response only when exactly one candidate has
// an exact native history echo. IPC receipts are deliberately not evidence.
func classifyInputRace(state json.RawMessage, request inputRequest, first, second map[string][]string) string {
	firstEcho := userInputEchoMatches(state, request, first)
	secondEcho := userInputEchoMatches(state, request, second)
	switch {
	case firstEcho && !secondEcho:
		return "first"
	case secondEcho && !firstEcho:
		return "second"
	default:
		return "unknown"
	}
}

type UserInputRaceResult struct {
	Stage           string `json:"stage"`
	QuestionCount   int    `json:"questionCount"`
	FirstReceiptOK  bool   `json:"firstReceiptOK"`
	SecondReceiptOK bool   `json:"secondReceiptOK"`
	Winner          string `json:"exactEchoWinner"`
	Completed       bool   `json:"completed"`
	CleanupStopped  bool   `json:"cleanupStopped"`
	TurnID          string `json:"-"`
	Marker          string `json:"-"`
}

func exerciseUserInputRace(ctx context.Context, h inputExperiment, second caller, owner, threadID, cwd string) (out UserInputRaceResult, err error) {
	out.Stage = "initial-snapshot"
	state, err := h.Snapshot(ctx)
	if err != nil {
		return out, err
	}
	out.Marker = "ARIEL_INPUT_RACE_" + requestID()
	out.Stage = "start-turn"
	out.TurnID, err = startInputFixtureTurn(ctx, h, owner, threadID, cwd, state, userInputFixturePrompt(out.Marker))
	if err != nil {
		return out, err
	}
	defer func() {
		if err != nil {
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
	out.QuestionCount = len(pending.Params.Questions)
	if out.QuestionCount != 2 || pending.Params.Questions[0].ID != "choice" || pending.Params.Questions[1].ID != "note" || len(pending.Params.Questions[0].Options) != 2 || pending.Params.Questions[0].Options[0].Label != "Blue" || pending.Params.Questions[0].Options[1].Label != "Green" || len(pending.Params.Questions[1].Options) != 2 {
		return out, errors.New("fixture questions differ from isolated test contract")
	}
	firstAnswers := map[string][]string{"choice": {"Blue"}, "note": {"ariel first free text"}}
	secondAnswers := map[string][]string{"choice": {"Green"}, "note": {"ariel second free text"}}
	out.Stage = "submit-competing-answers"
	state, err = h.Snapshot(ctx)
	if err != nil {
		return out, err
	}
	if _, err = pendingUserInput(state, threadID, out.TurnID, cwd); err != nil {
		return out, err
	}
	firstErr, secondErr := submitCompetingInputs(ctx, h, second, owner, cwd, state, pending, firstAnswers, secondAnswers)
	out.FirstReceiptOK = firstErr == nil
	out.SecondReceiptOK = secondErr == nil
	out.Stage = "await-exact-echo"
	for {
		state, err = h.Snapshot(ctx)
		if err != nil {
			return out, err
		}
		out.Winner = classifyInputRace(state, pending, firstAnswers, secondAnswers)
		status, known := nativeTurnStatus(state, out.TurnID)
		if known && status != "inProgress" {
			out.Completed = status == "completed" && idleFixture(state, cwd) && hasLiteral(state, out.Marker)
			out.Stage = "completed"
			return out, nil
		}
		select {
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
			return out, ctx.Err()
		}
	}
}

// ProbeUserInputRace is restricted by the CLI to a dedicated user-input fixture.
func (c *Client) ProbeUserInputRace(ctx context.Context, threadID, cwd string, second *Client) (UserInputRaceResult, error) {
	if second == nil {
		return UserInputRaceResult{}, ErrProtocol
	}
	r, err := c.Call(ctx, Request{Method: "thread-owner-discovery", Version: 1, Params: map[string]string{"hostId": "local", "conversationId": threadID}})
	if err != nil {
		return UserInputRaceResult{Stage: "discover-owner"}, err
	}
	owner := r.HandledByClientID
	if owner == "" {
		return UserInputRaceResult{}, ErrProtocol
	}
	if err := c.Broadcast(ctx, "thread-stream-following-changed", 1, map[string]any{"hostId": "local", "conversationId": threadID, "following": true}, []string{owner}); err != nil {
		return UserInputRaceResult{}, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		c.Broadcast(cleanup, "thread-stream-following-changed", 1, map[string]any{"hostId": "local", "conversationId": threadID, "following": false}, []string{owner})
	}()
	return exerciseUserInputRace(ctx, &fixtureFollower{c: c, o: newObservationState(threadID, owner)}, second, owner, threadID, cwd)
}
