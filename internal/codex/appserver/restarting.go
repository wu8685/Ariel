package appserver

import (
	"context"
	"sync"
	"time"
)

// ReadOnlyEndpoint is one disposable App Server child. No mutation method is
// routed through this wrapper; Desktop owner IPC remains mutation authority.
type ReadOnlyEndpoint interface {
	RPC
	Done() <-chan struct{}
	Close() error
}

type RestartingRPC struct {
	parent     context.Context
	start      func(context.Context) (ReadOnlyEndpoint, error)
	mu         sync.Mutex
	current    ReadOnlyEndpoint
	generation uint64
	closed     bool
	closedCh   chan struct{}
}

func NewRestartingRPC(parent context.Context, start func(context.Context) (ReadOnlyEndpoint, error)) (*RestartingRPC, error) {
	r := &RestartingRPC{parent: parent, start: start, closedCh: make(chan struct{})}
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
	switch method {
	case "thread/list", "thread/search", "thread/read", "thread/turns/list", "thread/items/list":
	default:
		return ErrProtocol
	}
	for attempt := 0; attempt < 2; attempt++ {
		endpoint, generation, err := r.endpoint()
		if err != nil {
			return err
		}
		err = endpoint.Call(ctx, method, params, result)
		if err == nil {
			return nil
		}
		select {
		case <-endpoint.Done():
			r.invalidate(generation)
			if attempt == 0 && ctx.Err() == nil {
				continue
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
