package desktopagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/codex/desktopipc"
)

type fakeHistory struct{}
type anyThreadHistory struct{}

func (anyThreadHistory) List(context.Context, string, int) (appserver.Page, error) {
	return appserver.Page{}, nil
}
func (anyThreadHistory) Read(_ context.Context, id string) (appserver.Thread, error) {
	return appserver.Thread{ID: id, Name: "Fixture", CWD: "/fixture", Status: json.RawMessage(`{"type":"idle"}`)}, nil
}

type heavyListHistory struct{ fakeHistory }

func (heavyListHistory) List(context.Context, string, int) (appserver.Page, error) {
	item, _ := json.Marshal(map[string]any{"type": "agentMessage", "id": "big", "text": strings.Repeat("x", 7<<20)})
	return appserver.Page{Data: []appserver.Thread{{ID: "thread", Name: "Title", CWD: "/fixture", UpdatedAt: 1, Status: json.RawMessage(`{"type":"idle"}`), Turns: []appserver.Turn{{ID: "turn", Status: "completed", Items: []json.RawMessage{item}}}}}}, nil
}

func (fakeHistory) List(context.Context, string, int) (appserver.Page, error) {
	return appserver.Page{Data: []appserver.Thread{{ID: "thread", Name: "Title", CWD: "/fixture", Status: json.RawMessage(`{"type":"idle"}`)}}}, nil
}
func (fakeHistory) Read(context.Context, string) (appserver.Thread, error) {
	return appserver.Thread{ID: "thread", Name: "Title", CWD: "/fixture", Status: json.RawMessage(`{"type":"idle"}`)}, nil
}

type fakeLive struct {
	mu        sync.Mutex
	busy      bool
	closed    bool
	responded string
	updates   chan struct{}
	done      chan struct{}
}

type slowLive struct {
	*fakeLive
	started chan struct{}
	release chan struct{}
	stopped chan struct{}
}

type observedLive struct {
	*fakeLive
	muCalls sync.Mutex
	calls   int
	second  chan struct{}
}

type largeLive struct{ *fakeLive }
type malformedLive struct {
	*fakeLive
	malformed   atomic.Bool
	placeholder atomic.Bool
}
type growingLive struct {
	*fakeLive
	large atomic.Bool
}
type droppingLive struct {
	*fakeLive
	done chan struct{}
}

func (l *droppingLive) Done() <-chan struct{} { return l.done }

func (l *growingLive) Current() (json.RawMessage, error) {
	if l.large.Load() {
		return (&largeLive{l.fakeLive}).Current()
	}
	return l.fakeLive.Current()
}

func (l *largeLive) Current() (json.RawMessage, error) {
	item, _ := json.Marshal(map[string]any{"type": "agentMessage", "id": "large", "text": strings.Repeat("x", 7<<20)})
	return json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[],"turns":[{"turnId":"one","status":"completed","items":[` + string(item) + `]}]}`), nil
}

func (l *malformedLive) Current() (json.RawMessage, error) {
	if l.malformed.Load() {
		return json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[],"turnHistory":{"kind":"canonical","history":{"islands":[{"entries":[{"value":"ghost"}]}],"entitiesByKey":{"ghost":{"turnId":"","status":"completed","items":[]}}}}}`), nil
	}
	if l.placeholder.Load() {
		return json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"active"},"requests":[],"turnHistory":{"kind":"canonical","history":{"islands":[{"entries":[{"value":"ghost"}]}],"entitiesByKey":{"ghost":{"turnId":"","status":"inProgress","items":[]}}}}}`), nil
	}
	return l.fakeLive.Current()
}

func TestServiceWaitsForTransientNativePlaceholderWithoutLosingSubscription(t *testing.T) {
	live := &malformedLive{fakeLive: &fakeLive{updates: make(chan struct{}, 1)}}
	s := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) { return live, nil })
	defer s.Close()
	events := make(chan map[string]any, 2)
	_, _, _, activate, err := s.Subscribe(context.Background(), "thread", func(event map[string]any) { events <- event })
	if err != nil {
		t.Fatal(err)
	}
	activate()
	live.placeholder.Store(true)
	live.updates <- struct{}{}
	select {
	case event := <-events:
		t.Fatalf("transient placeholder must not terminate subscription: %v", event)
	case <-time.After(50 * time.Millisecond):
	}
	live.placeholder.Store(false)
	live.updates <- struct{}{}
	select {
	case event := <-events:
		if event["event"] != "thread.update" {
			t.Fatalf("healthy state did not resume subscription: %v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("missing resumed update")
	}
}

func TestServicePersistentNativePlaceholderBecomesUncertain(t *testing.T) {
	live := &malformedLive{fakeLive: &fakeLive{updates: make(chan struct{}, 1)}}
	s := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) { return live, nil })
	s.placeholderGrace = 50 * time.Millisecond
	defer s.Close()
	events := make(chan map[string]any, 1)
	_, _, _, activate, err := s.Subscribe(context.Background(), "thread", func(event map[string]any) { events <- event })
	if err != nil {
		t.Fatal(err)
	}
	activate()
	live.placeholder.Store(true)
	live.updates <- struct{}{}
	select {
	case event := <-events:
		if event["event"] != "thread.error" || event["code"] != "NATIVE_STATE_UNCERTAIN" {
			t.Fatalf("persistent placeholder must fail closed: %v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("persistent placeholder did not time out")
	}
}

func TestServiceMalformedNativeStateIsTerminalNotResync(t *testing.T) {
	live := &malformedLive{fakeLive: &fakeLive{updates: make(chan struct{}, 1)}}
	s := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) { return live, nil })
	defer s.Close()
	events := make(chan map[string]any, 1)
	_, _, _, activate, err := s.Subscribe(context.Background(), "thread", func(event map[string]any) { events <- event })
	if err != nil {
		t.Fatal(err)
	}
	activate()
	live.malformed.Store(true)
	live.updates <- struct{}{}
	select {
	case event := <-events:
		if event["event"] != "thread.error" || event["code"] != "NATIVE_STATE_UNCERTAIN" {
			t.Fatalf("native shape must not cause an automatic resync: %v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("missing native-state error")
	}
	_, _, _, _, err = s.Subscribe(context.Background(), "thread", func(map[string]any) {})
	if err == nil || err.Error() != "NATIVE_STATE_UNCERTAIN" {
		t.Fatalf("malformed initial snapshot did not fail closed: %v", err)
	}
}

func TestPermissionChangeRequiresThreadUpdate(t *testing.T) {
	a := map[string]any{"runtime": "idle", "turns": []any{}, "pendingInteractions": []any{}, "permissions": map[string]any{"sandbox": "read_only", "approval": "on_request"}}
	b := map[string]any{"runtime": "idle", "turns": []any{}, "pendingInteractions": []any{}, "permissions": map[string]any{"sandbox": "full_access", "approval": "on_request"}}
	if sameThreadContent(a, b) {
		t.Fatal("permission mode change was hidden from subscribed Web")
	}
}

func TestServiceRejectsDirectMutationOfMalformedNativeState(t *testing.T) {
	live := &malformedLive{fakeLive: &fakeLive{updates: make(chan struct{}, 1)}}
	live.malformed.Store(true)
	s := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) { return live, nil })
	defer s.Close()
	if _, err := s.Start(context.Background(), "thread", "message-id", "hello"); err == nil || err.Error() != "NATIVE_STATE_UNCERTAIN" {
		t.Fatalf("start not rejected: %v", err)
	}
	if err := s.Interrupt(context.Background(), "thread", "turn"); err == nil || err.Error() != "NATIVE_STATE_UNCERTAIN" {
		t.Fatalf("interrupt not rejected: %v", err)
	}
	if err := s.Respond(context.Background(), "thread", "interaction", "deny", nil); err == nil || err.Error() != "NATIVE_STATE_UNCERTAIN" {
		t.Fatalf("respond not rejected: %v", err)
	}
	live.mu.Lock()
	defer live.mu.Unlock()
	if live.busy || live.responded != "" {
		t.Fatal("malformed native state was mutated")
	}
}

func (l *observedLive) Current() (json.RawMessage, error) {
	l.muCalls.Lock()
	l.calls++
	if l.calls == 2 {
		close(l.second)
	}
	l.muCalls.Unlock()
	return l.fakeLive.Current()
}

func (l *slowLive) Start(ctx context.Context, _, _ string) (string, error) {
	l.mu.Lock()
	l.busy = true
	l.mu.Unlock()
	l.updates <- struct{}{}
	close(l.started)
	select {
	case <-l.release:
		return "one", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (l *slowLive) Interrupt(context.Context, string) error {
	close(l.stopped)
	return nil
}

func (l *fakeLive) Current() (json.RawMessage, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	status := "idle"
	turns := `[]`
	if l.busy {
		status = "inProgress"
		turns = `[{"turnId":"one","status":"inProgress","items":[]}]`
	}
	return json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"` + status + `"},"requests":[],"turns":` + turns + `}`), nil
}
func (l *fakeLive) Start(context.Context, string, string) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.busy {
		return "", desktopipc.ErrTurnBusy
	}
	l.busy = true
	return "one", nil
}
func (l *fakeLive) Interrupt(context.Context, string) error { return nil }
func (l *fakeLive) Respond(_ context.Context, id, decision string, _ map[string][]string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.responded = id + ":" + decision
	return nil
}
func (l *fakeLive) Updates() <-chan struct{} { return l.updates }
func (l *fakeLive) Done() <-chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.done == nil {
		l.done = make(chan struct{})
		if l.closed {
			close(l.done)
		}
	}
	return l.done
}
func (l *fakeLive) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.closed {
		l.closed = true
		if l.done != nil {
			close(l.done)
		}
	}
	return nil
}

func TestServiceBoundsControllersAndRetiresIdleFollower(t *testing.T) {
	var first *fakeLive
	s := NewService(anyThreadHistory{}, func(_ context.Context, id, _ string) (Live, error) {
		live := &fakeLive{updates: make(chan struct{}, 1)}
		if id == "thread-0" {
			first = live
		}
		return live, nil
	})
	defer s.Close()
	var subscriptions []string
	for i := range 64 {
		id, _, _, _, err := s.Subscribe(context.Background(), fmt.Sprintf("thread-%d", i), func(map[string]any) {})
		if err != nil {
			t.Fatalf("subscribe %d: %v", i, err)
		}
		subscriptions = append(subscriptions, id)
	}
	if _, _, _, _, err := s.Subscribe(context.Background(), "thread-overflow", func(map[string]any) {}); err == nil || err.Error() != "OVERLOADED" {
		t.Fatalf("active controllers exceeded cap: %v", err)
	}
	if !s.Unsubscribe(subscriptions[0]) {
		t.Fatal("unsubscribe first")
	}
	first.mu.Lock()
	closed := first.closed
	first.mu.Unlock()
	if !closed {
		t.Fatal("idle follower remained open")
	}
	if _, _, _, _, err := s.Subscribe(context.Background(), "thread-after-retire", func(map[string]any) {}); err != nil {
		t.Fatalf("idle controller was not evicted: %v", err)
	}
}

func TestServiceBoundsSubscriptionsPerThread(t *testing.T) {
	s := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) {
		return &fakeLive{updates: make(chan struct{}, 1)}, nil
	})
	defer s.Close()
	for range 16 {
		if _, _, _, _, err := s.Subscribe(context.Background(), "thread", func(map[string]any) {}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, _, _, err := s.Subscribe(context.Background(), "thread", func(map[string]any) {}); err == nil || err.Error() != "OVERLOADED" {
		t.Fatalf("subscriptions exceeded cap: %v", err)
	}
}

func TestServiceDeduplicatesMessageAfterControllerEviction(t *testing.T) {
	s := NewService(anyThreadHistory{}, func(context.Context, string, string) (Live, error) {
		return &fakeLive{updates: make(chan struct{}, 1)}, nil
	})
	defer s.Close()
	if _, err := s.Start(context.Background(), "thread-original", "message-once", "hello"); err != nil {
		t.Fatal(err)
	}
	for i := range 64 {
		id, _, _, _, err := s.Subscribe(context.Background(), fmt.Sprintf("thread-%d", i), func(map[string]any) {})
		if err != nil {
			t.Fatal(err)
		}
		if !s.Unsubscribe(id) {
			t.Fatal("unsubscribe fixture")
		}
	}
	if _, err := s.Start(context.Background(), "thread-original", "message-once", "hello"); err == nil || err.Error() != "INVALID_ARGUMENT" {
		t.Fatalf("message replayed after controller eviction: %v", err)
	}
}

func TestServiceSerializesConcurrentStartsAndRejectsDuplicateID(t *testing.T) {
	var mu sync.Mutex
	attached := 0
	s := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) {
		mu.Lock()
		attached++
		mu.Unlock()
		return &fakeLive{updates: make(chan struct{}, 1)}, nil
	})
	defer s.Close()
	// Keep one owner follower attached. Otherwise the first Start can finish and
	// retire its fake owner before the second goroutine runs; a newly constructed
	// fake owner then forgets the real owner's busy state.
	if _, _, _, _, err := s.Subscribe(context.Background(), "thread", func(map[string]any) {}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan string, 2)
	for _, id := range []string{"id-1", "id-2"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, err := s.Start(context.Background(), "thread", id, "hello")
			if err != nil {
				results <- id + ":" + err.Error()
			} else {
				results <- id + ":accepted"
			}
		}(id)
	}
	wg.Wait()
	close(results)
	accepted, busy, acceptedID := 0, 0, ""
	for result := range results {
		if strings.HasSuffix(result, ":accepted") {
			accepted++
			acceptedID = strings.SplitN(result, ":", 2)[0]
		} else if strings.HasSuffix(result, ":TURN_BUSY") {
			busy++
		}
	}
	if accepted != 1 || busy != 1 {
		t.Fatalf("accepted=%d busy=%d", accepted, busy)
	}
	if _, err := s.Start(context.Background(), "thread", acceptedID, "hello"); err == nil || err.Error() != "INVALID_ARGUMENT" {
		t.Fatalf("duplicate: %v", err)
	}
	mu.Lock()
	count := attached
	mu.Unlock()
	if count < 1 {
		t.Fatal("not attached")
	}
}

func TestServiceSubscriptionActivatesAfterSnapshotAndTracksSequence(t *testing.T) {
	live := &fakeLive{updates: make(chan struct{}, 2)}
	s := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) { return live, nil })
	events := make(chan map[string]any, 2)
	id, stream, snapshot, activate, err := s.Subscribe(context.Background(), "thread", func(m map[string]any) { events <- m })
	if err != nil || id == "" || stream == "" || snapshot["runtime"] != "idle" {
		t.Fatalf("subscribe: %s %s %v %v", id, stream, snapshot, err)
	}
	activate()
	live.mu.Lock()
	live.busy = true
	live.mu.Unlock()
	live.updates <- struct{}{}
	select {
	case event := <-events:
		if event["seq"] != 2 || event["baseSeq"] != 1 || event["subscriptionId"] != id {
			t.Fatalf("event: %v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("update not delivered")
	}
	if !s.Unsubscribe(id) {
		t.Fatal("unsubscribe failed")
	}
	live.mu.Lock()
	live.busy = false
	live.mu.Unlock()
	live.updates <- struct{}{}
	select {
	case event := <-events:
		t.Fatalf("unsubscribed event: %v", event)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestServiceDeliversUpdateAndAcceptsInterruptDuringSlowStart(t *testing.T) {
	live := &slowLive{fakeLive: &fakeLive{updates: make(chan struct{}, 2)}, started: make(chan struct{}), release: make(chan struct{}), stopped: make(chan struct{})}
	s := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) { return live, nil })
	events := make(chan map[string]any, 2)
	_, _, _, activate, err := s.Subscribe(context.Background(), "thread", func(m map[string]any) { events <- m })
	if err != nil {
		t.Fatal(err)
	}
	activate()
	startDone := make(chan error, 1)
	go func() { _, err := s.Start(context.Background(), "thread", "message-1", "hello"); startDone <- err }()
	<-live.started
	select {
	case event := <-events:
		if event["event"] != "thread.update" || event["thread"].(map[string]any)["runtime"] != "inProgress" {
			t.Fatalf("wrong update: %v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("live update blocked by native start receipt")
	}
	interruptDone := make(chan error, 1)
	go func() { interruptDone <- s.Interrupt(context.Background(), "thread", "one") }()
	select {
	case err := <-interruptDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("interrupt blocked by native start receipt")
	}
	close(live.release)
	if err := <-startDone; err != nil {
		t.Fatal(err)
	}
}

func TestServiceActivationCatchesStateChangeBeforeSnapshotDelivery(t *testing.T) {
	live := &observedLive{fakeLive: &fakeLive{updates: make(chan struct{}, 2)}, second: make(chan struct{})}
	s := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) { return live, nil })
	events := make(chan map[string]any, 2)
	_, _, snapshot, activate, err := s.Subscribe(context.Background(), "thread", func(m map[string]any) { events <- m })
	if err != nil || snapshot["runtime"] != "idle" {
		t.Fatalf("snapshot: %v %v", snapshot, err)
	}
	live.mu.Lock()
	live.busy = true
	live.mu.Unlock()
	live.updates <- struct{}{}
	<-live.second // Pump discarded the update while the subscription was inactive.
	activate()
	select {
	case event := <-events:
		if event["seq"] != 2 || event["thread"].(map[string]any)["runtime"] != "inProgress" {
			t.Fatalf("catch-up update: %v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("state changed before activation was lost")
	}
}

func TestServiceRoutesInteractionToAttachedOwner(t *testing.T) {
	live := &fakeLive{updates: make(chan struct{}, 1)}
	s := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) { return live, nil })
	if err := s.Respond(context.Background(), "thread", "req:abc", "answer", map[string][]string{"q": {"yes"}}); err != nil {
		t.Fatal(err)
	}
	live.mu.Lock()
	defer live.mu.Unlock()
	if live.responded != "req:abc:answer" {
		t.Fatal("response did not reach live owner")
	}
}

func TestServiceRejectsOversizeSnapshotBeforeAcceptingSubscription(t *testing.T) {
	live := &largeLive{fakeLive: &fakeLive{updates: make(chan struct{}, 1)}}
	s := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) { return live, nil })
	_, _, _, _, err := s.Subscribe(context.Background(), "thread", func(map[string]any) {})
	if err != ErrHistoryTooLarge {
		t.Fatalf("oversize history was not rejected: %v", err)
	}
}

func TestServiceListExcludesHistoricalTurns(t *testing.T) {
	s := NewService(heavyListHistory{}, nil)
	page, _, err := s.List(context.Background(), 50, "")
	if err != nil || len(page) != 1 {
		t.Fatalf("list failed: %v count=%d", err, len(page))
	}
	if page[0]["threadId"] != "thread" || len(page[0]["turns"].([]any)) != 0 {
		t.Fatal("list leaked full history")
	}
}

func TestServiceStopsOversizeLiveStreamWithExplicitError(t *testing.T) {
	live := &growingLive{fakeLive: &fakeLive{updates: make(chan struct{}, 1)}}
	s := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) { return live, nil })
	defer s.Close()
	events := make(chan map[string]any, 2)
	_, _, _, activate, err := s.Subscribe(context.Background(), "thread", func(event map[string]any) { events <- event })
	if err != nil {
		t.Fatal(err)
	}
	activate()
	live.large.Store(true)
	live.updates <- struct{}{}
	select {
	case event := <-events:
		if event["event"] != "thread.error" || event["code"] != "HISTORY_TOO_LARGE" {
			t.Fatalf("oversize update leaked: event=%v code=%v", event["event"], event["code"])
		}
	case <-time.After(time.Second):
		t.Fatal("missing explicit stream error")
	}
}

func TestServiceReattachesAfterOwnerFollowerDrops(t *testing.T) {
	first := &droppingLive{fakeLive: &fakeLive{updates: make(chan struct{}, 1)}, done: make(chan struct{})}
	second := &droppingLive{fakeLive: &fakeLive{updates: make(chan struct{}, 1)}, done: make(chan struct{})}
	attachCount := 0
	s := NewService(fakeHistory{}, func(context.Context, string, string) (Live, error) {
		attachCount++
		if attachCount == 1 {
			return first, nil
		}
		return second, nil
	})
	defer s.Close()
	events := make(chan map[string]any, 1)
	_, oldStream, _, activate, err := s.Subscribe(context.Background(), "thread", func(event map[string]any) { events <- event })
	if err != nil {
		t.Fatal(err)
	}
	activate()
	close(first.done)
	select {
	case event := <-events:
		if event["event"] != "thread.error" || event["code"] != "RESYNC_REQUIRED" {
			t.Fatalf("owner loss was silent: event=%v code=%v", event["event"], event["code"])
		}
	case <-time.After(time.Second):
		t.Fatal("missing owner-loss notification")
	}
	_, newStream, _, _, err := s.Subscribe(context.Background(), "thread", func(map[string]any) {})
	if err != nil || newStream == oldStream || attachCount != 2 {
		t.Fatalf("owner not reattached: err=%v streamsEqual=%v attachCount=%d", err, newStream == oldStream, attachCount)
	}
}
