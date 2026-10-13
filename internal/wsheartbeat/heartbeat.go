package wsheartbeat

import (
	"context"
	"errors"
	"time"
)

const (
	DefaultInterval               = 30 * time.Second
	DefaultTimeout                = 15 * time.Second
	DefaultMaxConsecutiveFailures = 2
	DefaultWriteTimeout           = 15 * time.Second
)

type PingFunc func(context.Context) error

func Run(ctx context.Context, interval, timeout time.Duration, maxConsecutiveFailures int, ping PingFunc) error {
	if interval <= 0 {
		interval = DefaultInterval
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if maxConsecutiveFailures <= 0 {
		maxConsecutiveFailures = DefaultMaxConsecutiveFailures
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	return runWithTicks(ctx, timeout, maxConsecutiveFailures, ticker.C, ping)
}

func runWithTicks(ctx context.Context, timeout time.Duration, maxConsecutiveFailures int, ticks <-chan time.Time, ping PingFunc) error {
	consecutiveFailures := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticks:
			pingCtx, cancel := context.WithTimeout(ctx, timeout)
			err := ping(pingCtx)
			cancel()
			if err == nil {
				consecutiveFailures = 0
				continue
			}
			if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil
			}
			consecutiveFailures++
			if consecutiveFailures >= maxConsecutiveFailures {
				return err
			}
		}
	}
}
