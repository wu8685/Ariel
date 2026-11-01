package desktopagent

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/codex/desktopipc"
)

type manualCacheRetryTimer struct {
	stopped atomic.Bool
	delay   time.Duration
	fn      func()
}

type namedCacheHistory struct{ fakeHistory }

func (namedCacheHistory) Read(_ context.Context, id string) (appserver.Thread, error) {
	return appserver.Thread{ID: id, CWD: "/fixture", Name: "fixture"}, nil
}

type overflowCacheLive struct{ *fakeLive }

func (*overflowCacheLive) Err() error {
	return &desktopipc.EventQueueOverflow{EventQueuePressure: desktopipc.EventQueuePressure{Reason: "queue_bytes"}}
}

func TestCacheRecoveryOneOverflowDoesNotInterruptTwoOtherThreads(t *testing.T) {
	large := &overflowCacheLive{&fakeLive{busy: true, updates: make(chan struct{}, 1), done: make(chan struct{})}}
	smallA := &fakeLive{updates: make(chan struct{}, 1)}
	smallB := &fakeLive{updates: make(chan struct{}, 1)}
	var largeAttaches, smallAttaches atomic.Int32
	a, peer, ctx := newCacheRecoveryAgent(t, func(_ context.Context, id, _ string) (Live, error) {
		switch id {
		case "large":
			if largeAttaches.Add(1) == 1 {
				return large, nil
			}
			return &fakeLive{updates: make(chan struct{}, 1)}, nil
		case "small-a":
			smallAttaches.Add(1)
			return smallA, nil
		default:
			smallAttaches.Add(1)
			return smallB, nil
		}
	})
	a.service.history = namedCacheHistory{}
	clock := installRetryClock(a)
	for _, id := range []string{"large", "small-a", "small-b"} {
		if err := a.touchCache(ctx, id, true); err != nil {
			t.Fatal(err)
		}
		readCacheRecoveryEvent(t, ctx, peer)
	}
	large.Close()
	failed := readCacheRecoveryEvent(t, ctx, peer)
	if failed["reason"] != "OVERLOADED" {
		t.Fatalf("queue capacity cause lost: %v", failed)
	}
	a.cachePublishMu.Lock()
	a.cachePublishMu.Unlock() // Failure callback scheduled its timer.
	for index, small := range []*fakeLive{smallA, smallB} {
		small.mu.Lock()
		small.busy = true
		small.mu.Unlock()
		small.updates <- struct{}{}
		update := readCacheRecoveryEvent(t, ctx, peer)
		if update["event"] != "thread.cache.snapshot" || update["threadId"] != []string{"small-a", "small-b"}[index] || update["seq"] != float64(2) {
			t.Fatalf("unrelated thread stopped updating: %v", update)
		}
	}
	done := make(chan struct{})
	timer := clock.next(t)
	go func() { defer close(done); timer.fn() }()
	recovered := readCacheRecoveryEvent(t, ctx, peer)
	<-done
	if recovered["threadId"] != "large" || largeAttaches.Load() != 2 || smallAttaches.Load() != 2 {
		t.Fatal("recovery reattached unrelated threads")
	}
}

func (timer *manualCacheRetryTimer) Stop() bool { return !timer.stopped.Swap(true) }

type manualCacheRetryClock struct {
	mu     sync.Mutex
	timers []*manualCacheRetryTimer
}

func (clock *manualCacheRetryClock) after(delay time.Duration, fn func()) cacheRetryTimer {
	timer := &manualCacheRetryTimer{delay: delay, fn: fn}
	clock.mu.Lock()
	clock.timers = append(clock.timers, timer)
	clock.mu.Unlock()
	return timer
}
func (clock *manualCacheRetryClock) next(t *testing.T) *manualCacheRetryTimer {
	t.Helper()
	clock.mu.Lock()
	defer clock.mu.Unlock()
	for len(clock.timers) > 0 {
		timer := clock.timers[0]
		clock.timers = clock.timers[1:]
		if !timer.stopped.Load() {
			return timer
		}
	}
	t.Fatal("no scheduled recovery")
	return nil
}
func (clock *manualCacheRetryClock) pending() int {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	count := 0
	for _, timer := range clock.timers {
		if !timer.stopped.Load() {
			count++
		}
	}
	return count
}
func installRetryClock(a *agent) *manualCacheRetryClock {
	clock := &manualCacheRetryClock{}
	a.cacheAfterFunc = clock.after
	a.cacheRetryJitter = func(delay time.Duration) time.Duration { return delay }
	return clock
}

func TestCacheRecoveryBackoffAndPermanentReasons(t *testing.T) {
	if got := cacheFailureCode(context.DeadlineExceeded); got != "TIMEOUT" {
		t.Fatalf("read-only timeout was misclassified as %s", got)
	}
	for index, want := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second} {
		if got := cacheRetryDelay(index); got != want {
			t.Fatalf("attempt=%d delay=%v want=%v", index, got, want)
		}
	}
	for _, code := range []string{"AGENT_CAPACITY", "HISTORY_TOO_LARGE", "NATIVE_STATE_UNCERTAIN", "PROTOCOL_UNSUPPORTED", "NOT_FOUND", "INVALID_ARGUMENT", "CACHE_OVERSIZED"} {
		if recoverableCacheFailure(code) {
			t.Fatalf("permanent failure retries: %s", code)
		}
	}
}

func TestCacheReadTimeoutReplyNeverSuggestsMutationUnknown(t *testing.T) {
	a, peer, ctx := newCacheRecoveryAgent(t, func(context.Context, string, string) (Live, error) { return nil, errors.New("not used") })
	a.reply(ctx, "00000000-0000-4000-8000-000000000068", "thread.cache.touch", nil, &desktopipc.CallError{Cause: context.DeadlineExceeded, Outcome: "unknown"})
	event := readCacheRecoveryEvent(t, ctx, peer)
	if event["outcome"] != "rejected" || event["error"].(map[string]any)["code"] != "TIMEOUT" {
		t.Fatalf("read timeout reported as mutation ambiguity: %v", event)
	}
}

func TestCacheCancelledAttachDoesNotLeaveLoadingPlaceholder(t *testing.T) {
	var cancelLoad context.CancelFunc
	a, _, ctx := newCacheRecoveryAgent(t, func(context.Context, string, string) (Live, error) {
		cancelLoad()
		return &fakeLive{updates: make(chan struct{}, 1)}, nil
	})
	loadCtx, cancel := context.WithCancel(ctx)
	cancelLoad = cancel
	defer cancel()
	if err := a.touchCache(loadCtx, "thread", true); err == nil {
		t.Fatal("cancelled attach was accepted")
	}
	a.cacheMu.Lock()
	state := a.cacheSubs["thread"]
	a.cacheMu.Unlock()
	if state != nil {
		t.Fatal("cancelled attach left a placeholder that blocks every future rewatch")
	}
}

func TestCacheRecoveryTripsAfterThreeFailedAttachesAndExplicitRetryReopens(t *testing.T) {
	first := &fakeLive{busy: true, updates: make(chan struct{}, 1), done: make(chan struct{})}
	var attaches atomic.Int32
	a, peer, ctx := newCacheRecoveryAgent(t, func(context.Context, string, string) (Live, error) {
		count := attaches.Add(1)
		if count == 1 {
			return first, nil
		}
		if count <= 4 {
			return nil, errors.New("RESYNC_REQUIRED")
		}
		return &fakeLive{updates: make(chan struct{}, 1)}, nil
	})
	clock := installRetryClock(a)
	if err := a.touchCache(ctx, "thread", true); err != nil {
		t.Fatal(err)
	}
	readCacheRecoveryEvent(t, ctx, peer)
	first.Close()
	readCacheRecoveryEvent(t, ctx, peer)
	for attempt := range 3 {
		timer := clock.next(t)
		if timer.delay != time.Second<<attempt {
			t.Fatal("retry did not back off")
		}
		done := make(chan struct{})
		go func() { defer close(done); timer.fn() }()
		readCacheRecoveryEvent(t, ctx, peer)
		<-done
	}
	if attaches.Load() != 4 || clock.pending() != 0 {
		t.Fatalf("circuit did not stop: attaches=%d timers=%d", attaches.Load(), clock.pending())
	}
	if err := a.touchCache(ctx, "thread", true); err != nil {
		t.Fatal(err)
	}
	if event := readCacheRecoveryEvent(t, ctx, peer); event["event"] != "thread.cache.snapshot" {
		t.Fatal("explicit retry failed")
	}
	if attaches.Load() != 5 {
		t.Fatal("explicit promotion did not reopen circuit")
	}
}

func TestCacheRecoveryStaleTimerCannotAttachAfterExplicitPromotionOrClose(t *testing.T) {
	for _, closing := range []bool{false, true} {
		first := &fakeLive{updates: make(chan struct{}, 1), done: make(chan struct{})}
		var attaches atomic.Int32
		a, peer, ctx := newCacheRecoveryAgent(t, func(context.Context, string, string) (Live, error) {
			if attaches.Add(1) == 1 {
				return first, nil
			}
			return &fakeLive{updates: make(chan struct{}, 1)}, nil
		})
		clock := installRetryClock(a)
		if err := a.touchCache(ctx, "thread", true); err != nil {
			t.Fatal(err)
		}
		readCacheRecoveryEvent(t, ctx, peer)
		first.Close()
		readCacheRecoveryEvent(t, ctx, peer)
		timer := clock.next(t)
		if closing {
			a.releaseCacheSubscriptions()
		} else {
			if err := a.touchCache(ctx, "thread", true); err != nil {
				t.Fatal(err)
			}
			readCacheRecoveryEvent(t, ctx, peer)
		}
		before := attaches.Load()
		timer.fn() // Simulate a callback already dispatched when Stop won the race.
		if attaches.Load() != before {
			t.Fatal("retired callback reopened a follower")
		}
	}
}

func TestCacheRecoveryNoWatcherAndExpiredWarmGraceDoesNotRetry(t *testing.T) {
	first := &fakeLive{updates: make(chan struct{}, 1), done: make(chan struct{})}
	a, peer, ctx := newCacheRecoveryAgent(t, func(context.Context, string, string) (Live, error) { return first, nil })
	clock := installRetryClock(a)
	if err := a.touchCache(ctx, "thread", true); err != nil {
		t.Fatal(err)
	}
	readCacheRecoveryEvent(t, ctx, peer)
	a.cacheWarmGrace = 0
	if err := a.touchCache(ctx, "thread", false); err != nil {
		t.Fatal(err)
	}
	first.Close()
	readCacheRecoveryEvent(t, ctx, peer)
	if clock.pending() != 0 {
		t.Fatal("unwatched expired idle thread retried")
	}
}

type progressFakeLive struct {
	*fakeLive
	owner    string
	revision atomic.Int64
}

func (live *progressFakeLive) Progress() (string, int64) { return live.owner, live.revision.Load() }

func TestCacheRecoverySameSnapshotDoesNotResetCircuitButNewRevisionDoes(t *testing.T) {
	for _, advance := range []bool{false, true} {
		var lives []*progressFakeLive
		a, peer, ctx := newCacheRecoveryAgent(t, func(context.Context, string, string) (Live, error) {
			live := &progressFakeLive{fakeLive: &fakeLive{busy: true, updates: make(chan struct{}, 1), done: make(chan struct{})}, owner: "owner"}
			live.revision.Store(5)
			lives = append(lives, live)
			return live, nil
		})
		clock := installRetryClock(a)
		if err := a.touchCache(ctx, "thread", true); err != nil {
			t.Fatal(err)
		}
		readCacheRecoveryEvent(t, ctx, peer)
		for attempt := range 3 {
			lives[len(lives)-1].Close()
			readCacheRecoveryEvent(t, ctx, peer)
			timer := clock.next(t)
			if timer.delay != time.Second<<attempt {
				t.Fatalf("unchanged snapshot reset backoff: %v", timer.delay)
			}
			done := make(chan struct{})
			go func() { defer close(done); timer.fn() }()
			readCacheRecoveryEvent(t, ctx, peer)
			<-done
		}
		if advance {
			last := lives[len(lives)-1]
			last.revision.Store(6)
			last.updates <- struct{}{}
			readCacheRecoveryEvent(t, ctx, peer)
		}
		lives[len(lives)-1].Close()
		readCacheRecoveryEvent(t, ctx, peer)
		if advance {
			if timer := clock.next(t); timer.delay != time.Second {
				t.Fatal("real revision progress did not reset retry budget")
			}
		} else if clock.pending() != 0 {
			t.Fatal("three no-progress rebuilds did not trip")
		}
	}
}
