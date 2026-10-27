package filecontext

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type slowFileSource struct {
	active atomic.Int32
	peak   atomic.Int32
}

func (s *slowFileSource) Thread(ctx context.Context, _ string) (Thread, error) {
	n := s.active.Add(1)
	for {
		old := s.peak.Load()
		if n <= old || s.peak.CompareAndSwap(old, n) {
			break
		}
	}
	defer s.active.Add(-1)
	<-ctx.Done()
	return Thread{}, ctx.Err()
}
func (s *slowFileSource) Items(context.Context, string, string) ([]Item, error) { return nil, nil }

func TestFileWorkHasTwoSlotsEightWaitersAndCancellation(t *testing.T) {
	source := &slowFileSource{}
	service := NewWithWorkspaceBrowser(source, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	errors := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.WorkspaceRead(ctx, WorkspaceReadRequest{ThreadID: "fixture", Path: "a.md"})
			errors <- err
		}()
	}
	deadline := time.Now().Add(time.Second)
	for len(service.waiting) < 10 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(service.waiting) != 10 {
		t.Fatal("workers did not reach bounded admission")
	}
	if _, err := service.WorkspaceRead(ctx, WorkspaceReadRequest{ThreadID: "fixture", Path: "a.md"}); CodeOf(err) != CodeOverloaded {
		t.Fatalf("unbounded queue: %v", err)
	}
	cancel()
	wg.Wait()
	close(errors)
	for err := range errors {
		if CodeOf(err) != CodeTimeout {
			t.Fatalf("cancellation: %v", err)
		}
	}
	if source.peak.Load() > 2 || len(service.slots) != 0 || len(service.waiting) != 0 {
		t.Fatal("slot leak or excess concurrency")
	}
}
