package desktopipc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

const budgetEvent = `{"type":"broadcast","method":"fixture","secret":"must-not-be-logged"}`

func budgetClient(t *testing.T, budget *EventByteBudget, count, size int, capture func(EventQueuePressure)) *Client {
	t.Helper()
	local, peer := net.Pipe()
	c := NewClient(local, Options{MaxEvents: count, MaxEventBytes: size, EventBudget: budget, OnEventOverload: capture})
	t.Cleanup(func() { c.Close(); peer.Close() })
	return c
}

func TestEventByteBudgetChargesActualQueueAndDoesNotPreallocate(t *testing.T) {
	n := len(budgetEvent)
	budget := NewEventByteBudget(2 * n)
	first := budgetClient(t, budget, 8, 8*n, nil)
	second := budgetClient(t, budget, 8, 8*n, nil)
	if used, limit := budget.Usage(); used != 0 || limit != 2*n {
		t.Fatalf("idle clients reserved bytes: %d/%d", used, limit)
	}
	if !first.enqueueEvent([]byte(budgetEvent)) || !first.enqueueEvent([]byte(budgetEvent)) {
		t.Fatal("exact shared boundary rejected")
	}
	if used, _ := budget.Usage(); used != 2*n {
		t.Fatalf("actual queue charge=%d", used)
	}
	if second.enqueueEvent([]byte(budgetEvent)) || !errors.Is(second.Err(), ErrOverloaded) {
		t.Fatalf("global overcommit accepted: %v", second.Err())
	}
	var overflow *EventQueueOverflow
	if !errors.As(second.Err(), &overflow) || overflow.Reason != "shared_bytes" || overflow.SharedBytes != 2*n || overflow.IncomingBytes != n {
		t.Fatalf("missing safe shared-pressure details: %v", second.Err())
	}
	if strings.Contains(second.Err().Error(), "must-not-be-logged") {
		t.Fatal("error contains event text")
	}
	if first.Err() != nil {
		t.Fatal("other client failed with saturated newcomer")
	}
	if _, ok := first.NextEvent(); !ok {
		t.Fatal("could not dequeue")
	}
	third := budgetClient(t, budget, 8, 8*n, nil)
	if !third.enqueueEvent([]byte(budgetEvent)) {
		t.Fatal("dequeue did not release capacity")
	}
	first.Close()
	first.Close()
	second.Close()
	third.Close()
	if used, _ := budget.Usage(); used != 0 {
		t.Fatalf("close leaked bytes: %d", used)
	}
}

func TestEventBudgetLocalLimitsReleaseWholeQueueAndClassify(t *testing.T) {
	n := len(budgetEvent)
	for _, tc := range []struct {
		name        string
		count, size int
	}{{"event_count", 1, 4 * n}, {"queue_bytes", 4, n}} {
		t.Run(tc.name, func(t *testing.T) {
			budget := NewEventByteBudget(8 * n)
			notices := []EventQueuePressure{}
			c := budgetClient(t, budget, tc.count, tc.size, func(p EventQueuePressure) { notices = append(notices, p) })
			if !c.enqueueEvent([]byte(budgetEvent)) {
				t.Fatal("first event rejected")
			}
			if c.enqueueEvent([]byte(budgetEvent)) {
				t.Fatal("local overcommit accepted")
			}
			if len(notices) != 1 || notices[0].Reason != tc.name || notices[0].QueuedBytes != n || notices[0].QueuedEvents != 1 || notices[0].MaxEvents != tc.count || notices[0].MaxEventBytes != tc.size {
				t.Fatalf("pressure=%+v", notices)
			}
			if used, _ := budget.Usage(); used != 0 {
				t.Fatalf("overflow leaked %d", used)
			}
			c.Close()
			if c.enqueueEvent([]byte(budgetEvent)) {
				t.Fatal("closed client reacquired budget")
			}
			if used, _ := budget.Usage(); used != 0 {
				t.Fatalf("closed enqueue leaked %d", used)
			}
		})
	}
}

func TestSharedOverflowReleasesOnlyRejectedClientsExistingQueue(t *testing.T) {
	n := len(budgetEvent)
	pool := NewEventByteBudget(3 * n)
	first := budgetClient(t, pool, 8, 8*n, nil)
	second := budgetClient(t, pool, 8, 8*n, nil)
	if !first.enqueueEvent([]byte(budgetEvent)) || !second.enqueueEvent([]byte(budgetEvent)) || !second.enqueueEvent([]byte(budgetEvent)) {
		t.Fatal("initial shared charges failed")
	}
	if second.enqueueEvent([]byte(budgetEvent)) {
		t.Fatal("shared overflow accepted")
	}
	if used, _ := pool.Usage(); used != n {
		t.Fatalf("rejecting client failed to release only its own prefix: %d", used)
	}
	if !first.enqueueEvent([]byte(budgetEvent)) || !first.enqueueEvent([]byte(budgetEvent)) {
		t.Fatal("surviving client could not reuse released bytes")
	}
	first.Close()
	second.Close()
	if used, _ := pool.Usage(); used != 0 {
		t.Fatalf("shared failure cleanup leaked %d", used)
	}
}

func TestEventBudgetZeroCapacityAndUnsharedDefault(t *testing.T) {
	for _, limit := range []int{0, -1} {
		budget := NewEventByteBudget(limit)
		c := budgetClient(t, budget, 2, 1024, nil)
		if c.enqueueEvent([]byte(budgetEvent)) {
			t.Fatal("invalid/zero capacity became unlimited")
		}
	}
	c := budgetClient(t, nil, 2, 1024, nil)
	if !c.enqueueEvent([]byte(budgetEvent)) {
		t.Fatal("ordinary client unexpectedly requires a pool")
	}
	if _, ok := c.NextEvent(); !ok {
		t.Fatal("ordinary dequeue failed")
	}
}

func TestEventBudgetCloseRacesCannotLeakOrGoNegative(t *testing.T) {
	n := len(budgetEvent)
	budget := NewEventByteBudget(128 * n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 64 {
		c := budgetClient(t, budget, 1024, 1024*n, nil)
		if !c.enqueueEvent([]byte(budgetEvent)) {
			t.Fatal("initial charge rejected")
		}
		wg.Add(3)
		go func() {
			defer wg.Done()
			<-start
			for range 100 {
				if !c.enqueueEvent([]byte(budgetEvent)) {
					break
				}
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			for range 100 {
				c.NextEvent()
				used, limit := budget.Usage()
				if used < 0 || used > limit {
					t.Errorf("budget escaped bounds: %d/%d", used, limit)
				}
			}
		}()
		go func() { defer wg.Done(); <-start; c.Close(); c.Close() }()
	}
	close(start)
	wg.Wait()
	if used, _ := budget.Usage(); used != 0 {
		t.Fatalf("concurrent close leaked %d", used)
	}
}

func TestEventBudgetInitializeFailureCallerCleanup(t *testing.T) {
	budget := NewEventByteBudget(1024)
	local, peer := net.Pipe()
	defer peer.Close()
	c := NewClient(local, Options{EventBudget: budget})
	defer c.Close()
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		done <- c.Initialize(ctx)
	}()
	m := readMessage(t, peer)
	if err := WriteFrame(peer, json.RawMessage(budgetEvent), 4096); err != nil {
		t.Fatal(err)
	}
	sendReply(t, peer, stringField(m, "requestId"), "error", nil)
	if err := <-done; !errors.Is(err, ErrRemoteRejected) {
		t.Fatalf("initialization result: %v", err)
	}
	// openFollower's deferred cleanup closes every unsuccessfully initialized
	// Client; an unregistered caller must do the same.
	c.Close()
	if used, _ := budget.Usage(); used != 0 {
		t.Fatalf("initialization failure leaked %d", used)
	}
}
