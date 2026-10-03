package desktopagent

import (
	"context"
	"errors"
	"os/exec"
	"regexp"
	"time"
)

var threadIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type OwnerOps struct {
	Discover      func(context.Context, string) (string, error)
	Open          func(context.Context, string) error
	Verify        func(context.Context, string, string, string) error
	RetryInterval time.Duration
}

func OpenThread(ctx context.Context, threadID string) error {
	if !threadIDPattern.MatchString(threadID) {
		return errors.New("invalid Desktop thread ID")
	}
	return exec.CommandContext(ctx, "/usr/bin/open", "codex://threads/"+threadID).Run()
}

func EnsureOwner(ctx context.Context, threadID, cwd string, ops OwnerOps) (string, error) {
	if !threadIDPattern.MatchString(threadID) || cwd == "" || ops.Discover == nil || ops.Open == nil || ops.Verify == nil {
		return "", errors.New("invalid owner-load input")
	}
	if ops.RetryInterval <= 0 {
		ops.RetryInterval = 250 * time.Millisecond
	}
	owner, err := ops.Discover(ctx, threadID)
	if err == nil && owner != "" {
		if err := ops.Verify(ctx, threadID, owner, cwd); err != nil {
			return "", err
		}
		return owner, nil
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err := ops.Open(ctx, threadID); err != nil {
		return "", err
	}
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(ops.RetryInterval):
		}
		owner, err = ops.Discover(ctx, threadID)
		if err == nil && owner != "" {
			if err := ops.Verify(ctx, threadID, owner, cwd); err != nil {
				return "", err
			}
			return owner, nil
		}
	}
}
