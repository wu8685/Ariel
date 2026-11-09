package puck

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrUnavailable = errors.New("Puck handler unavailable")
	ErrCircuitOpen = errors.New("Puck circuit open")
)

type Supervisor struct {
	mu              sync.Mutex
	threshold       int
	window          time.Duration
	panics          []time.Time
	open            bool
	now             func() time.Time
	restartDelay    time.Duration
	restartMaxDelay time.Duration
	callTimeouts    atomic.Uint64
}

func NewSupervisor(threshold int, window time.Duration) *Supervisor {
	if threshold < 1 {
		threshold = 3
	}
	if window <= 0 {
		window = time.Minute
	}
	return &Supervisor{threshold: threshold, window: window, now: time.Now, restartDelay: 100 * time.Millisecond, restartMaxDelay: 5 * time.Second}
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
	handlerCtx, cancelHandler := context.WithCancel(ctx)
	defer cancelHandler()
	result := make(chan error, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.recordPanic()
				result <- fmt.Errorf("%w: panic recovered", ErrUnavailable)
			}
		}()
		result <- handler(handlerCtx)
	}()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		cancelHandler()
		count := s.callTimeouts.Add(1)
		log.Printf("puck supervisor call context ended while handler was running: count=%d", count)
		return ctx.Err()
	}
}

func (s *Supervisor) Go(handler func()) {
	go func() {
		delay := s.restartDelay
		for {
			panicked := false
			func() {
				defer func() {
					if recover() != nil {
						panicked = true
						s.recordPanic()
					}
				}()
				handler()
			}()
			if !panicked || s.Open() {
				return
			}
			if delay <= 0 {
				delay = 100 * time.Millisecond
			}
			maxDelay := s.restartMaxDelay
			if maxDelay <= 0 {
				maxDelay = 5 * time.Second
			}
			log.Printf("puck supervisor background panic recovered: restarting_after=%s", delay)
			time.Sleep(delay)
			if delay < maxDelay {
				delay *= 2
				if delay > maxDelay {
					delay = maxDelay
				}
			}
		}
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
