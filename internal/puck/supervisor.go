package puck

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrUnavailable = errors.New("Puck handler unavailable")
	ErrCircuitOpen = errors.New("Puck circuit open")
)

type Supervisor struct {
	mu        sync.Mutex
	threshold int
	window    time.Duration
	panics    []time.Time
	open      bool
	now       func() time.Time
}

func NewSupervisor(threshold int, window time.Duration) *Supervisor {
	if threshold < 1 {
		threshold = 3
	}
	if window <= 0 {
		window = time.Minute
	}
	return &Supervisor{threshold: threshold, window: window, now: time.Now}
}

func (s *Supervisor) Open() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.open
}

func (s *Supervisor) Call(ctx context.Context, handler func(context.Context) error) error {
	if s.Open() {
		return ErrCircuitOpen
	}
	result := make(chan error, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.recordPanic()
				result <- fmt.Errorf("%w: panic recovered", ErrUnavailable)
			}
		}()
		result <- handler(ctx)
	}()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Supervisor) Go(handler func()) {
	go func() {
		defer func() {
			if recover() != nil {
				s.recordPanic()
			}
		}()
		handler()
	}()
}

func (s *Supervisor) recordPanic() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	cutoff := now.Add(-s.window)
	kept := s.panics[:0]
	for _, at := range s.panics {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	s.panics = append(kept, now)
	if len(s.panics) >= s.threshold {
		s.open = true
	}
}
