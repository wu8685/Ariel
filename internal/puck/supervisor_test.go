package puck

import (
	"context"
	"errors"
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

func TestSupervisorBoundsBlockingHandler(t *testing.T) {
	s := NewSupervisor(3, time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := s.Call(ctx, func(context.Context) error { select {} }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocking handler: %v", err)
	}
}
