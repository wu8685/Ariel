package puck

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestSupervisorRecoversPanicsAndOpensCircuit(t *testing.T) {
	s := NewSupervisor(3, time.Minute)
	for i := 0; i < 3; i++ {
		err := s.Call(context.Background(), func(context.Context) error { panic("fixture") })
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("panic %d: %v", i, err)
		}
	}
	if !s.Open() {
		t.Fatal("circuit did not open")
	}
	called := false
	if err := s.Call(context.Background(), func(context.Context) error { called = true; return nil }); !errors.Is(err, ErrCircuitOpen) || called {
		t.Fatalf("open circuit err=%v called=%v", err, called)
	}
}

func TestSupervisorRestartsPanickedBackgroundLoopWithBackoff(t *testing.T) {
	s := NewSupervisor(10, time.Minute)
	s.restartDelay = time.Millisecond
	s.restartMaxDelay = 4 * time.Millisecond
	var calls atomic.Int32
	done := make(chan struct{})
	s.Go(func() {
		if calls.Add(1) < 3 {
			panic("fixture")
		}
		close(done)
	})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("background handler was not restarted")
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestSupervisorCancelsTimedOutHandlerContextAndRecordsIt(t *testing.T) {
	s := NewSupervisor(3, time.Minute)
	observed := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := s.Call(ctx, func(handlerCtx context.Context) error {
		<-handlerCtx.Done()
		close(observed)
		return handlerCtx.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	select {
	case <-observed:
	case <-time.After(time.Second):
		t.Fatal("handler did not observe cancellation")
	}
	if s.callTimeouts.Load() != 1 {
		t.Fatalf("timeouts = %d", s.callTimeouts.Load())
	}
}

func TestSupervisorBoundsBlockingHandler(t *testing.T) {
	s := NewSupervisor(3, time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := s.Call(ctx, func(context.Context) error { select {} }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocking handler: %v", err)
	}
}
