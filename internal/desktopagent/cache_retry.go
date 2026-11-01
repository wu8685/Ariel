package desktopagent

import (
	"context"
	"math/rand/v2"
	"time"
)

type cacheRetryTimer interface{ Stop() bool }

// Demand outlives an individual native follower. Its identity fences timers
// from an old transport; cacheSubscription identity fences old native events.
type cacheDemand struct {
	watching    bool
	warmUntil   time.Time
	runtime     string
	attempts    int
	owner       string
	revision    int64
	hasProgress bool
	inFlight    bool
	timer       cacheRetryTimer
	cancel      context.CancelFunc
	generation  uint64
}

func (d *cacheDemand) stop() {
	d.generation++
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
}

func (d *cacheDemand) needed(now time.Time) bool {
	return d.watching || d.runtime == "inProgress" || now.Before(d.warmUntil)
}

func cacheRetryDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	return min(time.Second<<min(attempt, 5), 30*time.Second)
}

func recoverableCacheFailure(code string) bool {
	switch code {
	case "OVERLOADED", "RESYNC_REQUIRED", "TIMEOUT", "DEVICE_OFFLINE":
		return true
	default:
		return false
	}
}

func (a *agent) touchCache(ctx context.Context, id string, watching bool) error {
	if id == "" {
		return a.touchCacheNative(ctx, id, watching)
	}
	a.cacheMu.Lock()
	if a.cacheClosed {
		a.cacheMu.Unlock()
		return context.Canceled
	}
	if a.cacheDemands == nil {
		a.cacheDemands = map[string]*cacheDemand{}
	}
	demand := a.cacheDemands[id]
	if demand == nil && watching {
		demand = &cacheDemand{}
		a.cacheDemands[id] = demand
	}
	if demand != nil {
		demand.watching = watching
		if watching {
			demand.generation++
			demand.warmUntil = time.Time{}
			demand.attempts = 0 // An explicit promotion reopens a tripped circuit.
			if demand.timer != nil {
				demand.timer.Stop()
				demand.timer = nil
			}
		} else {
			demand.warmUntil = time.Now().Add(a.cacheWarmGrace)
		}
		if demand.inFlight {
			// Reuse the in-flight attach. A later unwatch still changes demand.
			a.cacheMu.Unlock()
			return nil
		}
	}
	a.cacheMu.Unlock()
	err := a.touchCacheNative(ctx, id, watching)
	if err != nil && watching {
		a.scheduleCacheRecovery(id, cacheFailureCode(err))
	}
	return err
}

func cacheFailureCode(err error) string {
	if err != nil && err.Error() == "AGENT_CAPACITY" {
		return "AGENT_CAPACITY"
	}
	return requestErrorCode("thread.cache.touch", err)
}

// No healthy-follower polling. This timer is created only after an actual
// failed attach/follower, and shares the existing serialized cache load gate.
func (a *agent) scheduleCacheRecovery(id, code string) {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	d := a.cacheDemands[id]
	if !recoverableCacheFailure(code) {
		if d != nil && a.cacheSubs[id] == nil {
			d.stop()
			delete(a.cacheDemands, id)
		}
		return
	}
	if a.cacheClosed || d == nil || d.inFlight || d.timer != nil || d.attempts >= 3 {
		return
	}
	if !d.needed(time.Now()) {
		delete(a.cacheDemands, id)
		return
	}
	delay := cacheRetryDelay(d.attempts)
	if a.cacheRetryJitter != nil {
		delay = a.cacheRetryJitter(delay)
	} else {
		delay = time.Duration(float64(delay) * (0.8 + rand.Float64()*0.4))
	}
	delay = min(delay, 30*time.Second)
	after := a.cacheAfterFunc
	if after == nil {
		after = func(d time.Duration, fn func()) cacheRetryTimer { return time.AfterFunc(d, fn) }
	}
	d.generation++
	generation := d.generation
	d.timer = after(delay, func() { a.runCacheRetry(id, d, generation) })
}

func (a *agent) runCacheRetry(id string, d *cacheDemand, generation uint64) {
	a.cacheMu.Lock()
	if a.cacheClosed || a.cacheDemands[id] != d || generation != d.generation {
		a.cacheMu.Unlock()
		return
	}
	d.timer = nil
	if !d.needed(time.Now()) {
		delete(a.cacheDemands, id)
		a.cacheMu.Unlock()
		return
	}
	if d.inFlight || d.attempts >= 3 {
		a.cacheMu.Unlock()
		return
	}
	if a.cacheSubs[id] != nil {
		a.cacheMu.Unlock()
		return
	}
	parent := a.cacheContext
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, threadSubscribeLoadTimeout)
	d.cancel = cancel
	d.inFlight = true
	d.attempts++
	attempt := d.attempts
	a.cacheRetryWG.Add(1)
	a.cacheMu.Unlock()
	defer a.cacheRetryWG.Done()
	err := a.touchCacheNative(ctx, id, true)
	cancel()
	code := cacheFailureCode(err)
	logPhase("cache-follower-retry", id, "attempt", attempt, "ok", err == nil, "error", code)
	a.cacheMu.Lock()
	if a.cacheDemands[id] != d || a.cacheClosed {
		a.cacheMu.Unlock()
		return
	}
	d.inFlight = false
	d.cancel = nil
	if state := a.cacheSubs[id]; state != nil && !d.watching {
		state.watching = false
		state.evictableAt = d.warmUntil
	}
	// The follower can fail immediately after activate, before this attempt
	// returns. Its invalidation could not schedule while inFlight was true.
	missing := a.cacheSubs[id] == nil
	a.cacheMu.Unlock()
	if err != nil {
		a.scheduleCacheRecovery(id, code)
	} else if missing {
		a.scheduleCacheRecovery(id, "RESYNC_REQUIRED")
	}
}

func (a *agent) observeCacheProgress(id string, state *cacheSubscription, thread map[string]any) {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	a.observeCacheProgressLocked(id, state, thread)
}

func (a *agent) observeCacheProgressLocked(id string, state *cacheSubscription, thread map[string]any) {
	var owner string
	var revision int64
	known := false
	if state.progress != nil {
		owner, revision, known = state.progress()
	}
	runtime, _ := thread["runtime"].(string)
	if a.cacheSubs[id] != state {
		return
	}
	d := a.cacheDemands[id]
	if d == nil {
		return
	}
	advanced := d.hasProgress && ((known && (owner != d.owner || revision > d.revision)) || (runtime == "idle" && d.runtime == "inProgress"))
	if advanced {
		d.attempts = 0
	}
	if known && (!d.hasProgress || owner != d.owner || revision > d.revision) {
		d.owner, d.revision = owner, revision
	}
	d.hasProgress = true
	d.runtime = runtime
}
