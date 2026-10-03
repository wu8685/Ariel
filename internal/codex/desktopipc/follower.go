package desktopipc

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// Follower owns one Desktop IPC subscription. The caller must have discovered
// the real owner; this never creates a second Codex executor.
type Follower struct {
	c         *Client
	o         *observationState
	cwd       string
	opMu      sync.Mutex
	startMu   sync.Mutex
	respondMu sync.Mutex
	stateMu   sync.Mutex
	updates   chan struct{}
	wake      chan struct{}
	closed    chan struct{}
	once      sync.Once
	err       error
}

func Follow(ctx context.Context, c *Client, threadID, owner, cwd string) (*Follower, error) {
	if c == nil || threadID == "" || owner == "" || cwd == "" {
		return nil, ErrProtocol
	}
	f := &Follower{c: c, o: newObservationState(threadID, owner), cwd: cwd, updates: make(chan struct{}, 1), wake: make(chan struct{}, 1), closed: make(chan struct{})}
	if err := c.Broadcast(ctx, "thread-stream-following-changed", 1, map[string]any{"hostId": "local", "conversationId": threadID, "following": true}, []string{owner}); err != nil {
		return nil, err
	}
	go f.consume()
	if _, err := f.Refresh(ctx); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func (f *Follower) consume() {
	for {
		select {
		case <-f.c.EventsReady():
			for {
				body, ok := f.c.NextEvent()
				if !ok {
					break
				}
				f.stateMu.Lock()
				before := f.o.summary.Revision
				beforeSnapshots := f.o.summary.Snapshots
				err := f.o.apply(body)
				if err != nil {
					f.err = err
				}
				changed := f.o.summary.Revision != before || f.o.summary.Snapshots > beforeSnapshots
				f.stateMu.Unlock()
				if err != nil {
					f.c.Close()
					return
				}
				if changed {
					select {
					case f.updates <- struct{}{}:
					default:
					}
					select {
					case f.wake <- struct{}{}:
					default:
					}
				}
			}
		case <-f.c.Done():
			return
		case <-f.closed:
			return
		}
	}
}

func (f *Follower) Updates() <-chan struct{} { return f.updates }
func (f *Follower) Done() <-chan struct{}    { return f.c.Done() }
func (f *Follower) Owner() string            { return f.o.owner }

func (f *Follower) Current() (json.RawMessage, error) {
	f.stateMu.Lock()
	defer f.stateMu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	state, _, ok := f.o.reducer.Snapshot()
	if !ok {
		return nil, ErrResyncRequired
	}
	return state, nil
}

func (f *Follower) Refresh(ctx context.Context) (json.RawMessage, error) {
	f.opMu.Lock()
	defer f.opMu.Unlock()
	return f.refreshLocked(ctx)
}

func (f *Follower) refreshLocked(ctx context.Context) (json.RawMessage, error) {
	f.stateMu.Lock()
	before := f.o.summary.Snapshots
	f.stateMu.Unlock()
	_, err := f.c.Call(ctx, Request{Method: "thread-follower-load-complete-history", Version: 1, TargetClientID: f.o.owner, Params: map[string]string{"conversationId": f.o.threadID}})
	if err != nil {
		return nil, err
	}
	for {
		f.stateMu.Lock()
		count := f.o.summary.Snapshots
		state, _, ok := f.o.reducer.Snapshot()
		stateErr := f.err
		f.stateMu.Unlock()
		if stateErr != nil {
			return nil, stateErr
		}
		if count > before && ok {
			var identity struct {
				CWD string `json:"cwd"`
			}
			if json.Unmarshal(state, &identity) != nil || identity.CWD != f.cwd {
				return nil, ErrProtocol
			}
			if !nativeCanonicalAddressable(state) {
				return nil, ErrNativeStateUncertain
			}
			return state, nil
		}
		select {
		case <-f.wake:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.c.Done():
			return nil, f.c.Err()
		}
	}
}

func (f *Follower) Start(ctx context.Context, clientMessageID, text string) (string, error) {
	f.startMu.Lock()
	defer f.startMu.Unlock()
	f.opMu.Lock()
	state, err := f.refreshLocked(ctx)
	f.opMu.Unlock()
	if err != nil {
		return "", err
	}
	turnID, err := StartProductionTurn(ctx, f.c, f.o.owner, f.o.threadID, f.cwd, state, clientMessageID, text)
	if err != nil {
		return "", err
	}
	verifyCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	for {
		current, err := f.Refresh(verifyCtx)
		if err != nil {
			return "", &CallError{Cause: err, Outcome: "unknown"}
		}
		if TurnContainsClientMessage(current, turnID, clientMessageID, text) {
			return turnID, nil
		}
		select {
		case <-time.After(200 * time.Millisecond):
		case <-verifyCtx.Done():
			return "", &CallError{Cause: verifyCtx.Err(), Outcome: "unknown"}
		}
	}
}

func (f *Follower) Interrupt(ctx context.Context, expectedTurnID string) error {
	f.opMu.Lock()
	defer f.opMu.Unlock()
	state, err := f.refreshLocked(ctx)
	if err != nil {
		return err
	}
	return InterruptProductionTurn(ctx, f.c, f.o.owner, f.o.threadID, f.cwd, state, expectedTurnID)
}

func (f *Follower) Respond(ctx context.Context, interactionID, decision string, answers map[string][]string) error {
	f.respondMu.Lock()
	defer f.respondMu.Unlock()
	state, err := f.Refresh(ctx)
	if err != nil {
		return err
	}
	receipt, err := SubmitProductionInteraction(ctx, f.c, f.o.owner, f.o.threadID, f.cwd, state, interactionID, decision, answers)
	if err != nil {
		return err
	}
	verifyCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	for {
		state, err = f.Refresh(verifyCtx)
		if err != nil {
			return &CallError{Cause: err, Outcome: "unknown"}
		}
		if receipt.Confirmed(state) {
			return nil
		}
		select {
		case <-time.After(200 * time.Millisecond):
		case <-verifyCtx.Done():
			return &CallError{Cause: verifyCtx.Err(), Outcome: "unknown"}
		}
	}
}

func (f *Follower) Close() error {
	f.once.Do(func() {
		close(f.closed)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = f.c.Broadcast(ctx, "thread-stream-following-changed", 1, map[string]any{"hostId": "local", "conversationId": f.o.threadID, "following": false}, []string{f.o.owner})
		_ = f.c.Close()
	})
	return nil
}
