package desktopagent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestMonitorDesktopDisconnectsAfterTwoConsecutiveFailures(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var checks atomic.Int32
	err := monitorDesktop(ctx, time.Millisecond, func(context.Context) error {
		checks.Add(1)
		return errors.New("Desktop unavailable")
	})
	if err == nil || checks.Load() != 2 {
		t.Fatalf("health failure=%v checks=%d", err, checks.Load())
	}
}

func TestMonitorDesktopResetsFailureCountAfterSuccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var checks atomic.Int32
	err := monitorDesktop(ctx, time.Millisecond, func(context.Context) error {
		switch checks.Add(1) {
		case 1, 3, 4:
			return errors.New("transient")
		default:
			return nil
		}
	})
	if err == nil || checks.Load() != 4 {
		t.Fatalf("failure counter not reset: %v checks=%d", err, checks.Load())
	}
}
