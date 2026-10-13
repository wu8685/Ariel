package wsheartbeat

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunWithTicksToleratesOneFailureAndResetsAfterSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ticks := make(chan time.Time)
	results := []error{errors.New("first"), nil, errors.New("after reset"), errors.New("second consecutive")}
	var calls atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- runWithTicks(ctx, time.Second, 2, ticks, func(context.Context) error {
			return results[int(calls.Add(1))-1]
		})
	}()

	for i := 0; i < 3; i++ {
		ticks <- time.Now()
		select {
		case err := <-done:
			t.Fatalf("heartbeat exited after call %d: %v", i+1, err)
		default:
		}
	}
	ticks <- time.Now()
	select {
	case err := <-done:
		if err == nil || err.Error() != "second consecutive" {
			t.Fatalf("heartbeat error = %v, want second consecutive", err)
		}
	case <-time.After(time.Second):
		t.Fatal("heartbeat did not exit after two consecutive failures")
	}
}

func TestRunWithTicksStopsCleanlyWhenParentIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time)
	done := make(chan error, 1)
	go func() {
		done <- runWithTicks(ctx, time.Second, 2, ticks, func(context.Context) error {
			t.Fatal("ping called after cancellation")
			return nil
		})
	}()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("canceled heartbeat returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled heartbeat did not exit")
	}
}

func TestProductionDefaults(t *testing.T) {
	if DefaultInterval != 30*time.Second {
		t.Fatalf("default interval = %s, want 30s", DefaultInterval)
	}
	if DefaultTimeout != 15*time.Second {
		t.Fatalf("default timeout = %s, want 15s", DefaultTimeout)
	}
	if DefaultMaxConsecutiveFailures != 2 {
		t.Fatalf("default consecutive failures = %d, want 2", DefaultMaxConsecutiveFailures)
	}
	if DefaultWriteTimeout != 15*time.Second {
		t.Fatalf("default write timeout = %s, want 15s", DefaultWriteTimeout)
	}
}
