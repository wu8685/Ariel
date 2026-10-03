package mockagent

import (
	"sync"
	"testing"
	"time"
)

func TestMockStoreListsSameTitleDistinctWorkspaceWithCursor(t *testing.T) {
	s := NewStore(StoreConfig{StepInterval: 10 * time.Millisecond})
	first, cursor, err := s.List(1, "")
	if err != nil || len(first) != 1 || cursor == "" {
		t.Fatalf("first page: %+v %q %v", first, cursor, err)
	}
	second, end, err := s.List(1, cursor)
	if err != nil || len(second) != 1 || end != "" {
		t.Fatalf("second page: %+v %q %v", second, end, err)
	}
	if first[0]["title"] != second[0]["title"] || first[0]["threadId"] == second[0]["threadId"] || first[0]["cwd"] == second[0]["cwd"] {
		t.Fatalf("fixture identity not distinct: %+v %+v", first[0], second[0])
	}
	if _, _, err := s.List(1, "bad-cursor"); err == nil {
		t.Fatal("invalid cursor accepted")
	}
}

func TestMockStoreBusyAndExactInterruptUnderConcurrentStarts(t *testing.T) {
	s := NewStore(StoreConfig{StepInterval: time.Second})
	const threadID = "mock-thread-a"
	var wg sync.WaitGroup
	results := make(chan string, 2)
	for _, id := range []string{"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			turn, err := s.Start(threadID, id, "hello")
			if err != nil {
				results <- err.Error()
			} else {
				results <- turn
			}
		}(id)
	}
	wg.Wait()
	close(results)
	accepted, busy := "", 0
	for r := range results {
		if r == "TURN_BUSY" {
			busy++
		} else {
			accepted = r
		}
	}
	if accepted == "" || busy != 1 {
		t.Fatalf("accepted=%q busy=%d", accepted, busy)
	}
	if err := s.Interrupt(threadID, "old-turn"); err == nil || err.Error() != "STALE_TURN" {
		t.Fatalf("stale stop: %v", err)
	}
	if err := s.Interrupt(threadID, accepted); err != nil {
		t.Fatal(err)
	}
	thread, err := s.Read(threadID)
	if err != nil || thread["runtime"] != "idle" {
		t.Fatalf("after stop: %+v %v", thread, err)
	}
	turns := thread["turns"].([]any)
	if len(turns) != 1 || turns[0].(map[string]any)["status"] != "interrupted" {
		t.Fatalf("wrong terminal turn: %+v", turns)
	}
}

func TestMockStoreSubscriptionsUseStableItemIDsAndIndependentUnsubscribe(t *testing.T) {
	s := NewStore(StoreConfig{StepInterval: 10 * time.Millisecond})
	var mu sync.Mutex
	counts := map[string]int{}
	callback := func(name string) func(map[string]any) {
		return func(event map[string]any) {
			mu.Lock()
			counts[name]++
			mu.Unlock()
		}
	}
	sub1, stream1, snapshot1, err := s.Subscribe("mock-thread-a", callback("first"))
	if err != nil || sub1 == "" || stream1 == "" || snapshot1["runtime"] != "idle" {
		t.Fatalf("subscribe one: %s %s %+v %v", sub1, stream1, snapshot1, err)
	}
	sub2, _, _, err := s.Subscribe("mock-thread-a", callback("second"))
	if err != nil || sub1 == sub2 {
		t.Fatal("second subscriber not independent")
	}
	if !s.Unsubscribe(sub1) {
		t.Fatal("first unsubscribe failed")
	}
	if _, err := s.Start("mock-thread-a", "00000000-0000-4000-8000-000000000003", "hello"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	mu.Lock()
	first, second := counts["first"], counts["second"]
	mu.Unlock()
	if first != 0 || second < 2 {
		t.Fatalf("subscriber counts first=%d second=%d", first, second)
	}
	thread, _ := s.Read("mock-thread-a")
	turn := thread["turns"].([]any)[0].(map[string]any)
	items := turn["items"].([]any)
	if len(items) != 2 || items[1].(map[string]any)["itemId"] == "" || turn["status"] != "completed" {
		t.Fatalf("stream terminal state: %+v", turn)
	}
}

func TestMockInteractionIsConsumedOnceAndRequiresAnswer(t *testing.T) {
	s := NewStore(StoreConfig{})
	if err := s.Respond("mock-thread-b", "mock-input-b", "answer", nil); err == nil || err.Error() != "INVALID_ARGUMENT" {
		t.Fatalf("missing answer: %v", err)
	}
	if err := s.Respond("mock-thread-b", "mock-input-b", "deny", nil); err == nil || err.Error() != "INTERACTION_UNSUPPORTED" {
		t.Fatalf("unsupported decision: %v", err)
	}
	if err := s.Respond("mock-thread-b", "mock-input-b", "answer", map[string][]string{"choice": {"A"}}); err != nil {
		t.Fatal(err)
	}
	thread, _ := s.Read("mock-thread-b")
	if len(thread["pendingInteractions"].([]any)) != 0 {
		t.Fatalf("interaction remains: %v", thread)
	}
	if err := s.Respond("mock-thread-b", "mock-input-b", "answer", map[string][]string{"choice": {"A"}}); err == nil || err.Error() != "INTERACTION_UNSUPPORTED" {
		t.Fatalf("stale answer: %v", err)
	}
}
