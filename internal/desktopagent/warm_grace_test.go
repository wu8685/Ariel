package desktopagent

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"encoding/json"
)

// Issue #5: the last Web subscription disappearing must not immediately close
// the live follower. A short warm grace lets a backgrounded mobile browser or
// a transient WebSocket reconnect reattach without a full owner cold load.

func liveClosed(l *fakeLive) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closed
}

func TestServiceKeepsLiveFollowerWarmAfterLastUnsubscribe(t *testing.T) {
	var attached atomic.Int32
	live := &fakeLive{updates: make(chan struct{}, 1)}
	s := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) {
		attached.Add(1)
		return live, nil
	})
	s.idleGrace = time.Hour
	defer s.Close()
	id, _, _, activate, err := s.Subscribe(context.Background(), "thread", func(map[string]any) {})
	if err != nil {
		t.Fatal(err)
	}
	activate()
	if !s.Unsubscribe(id) {
		t.Fatal("unsubscribe failed")
	}
	if liveClosed(live) {
		t.Fatal("idle follower closed immediately instead of entering warm grace")
	}
	again, _, _, activateAgain, err := s.Subscribe(context.Background(), "thread", func(map[string]any) {})
	if err != nil {
		t.Fatal(err)
	}
	activateAgain()
	if again == id {
		t.Fatal("warm reuse must still create a fresh subscription identity")
	}
	if attached.Load() != 1 {
		t.Fatalf("warm resubscribe re-attached the owner: attaches=%d", attached.Load())
	}
}

func TestServiceWarmGraceExpiresAndNextSubscribeReattaches(t *testing.T) {
	var mu sync.Mutex
	var lives []*fakeLive
	s := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) {
		live := &fakeLive{updates: make(chan struct{}, 1)}
		mu.Lock()
		lives = append(lives, live)
		mu.Unlock()
		return live, nil
	})
	s.idleGrace = 30 * time.Millisecond
	defer s.Close()
	id, _, _, activate, err := s.Subscribe(context.Background(), "thread", func(map[string]any) {})
	if err != nil {
		t.Fatal(err)
	}
	activate()
	if !s.Unsubscribe(id) {
		t.Fatal("unsubscribe failed")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		first := lives[0]
		mu.Unlock()
		if liveClosed(first) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("warm follower survived past idle grace")
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, _, _, activate2, err := s.Subscribe(context.Background(), "thread", func(map[string]any) {})
	if err != nil {
		t.Fatal(err)
	}
	activate2()
	mu.Lock()
	count := len(lives)
	mu.Unlock()
	if count != 2 {
		t.Fatalf("post-grace resubscribe did not re-attach: attaches=%d", count)
	}
}

func TestServiceInvalidationClosesWarmFollowerImmediately(t *testing.T) {
	live := &malformedLive{fakeLive: &fakeLive{updates: make(chan struct{}, 1)}}
	s := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) { return live, nil })
	s.idleGrace = time.Hour
	defer s.Close()
	id, _, _, activate, err := s.Subscribe(context.Background(), "thread", func(map[string]any) {})
	if err != nil {
		t.Fatal(err)
	}
	activate()
	if !s.Unsubscribe(id) {
		t.Fatal("unsubscribe failed")
	}
	live.malformed.Store(true)
	live.updates <- struct{}{}
	deadline := time.Now().Add(2 * time.Second)
	for !liveClosed(live.fakeLive) {
		if time.Now().After(deadline) {
			t.Fatal("native state uncertainty did not close the warm follower immediately")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestServiceCloseStopsWarmFollowerImmediately(t *testing.T) {
	live := &fakeLive{updates: make(chan struct{}, 1)}
	s := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) { return live, nil })
	s.idleGrace = time.Hour
	id, _, _, activate, err := s.Subscribe(context.Background(), "thread", func(map[string]any) {})
	if err != nil {
		t.Fatal(err)
	}
	activate()
	if !s.Unsubscribe(id) {
		t.Fatal("unsubscribe failed")
	}
	s.Close()
	if !liveClosed(live) {
		t.Fatal("Service Close left a warm follower running")
	}
}

func TestServiceMutationReusesWarmFollower(t *testing.T) {
	var attached atomic.Int32
	live := &fakeLive{updates: make(chan struct{}, 1)}
	s := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) {
		attached.Add(1)
		return live, nil
	})
	s.idleGrace = time.Hour
	defer s.Close()
	id, _, _, activate, err := s.Subscribe(context.Background(), "thread", func(map[string]any) {})
	if err != nil {
		t.Fatal(err)
	}
	activate()
	if !s.Unsubscribe(id) {
		t.Fatal("unsubscribe failed")
	}
	if _, err := s.Start(context.Background(), "thread", "message-warm", "hello"); err != nil {
		t.Fatalf("warm mutation failed: %v", err)
	}
	if attached.Load() != 1 {
		t.Fatalf("mutation during warm grace re-attached the owner: attaches=%d", attached.Load())
	}
}

func TestServiceWarmRetireSkipsCloseWhileResubscribed(t *testing.T) {
	live := &fakeLive{updates: make(chan struct{}, 1)}
	s := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) { return live, nil })
	s.idleGrace = 20 * time.Millisecond
	defer s.Close()
	id, _, _, activate, err := s.Subscribe(context.Background(), "thread", func(map[string]any) {})
	if err != nil {
		t.Fatal(err)
	}
	activate()
	if !s.Unsubscribe(id) {
		t.Fatal("unsubscribe failed")
	}
	// Resubscribe before the grace fires: the pending retire timer must not
	// close a follower that has active subscriptions again.
	again, _, _, activateAgain, err := s.Subscribe(context.Background(), "thread", func(map[string]any) {})
	if err != nil {
		t.Fatal(err)
	}
	activateAgain()
	time.Sleep(5 * s.idleGrace)
	if liveClosed(live) {
		t.Fatal("pending warm retire closed a resubscribed follower")
	}
	if !s.Unsubscribe(again) {
		t.Fatal("second unsubscribe failed")
	}
	deadline := time.Now().Add(2 * time.Second)
	for !liveClosed(live) {
		if time.Now().After(deadline) {
			t.Fatal("follower never retired after the final unsubscribe")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Regression for PR #6 review: reattaching during the warm grace must cancel
// the pending retire timer, so a later unsubscribe gets a full fresh grace
// instead of the remainder of the first one. Spec 0086 starts the 90 seconds
// when the last subscription disappears — measured from the LAST unsubscribe.
func TestServiceResubscribeRearmsFullWarmGrace(t *testing.T) {
	live := &fakeLive{updates: make(chan struct{}, 1)}
	s := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) { return live, nil })
	s.idleGrace = 200 * time.Millisecond
	defer s.Close()
	id, _, _, activate, err := s.Subscribe(context.Background(), "thread", func(map[string]any) {})
	if err != nil {
		t.Fatal(err)
	}
	activate()
	if !s.Unsubscribe(id) {
		t.Fatal("unsubscribe failed")
	}
	// Reattach halfway through the first grace.
	time.Sleep(s.idleGrace / 2)
	again, _, _, activateAgain, err := s.Subscribe(context.Background(), "thread", func(map[string]any) {})
	if err != nil {
		t.Fatal(err)
	}
	activateAgain()
	time.Sleep(s.idleGrace / 4)
	if !s.Unsubscribe(again) {
		t.Fatal("second unsubscribe failed")
	}
	// The timer armed by the first unsubscribe fires around now. The follower
	// must survive on the fresh grace armed by the second unsubscribe.
	time.Sleep(s.idleGrace / 2)
	if liveClosed(live) {
		t.Fatal("follower retired on the first unsubscribe's timer instead of a full fresh grace after the second unsubscribe")
	}
	// And it must still retire once that fresh grace expires.
	deadline := time.Now().Add(2 * time.Second)
	for !liveClosed(live) {
		if time.Now().After(deadline) {
			t.Fatal("follower never retired after the second unsubscribe's grace expired")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestServiceInitialSnapshotBoundsRecentWindow(t *testing.T) {
	turns := make([]string, 0, 6)
	for _, id := range []string{"t1", "t2", "t3", "t4", "t5", "t6"} {
		turns = append(turns, `{"turnId":"`+id+`","status":"completed","items":[{"id":"`+id+`-item","type":"agentMessage","text":"`+id+`"}]}`)
	}
	state := json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[],"turns":[` + strings.Join(turns, ",") + `]}`)
	live := &staticLive{fakeLive: &fakeLive{updates: make(chan struct{}, 1)}, state: state}
	s := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) { return live, nil })
	defer s.Close()
	_, _, snapshot, activate, err := s.Subscribe(context.Background(), "thread", func(map[string]any) {})
	if err != nil {
		t.Fatal(err)
	}
	activate()
	got := snapshot["turns"].([]any)
	if len(got) > recentTurnLimit {
		t.Fatalf("initial snapshot exceeds the bounded recent window: %d turns", len(got))
	}
	if snapshot["historyComplete"] != false {
		t.Fatalf("initial snapshot with older turns must be marked incomplete: %v", snapshot["historyComplete"])
	}
	first := got[0].(map[string]any)["turnId"]
	if first != "t5" {
		t.Fatalf("initial snapshot must keep the newest turns, first=%v", first)
	}
}
