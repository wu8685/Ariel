package desktopipc

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

// These wire versions were inspected in Desktop 26.930.31730 / Codex 0.160.0.
// Presence in source is not an assertion that unexercised operations are supported.
const streamVersion = 11

type Observation struct {
	OwnerFound            bool     `json:"ownerFound"`
	SnapshotSeen          bool     `json:"snapshotSeen"`
	Snapshots             int      `json:"snapshots"`
	PatchBatches          int      `json:"patchBatches"`
	Revision              int64    `json:"revision"`
	StateBytes            int      `json:"stateBytes"`
	StateFields           []string `json:"stateFields"`
	PendingRequestCount   int      `json:"pendingRequestCount"`
	InteractionStateKnown bool     `json:"interactionStateKnown"`
}
type observationState struct {
	threadID, owner string
	reducer         *Reducer
	summary         Observation
}

func newObservationState(threadID, owner string) *observationState {
	return &observationState{threadID: threadID, owner: owner, reducer: NewReducer(int(DefaultMaxFrameBytes)), summary: Observation{OwnerFound: true}}
}
func (o *observationState) apply(body json.RawMessage) error {
	var event struct {
		Method  string `json:"method"`
		Version int    `json:"version"`
		Source  string `json:"sourceClientId"`
		Params  struct {
			ThreadID string          `json:"conversationId"`
			HostID   string          `json:"hostId"`
			Change   json.RawMessage `json:"change"`
		} `json:"params"`
	}
	if json.Unmarshal(body, &event) != nil {
		return ErrProtocol
	}
	if event.Method == "ipc-connection-reset" {
		return ErrResyncRequired
	}
	if event.Method != "thread-stream-state-changed" || event.Params.ThreadID != o.threadID {
		return nil
	}
	if event.Source != o.owner || event.Version != streamVersion || event.Params.HostID != "local" {
		return ErrProtocol
	}
	if err := o.reducer.Apply(event.Params.Change); err != nil {
		return err
	}
	var change struct {
		Type string `json:"type"`
	}
	json.Unmarshal(event.Params.Change, &change)
	if change.Type == "snapshot" {
		o.summary.Snapshots++
		o.summary.SnapshotSeen = true
	} else {
		o.summary.PatchBatches++
	}
	state, revision, _ := o.reducer.Snapshot()
	o.summary.Revision = revision
	o.summary.StateBytes = len(state)
	var fields map[string]json.RawMessage
	json.Unmarshal(state, &fields)
	o.summary.StateFields = nil
	for k := range fields {
		o.summary.StateFields = append(o.summary.StateFields, k)
	}
	sort.Strings(o.summary.StateFields)
	o.summary.InteractionStateKnown = false
	o.summary.PendingRequestCount = 0
	if requests, ok := fields["requests"]; ok {
		var list []json.RawMessage
		if json.Unmarshal(requests, &list) == nil && list != nil {
			o.summary.InteractionStateKnown = true
			o.summary.PendingRequestCount = len(list)
		}
	}
	return nil
}

func (c *Client) Observe(ctx context.Context, threadID string, watch time.Duration) (Observation, error) {
	if threadID == "" || watch <= 0 {
		return Observation{}, ErrProtocol
	}
	r, err := c.Call(ctx, Request{Method: "thread-owner-discovery", Version: 1, Params: map[string]string{"hostId": "local", "conversationId": threadID}})
	if err != nil {
		return Observation{}, err
	}
	owner := r.HandledByClientID
	if owner == "" {
		return Observation{}, ErrProtocol
	}
	o := newObservationState(threadID, owner)
	params := map[string]any{"hostId": "local", "conversationId": threadID, "following": true}
	if err := c.Broadcast(ctx, "thread-stream-following-changed", 1, params, []string{owner}); err != nil {
		return o.summary, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		c.Broadcast(cleanup, "thread-stream-following-changed", 1, map[string]any{"hostId": "local", "conversationId": threadID, "following": false}, []string{owner})
	}()
	loadCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	loadResult := make(chan error, 1)
	go func() {
		_, err := c.Call(loadCtx, Request{Method: "thread-follower-load-complete-history", Version: 1, TargetClientID: owner, Params: map[string]string{"conversationId": threadID}})
		loadResult <- err
	}()
	timer := time.NewTimer(watch)
	defer timer.Stop()
	loaded, watched := false, false
	for {
		if loaded && watched {
			if !o.summary.SnapshotSeen {
				return o.summary, errors.New("Desktop owner returned no snapshot")
			}
			return o.summary, nil
		}
		select {
		case err := <-loadResult:
			if err != nil {
				return o.summary, err
			}
			loaded = true
			loadResult = nil
		case <-c.EventsReady():
			body, ok := c.NextEvent()
			if !ok {
				if c.Err() != nil {
					return o.summary, c.Err()
				}
				continue
			}
			if err := o.apply(body); err != nil {
				return o.summary, err
			}
		case <-timer.C:
			watched = true
		case <-ctx.Done():
			return o.summary, ctx.Err()
		case <-c.Done():
			return o.summary, c.Err()
		}
	}
}
