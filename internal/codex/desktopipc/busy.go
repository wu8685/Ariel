package desktopipc

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type BusyAttempt struct {
	Outcome    string
	TurnID     string
	SameTurnID bool
}

// This intentionally reuses the pre-busy snapshot, solely inside an isolated
// fixture, to measure whether the native owner admits a competing start.
func attemptBusySecond(ctx context.Context, c caller, owner, threadID, cwd, firstTurnID string, idle, active json.RawMessage) (BusyAttempt, error) {
	var out BusyAttempt
	if !idleFixture(idle, cwd) {
		return out, errors.New("first snapshot is not idle fixture")
	}
	var s struct {
		CWD     string `json:"cwd"`
		Runtime struct {
			Type string `json:"type"`
		} `json:"threadRuntimeStatus"`
	}
	status, ok := nativeTurnStatus(active, firstTurnID)
	if json.Unmarshal(active, &s) != nil || s.CWD != cwd || s.Runtime.Type == "" || s.Runtime.Type == "idle" || !ok || status != "inProgress" {
		return out, errors.New("first fixture turn is not verifiably active")
	}
	id, err := startFixtureTurn(ctx, c, owner, threadID, cwd, idle, "Authorized isolated busy-submit test. Do not use tools or access files. Reply with exactly ARIEL_BUSY_"+requestID()+".")
	if err == nil {
		return BusyAttempt{Outcome: "accepted", TurnID: id, SameTurnID: id == firstTurnID}, nil
	}
	var ce *CallError
	if errors.As(err, &ce) {
		switch ce.Outcome {
		case "rejected":
			return BusyAttempt{Outcome: "rejected"}, nil
		case "not_submitted":
			return BusyAttempt{Outcome: "not_submitted"}, err
		}
	}
	return BusyAttempt{Outcome: "unknown"}, err
}

type BusySubmitResult struct {
	FirstAccepted       bool   `json:"firstAccepted"`
	FirstActiveObserved bool   `json:"firstActiveObserved"`
	SecondOutcome       string `json:"secondOutcome"`
	SecondSameTurnID    bool   `json:"secondSameTurnId"`
	FirstStopped        bool   `json:"firstStopped"`
	SecondStopped       bool   `json:"secondStopped"`
	SecondCompleted     bool   `json:"secondCompleted"`
	CleanupAttempted    bool   `json:"cleanupAttempted"`
	FirstTurnID         string `json:"-"`
	SecondTurnID        string `json:"-"`
}

func (c *Client) ProbeBusySubmit(ctx context.Context, threadID, cwd string, second *Client) (out BusySubmitResult, err error) {
	if second == nil {
		return out, ErrProtocol
	}
	r, err := c.Call(ctx, Request{Method: "thread-owner-discovery", Version: 1, Params: map[string]string{"hostId": "local", "conversationId": threadID}})
	if err != nil {
		return out, err
	}
	owner := r.HandledByClientID
	if owner == "" {
		return out, ErrProtocol
	}
	if err := c.Broadcast(ctx, "thread-stream-following-changed", 1, map[string]any{"hostId": "local", "conversationId": threadID, "following": true}, []string{owner}); err != nil {
		return out, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		c.Broadcast(cleanup, "thread-stream-following-changed", 1, map[string]any{"hostId": "local", "conversationId": threadID, "following": false}, []string{owner})
	}()
	f := &fixtureFollower{c: c, o: newObservationState(threadID, owner)}
	idle, err := f.Snapshot(ctx)
	if err != nil {
		return out, err
	}
	out.FirstTurnID, err = startFixtureTurn(ctx, f, owner, threadID, cwd, idle, "Authorized isolated busy-submit fixture. Do not use tools or access files. Output the integers 1 through 5000, one per line, without commentary.")
	if err != nil {
		return out, err
	}
	out.FirstAccepted = true
	defer func() {
		if !out.FirstStopped {
			out.CleanupAttempted = true
			cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if interruptFixtureTurn(cleanup, c, owner, threadID, out.FirstTurnID) == nil {
				out.FirstStopped = true
			}
		}
	}()
	var active json.RawMessage
	for {
		active, err = f.Snapshot(ctx)
		if err != nil {
			return out, err
		}
		status, ok := nativeTurnStatus(active, out.FirstTurnID)
		if ok && status == "inProgress" {
			out.FirstActiveObserved = true
			break
		}
		if ok && status != "inProgress" {
			return out, errors.New("first fixture turn ended before busy probe")
		}
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			return out, ctx.Err()
		}
	}
	secondResult, secondErr := attemptBusySecond(ctx, second, owner, threadID, cwd, out.FirstTurnID, idle, active)
	out.SecondOutcome = secondResult.Outcome
	out.SecondTurnID = secondResult.TurnID
	out.SecondSameTurnID = secondResult.SameTurnID
	// Always target the exact known first turn; never issue an unscoped stop.
	out.CleanupAttempted = true
	if stopErr := interruptFixtureTurn(ctx, f, owner, threadID, out.FirstTurnID); stopErr == nil {
		out.FirstStopped = true
	}
	if secondResult.Outcome == "accepted" && !out.SecondSameTurnID {
		stopCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		defer cancel()
		for {
			state, e := f.Snapshot(stopCtx)
			if e != nil {
				break
			}
			status, ok := nativeTurnStatus(state, out.SecondTurnID)
			if ok {
				if status == "inProgress" {
					if interruptFixtureTurn(stopCtx, second, owner, threadID, out.SecondTurnID) == nil {
						out.SecondStopped = true
					}
					break
				}
				if status == "completed" {
					out.SecondCompleted = true
					break
				}
				if status == "interrupted" {
					out.SecondStopped = true
					break
				}
			}
			select {
			case <-time.After(100 * time.Millisecond):
			case <-stopCtx.Done():
				break
			}
		}
		if !out.SecondStopped && !out.SecondCompleted {
			cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			if interruptFixtureTurn(cleanup, second, owner, threadID, out.SecondTurnID) == nil {
				out.SecondStopped = true
			}
			cancel()
		}
	}
	if secondErr != nil {
		return out, secondErr
	}
	if out.SecondSameTurnID {
		out.SecondStopped = out.FirstStopped
	}
	if !out.FirstStopped || (out.SecondOutcome == "accepted" && !out.SecondStopped && !out.SecondCompleted) {
		return out, errors.New("exact fixture turns not confirmed terminal after busy test")
	}
	return out, nil
}
