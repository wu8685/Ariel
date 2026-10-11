package appserver

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ReadOnlyEndpoint is one disposable App Server child. In addition to reads,
// RestartingRPC admits only durable queue coordination mutations. It never
// starts or steers a turn; Desktop owner IPC remains execution authority.
type ReadOnlyEndpoint interface {
	RPC
	Done() <-chan struct{}
	Close() error
}

type RestartingRPC struct {
	parent                   context.Context
	start                    func(context.Context) (ReadOnlyEndpoint, error)
	threadListAttemptTimeout time.Duration
	mu                       sync.Mutex
	current                  ReadOnlyEndpoint
	generation               uint64
	closed                   bool
	closedCh                 chan struct{}
}

func NewRestartingRPC(parent context.Context, start func(context.Context) (ReadOnlyEndpoint, error)) (*RestartingRPC, error) {
	return NewRestartingRPCWithThreadListTimeout(parent, 0, start)
}

// NewRestartingRPCWithThreadListTimeout bounds each thread/list attempt. A
// timed-out read invalidates an otherwise live child and is replayed once on a
// fresh endpoint. The timeout is intentionally scoped to thread/list: search
// and history reads retain their larger protocol budgets.
func NewRestartingRPCWithThreadListTimeout(parent context.Context, timeout time.Duration, start func(context.Context) (ReadOnlyEndpoint, error)) (*RestartingRPC, error) {
	r := &RestartingRPC{parent: parent, start: start, threadListAttemptTimeout: timeout, closedCh: make(chan struct{})}
	if _, _, err := r.endpoint(); err != nil {
		return nil, err
	}
	go r.watch()
	return r, nil
}

func (r *RestartingRPC) watch() {
	for {
		endpoint, generation, err := r.endpoint()
		if err != nil {
			select {
			case <-r.closedCh:
				return
			case <-r.parent.Done():
				return
			case <-time.After(time.Second):
				continue
			}
		}
		select {
		case <-r.closedCh:
			return
		case <-r.parent.Done():
			return
		case <-endpoint.Done():
			r.invalidate(generation)
		}
	}
}

func (r *RestartingRPC) endpoint() (ReadOnlyEndpoint, uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, 0, ErrClosed
	}
	if r.current != nil {
		select {
		case <-r.current.Done():
			_ = r.current.Close()
			r.current = nil
		default:
		}
	}
	if r.current == nil {
		endpoint, err := r.start(r.parent)
		if err != nil {
			return nil, 0, err
		}
		r.current = endpoint
		r.generation++
	}
	return r.current, r.generation, nil
}

func (r *RestartingRPC) invalidate(generation uint64) {
	r.mu.Lock()
	if r.generation != generation || r.current == nil {
		r.mu.Unlock()
		return
	}
	old := r.current
	r.current = nil
	r.mu.Unlock()
	_ = old.Close()
}

func (r *RestartingRPC) Call(ctx context.Context, method string, params any, result any) error {
	retryable := true
	switch method {
	case "thread/list", "thread/search", "thread/read", "thread/turns/list", "thread/items/list", "thread/queue/list":
	case "thread/queue/add", "thread/queue/update", "thread/queue/delete", "thread/queue/reorder":
		retryable = false
	default:
		return ErrProtocol
	}
	for attempt := 0; attempt < 2; attempt++ {
		endpoint, generation, err := r.endpoint()
		if err != nil {
			return err
		}
		callCtx := ctx
		cancel := func() {}
		if method == "thread/list" && r.threadListAttemptTimeout > 0 {
			callCtx, cancel = context.WithTimeout(ctx, r.threadListAttemptTimeout)
		}
		err = endpoint.Call(callCtx, method, params, result)
		attemptTimedOut := errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil
		cancel()
		if err == nil {
			return nil
		}
		if attemptTimedOut {
			r.invalidate(generation)
			if retryable && attempt == 0 && ctx.Err() == nil {
				continue
			}
			return err
		}
		select {
		case <-endpoint.Done():
			r.invalidate(generation)
			if retryable && attempt == 0 && ctx.Err() == nil {
				continue
			}
			if !retryable && ctx.Err() == nil {
				return ErrOutcomeUnknown
			}
		default:
		}
		return err
	}
	return ErrClosed
}

func (r *RestartingRPC) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	close(r.closedCh)
	current := r.current
	r.current = nil
	r.mu.Unlock()
	if current != nil {
		return current.Close()
	}
	return nil
}
