package desktopagent

import (
	"context"
	"errors"
	"time"

	"github.com/wu8685/Ariel/internal/probe"
)

var errDesktopUnavailable = errors.New("Desktop IPC unavailable")
var errAppServerUnavailable = errors.New("Codex App Server unavailable")

func serveWithAppServer(ctx context.Context, done <-chan struct{}, run func(context.Context) error) error {
	runCtx, cancel := context.WithCancel(ctx)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-done:
			cancel()
		case <-runCtx.Done():
		}
	}()
	err := run(runCtx)
	// Inspect the child before canceling runCtx: cancellation itself closes
	// the CommandContext process, and must not mask a Relay failure.
	childExited := false
	select {
	case <-done:
		childExited = true
	default:
	}
	cancel()
	<-stopped
	if childExited {
		return errAppServerUnavailable
	}
	return err
}

func checkDesktop(ctx context.Context, socket string) error {
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	client, err := probe.Connect(checkCtx, socket)
	if err != nil {
		return errDesktopUnavailable
	}
	defer client.Close()
	return nil
}

func monitorDesktop(ctx context.Context, interval time.Duration, check func(context.Context) error) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	failures := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if check(ctx) != nil {
				failures++
				if failures >= 2 {
					return errDesktopUnavailable
				}
			} else {
				failures = 0
			}
		}
	}
}
