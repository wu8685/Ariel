package appserver

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeReadOnlyEndpoint struct {
	mu    sync.Mutex
	done  chan struct{}
	fail  bool
	calls int
}

func (f *fakeReadOnlyEndpoint) Call(_ context.Context, _ string, _ any, result any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.fail {
		select {
		case <-f.done:
		default:
			close(f.done)
		}
		return ErrProtocol
	}
	if result != nil {
		result.(*struct{ OK bool }).OK = true
	}
	return nil
}
func (f *fakeReadOnlyEndpoint) Done() <-chan struct{} { return f.done }
func (f *fakeReadOnlyEndpoint) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	select {
	case <-f.done:
	default:
		close(f.done)
	}
	return nil
}

func TestRestartingRPCRecoversReadOnlyCallWithoutDroppingCaller(t *testing.T) {
	first := &fakeReadOnlyEndpoint{done: make(chan struct{}), fail: true}
	second := &fakeReadOnlyEndpoint{done: make(chan struct{})}
	var started atomic.Int32
	rpc, err := NewRestartingRPC(context.Background(), func(context.Context) (ReadOnlyEndpoint, error) {
		if started.Add(1) == 1 {
			return first, nil
		}
		return second, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer rpc.Close()
	var result struct{ OK bool }
	if err := rpc.Call(context.Background(), "thread/list", nil, &result); err != nil || !result.OK || started.Load() != 2 || first.calls != 1 || second.calls != 1 {
		t.Fatalf("read-only process was not restarted: result=%v err=%v starts=%d calls=%d/%d", result.OK, err, started.Load(), first.calls, second.calls)
	}
	if err := rpc.Call(context.Background(), "turn/start", nil, nil); !errors.Is(err, ErrProtocol) || second.calls != 1 {
		t.Fatalf("turn execution routed through coordination process: %v", err)
	}
}

func TestRestartingRPCNeverRetriesQueueMutationWithUnknownOutcome(t *testing.T) {
	first := &fakeReadOnlyEndpoint{done: make(chan struct{}), fail: true}
	second := &fakeReadOnlyEndpoint{done: make(chan struct{})}
	var started atomic.Int32
	rpc, err := NewRestartingRPC(context.Background(), func(context.Context) (ReadOnlyEndpoint, error) {
		if started.Add(1) == 1 {
			return first, nil
		}
		return second, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer rpc.Close()
	if err := rpc.Call(context.Background(), "thread/queue/add", map[string]any{}, nil); !errors.Is(err, ErrOutcomeUnknown) {
		t.Fatalf("queue mutation outcome: %v", err)
	}
	if first.calls != 1 || second.calls != 0 {
		t.Fatalf("queue mutation was replayed: %d/%d", first.calls, second.calls)
	}
	if err := rpc.Call(context.Background(), "thread/queue/start", nil, nil); !errors.Is(err, ErrProtocol) {
		t.Fatalf("queue start must remain owner-only: %v", err)
	}
}

func TestRestartingRPCAllowsNativeSearchAsReadOnly(t *testing.T) {
	endpoint := &fakeReadOnlyEndpoint{done: make(chan struct{})}
	rpc, err := NewRestartingRPC(context.Background(), func(context.Context) (ReadOnlyEndpoint, error) { return endpoint, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer rpc.Close()
	var result struct{ OK bool }
	if err := rpc.Call(context.Background(), "thread/search", map[string]any{"searchTerm": "fixture"}, &result); err != nil || !result.OK || endpoint.calls != 1 {
		t.Fatalf("native read-only search was blocked: result=%v err=%v calls=%d", result.OK, err, endpoint.calls)
	}
}

func TestRestartingRPCReplacesDeadChildEvenBeforeNextRequest(t *testing.T) {
	first := &fakeReadOnlyEndpoint{done: make(chan struct{})}
	second := &fakeReadOnlyEndpoint{done: make(chan struct{})}
	restarted := make(chan struct{})
	var started atomic.Int32
	rpc, err := NewRestartingRPC(context.Background(), func(context.Context) (ReadOnlyEndpoint, error) {
		if started.Add(1) == 1 {
			return first, nil
		}
		close(restarted)
		return second, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer rpc.Close()
	first.Close()
	select {
	case <-restarted:
	case <-time.After(time.Second):
		t.Fatal("dead App Server was not restarted while Relay stayed online")
	}
}
