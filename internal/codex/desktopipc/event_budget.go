package desktopipc

import (
	"fmt"
	"sync"
)

// EventByteBudget charges only bytes retained in queues, not a reservation per
// follower or a process RSS limit. It is opt-in and never preallocates payloads.
type EventByteBudget struct {
	mu          sync.Mutex
	used, limit int
}

func NewEventByteBudget(limit int) *EventByteBudget {
	return &EventByteBudget{limit: max(0, limit)}
}

func (b *EventByteBudget) Usage() (used, limit int) {
	if b == nil {
		return 0, 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used, b.limit
}

func (b *EventByteBudget) tryAcquire(size int) (used, limit int, ok bool) {
	if b == nil {
		return 0, 0, true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if size < 0 || size > b.limit-b.used {
		return b.used, b.limit, false
	}
	b.used += size
	return b.used, b.limit, true
}

func (b *EventByteBudget) release(size int) {
	if b == nil || size == 0 {
		return
	}
	b.mu.Lock()
	b.used -= size
	b.mu.Unlock()
}

// EventQueuePressure deliberately contains no content, owner, thread, socket or
// remote error text. Callbacks run once after invalidation, outside Client.mu.
type EventQueuePressure struct {
	Reason                                   string
	QueuedEvents, QueuedBytes, IncomingBytes int
	MaxEvents, MaxEventBytes                 int
	SharedBytes, MaxSharedBytes              int
}

type EventQueueOverflow struct{ EventQueuePressure }

func (e *EventQueueOverflow) Error() string {
	return fmt.Sprintf("IPC event queue exceeded bounded capacity (%s); resync required", e.Reason)
}
func (e *EventQueueOverflow) Unwrap() error { return ErrOverloaded }

// enqueueEvent never waits for the consumer: the single IPC reader must remain
// able to deliver RPC receipts. Overflow invalidates, it never skips a patch.
func (c *Client) enqueueEvent(body []byte) bool {
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return false
	}
	p := EventQueuePressure{QueuedEvents: len(c.events), QueuedBytes: c.eventBytes, IncomingBytes: len(body), MaxEvents: c.opts.MaxEvents, MaxEventBytes: c.opts.MaxEventBytes}
	switch {
	case len(c.events) >= c.opts.MaxEvents:
		p.Reason = "event_count"
	case len(body) > c.opts.MaxEventBytes-c.eventBytes:
		p.Reason = "queue_bytes"
	default:
		var ok bool
		p.SharedBytes, p.MaxSharedBytes, ok = c.opts.EventBudget.tryAcquire(len(body))
		if !ok {
			p.Reason = "shared_bytes"
		}
	}
	if p.Reason != "" {
		if p.Reason != "shared_bytes" {
			p.SharedBytes, p.MaxSharedBytes = c.opts.EventBudget.Usage()
		}
		c.failLocked(&EventQueueOverflow{EventQueuePressure: p})
		c.mu.Unlock()
		if c.opts.OnEventOverload != nil {
			c.opts.OnEventOverload(p)
		}
		return false
	}
	c.events = append(c.events, body)
	c.eventBytes += len(body)
	select {
	case c.eventsReady <- struct{}{}:
	default:
	}
	c.mu.Unlock()
	return true
}
